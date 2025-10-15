package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	// Used for checking if Redis client is available
	"github.com/user00265/dxclustergoapi/internal/cluster"
	"github.com/user00265/dxclustergoapi/internal/config"
	"github.com/user00265/dxclustergoapi/internal/db"
	"github.com/user00265/dxclustergoapi/internal/dxcc"
	"github.com/user00265/dxclustergoapi/internal/logging"
	"github.com/user00265/dxclustergoapi/internal/lotw"
	"github.com/user00265/dxclustergoapi/internal/pota"
	"github.com/user00265/dxclustergoapi/internal/redisclient"
	"github.com/user00265/dxclustergoapi/internal/spot"
	"github.com/user00265/dxclustergoapi/version" // For User-Agent string
)

// Global spot cache (in-memory, protected by mutex)
// If Redis is enabled, this acts as the "source of truth" after aggregation,
// and API queries will still filter this memory cache.
// For persistence + direct Redis query, we'd need a different strategy.
// For now, mirroring Node.js: in-memory is the primary API source,
// Redis for POTA dedupe.
type spotCache struct {
	sync.RWMutex
	spots   []spot.Spot
	maxSize int
}

func newSpotCache(maxSize int) *spotCache {
	return &spotCache{
		spots:   make([]spot.Spot, 0, maxSize),
		maxSize: maxSize,
	}
}

func (c *spotCache) AddSpot(newSpot spot.Spot) {
	c.Lock()
	defer c.Unlock()

	c.spots = append(c.spots, newSpot)

	// Remove oldest spots if cache exceeds max size
	if len(c.spots) > c.maxSize {
		c.spots = c.spots[len(c.spots)-c.maxSize:]
	}
	// Note: The original Node.js also had a `reduce_spots` function for deduplication.
	// We might need to port that logic here if simple size-based trimming isn't enough.
}

func (c *spotCache) GetAllSpots() []spot.Spot {
	c.RLock()
	defer c.RUnlock()
	// Return a copy to prevent external modification
	return append([]spot.Spot{}, c.spots...)
}

func main() {
	// Call RunApplication with background context and os.Args (excluding program name)
	status := RunApplication(context.Background(), os.Args[1:])
	if status != 0 {
		os.Exit(status)
	}
}

// RunApplication runs the application logic. It returns an exit code integer.
// Tests call this function directly to start the app in-process.
func RunApplication(ctx context.Context, args []string) int {
	// Apply LOG_LEVEL early so the very first log lines respect runtime verbosity.
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		lvl := strings.ToLower(strings.TrimSpace(v))
		switch lvl {
		case "crit", "critical", "c", "0":
			logging.SetLevel(logging.LevelCrit)
		case "error", "err", "e", "1":
			logging.SetLevel(logging.LevelError)
		case "warn", "warning", "w", "2":
			logging.SetLevel(logging.LevelWarn)
		case "notice", "n", "3":
			logging.SetLevel(logging.LevelNotice)
		case "info", "i", "4":
			logging.SetLevel(logging.LevelInfo)
		case "debug", "d", "5":
			logging.SetLevel(logging.LevelDebug)
		default:
			// Unknown value; emit a warning via leveled logger and keep default
			logging.Warn("Unrecognized LOG_LEVEL=%q; valid: crit,error,warn,notice,info,debug or 0-5. Using default (NOTICE).", v)
		}
	}

	logging.Info("Starting %s v%s (+%s)", version.ProjectName, version.ProjectVersion, version.ProjectGitHubURL)
	logging.Info("User-Agent: %s", version.UserAgent)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Post-cleanup diagnostic: this deferred function is declared early so it
	// executes after all other defers in this function. It prints the final
	// goroutine count and a full stack dump once all deferred Close/Stop
	// functions have run. Keep the sleep short to allow any last-minute
	// goroutines to unwind.
	defer func() {
		// If this RunApplication invocation was a quick healthcheck, skip the
		// heavy post-cleanup dump — tests call RunApplication("healthcheck")
		// earlier and we don't want that to produce the full goroutine dump.
		if len(args) > 0 && strings.ToLower(args[0]) == "healthcheck" {
			return
		}
		// Only emit the full post-cleanup dump when explicitly requested by
		// setting DX_API_DUMP_POST_CLEANUP=1 (or true). This keeps CI and
		// normal developer runs quiet while preserving the debug option.
		if v := os.Getenv("DX_API_DUMP_POST_CLEANUP"); !(v == "1" || strings.ToLower(v) == "true") {
			logging.Debug("Debug (post-cleanup): goroutines final: %d (set DX_API_DUMP_POST_CLEANUP=1 to see full stacks)", runtime.NumGoroutine())
			return
		}
		// Give deferred cleanup a small moment to finish releasing resources.
		time.Sleep(150 * time.Millisecond)
		logging.Debug("Debug (post-cleanup): goroutines final: %d", runtime.NumGoroutine())
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		log.Printf("=== goroutine stack dump (post-cleanup len=%d) ===\n%s\n=== end goroutine stack dump ===", n, string(buf[:n]))
	}()

	// --- 1. Load Configuration ---
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Printf("FATAL: Failed to load configuration: %v", err)
		return 1
	}

	// LOG_LEVEL was applied earlier so subsequent logs respect the selected level.
	logging.Info("Configuration loaded. WebPort: %d, MaxCache: %d, DataDir: %s", cfg.WebPort, cfg.MaxCache, cfg.DataDir)

	// Debug configuration details
	logging.Debug("CLUSTERS JSON: '%s'", cfg.RawClustersJSON)
	logging.Debug("Global Callsign: '%s'", cfg.Callsign)

	if len(cfg.Clusters) > 0 {
		logging.Info("DX Clusters configured: %d", len(cfg.Clusters))
		for i, cluster := range cfg.Clusters {
			sotaFlag := "no"
			if cluster.SOTA {
				sotaFlag = "yes"
			}
			logging.Info("  Cluster %d: %s:%s callsign=%s sota=%s", i+1, cluster.Host, cluster.Port, cluster.Callsign, sotaFlag)
		}
	} else {
		logging.Warn("No DX Clusters configured.")
	}
	if cfg.EnablePOTA {
		logging.Info("POTA integration enabled. Polling interval: %s", cfg.POTAPollInterval)
	} else {
		logging.Info("POTA integration disabled.")
	}
	if cfg.Redis.Enabled {
		logging.Info("Redis cache enabled. Host: %s:%s, DB: %d, TLS: %t", cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.DB, cfg.Redis.UseTLS)
	} else {
		logging.Info("Redis cache disabled (using in-memory).")
	}

	// If caller requested a lightweight healthcheck subcommand, perform a quick
	// readiness check here and return without starting long-lived services
	// (HTTP server, cluster connections, pollers). Tests call RunApplication
	// with the "healthcheck" argument and expect a quick, stdout-visible
	// confirmation string and an exit code of 0.
	if len(args) > 0 && strings.ToLower(args[0]) == "healthcheck" {
		// Minimal validation already done: config loaded successfully. Print
		// to stdout (tests capture stdout) and exit with success.
		fmt.Println("Health check successful")
		return 0
	}

	// --- 2. Initialize Shared Redis Client (if enabled) ---
	var rdb *redisclient.Client
	if cfg.Redis.Enabled {
		rdb, err = redisclient.NewClient(ctx, cfg.Redis)
		if err != nil {
			log.Fatalf("FATAL: Failed to initialize Redis client: %v\n", err)
		}
		defer rdb.Close()
		logging.Info("Redis client initialized and connected.")
	}

	// --- 3. Initialize LoTW Client ---
	lotwDBClient, err := db.NewSQLiteClient(cfg.DataDir, lotw.DBFileName)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize LoTW DB client: %v\n", err)
	}
	defer lotwDBClient.Close()

	lotwClient, err := lotw.NewClient(ctx, *cfg, lotwDBClient)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize LoTW client: %v\n", err)
	}

	// Perform initial synchronous LoTW data check and download BEFORE starting spot sources
	logging.Info("Checking LoTW data status...")
	lastLoTWUpdate, err := lotwClient.GetLastDownloadTime(ctx)
	if err != nil {
		logging.Warn("Failed to check LoTW last update time: %v", err)
	}
	needsLoTWUpdate := lastLoTWUpdate.IsZero() || time.Since(lastLoTWUpdate) >= cfg.LoTWUpdateInterval
	if needsLoTWUpdate {
		logging.Info("LoTW data needs update, downloading now (this may take a moment)...")
		if err := lotwClient.FetchAndStoreUsers(ctx); err != nil {
			logging.Error("Initial LoTW data fetch failed: %v", err)
			log.Fatalf("FATAL: Cannot start without LoTW data\n")
		}
		logging.Info("LoTW data downloaded and loaded successfully.")
	} else {
		logging.Info("LoTW data is up to date (last updated: %s)", lastLoTWUpdate.Format(time.RFC3339))
	}

	// Now start background periodic updater
	lotwClient.StartUpdater(ctx)
	logging.Info("LoTW client ready. Background updater started for periodic checks.")
	defer func() {
		logging.Info("Deferred: stopping LoTW updater...")
		lotwClient.StopUpdater()
		logging.Info("Deferred: LoTW updater stopped.")
	}() // --- 4. Initialize DXCC Client ---
	dxccDBClient, err := db.NewSQLiteClient(cfg.DataDir, dxcc.DBFileName)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize DXCC DB client: %v\n", err)
	}
	defer dxccDBClient.Close()

	dxccClient, err := dxcc.NewClient(ctx, *cfg, dxccDBClient)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize DXCC client: %v\n", err)
	}

	// Perform initial synchronous DXCC data check and download BEFORE starting spot sources
	logging.Info("Checking DXCC data status...")
	lastDXCCUpdate, err := dxccClient.GetLastDownloadTime(ctx)
	if err != nil {
		logging.Warn("Failed to check DXCC last update time: %v", err)
	}
	needsDXCCUpdate := lastDXCCUpdate.IsZero() || time.Since(lastDXCCUpdate) >= cfg.DXCCUpdateInterval
	if needsDXCCUpdate {
		logging.Info("DXCC data needs update, downloading now (this may take a moment)...")
		dxccClient.FetchAndStoreData(ctx)
		logging.Info("DXCC data downloaded and loaded successfully.")
	} else {
		logging.Info("DXCC data is up to date (last updated: %s)", lastDXCCUpdate.Format(time.RFC3339))
		// Load in-memory maps from database NOW to ensure they're ready before spots arrive
		if err := dxccClient.LoadMapsFromDB(ctx); err != nil {
			log.Fatalf("FATAL: Failed to load DXCC maps from database: %v\n", err)
		}
		// Verify maps are actually populated (database might be empty despite recent update timestamp)
		if len(dxccClient.PrefixesMap) == 0 {
			logging.Warn("DXCC database is empty despite recent update timestamp. Forcing download now...")
			dxccClient.FetchAndStoreData(ctx)
			logging.Info("DXCC data downloaded and loaded successfully after empty database detection.")
		}
	}

	// Now start background periodic updater
	dxccClient.StartUpdater(ctx)
	logging.Info("DXCC client ready. Background updater started for periodic checks.")
	defer func() {
		logging.Info("Deferred: closing DXCC client (stopping updater)...")
		dxccClient.Close()
		logging.Info("Deferred: DXCC client closed.")
	}()

	// ═══════════════════════════════════════════════════════════════════════════
	// CRITICAL BARRIER: DXCC and LoTW data are now FULLY LOADED and ready.
	// ═══════════════════════════════════════════════════════════════════════════
	// Spots can now be enriched immediately upon arrival from any source.
	logging.Info("DXCC and LoTW data loaded. Ready to initialize spot sources and HTTP API.")

	// --- 6. Initialize POTA Client (if enabled) ---
	var potaClient *pota.Client
	if cfg.EnablePOTA {
		potaClient, err = pota.NewClient(ctx, *cfg, rdb)
		if err != nil {
			log.Fatalf("FATAL: Failed to initialize POTA client: %v\n", err)
		}
		// Do not start polling yet. We'll start POTA polling after the
		// aggregator goroutine is running to ensure emitted POTA spots are
		// visible in the central cache and not immediately evicted by a
		// burst of cluster spots during startup.
		logging.Info("POTA client initialized (polling deferred until aggregator is running).")
		defer func() {
			logging.Info("Deferred: stopping POTA polling...")
			potaClient.StopPolling()
			logging.Info("Deferred: POTA polling stopped.")
		}()
	}

	//  --- 6. Initialize DX Cluster Clients (but don't connect yet) ---
	dxClusterClients := make([]*cluster.Client, 0, len(cfg.Clusters))
	dxClusterHosts := make([]string, 0, len(cfg.Clusters))
	for _, cc := range cfg.Clusters {
		client, err := cluster.NewClient(cc)
		if err != nil {
			logging.Warn("Failed to create DX Cluster client for %s:%s: %v. Skipping this cluster.", cc.Host, cc.Port, err)
			continue
		}
		dxClusterClients = append(dxClusterClients, client)
		dxClusterHosts = append(dxClusterHosts, cc.Host)
		// DON'T call Connect() yet - we'll do that after HTTP API is ready
		buf := cap(client.SpotChan)
		logging.Info("DX Cluster client for %s:%s created. channel_buffer=%d", cc.Host, cc.Port, buf)
	}
	if len(dxClusterClients) == 0 && !cfg.EnablePOTA {
		logging.Error("No active DX Cluster connections and POTA is disabled. Exiting as there are no spot sources.")
		return 2
	}

	// --- 7. Create Spot Cache and Set up HTTP API BEFORE connecting spot sources ---
	centralSpotCache := newSpotCache(cfg.MaxCache)

	// Create Gin router with our custom logging (not gin.Default())
	gin.SetMode(gin.ReleaseMode) // Suppress gin's startup messages
	router := gin.New()
	router.Use(logging.GinRecovery()) // Our recovery middleware
	router.Use(logging.GinLogger())   // Our logging middleware
	router.SetTrustedProxies(nil)     // To prevent "x-forwarded-for" issues if not behind a proxy

	// Middleware to handle BaseURL prefix if configured
	if cfg.BaseURL != "/" && cfg.BaseURL != "" {
		router.Group(cfg.BaseURL).Use(func(c *gin.Context) {
			// Trim base URL from request path for internal routing
			c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, cfg.BaseURL)
			c.Next()
		}).GET("/healthz", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})
		apiGroup := router.Group(cfg.BaseURL)
		setupAPIRoutes(apiGroup, centralSpotCache, dxccClient, lotwClient)
	} else {
		router.GET("/healthz", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})
		setupAPIRoutes(router.Group("/"), centralSpotCache, dxccClient, lotwClient)
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.WebPort),
		Handler: router,
	}

	// Start HTTP server in goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("FATAL: HTTP server failed: %v\n", err)
		}
	}()
	logging.Info("HTTP API listening on :%d (BaseURL: %s)", cfg.WebPort, cfg.BaseURL)

	// ═══════════════════════════════════════════════════════════════════════════
	// NOW connect DX clusters and start POTA polling - everything is ready!
	// ═══════════════════════════════════════════════════════════════════════════

	for i, client := range dxClusterClients {
		client.Connect(ctx) // Start connection and reconnection loop
		logging.Info("DX Cluster client %s:%s connecting...", dxClusterHosts[i], cfg.Clusters[i].Port)
	}

	// --- 8. Spot Aggregation & Enrichment ---
	spotChannels := make([]<-chan spot.Spot, 0) // Collect all spot output channels

	// Add DX Cluster spot channels (convert cluster.Spot -> unified spot.Spot)
	for i, client := range dxClusterClients {
		clusterHost := dxClusterHosts[i]
		// Buffer the forwarder channel so brief startup ordering doesn't drop
		// spots that are emitted before the merge goroutines are scheduled.
		ch := make(chan spot.Spot, 8)
		// Forwarder goroutine: convert cluster.Spot to spot.Spot and send on ch
		go func(c *cluster.Client, out chan<- spot.Spot, clusterHost string) {
			defer close(out)
			forwardedCount := 0
			for {
				select {
				case <-ctx.Done():
					logging.Info("Cluster forwarder [%s] shutting down. Forwarded %d spots.", clusterHost, forwardedCount)
					return
				case s, ok := <-c.SpotChan:
					if !ok {
						logging.Info("Cluster forwarder [%s] spot channel closed. Forwarded %d spots.", clusterHost, forwardedCount)
						return
					}
					forwardedCount++
					logging.Info("CLUSTER [%s] RAW SPOT #%d: %s -> %s @ %.3f kHz - %s", clusterHost, forwardedCount, s.Spotter, s.Spotted, s.Frequency, s.Message)

					// Send into the buffered forwarder channel. If the application is
					// shutting down, exit promptly via ctx.Done.
					select {
					case out <- spot.Spot{
						Spotter:   s.Spotter,
						Spotted:   s.Spotted,
						Frequency: s.Frequency,
						Message:   s.Message,
						When:      s.When,
						Source:    s.Source,
					}:
						logging.Info("CLUSTER [%s] FORWARDED SPOT #%d to aggregator", clusterHost, forwardedCount)
					case <-ctx.Done():
						return
					}
				}
			}
		}(client, ch, clusterHost)

		spotChannels = append(spotChannels, ch)

		// Also listen to errors and messages from DX Clusters
		go func(c *cluster.Client) {
			for {
				select {
				case err := <-c.ErrorChan:
					if err != nil {
						logging.Error("from DX Cluster: %v", err)
					}
				case msg := <-c.MessageChan:
					_ = msg // Ignore generic messages for now
				case <-ctx.Done():
					return
				}
			}
		}(client)
	}

	// Add POTA spot channel (if enabled) — adapt pota.Spot to unified spot.Spot
	if potaClient != nil {
		// Buffer the POTA forwarder channel so initial immediate polls can
		// emit spots before downstream consumers are wired up.
		ch := make(chan spot.Spot, 8)
		go func(in <-chan pota.Spot, out chan<- spot.Spot) {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case s, ok := <-in:
					if !ok {
						return
					}
					sp := func() spot.Spot {
						sp := spot.Spot{
							Spotter:   s.Spotter,
							Spotted:   s.Spotted,
							Frequency: s.Frequency,
							Message:   s.Message,
							When:      s.When,
							Source:    s.Source,
						}
						sp.AdditionalData.PotaRef = s.AdditionalData.PotaRef
						sp.AdditionalData.PotaMode = s.AdditionalData.PotaMode
						return sp
					}()
					select {
					case out <- sp:
					case <-ctx.Done():
						return
					}
				}
			}
		}(potaClient.SpotChan, ch)
		spotChannels = append(spotChannels, ch)
	}

	// Goroutine to aggregate, enrich, and cache spots
	go func() {
		logging.Info("Spot aggregation goroutine starting.")
		merged := mergeSpotChannels(spotChannels...)
		// Verbose tracing for spot pipeline; enabled by setting DX_API_VERBOSE_SPOT_PIPELINE=1
		verbose := os.Getenv("DX_API_VERBOSE_SPOT_PIPELINE") == "1" || strings.ToLower(os.Getenv("DX_API_VERBOSE_SPOT_PIPELINE")) == "true"
		spotCount := 0

		// Duplicate detection: track spotter+spotted+band with timestamp
		// (same station can be on multiple bands simultaneously)
		type spotKey struct {
			spotter string
			spotted string
			band    string
		}
		recentSpots := make(map[spotKey]time.Time)
		const dedupeWindow = 30 * time.Second

		for {
			select {
			case <-ctx.Done():
				logging.Info("Spot aggregation goroutine shutting down due to context. Total spots processed: %d", spotCount)
				return
			case receivedSpot, ok := <-merged:
				if !ok {
					logging.Info("Spot aggregation: merged channel closed, exiting goroutine. Total spots processed: %d", spotCount)
					return
				}

				// Validate required fields - reject spots with missing callsigns
				if strings.TrimSpace(receivedSpot.Spotter) == "" {
					logging.Warn("SPOT REJECTED: missing spotter callsign. spotted=%q freq=%.3f source=%s", receivedSpot.Spotted, receivedSpot.Frequency, receivedSpot.Source)
					continue
				}
				if strings.TrimSpace(receivedSpot.Spotted) == "" {
					logging.Warn("SPOT REJECTED: missing spotted callsign. spotter=%q freq=%.3f source=%s", receivedSpot.Spotter, receivedSpot.Frequency, receivedSpot.Source)
					continue
				}

				// Determine band from frequency for duplicate detection
				// (same station can be on multiple bands simultaneously)
				band := spot.BandFromName(receivedSpot.Frequency)

				// Check for duplicate spot (same spotter+spotted+band within 30 seconds)
				key := spotKey{
					spotter: receivedSpot.Spotter,
					spotted: receivedSpot.Spotted,
					band:    band,
				}
				now := time.Now()

				// Clean up old entries from the map (older than 30 seconds)
				for k, t := range recentSpots {
					if now.Sub(t) > dedupeWindow {
						delete(recentSpots, k)
					}
				}

				// Check if this is a duplicate
				if lastSeen, exists := recentSpots[key]; exists {
					if now.Sub(lastSeen) < dedupeWindow {
						logging.Debug("DUPLICATE SPOT FILTERED: %s -> %s @ %.3f kHz (%s) [%s] (seen %.1f seconds ago)",
							receivedSpot.Spotter, receivedSpot.Spotted, receivedSpot.Frequency, band, receivedSpot.Source, now.Sub(lastSeen).Seconds())
						continue
					}
				}

				// Record this spot
				recentSpots[key] = now

				spotCount++
				logging.Info("SPOT #%d RECEIVED: %s -> %s @ %.3f kHz [%s] - %s", spotCount, receivedSpot.Spotter, receivedSpot.Spotted, receivedSpot.Frequency, receivedSpot.Source, receivedSpot.Message)
				if verbose {
					logging.Debug("Aggregator received spot: source=%s spotter=%s spotted=%s freq=%.3f msg=%q when=%s", receivedSpot.Source, receivedSpot.Spotter, receivedSpot.Spotted, receivedSpot.Frequency, receivedSpot.Message, receivedSpot.When.Format(time.RFC3339))
				}
				enrichedSpot, err := enrichSpot(ctx, receivedSpot, dxccClient, lotwClient)
				if err != nil {
					logging.Error("Failed to enrich spot from %s (%s->%s @ %.3f kHz): %v", receivedSpot.Source, receivedSpot.Spotter, receivedSpot.Spotted, receivedSpot.Frequency, err)
					// Still add the non-enriched spot if enrichment fails, or discard?
					// For now, add the partially enriched one.
					centralSpotCache.AddSpot(receivedSpot)
					logging.Info("SPOT #%d CACHED (non-enriched): %s -> %s @ %.3f kHz [%s]", spotCount, receivedSpot.Spotter, receivedSpot.Spotted, receivedSpot.Frequency, receivedSpot.Source)
					if verbose {
						logging.Debug("Aggregator added non-enriched spot from %s (spotter=%s)", receivedSpot.Source, receivedSpot.Spotter)
					}
					continue
				}
				centralSpotCache.AddSpot(enrichedSpot)
				logging.Info("SPOT #%d CACHED (enriched): %s -> %s @ %.3fMHz (%s) [%s]", spotCount, enrichedSpot.Spotter, enrichedSpot.Spotted, enrichedSpot.Frequency, enrichedSpot.Band, enrichedSpot.Source)
				if verbose {
					logging.Debug("Aggregator added enriched spot from %s (spotter=%s)", enrichedSpot.Source, enrichedSpot.Spotter)
				}
				// logging.Debug("New Spot: %s -> %s @ %.3f MHz (%s) from %s", enrichedSpot.Spotter, enrichedSpot.Spotted, enrichedSpot.Frequency, enrichedSpot.Band, enrichedSpot.Source)
			}
		}
	}()

	// Now that the aggregator is running and forwarder channels are wired,
	// start POTA polling so POTA spots are emitted into an active pipeline
	// and have a better chance of remaining in the cache for tests.
	if potaClient != nil {
		potaClient.StartPolling(ctx)
		logging.Info("POTA polling started (deferred).")
	}

	// --- 9. Graceful Shutdown ---
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Wait for either an OS signal or the provided context to be cancelled by tests.
	select {
	case <-quit:
		logging.Info("Received OS shutdown signal. Shutting down server...")
	case <-ctx.Done():
		logging.Info("Context cancelled. Shutting down server...")
	}

	// Debug: show goroutine count before shutdown
	logging.Debug("goroutines before shutdown: %d", runtime.NumGoroutine())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("FATAL: Server forced to shutdown: %v", err)
		return 3
	}
	// Debug: show goroutine count after HTTP server shutdown
	logging.Debug("goroutines after HTTP server shutdown: %d", runtime.NumGoroutine())

	// Explicitly close DX Cluster clients with a short per-client timeout so
	// a stuck client Close() won't hang the entire shutdown process.
	for i, c := range dxClusterClients {
		name := "<unknown>"
		if i < len(dxClusterHosts) {
			name = dxClusterHosts[i]
		}
		logging.Info("Closing DX Cluster client %s...", name)
		done := make(chan struct{})
		go func(cl *cluster.Client) {
			cl.Close()
			close(done)
		}(c)
		select {
		case <-done:
			logging.Info("DX Cluster client %s closed.", name)
		case <-time.After(1 * time.Second):
			logging.Warn("Timeout closing DX Cluster client %s; continuing shutdown.", name)
		}
	}
	// Optionally dump full goroutine stacks before deferred cleanup. This
	// is noisy in CI and normal runs, so gate it behind an env var.
	if v := os.Getenv("DX_API_DUMP_PRE_SHUTDOWN"); v == "1" || strings.ToLower(v) == "true" {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		log.Printf("=== goroutine stack dump (len=%d) ===\n%s\n=== end goroutine stack dump ===", n, string(buf[:n]))
	} else {
		logging.Debug("set DX_API_DUMP_PRE_SHUTDOWN=true to enable full pre-shutdown goroutine stack dump")
	}
	logging.Info("Server exited gracefully.")
	return 0
}

// enrichSpot enriches a raw spot with DXCC and LoTW information.
func enrichSpot(ctx context.Context, s spot.Spot, dxccClient *dxcc.Client, lotwClient *lotw.Client) (spot.Spot, error) {
	// Known pseudo-callsigns used by automated systems (skip DXCC/LoTW lookups)
	pseudoCallsigns := map[string]bool{
		"RBNHOLE": true, // Reverse Beacon Network aggregated spots
		"SOTAMAT": true, // SOTA automated spotting system
	}

	// Enrich Spotter Info (skip if pseudo-callsign)
	if !pseudoCallsigns[s.Spotter] {
		spotterDxcc, err := dxccClient.GetDxccInfo(ctx, s.Spotter, nil) // No historical lookup date
		if err != nil {
			logging.Warn("DXCC lookup failed for spotter %s: %v", s.Spotter, err)
		}
		spotterLoTW, err := lotwClient.GetLoTWUserActivity(ctx, s.Spotter)
		if err != nil {
			logging.Warn("LoTW lookup failed for spotter %s: %v", s.Spotter, err)
		}
		s.SpotterInfo.DXCC = spotterDxcc
		s.SpotterInfo.LoTW = spotterLoTW
		s.SpotterInfo.IsLoTWUser = spotterLoTW != nil // Convenience field
	} else {
		logging.Debug("Skipping DXCC/LoTW lookup for pseudo-callsign spotter: %s", s.Spotter)
	}

	// Enrich Spotted Info
	spottedDxcc, err := dxccClient.GetDxccInfo(ctx, s.Spotted, nil)
	if err != nil {
		logging.Warn("DXCC lookup failed for spotted %s: %v", s.Spotted, err)
	}
	spottedLoTW, err := lotwClient.GetLoTWUserActivity(ctx, s.Spotted)
	if err != nil {
		logging.Warn("LoTW lookup failed for spotted %s: %v", s.Spotted, err)
	}
	s.SpottedInfo.DXCC = spottedDxcc
	s.SpottedInfo.LoTW = spottedLoTW
	s.SpottedInfo.IsLoTWUser = spottedLoTW != nil // Convenience field

	// Add Band information
	s.Band = spot.BandFromName(s.Frequency)
	logging.Debug("Band assignment: %.3f kHz -> %s", s.Frequency, s.Band)

	return s, nil
}

// mergeSpotChannels uses the fan-in pattern to merge multiple spot channels into one.
func mergeSpotChannels(channels ...<-chan spot.Spot) <-chan spot.Spot {
	var wg sync.WaitGroup
	out := make(chan spot.Spot)

	// Function to read from a channel and send to the output channel
	output := func(c <-chan spot.Spot) {
		for n := range c {
			out <- n
		}
		wg.Done()
	}

	wg.Add(len(channels))
	for _, c := range channels {
		go output(c)
	}

	// Start a goroutine to close the output channel once all input channels are closed
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

// setupAPIRoutes configures all API endpoints.
func setupAPIRoutes(r *gin.RouterGroup, cache *spotCache, dxccClient *dxcc.Client, lotwClient *lotw.Client) {
	// GET /spots - Retrieve all cached spots.
	r.GET("/spots", func(c *gin.Context) {
		spots := cache.GetAllSpots()
		c.JSON(http.StatusOK, spots)
	})

	// GET /spot/:qrg - Retrieve the latest spot for a given frequency (QRG in MHz).
	r.GET("/spot/:qrg", func(c *gin.Context) {
		qrgParam := c.Param("qrg")
		qrg, err := strconv.ParseFloat(qrgParam, 64) // Frequency is in MHz
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid frequency format"})
			return
		}

		spots := cache.GetAllSpots()
		var latestSpot *spot.Spot
		var youngestTime time.Time

		for i := range spots {
			// Compare frequencies with a small tolerance if needed, or exact match for now
			// Given spots are float64, exact match is often problematic.
			// However, the Node.js used `qrg*1 === single.frequency`, implying exact match.
			if spots[i].Frequency == qrg {
				if latestSpot == nil || spots[i].When.After(youngestTime) {
					latestSpot = &spots[i]
					youngestTime = spots[i].When
				}
			}
		}

		if latestSpot != nil {
			c.JSON(http.StatusOK, latestSpot)
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": "No spot found for this frequency"})
		}
	})

	// GET /spots/:band - Retrieve all cached spots for a given band.
	r.GET("/spots/:band", func(c *gin.Context) {
		bandParam := strings.ToLower(c.Param("band"))
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToLower(s.Band) == bandParam {
				filteredSpots = append(filteredSpots, s)
			}
		}
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spots/source/:source - Retrieve all cached spots from a given source.
	r.GET("/spots/source/:source", func(c *gin.Context) {
		sourceParam := strings.ToLower(c.Param("source"))
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToLower(s.Source) == sourceParam {
				filteredSpots = append(filteredSpots, s)
			}
		}
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spots/callsign/:callsign - Retrieve all cached spots involving a given callsign (spotter or spotted).
	r.GET("/spots/callsign/:callsign", func(c *gin.Context) {
		callsignParam := strings.ToUpper(c.Param("callsign"))
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToUpper(s.Spotter) == callsignParam || strings.ToUpper(s.Spotted) == callsignParam {
				filteredSpots = append(filteredSpots, s)
			}
		}
		// Sort by 'When' descending (latest first)
		sort.Slice(filteredSpots, func(i, j int) bool {
			return filteredSpots[i].When.After(filteredSpots[j].When)
		})
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spotter/:callsign - Retrieve all cached spots where the given callsign is the spotter.
	r.GET("/spotter/:callsign", func(c *gin.Context) {
		callsignParam := strings.ToUpper(c.Param("callsign"))
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToUpper(s.Spotter) == callsignParam {
				filteredSpots = append(filteredSpots, s)
			}
		}
		sort.Slice(filteredSpots, func(i, j int) bool {
			return filteredSpots[i].When.After(filteredSpots[j].When)
		})
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spotted/:callsign - Retrieve all cached spots where the given callsign is the spotted station.
	r.GET("/spotted/:callsign", func(c *gin.Context) {
		callsignParam := strings.ToUpper(c.Param("callsign"))
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if strings.ToUpper(s.Spotted) == callsignParam {
				filteredSpots = append(filteredSpots, s)
			}
		}
		sort.Slice(filteredSpots, func(i, j int) bool {
			return filteredSpots[i].When.After(filteredSpots[j].When)
		})
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /spots/dxcc/:dxcc - Retrieve all cached spots for a given DXCC entity name.
	r.GET("/spots/dxcc/:dxcc", func(c *gin.Context) {
		dxccNameParam := strings.ToLower(c.Param("dxcc")) // Match against entity name (e.g., "Italy")
		filteredSpots := make([]spot.Spot, 0)
		for _, s := range cache.GetAllSpots() {
			if (s.SpottedInfo.DXCC != nil && strings.ToLower(s.SpottedInfo.DXCC.Entity) == dxccNameParam) ||
				(s.SpotterInfo.DXCC != nil && strings.ToLower(s.SpotterInfo.DXCC.Entity) == dxccNameParam) {
				filteredSpots = append(filteredSpots, s)
			}
		}
		sort.Slice(filteredSpots, func(i, j int) bool {
			return filteredSpots[i].When.After(filteredSpots[j].When)
		})
		c.JSON(http.StatusOK, filteredSpots)
	})

	// GET /stats - Retrieve statistics about the cached spots.
	r.GET("/stats", func(c *gin.Context) {
		spots := cache.GetAllSpots()
		stats := gin.H{
			"entries":  len(spots),
			"cluster":  0,
			"pota":     0,
			"freshest": nil, // Will be ISO string or null
			"oldest":   nil, // Will be ISO string or null
		}

		// Add DXCC and LoTW last update times
		if dxccLastUpdate, err := dxccClient.GetLastDownloadTime(context.Background()); err == nil && !dxccLastUpdate.IsZero() {
			stats["dxcc_last_updated"] = dxccLastUpdate.Format(time.RFC3339)
		} else {
			stats["dxcc_last_updated"] = nil
		}

		if lotwLastUpdate, err := lotwClient.GetLastDownloadTime(context.Background()); err == nil && !lotwLastUpdate.IsZero() {
			stats["lotw_last_updated"] = lotwLastUpdate.Format(time.RFC3339)
		} else {
			stats["lotw_last_updated"] = nil
		}

		if len(spots) > 0 {
			youngest := time.Time{}                              // Zero time
			oldest := time.Now().Add(100 * 365 * 24 * time.Hour) // Far future

			for i := range spots {
				// Treat any non-pota source as a cluster-derived spot for stats
				if strings.ToLower(spots[i].Source) == "pota" {
					stats["pota"] = stats["pota"].(int) + 1
				} else {
					stats["cluster"] = stats["cluster"].(int) + 1
				}

				if spots[i].When.After(youngest) {
					youngest = spots[i].When
				}
				if spots[i].When.Before(oldest) {
					oldest = spots[i].When
				}
			}
			stats["freshest"] = youngest.Format(time.RFC3339)
			stats["oldest"] = oldest.Format(time.RFC3339)
		}
		c.JSON(http.StatusOK, stats)
	})

	// Test-only debug endpoint: reports per-source counts.
	// This route is registered only when DX_API_TEST_DEBUG=1 is set so it
	// remains unavailable in production by default.
	if v := os.Getenv("DX_API_TEST_DEBUG"); v == "1" || strings.ToLower(v) == "true" {
		r.GET("/debug", func(c *gin.Context) {
			counts := make(map[string]int)
			for _, s := range cache.GetAllSpots() {
				src := strings.ToLower(s.Source)
				counts[src]++
			}
			c.JSON(http.StatusOK, gin.H{"per_source": counts})
		})
	}
}
