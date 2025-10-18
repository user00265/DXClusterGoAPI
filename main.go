package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/user00265/dxclustergoapi/backend/cluster"
	"github.com/user00265/dxclustergoapi/backend/dxcc"
	"github.com/user00265/dxclustergoapi/backend/lotw"
	"github.com/user00265/dxclustergoapi/backend/pota"
	"github.com/user00265/dxclustergoapi/config"
	"github.com/user00265/dxclustergoapi/db"
	"github.com/user00265/dxclustergoapi/frontend"
	"github.com/user00265/dxclustergoapi/logging"
	"github.com/user00265/dxclustergoapi/redisclient"
	"github.com/user00265/dxclustergoapi/spot"
	"github.com/user00265/dxclustergoapi/utils"
	"github.com/user00265/dxclustergoapi/version"
)

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

	logging.Notice("Starting %s %s (+%s)", version.ProjectName, version.ProjectVersion, version.ProjectGitHubURL)

	// Track startup time for uptime calculations
	startupTime := time.Now()

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
	logging.Notice("Configuration loaded. WebPort: %d, MaxCache: %d, DataDir: %s", cfg.WebPort, cfg.MaxCache, cfg.DataDir)

	// Debug configuration details
	logging.Debug("CLUSTERS JSON: '%s'", cfg.RawClustersJSON)
	logging.Debug("Global Callsign: '%s'", cfg.Callsign)

	if len(cfg.Clusters) > 0 {
		logging.Notice("DX Clusters configured: %d", len(cfg.Clusters))
		for i, cluster := range cfg.Clusters {
			sotaFlag := "false"
			if cluster.SOTA {
				sotaFlag = "true"
			}
			logging.Notice("  Cluster %d: %s port=%s callsign=%s sota=%s", i+1, cluster.Host, cluster.Port.String(), cluster.Callsign, sotaFlag)
		}
	} else {
		logging.Warn("No DX Clusters configured.")
	}
	if cfg.EnablePOTA {
		logging.Notice("POTA integration enabled. Polling interval: %s", cfg.POTAPollInterval)
	} else {
		logging.Info("POTA integration disabled.")
	}
	if cfg.Redis.Enabled {
		logging.Notice("Redis cache enabled. Host: %s:%s, DB: %d, TLS: %t", cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.DB, cfg.Redis.UseTLS)
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
			logging.Warn("Redis client initialization failed: %v. Continuing without Redis (in-memory mode).", err)
			rdb = nil
		} else {
			defer rdb.Close()
			logging.Notice("Redis client initialized and connected.")
		}
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
	logging.Notice("Checking LoTW data status...")
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
	logging.Notice("Checking DXCC data status...")
	lastDXCCUpdate, err := dxccClient.GetLastDownloadTime(ctx)
	if err != nil {
		logging.Warn("Failed to check DXCC last update time: %v", err)
	}
	needsDXCCUpdate := lastDXCCUpdate.IsZero() || time.Since(lastDXCCUpdate) >= cfg.DXCCUpdateInterval
	if needsDXCCUpdate {
		logging.Info("DXCC data needs update, downloading now (this may take a moment)...")
		dxccClient.FetchAndStoreData(ctx)
		logging.Notice("DXCC data downloaded and loaded successfully.")
	} else {
		logging.Notice("DXCC data is up to date (last updated: %s)", lastDXCCUpdate.Format(time.RFC3339))
		// Load in-memory maps from database to check if they're populated
		if err := dxccClient.LoadMapsFromDB(ctx); err != nil {
			log.Fatalf("FATAL: Failed to load DXCC maps from database: %v\n", err)
		}
		// Verify maps are actually populated (database might be empty despite recent update timestamp)
		if len(dxccClient.PrefixesMap) == 0 {
			logging.Warn("DXCC database is empty despite recent update timestamp. Forcing download now...")
			dxccClient.FetchAndStoreData(ctx) // This will reload maps, so no need to call LoadMapsFromDB again
			logging.Notice("DXCC data downloaded and loaded successfully after empty database detection.")
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
	// Spots can now be enhanced immediately upon arrival from any source.
	logging.Notice("DXCC and LoTW data loaded. Ready to initialize spot sources and HTTP API.")

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
			logging.Warn("Failed to create DX Cluster client for %s:%s: %v. Skipping this cluster.", cc.Host, cc.Port.String(), err)
			continue
		}
		dxClusterClients = append(dxClusterClients, client)
		dxClusterHosts = append(dxClusterHosts, cc.Host)
		// DON'T call Connect() yet - we'll do that after HTTP API is ready
		buf := cap(client.SpotChan)
		logging.Info("DX Cluster client for %s:%s created. channel_buffer=%d", cc.Host, cc.Port.String(), buf)
	}
	if len(dxClusterClients) == 0 && !cfg.EnablePOTA {
		logging.Error("No active DX Cluster connections and POTA is disabled. Exiting as there are no spot sources.")
		return 2
	}

	// --- 7. Create Spot Cache and Set up HTTP API BEFORE connecting spot sources ---
	centralSpotCache := frontend.NewCache()

	// Create Gin router with our custom logging (not gin.Default())
	gin.SetMode(gin.ReleaseMode) // Suppress gin's startup messages
	router := gin.New()
	router.Use(logging.GinRecovery()) // Our recovery middleware
	router.Use(logging.GinLogger())   // Our logging middleware

	// Configure trusted proxies for X-Forwarded-For header handling
	if cfg.TrustedProxies != "" {
		// Parse comma-separated list of trusted proxies (IPs or CIDR ranges)
		trustedProxiesSlice := strings.Split(cfg.TrustedProxies, ",")
		for i := range trustedProxiesSlice {
			trustedProxiesSlice[i] = strings.TrimSpace(trustedProxiesSlice[i])
		}
		router.SetTrustedProxies(trustedProxiesSlice)
		logging.Info("Trusted proxies configured: %v", trustedProxiesSlice)

		// Configure remote IP header and inversion
		if cfg.RemoteIPInvert {
			// Pick the FIRST IP in the header
			router.RemoteIPHeaders = []string{cfg.RemoteIPHeader}
		} else {
			// Pick the LAST IP in the header (Gin's default behavior)
			router.RemoteIPHeaders = []string{cfg.RemoteIPHeader}
		}
		logging.Info("Remote IP header: %s, invert (first IP): %v", cfg.RemoteIPHeader, cfg.RemoteIPInvert)
	}

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
		frontend.SetupRoutes(apiGroup, centralSpotCache, dxccClient, lotwClient)
	} else {
		router.GET("/healthz", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})
		frontend.SetupRoutes(router.Group("/"), centralSpotCache, dxccClient, lotwClient)
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", cfg.WebPort),
		Handler: router,
	}

	// Start HTTP server in goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("FATAL: HTTP server failed: %v\n", err)
		}
	}()
	logging.Info("HTTP API listening on 0.0.0.0:%d (BaseURL: %s)", cfg.WebPort, cfg.BaseURL)

	// ═══════════════════════════════════════════════════════════════════════════
	// NOW connect DX clusters and start POTA polling - everything is ready!
	// ═══════════════════════════════════════════════════════════════════════════

	for i, client := range dxClusterClients {
		client.Connect(ctx) // Start connection and reconnection loop
		logging.Info("DX Cluster client %s:%s connecting...", dxClusterHosts[i], cfg.Clusters[i].Port.String())
	}

	// --- 8. Spot Aggregation & Enhancement ---
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
					logging.Info("CLUSTER [%s] RAW SPOT #%d: %s -> %s @ %s - %s", clusterHost, forwardedCount, s.Spotter, s.Spotted, utils.FormatFrequency(s.Frequency), s.Message) // Send into the buffered forwarder channel. If the application is
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
						// Suppress "use of closed network connection" during graceful shutdown
						// This is expected when Close() is called while read loop is active
						if strings.Contains(err.Error(), "use of closed network connection") {
							logging.Debug("DX Cluster connection closed (expected during shutdown): %v", err)
						} else {
							logging.Error("from DX Cluster: %v", err)
						}
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
		// POTA now emits canonical spot.Spot values directly. Forward them
		// into the aggregator without re-wrapping to avoid accidental
		// inclusion of raw API fields.
		ch := make(chan spot.Spot, 8)
		go func(in <-chan spot.Spot, out chan<- spot.Spot) {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case s, ok := <-in:
					if !ok {
						return
					}
					select {
					case out <- s:
					case <-ctx.Done():
						return
					}
				}
			}
		}(potaClient.SpotChan, ch)
		spotChannels = append(spotChannels, ch)
	}

	// Goroutine to aggregate, enhance, and cache spots
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
					logging.Warn("SPOT REJECTED: missing spotter callsign. spotted=%q freq=%s source=%s", receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), receivedSpot.Source)
					continue
				}
				if strings.TrimSpace(receivedSpot.Spotted) == "" {
					logging.Warn("SPOT REJECTED: missing spotted callsign. spotter=%q freq=%s source=%s", receivedSpot.Spotter, utils.FormatFrequency(receivedSpot.Frequency), receivedSpot.Source)
					continue
				} // Determine band from frequency for duplicate detection
				// (same station can be on multiple bands simultaneously)
				band := utils.BandFromFreq(receivedSpot.Frequency)

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
						logging.Debug("DUPLICATE SPOT FILTERED: %s -> %s @ %s (%s) [%s] (seen %.1f seconds ago)",
							receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), band, receivedSpot.Source, now.Sub(lastSeen).Seconds())
						continue
					}
				}

				// Record this spot
				recentSpots[key] = now

				spotCount++
				logging.Info("SPOT #%d RECEIVED: %s -> %s @ %s [%s] - %s", spotCount, receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), receivedSpot.Source, receivedSpot.Message)
				if verbose {
					logging.Debug("Aggregator received spot: source=%s spotter=%s spotted=%s freq=%s msg=%q when=%s", receivedSpot.Source, receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), receivedSpot.Message, receivedSpot.When.Format(time.RFC3339))
				}
				enhancedSpot, err := enhanceSpot(ctx, receivedSpot, dxccClient, lotwClient)
				if err != nil {
					logging.Error("Failed to enhance spot from %s (%s->%s @ %s): %v", receivedSpot.Source, receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), err)
					// Still add the non-enhanced spot if enhancement fails, or discard?
					// For now, add the partially enhanced one.
					centralSpotCache.AddSpot(receivedSpot)
					logging.Info("SPOT #%d CACHED (non-enhanced): %s -> %s @ %s [%s]", spotCount, receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), receivedSpot.Source)
					if verbose {
						logging.Debug("Aggregator added non-enhanced spot from %s (spotter=%s)", receivedSpot.Source, receivedSpot.Spotter)
					}
					continue
				}
				centralSpotCache.AddSpot(enhancedSpot)
				logging.Info("SPOT #%d CACHED (enhanced): %s -> %s @ %s (%s) [%s]", spotCount, enhancedSpot.Spotter, enhancedSpot.Spotted, utils.FormatFrequency(enhancedSpot.Frequency), enhancedSpot.Band, enhancedSpot.Source)
				if verbose {
					logging.Debug("Aggregator added enhanced spot from %s (spotter=%s)", enhancedSpot.Source, enhancedSpot.Spotter)
				}
				// logging.Debug("New Spot: %s -> %s @ %s (%s) from %s", enhancedSpot.Spotter, enhancedSpot.Spotted, utils.FormatFrequency(enhancedSpot.Frequency), enhancedSpot.Band, enhancedSpot.Source)
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

	// --- Hourly Status Reporter ---
	go func() {
		// Helper function to generate status report
		generateStatusReport := func() {
			// Calculate uptime
			uptime := time.Since(startupTime)
			uptimeStr := fmt.Sprintf("%dh%dm", int(uptime.Hours()), int(uptime.Minutes())%60)

			// Get memory usage
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			memoryMB := m.Alloc / 1024 / 1024

			// Count spots by source
			spots := centralSpotCache.GetAllSpots()
			sourceCounts := make(map[string]int)
			for _, s := range spots {
				source := strings.ToLower(s.Source)
				sourceCounts[source]++
			}

			// Build source counts string - always show enabled services even if 0
			var sourceStats []string

			// Always show SOTA count if any cluster has SOTA enabled
			sotaEnabled := false
			for _, cluster := range cfg.Clusters {
				if cluster.SOTA {
					sotaEnabled = true
					break
				}
			}

			if sotaEnabled {
				count := sourceCounts["sota"] // will be 0 if not found
				sourceStats = append(sourceStats, fmt.Sprintf("sota=%d", count))
			}

			// Always show POTA count if POTA client is enabled
			if potaClient != nil {
				count := sourceCounts["pota"] // will be 0 if not found
				sourceStats = append(sourceStats, fmt.Sprintf("pota=%d", count))
			}

			// Only show cluster count if there are non-SOTA clusters configured
			nonSotaClustersExist := false
			for _, cluster := range cfg.Clusters {
				if !cluster.SOTA {
					nonSotaClustersExist = true
					break
				}
			}
			if nonSotaClustersExist {
				clusterCount := 0
				for source, count := range sourceCounts {
					if source != "sota" && source != "pota" {
						clusterCount += count
					}
				}
				sourceStats = append(sourceStats, fmt.Sprintf("cluster=%d", clusterCount))
			}

			sourceStatsStr := strings.Join(sourceStats, ", ")
			logging.Notice("DXClusterGoAPI up %s using %dMB of memory. Spots processed: %s", uptimeStr, memoryMB, sourceStatsStr)
		}

		// First report after 1 minute
		firstTimer := time.NewTimer(1 * time.Minute)
		select {
		case <-firstTimer.C:
			generateStatusReport()
		case <-ctx.Done():
			firstTimer.Stop()
			return
		}

		// Then hourly reports
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				generateStatusReport()
			case <-ctx.Done():
				return
			}
		}
	}()

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

// cleanCallsignForDXCC removes invalid characters and suffixes that interfere with DXCC lookup
func cleanCallsignForDXCC(call string) string {
	call = strings.TrimSpace(call)
	// Remove common system suffixes that aren't part of the actual callsign
	if idx := strings.Index(call, "#"); idx != -1 {
		call = call[:idx]
	}
	if idx := strings.Index(call, "-#"); idx != -1 {
		call = call[:idx]
	}
	return strings.TrimSpace(call)
}

// enhanceSpot enhances a raw spot with DXCC and LoTW information.
func enhanceSpot(ctx context.Context, s spot.Spot, dxccClient *dxcc.Client, lotwClient *lotw.Client) (spot.Spot, error) {
	// Known pseudo-callsigns used by automated systems (skip DXCC/LoTW lookups)
	pseudoCallsigns := map[string]bool{
		"RBNHOLE": true, // Reverse Beacon Network aggregated spots
		"SOTAMAT": true, // SOTA automated spotting system
	}

	// Enrich Spotter Info (skip if pseudo-callsign)
	if !pseudoCallsigns[s.Spotter] {
		spotterDxcc, _ := utils.LookupDXCC(s.Spotter, dxccClient)
		if spotterDxcc != nil {
			s.SpotterInfo.DXCC = spotterDxcc
			// Extract core callsign for LoTW lookup
			parsed := utils.ParseCallsign(s.Spotter)
			spotterLoTW, err := lotwClient.GetLoTWUserActivity(ctx, parsed.B)
			if err != nil {
				logging.Warn("LoTW lookup failed for spotter %s (parsed: %s): %v", s.Spotter, parsed.B, err)
			}
			s.SpotterInfo.LoTW = spotterLoTW
			s.SpotterInfo.IsLoTWUser = spotterLoTW != nil // Convenience field
		}
	} else {
		logging.Debug("Skipping DXCC/LoTW lookup for pseudo-callsign spotter: %s", s.Spotter)
	}

	// Enrich Spotted Info
	spottedDxcc, _ := utils.LookupDXCC(s.Spotted, dxccClient)
	if spottedDxcc != nil {
		s.SpottedInfo.DXCC = spottedDxcc
		// Extract core callsign for LoTW lookup
		parsed := utils.ParseCallsign(s.Spotted)
		spottedLoTW, err := lotwClient.GetLoTWUserActivity(ctx, parsed.B)
		if err != nil {
			logging.Warn("LoTW lookup failed for spotted %s (parsed: %s): %v", s.Spotted, parsed.B, err)
		}
		s.SpottedInfo.LoTW = spottedLoTW
		s.SpottedInfo.IsLoTWUser = spottedLoTW != nil // Convenience field
	}

	// Add Band information
	s.Band = utils.BandFromFreq(s.Frequency)
	logging.Debug("Band assignment: %s -> %s", utils.FormatFrequency(s.Frequency), s.Band)

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
