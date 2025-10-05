package config_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/user00265/dxclustergoapi/internal/config"
)

// setEnv temporarily sets an environment variable and returns a cleanup function.
func setEnv(t *testing.T, key, value string) func() {
	oldValue, present := os.LookupEnv(key)
	os.Setenv(key, value)
	return func() {
		if present {
			os.Setenv(key, oldValue)
		} else {
			os.Unsetenv(key)
		}
	}
}

// clearEnvs clears a list of environment variables and returns a cleanup function.
func clearEnvs(t *testing.T, keys ...string) func() {
	oldValues := make(map[string]string)
	presentKeys := make(map[string]bool)

	for _, key := range keys {
		if val, present := os.LookupEnv(key); present {
			oldValues[key] = val
			presentKeys[key] = true
			os.Unsetenv(key)
		} else {
			presentKeys[key] = false
		}
	}

	return func() {
		for _, key := range keys {
			if presentKeys[key] {
				os.Setenv(key, oldValues[key])
			} else {
				os.Unsetenv(key)
			}
		}
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear all relevant environment variables to ensure defaults are picked up
	cleanup := clearEnvs(t,
		"WEBPORT", "WEBURL", "MAXCACHE", "DATA_DIR",
		"CLUSTERS", "DXHOST", "DXPORT", "DXCALL", "DXPASSWORD",
		"POTA_INTEGRATION", "POTA_POLLING_INTERVAL",
		"CLUBLOG_API_KEY", "DXCC_UPDATE_INTERVAL",
		"LOTW_UPDATE_INTERVAL",
		"REDIS_ENABLED", "REDIS_HOST", "REDIS_PORT", "REDIS_USER", "REDIS_PASSWORD", "REDIS_DB", "REDIS_USE_TLS", "REDIS_INSECURE_SKIP_VERIFY", "REDIS_SPOT_EXPIRY",
	)
	defer cleanup()

	// Ensure /data directory exists for default DATA_DIR, or create mock
	tempDir := t.TempDir()
	tempCleanup := setEnv(t, "DATA_DIR", tempDir)
	defer tempCleanup()

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.WebPort != config.DefaultWebPort {
		t.Errorf("Expected WebPort to be %d, got %d", config.DefaultWebPort, cfg.WebPort)
	}
	if cfg.MaxCache != config.DefaultMaxCache {
		t.Errorf("Expected MaxCache to be %d, got %d", config.DefaultMaxCache, cfg.MaxCache)
	}
	if cfg.DataDir != tempDir {
		t.Errorf("Expected DataDir to be %s, got %s", tempDir, cfg.DataDir)
	}
	if cfg.EnablePOTA != false {
		t.Errorf("Expected EnablePOTA to be false, got %t", cfg.EnablePOTA)
	}
	if cfg.POTAPollInterval != config.DefaultPOTAPollInterval {
		t.Errorf("Expected POTAPollInterval to be %s, got %s", config.DefaultPOTAPollInterval, cfg.POTAPollInterval)
	}
	if cfg.ClubLogAPIKey != config.DefaultClubLogAPIKey {
		t.Errorf("Expected ClubLogAPIKey to be %s, got %s", config.DefaultClubLogAPIKey, cfg.ClubLogAPIKey)
	}
	if cfg.DXCCUpdateInterval != config.DefaultDXCCUpdateInterval {
		t.Errorf("Expected DXCCUpdateInterval to be %s, got %s", config.DefaultDXCCUpdateInterval, cfg.DXCCUpdateInterval)
	}
	if cfg.LoTWUpdateInterval != config.DefaultLoTWUpdateInterval {
		t.Errorf("Expected LoTWUpdateInterval to be %s, got %s", config.DefaultLoTWUpdateInterval, cfg.LoTWUpdateInterval)
	}

	// Check Redis defaults
	if cfg.Redis.Enabled != false {
		t.Errorf("Expected Redis.Enabled to be false, got %t", cfg.Redis.Enabled)
	}
	if cfg.Redis.Port != "6379" {
		t.Errorf("Expected Redis.Port to be 6379, got %s", cfg.Redis.Port)
	}
	if cfg.Redis.DB != 0 {
		t.Errorf("Expected Redis.DB to be 0, got %d", cfg.Redis.DB)
	}
	if cfg.Redis.SpotExpiry != 360*time.Second {
		t.Errorf("Expected Redis.SpotExpiry to be 360s, got %s", cfg.Redis.SpotExpiry)
	}

	// Check DX Cluster defaults (if no CLUSTERS JSON and no DXCALL)
	if len(cfg.Clusters) != 0 {
		t.Errorf("Expected 0 clusters, got %d when DXCALL is not set", len(cfg.Clusters))
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	cleanup := clearEnvs(t,
		"WEBPORT", "MAXCACHE", "DATA_DIR", "POTA_INTEGRATION", "POTA_POLLING_INTERVAL",
		"REDIS_ENABLED", "REDIS_HOST", "REDIS_PASSWORD", "REDIS_DB", "REDIS_USE_TLS", "REDIS_SPOT_EXPIRY",
	)
	defer cleanup()

	tempDir := t.TempDir()
	os.MkdirAll(tempDir, 0755) // Ensure the temp directory exists

	cleanup1 := setEnv(t, "WEBPORT", "8888")
	defer cleanup1()
	cleanup2 := setEnv(t, "MAXCACHE", "500")
	defer cleanup2()
	cleanup3 := setEnv(t, "DATA_DIR", tempDir)
	defer cleanup3()
	cleanup4 := setEnv(t, "POTA_INTEGRATION", "true")
	defer cleanup4()
	cleanup5 := setEnv(t, "POTA_POLLING_INTERVAL", "60s")
	defer cleanup5()
	cleanup6 := setEnv(t, "REDIS_ENABLED", "true")
	defer cleanup6()
	cleanup7 := setEnv(t, "REDIS_HOST", "myredis.example.com")
	defer cleanup7()
	cleanup8 := setEnv(t, "REDIS_PASSWORD", "supersecret")
	defer cleanup8()
	cleanup9 := setEnv(t, "REDIS_DB", "1")
	defer cleanup9()
	cleanup10 := setEnv(t, "REDIS_USE_TLS", "true")
	defer cleanup10()
	cleanup11 := setEnv(t, "REDIS_SPOT_EXPIRY", "10m")
	defer cleanup11()

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.WebPort != 8888 {
		t.Errorf("Expected WebPort to be 8888, got %d", cfg.WebPort)
	}
	if cfg.MaxCache != 500 {
		t.Errorf("Expected MaxCache to be 500, got %d", cfg.MaxCache)
	}
	if cfg.DataDir != tempDir {
		t.Errorf("Expected DataDir to be %s, got %s", tempDir, cfg.DataDir)
	}
	if cfg.EnablePOTA != true {
		t.Errorf("Expected EnablePOTA to be true, got %t", cfg.EnablePOTA)
	}
	if cfg.POTAPollInterval != 60*time.Second {
		t.Errorf("Expected POTAPollInterval to be 60s, got %s", cfg.POTAPollInterval)
	}

	// Check Redis overrides
	if cfg.Redis.Enabled != true {
		t.Errorf("Expected Redis.Enabled to be true, got %t", cfg.Redis.Enabled)
	}
	if cfg.Redis.Host != "myredis.example.com" {
		t.Errorf("Expected Redis.Host to be 'myredis.example.com', got %s", cfg.Redis.Host)
	}
	if cfg.Redis.Password != "supersecret" {
		t.Errorf("Expected Redis.Password to be 'supersecret', got %s", cfg.Redis.Password)
	}
	if cfg.Redis.DB != 1 {
		t.Errorf("Expected Redis.DB to be 1, got %d", cfg.Redis.DB)
	}
	if cfg.Redis.UseTLS != true {
		t.Errorf("Expected Redis.UseTLS to be true, got %t", cfg.Redis.UseTLS)
	}
	if cfg.Redis.SpotExpiry != 10*time.Minute {
		t.Errorf("Expected Redis.SpotExpiry to be 10m, got %s", cfg.Redis.SpotExpiry)
	}
}

func TestLoadConfig_ClusterJSON(t *testing.T) {
	cleanup := clearEnvs(t, "CLUSTERS", "DXHOST", "DXPORT", "DXCALL", "DXPASSWORD")
	defer cleanup()

	jsonConfig := `[{"host":"c1.example.com","port":"8000","call":"TEST1","cluster":"ClusterOne"},{"host":"c2.example.com","port":"7300","call":"TEST2","password":"pass","loginPrompt":"user:","cluster":"ClusterTwo"}]`
	clustersCleanup := setEnv(t, "CLUSTERS", jsonConfig)
	defer clustersCleanup()

	tempDir := t.TempDir()
	tempCleanup := setEnv(t, "DATA_DIR", tempDir)
	defer tempCleanup()

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.Clusters) != 2 {
		t.Fatalf("Expected 2 clusters from JSON, got %d", len(cfg.Clusters))
	}

	c1 := cfg.Clusters[0]
	if c1.Host != "c1.example.com" || c1.Port != "8000" || c1.Callsign != "TEST1" || c1.ClusterName != "ClusterOne" {
		t.Errorf("Cluster 1 mismatch: %+v", c1)
	}

	c2 := cfg.Clusters[1]
	if c2.Host != "c2.example.com" || c2.Port != "7300" || c2.Callsign != "TEST2" || c2.Password != "pass" || c2.LoginPrompt != "user:" || c2.ClusterName != "ClusterTwo" {
		t.Errorf("Cluster 2 mismatch: %+v", c2)
	}
}

func TestLoadConfig_IndividualClusterFallback(t *testing.T) {
	cleanup := clearEnvs(t, "CLUSTERS", "DXHOST", "DXPORT", "DXCALL", "DXPASSWORD")
	defer cleanup()

	d1 := setEnv(t, "DXHOST", "legacy.example.com")
	defer d1()
	d2 := setEnv(t, "DXPORT", "7301")
	defer d2()
	d3 := setEnv(t, "DXCALL", "LEGACYCALL")
	defer d3()
	d4 := setEnv(t, "DXPASSWORD", "legacypass")
	defer d4()

	tempDir := t.TempDir()
	tempCleanup := setEnv(t, "DATA_DIR", tempDir)
	defer tempCleanup()

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.Clusters) != 1 {
		t.Fatalf("Expected 1 cluster from individual vars, got %d", len(cfg.Clusters))
	}

	c1 := cfg.Clusters[0]
	if c1.Host != "legacy.example.com" || c1.Port != "7301" || c1.Callsign != "LEGACYCALL" || c1.Password != "legacypass" || c1.ClusterName != "DXCluster" {
		t.Errorf("Legacy cluster mismatch: %+v", c1)
	}
}

func TestLoadConfig_IndividualClusterFallback_NoCallsign(t *testing.T) {
	cleanup := clearEnvs(t, "CLUSTERS", "DXHOST", "DXPORT", "DXCALL", "DXPASSWORD")
	defer cleanup()

	setEnv(t, "DXHOST", "legacy.example.com")()
	setEnv(t, "DXPORT", "7301")()
	// No DXCALL set

	tempDir := t.TempDir()
	tempCleanup := setEnv(t, "DATA_DIR", tempDir)
	defer tempCleanup()

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.Clusters) != 0 {
		t.Errorf("Expected 0 clusters, got %d, because DXCALL was not set", len(cfg.Clusters))
	}
}

func TestLoadConfig_POTAPollIntervalMin(t *testing.T) {
	cleanup := clearEnvs(t, "POTA_POLLING_INTERVAL", "DATA_DIR")
	defer cleanup()

	tempDir := t.TempDir()
	tempCleanup := setEnv(t, "DATA_DIR", tempDir)
	defer tempCleanup()

	p := setEnv(t, "POTA_POLLING_INTERVAL", "15s") // Below minimum
	defer p()

	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.POTAPollInterval != config.MinPOTAPollInterval {
		t.Errorf("Expected POTAPollInterval to be enforced to min %s, got %s", config.MinPOTAPollInterval, cfg.POTAPollInterval)
	}
}

func TestLoadConfig_DataDirCreation(t *testing.T) {
	cleanup := clearEnvs(t, "DATA_DIR")
	defer cleanup()

	nonExistentDir := "./testdata/nonexistent_dir_12345"
	dd := setEnv(t, "DATA_DIR", nonExistentDir)
	defer dd()

	_, err := config.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed to create DATA_DIR: %v", err)
	}
	if _, err := os.Stat(nonExistentDir); os.IsNotExist(err) {
		t.Errorf("DATA_DIR was not created: %s", nonExistentDir)
	}
	os.RemoveAll(nonExistentDir) // Clean up
}

func TestLoadConfig_InvalidClustersJSON(t *testing.T) {
	cleanup := clearEnvs(t, "CLUSTERS", "DATA_DIR")
	defer cleanup()

	bad := setEnv(t, "CLUSTERS", "invalid json")
	defer bad()

	tempDir := t.TempDir()
	tempCleanup := setEnv(t, "DATA_DIR", tempDir)
	defer tempCleanup()

	_, err := config.LoadConfig()
	if err == nil || !strings.Contains(err.Error(), "failed to parse CLUSTERS JSON") {
		t.Errorf("Expected error for invalid CLUSTERS JSON, got: %v", err)
	}
}
