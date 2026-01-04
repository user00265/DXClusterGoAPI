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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Early setup and validation
	if err := setupLogging(args); err != nil {
		return 1
	}

	defer postCleanupDump(args)

	startupTime := time.Now()

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Printf("FATAL: Failed to load configuration: %v", err)
		return 1
	}

	logConfiguration(cfg)

	if len(args) > 0 && strings.ToLower(args[0]) == "healthcheck" {
		fmt.Println("Health check successful")
		return 0
	}

	// Initialize core services
	rdb := initializeRedis(ctx, cfg)
	defer func() {
		if rdb != nil {
			rdb.Close()
		}
	}()

	lotwClient, err := initializeLoTW(ctx, cfg)
	if err != nil {
		return 1
	}

	dxccClient, err := initializeDXCC(ctx, cfg)
	if err != nil {
		return 1
	}

	logging.Notice("DXCC and LoTW data loaded. Ready to initialize spot sources and HTTP API.")

	potaClient := initializePOTA(ctx, cfg, rdb)
	if potaClient != nil {
		defer potaClient.StopPolling()
	}

	dxClusterClients, dxClusterHosts := initializeDXClusters(ctx, cfg)
	if len(dxClusterClients) == 0 && potaClient == nil {
		logging.Error("No active DX Cluster connections and POTA is disabled. Exiting as there are no spot sources.")
		return 2
	}

	// Set up HTTP API
	centralSpotCache := frontend.NewCache()
	router := setupHTTPRouter(cfg)
	frontend.SetupRoutes(router.Group(cfg.BaseURL), centralSpotCache, dxccClient, lotwClient)

	srv := &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", cfg.WebPort),
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("FATAL: HTTP server failed: %v\n", err)
		}
	}()
	logging.Info("HTTP API listening on 0.0.0.0:%d (BaseURL: %s)", cfg.WebPort, cfg.BaseURL)

	// Start spot sources
	connectClusters(ctx, dxClusterClients, dxClusterHosts, cfg)
	spotChannels := buildSpotChannels(ctx, dxClusterClients, dxClusterHosts, potaClient)

	// Spot aggregation and enhancement
	go spotAggregator(ctx, spotChannels, centralSpotCache, dxccClient, lotwClient)

	if potaClient != nil {
		potaClient.StartPolling(ctx)
		logging.Info("POTA polling started (deferred).")
	}

	// Status reporting
	go statusReporter(ctx, startupTime, cfg, centralSpotCache)

	// Graceful shutdown
	status := gracefulShutdown(ctx, srv, dxClusterClients, dxClusterHosts)

	// Perform cleanup for clients that were not closed by gracefulShutdown
	logging.Info("Performing final client cleanup...")
	lotwClient.StopUpdater()
	if err := lotwClient.DbClient.Close(); err != nil {
		logging.Error("Error closing LoTW DB client: %v", err)
	}
	if err := dxccClient.Close(); err != nil {
		logging.Error("Error closing DXCC client: %v", err)
	}

	return status
}

// setupLogging applies LOG_LEVEL environment variable to logging system
func setupLogging(args []string) error {
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
			logging.Warn("Unrecognized LOG_LEVEL=%q; valid: crit,error,warn,notice,info,debug or 0-5. Using default (NOTICE).", v)
		}
	}

	logging.Notice("Starting %s %s (+%s)", version.ProjectName, version.ProjectVersion, version.ProjectGitHubURL)
	return nil
}

// postCleanupDump emits diagnostics after shutdown
func postCleanupDump(args []string) {
	if len(args) > 0 && strings.ToLower(args[0]) == "healthcheck" {
		return
	}
	if v := os.Getenv("DX_API_DUMP_POST_CLEANUP"); !(v == "1" || strings.ToLower(v) == "true") {
		logging.Debug("Debug (post-cleanup): goroutines final: %d (set DX_API_DUMP_POST_CLEANUP=1 to see full stacks)", runtime.NumGoroutine())
		return
	}
	time.Sleep(150 * time.Millisecond)
	logging.Debug("Debug (post-cleanup): goroutines final: %d", runtime.NumGoroutine())
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	log.Printf("=== goroutine stack dump (post-cleanup len=%d) ===\n%s\n=== end goroutine stack dump ===", n, string(buf[:n]))
}

// logConfiguration logs configuration details
func logConfiguration(cfg *config.Config) {
	logging.Notice("Configuration loaded. WebPort: %d, MaxCache: %d, DataDir: %s", cfg.WebPort, cfg.MaxCache, cfg.DataDir)
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
}

// initializeRedis creates and tests Redis client, returns nil on failure
func initializeRedis(ctx context.Context, cfg *config.Config) *redisclient.Client {
	if !cfg.Redis.Enabled {
		return nil
	}
	rdb, err := redisclient.NewClient(ctx, cfg.Redis)
	if err != nil {
		logging.Warn("Redis client initialization failed: %v. Continuing without Redis (in-memory mode).", err)
		return nil
	}
	logging.Notice("Redis client initialized and connected.")
	return rdb
}

// initializeLoTW creates and validates LoTW client with data download
func initializeLoTW(ctx context.Context, cfg *config.Config) (*lotw.Client, error) {
	lotwDBClient, err := db.NewSQLiteClient(cfg.DataDir, lotw.DBFileName)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize LoTW DB client: %v\n", err)
	}

	lotwClient, err := lotw.NewClient(ctx, *cfg, lotwDBClient)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize LoTW client: %v\n", err)
	}

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

	lotwClient.StartUpdater(ctx)
	logging.Info("LoTW client ready. Background updater started for periodic checks.")
	return lotwClient, nil
}

// initializeDXCC creates and validates DXCC client with data download
func initializeDXCC(ctx context.Context, cfg *config.Config) (*dxcc.Client, error) {
	dxccDBClient, err := db.NewSQLiteClient(cfg.DataDir, dxcc.DBFileName)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize DXCC DB client: %v\n", err)
	}

	dxccClient, err := dxcc.NewClient(ctx, *cfg, dxccDBClient)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize DXCC client: %v\n", err)
	}

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
		if err := dxccClient.LoadMapsFromDB(ctx); err != nil {
			log.Fatalf("FATAL: Failed to load DXCC maps from database: %v\n", err)
		}
		if len(dxccClient.PrefixesMap) == 0 {
			logging.Warn("DXCC database is empty despite recent update timestamp. Forcing download now...")
			dxccClient.FetchAndStoreData(ctx)
			logging.Notice("DXCC data downloaded and loaded successfully after empty database detection.")
		}
	}

	dxccClient.StartUpdater(ctx)
	logging.Info("DXCC client ready. Background updater started for periodic checks.")
	return dxccClient, nil
}

// initializePOTA creates POTA client if enabled
func initializePOTA(ctx context.Context, cfg *config.Config, rdb *redisclient.Client) *pota.Client {
	if !cfg.EnablePOTA {
		return nil
	}
	potaClient, err := pota.NewClient(ctx, *cfg, rdb)
	if err != nil {
		log.Fatalf("FATAL: Failed to initialize POTA client: %v\n", err)
	}
	logging.Info("POTA client initialized (polling deferred until aggregator is running).")
	return potaClient
}

// initializeDXClusters creates cluster clients from configuration
func initializeDXClusters(ctx context.Context, cfg *config.Config) ([]*cluster.Client, []string) {
	clients := make([]*cluster.Client, 0, len(cfg.Clusters))
	hosts := make([]string, 0, len(cfg.Clusters))
	for _, cc := range cfg.Clusters {
		client, err := cluster.NewClient(cc)
		if err != nil {
			logging.Warn("Failed to create DX Cluster client for %s:%s: %v. Skipping this cluster.", cc.Host, cc.Port.String(), err)
			continue
		}
		clients = append(clients, client)
		hosts = append(hosts, cc.Host)
		buf := cap(client.SpotChan)
		logging.Info("DX Cluster client for %s:%s created. channel_buffer=%d", cc.Host, cc.Port.String(), buf)
	}
	return clients, hosts
}

// setupHTTPRouter creates and configures Gin router
func setupHTTPRouter(cfg *config.Config) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false

	// Normalize paths: remove trailing slashes and collapse multiple slashes
	router.Use(func(c *gin.Context) {
		path := c.Request.URL.Path
		// Collapse multiple slashes
		for strings.Contains(path, "//") {
			path = strings.ReplaceAll(path, "//", "/")
		}
		// Remove trailing slash (except for root)
		if len(path) > 1 && strings.HasSuffix(path, "/") {
			path = strings.TrimSuffix(path, "/")
		}
		c.Request.URL.Path = path
		c.Next()
	})

	router.Use(logging.GinRecovery())
	router.Use(logging.GinLogger())

	if cfg.TrustedProxies != "" {
		trustedProxiesSlice := strings.Split(cfg.TrustedProxies, ",")
		for i := range trustedProxiesSlice {
			trustedProxiesSlice[i] = strings.TrimSpace(trustedProxiesSlice[i])
		}
		router.SetTrustedProxies(trustedProxiesSlice)
		logging.Info("Trusted proxies configured: %v", trustedProxiesSlice)

		if cfg.RemoteIPHeader != "" {
			router.RemoteIPHeaders = []string{cfg.RemoteIPHeader}
			logging.Info("Remote IP header: %s, invert (first IP): %v", cfg.RemoteIPHeader, cfg.RemoteIPInvert)
		}
	}

	// Setup base URL routing
	if cfg.BaseURL != "/" && cfg.BaseURL != "" {
		router.Group(cfg.BaseURL).Use(func(c *gin.Context) {
			c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, cfg.BaseURL)
			c.Next()
		}).GET("/healthz", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})
	} else {
		router.GET("/healthz", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})
	}

	return router
}

// connectClusters starts connection loops for all DX Cluster clients
func connectClusters(ctx context.Context, clients []*cluster.Client, hosts []string, cfg *config.Config) {
	for i, client := range clients {
		client.Connect(ctx)
		logging.Info("DX Cluster client %s:%s connecting...", hosts[i], cfg.Clusters[i].Port.String())
	}
}

// buildSpotChannels aggregates spot channels from all sources
func buildSpotChannels(ctx context.Context, dxClusterClients []*cluster.Client, dxClusterHosts []string, potaClient *pota.Client) []<-chan spot.Spot {
	spotChannels := make([]<-chan spot.Spot, 0)

	// Add DX Cluster channels
	for i, client := range dxClusterClients {
		clusterHost := dxClusterHosts[i]
		ch := make(chan spot.Spot, 8)
		go forwardClusterSpots(ctx, client, ch, clusterHost)
		go handleClusterErrors(ctx, client)
		spotChannels = append(spotChannels, ch)
	}

	// Add POTA channel
	if potaClient != nil {
		ch := make(chan spot.Spot, 8)
		go forwardPOTASpots(ctx, potaClient.SpotChan, ch)
		spotChannels = append(spotChannels, ch)
	}

	return spotChannels
}

// forwardClusterSpots converts cluster.Spot to unified spot.Spot
func forwardClusterSpots(ctx context.Context, client *cluster.Client, out chan<- spot.Spot, clusterHost string) {
	defer close(out)
	forwardedCount := 0
	for {
		select {
		case <-ctx.Done():
			logging.Info("Cluster forwarder [%s] shutting down. Forwarded %d spots.", clusterHost, forwardedCount)
			return
		case s, ok := <-client.SpotChan:
			if !ok {
				logging.Info("Cluster forwarder [%s] spot channel closed. Forwarded %d spots.", clusterHost, forwardedCount)
				return
			}
			forwardedCount++
			logging.Info("CLUSTER [%s] RAW SPOT #%d: %s -> %s @ %s - %s", clusterHost, forwardedCount, s.Spotter, s.Spotted, utils.FormatFrequency(s.Frequency), s.Message)
			select {
			case out <- spot.Spot{
				Spotter:   s.Spotter,
				Spotted:   s.Spotted,
				Frequency: s.Frequency,
				Message:   s.Message,
				When:      s.When,
				Source:    s.Source,
				Band:      "",      // Will be calculated downstream
				Mode:      "",      // Not available from cluster
				Submode:   "",      // Not available from cluster
			}:
				logging.Info("CLUSTER [%s] FORWARDED SPOT #%d to aggregator", clusterHost, forwardedCount)
			case <-ctx.Done():
				return
			}
		}
	}
}

// handleClusterErrors processes error channel from cluster
func handleClusterErrors(ctx context.Context, client *cluster.Client) {
	for {
		select {
		case err := <-client.ErrorChan:
			if err != nil {
				if strings.Contains(err.Error(), "use of closed network connection") {
					logging.Debug("DX Cluster connection closed (expected during shutdown): %v", err)
				} else {
					logging.Error("from DX Cluster: %v", err)
				}
			}
		case msg := <-client.MessageChan:
			_ = msg
		case <-ctx.Done():
			return
		}
	}
}

// forwardPOTASpots forwards POTA spots to aggregator
func forwardPOTASpots(ctx context.Context, in <-chan spot.Spot, out chan<- spot.Spot) {
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
}

// spotKey identifies a unique spot by spotter, spotted, and band
type spotKey struct {
	spotter string
	spotted string
	band    string
}

// spotAggregator merges, deduplicates, enhances, and caches spots
func spotAggregator(ctx context.Context, spotChannels []<-chan spot.Spot, cache *frontend.Cache, dxccClient *dxcc.Client, lotwClient *lotw.Client) {
	logging.Info("Spot aggregation goroutine starting.")
	merged := mergeSpotChannels(spotChannels...)
	verbose := os.Getenv("DX_API_VERBOSE_SPOT_PIPELINE") == "1" || strings.ToLower(os.Getenv("DX_API_VERBOSE_SPOT_PIPELINE")) == "true"
	spotCount := 0

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

			// Validate and process spot
			if !validateSpot(receivedSpot) {
				continue
			}

			// Deduplicate
			if isDuplicate(receivedSpot, recentSpots) {
				continue
			}

			band := utils.BandFromFreq(receivedSpot.Frequency)
			key := spotKey{spotter: receivedSpot.Spotter, spotted: receivedSpot.Spotted, band: band}
			recentSpots[key] = time.Now()

			// Clean old entries
			now := time.Now()
			for k, t := range recentSpots {
				if now.Sub(t) > dedupeWindow {
					delete(recentSpots, k)
				}
			}

			spotCount++
			logging.Info("SPOT #%d RECEIVED: %s -> %s @ %s [%s] - %s", spotCount, receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), receivedSpot.Source, receivedSpot.Message)
			if verbose {
				logging.Debug("Aggregator received spot: source=%s spotter=%s spotted=%s freq=%s msg=%q when=%s", receivedSpot.Source, receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), receivedSpot.Message, receivedSpot.When.Format(time.RFC3339))
			}

			enhancedSpot, err := enhanceSpot(ctx, receivedSpot, dxccClient, lotwClient)
			if err != nil {
				logging.Error("Failed to enhance spot from %s (%s->%s @ %s): %v", receivedSpot.Source, receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), err)
				cache.AddSpot(receivedSpot)
				logging.Info("SPOT #%d CACHED (non-enhanced): %s -> %s @ %s [%s]", spotCount, receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), receivedSpot.Source)
				continue
			}
			cache.AddSpot(enhancedSpot)
			logging.Info("SPOT #%d CACHED (enhanced): %s -> %s @ %s (%s) [%s]", spotCount, enhancedSpot.Spotter, enhancedSpot.Spotted, utils.FormatFrequency(enhancedSpot.Frequency), enhancedSpot.Band, enhancedSpot.Source)
			if verbose {
				logging.Debug("Aggregator added enhanced spot from %s (spotter=%s)", enhancedSpot.Source, enhancedSpot.Spotter)
			}
		}
	}
}

// validateSpot checks required fields
func validateSpot(s spot.Spot) bool {
	if strings.TrimSpace(s.Spotter) == "" {
		logging.Warn("SPOT REJECTED: missing spotter callsign. spotted=%q freq=%s source=%s", s.Spotted, utils.FormatFrequency(s.Frequency), s.Source)
		return false
	}
	if strings.TrimSpace(s.Spotted) == "" {
		logging.Warn("SPOT REJECTED: missing spotted callsign. spotter=%q freq=%s source=%s", s.Spotter, utils.FormatFrequency(s.Frequency), s.Source)
		return false
	}
	return true
}

// isDuplicate checks if spot was seen recently
func isDuplicate(receivedSpot spot.Spot, recentSpots map[spotKey]time.Time) bool {
	band := utils.BandFromFreq(receivedSpot.Frequency)
	key := spotKey{spotter: receivedSpot.Spotter, spotted: receivedSpot.Spotted, band: band}
	if lastSeen, exists := recentSpots[key]; exists {
		if time.Since(lastSeen) < 30*time.Second {
			logging.Debug("DUPLICATE SPOT FILTERED: %s -> %s @ %s (%s) [%s] (seen %.1f seconds ago)",
				receivedSpot.Spotter, receivedSpot.Spotted, utils.FormatFrequency(receivedSpot.Frequency), band, receivedSpot.Source, time.Since(lastSeen).Seconds())
			return true
		}
	}
	return false
}

// statusReporter generates hourly status reports
func statusReporter(ctx context.Context, startupTime time.Time, cfg *config.Config, cache *frontend.Cache) {
	firstTimer := time.NewTimer(1 * time.Minute)
	select {
	case <-firstTimer.C:
		generateStatusReport(startupTime, cfg, cache)
	case <-ctx.Done():
		firstTimer.Stop()
		return
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			generateStatusReport(startupTime, cfg, cache)
		case <-ctx.Done():
			return
		}
	}
}

// generateStatusReport creates and logs a status report
func generateStatusReport(startupTime time.Time, cfg *config.Config, cache *frontend.Cache) {
	uptime := time.Since(startupTime)
	uptimeStr := fmt.Sprintf("%dh%dm", int(uptime.Hours()), int(uptime.Minutes())%60)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memoryMB := m.Alloc / 1024 / 1024

	spots := cache.GetAllSpots()
	sourceCounts := make(map[string]int)
	for _, s := range spots {
		source := strings.ToLower(s.Source)
		sourceCounts[source]++
	}

	var sourceStats []string

	sotaEnabled := false
	for _, cluster := range cfg.Clusters {
		if cluster.SOTA {
			sotaEnabled = true
			break
		}
	}

	if sotaEnabled {
		sourceStats = append(sourceStats, fmt.Sprintf("sota=%d", sourceCounts["sota"]))
	}

	if cfg.EnablePOTA {
		sourceStats = append(sourceStats, fmt.Sprintf("pota=%d", sourceCounts["pota"]))
	}

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

// gracefulShutdown performs orderly application shutdown
func gracefulShutdown(ctx context.Context, srv *http.Server, dxClusterClients []*cluster.Client, dxClusterHosts []string) int {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quit:
		logging.Info("Received OS shutdown signal. Shutting down server...")
	case <-ctx.Done():
		logging.Info("Context cancelled. Shutting down server...")
	}

	logging.Debug("goroutines before shutdown: %d", runtime.NumGoroutine())

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("FATAL: Server forced to shutdown: %v", err)
		return 3
	}
	logging.Debug("goroutines after HTTP server shutdown: %d", runtime.NumGoroutine())

	// Close DX Cluster clients with timeout
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