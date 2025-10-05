package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/caarlos0/env/v11"
)

// Default values and hard-coded URLs for various configurations.
const (
	DefaultWebPort            = 8192
	DefaultMaxCache           = 100
	DefaultDXCPort            = "7300"
	DefaultPOTAPollInterval   = 120 * time.Second
	MinPOTAPollInterval       = 30 * time.Second
	DefaultDXCCUpdateInterval = 7 * 24 * time.Hour // Weekly
	DefaultLoTWUpdateInterval = 7 * 24 * time.Hour // Weekly
	DefaultDataDir            = "/data"            // Inside the container

	// Hard-coded Club Log API details (per-application, not per-user)
	// These values are based on the original Node.js project's use.
	DefaultClubLogAPIKey = "608df94896cb9c5421ae748235492b43815610c9"
)

var (
	// These are baked-in, not user-configurable. Keep them package-level so they can be
	// changed by developers in code if necessary.
	ClubLogAPIURL     = "https://cdn.clublog.org/cty.php?api=608df94896cb9c5421ae748235492b43815610c9"
	FallbackGitHubURL = "https://github.com/wavelog/dxcc_data/raw/refs/heads/master/cty.xml.gz"

	LoTWActivityURL = "https://lotw.arrl.org/lotw-user-activity.csv"
	POTAAPIEndpoint = "https://api.pota.app/spot/activator"
)

// ClusterConfig holds connection details for a single DX cluster.
type ClusterConfig struct {
	Host        string `json:"host"`
	Port        string `json:"port"`
	Callsign    string `json:"call"`
	Password    string `json:"password"`
	LoginPrompt string `json:"loginPrompt"`
	// Name of the cluster, used as the spot source (e.g., "DXCluster", "SOTA")
	ClusterName string `json:"cluster"`
	// ChannelBuffer controls the size of the internal channels used by the cluster client.
	// If zero, a sensible default will be used by the client.
	ChannelBuffer int `json:"channelBuffer"`
}

// RedisConfig holds configuration for the optional Redis cache.
type RedisConfig struct {
	Enabled            bool          `env:"REDIS_ENABLED" envDefault:"false"`
	Host               string        `env:"REDIS_HOST"`
	Port               string        `env:"REDIS_PORT" envDefault:"6379"`
	User               string        `env:"REDIS_USER"`
	Password           string        `env:"REDIS_PASSWORD"`
	DB                 int           `env:"REDIS_DB" envDefault:"0"`
	UseTLS             bool          `env:"REDIS_USE_TLS" envDefault:"false"`
	InsecureSkipVerify bool          `env:"REDIS_INSECURE_SKIP_VERIFY" envDefault:"false"`
	SpotExpiry         time.Duration `env:"REDIS_SPOT_EXPIRY" envDefault:"360s"` // Default 6 minutes
}

// Config holds all application configuration.
type Config struct {
	WebPort  int    `env:"WEBPORT" envDefault:"8192"`
	BaseURL  string `env:"WEBURL" envDefault:"/"`
	MaxCache int    `env:"MAXCACHE" envDefault:"100"`
	DataDir  string `env:"DATA_DIR" envDefault:"/data"` // Directory for SQLite files

	// DX Cluster(s) Configuration
	// CLUSTERS env var, if present, overrides individual DXHOST, DXPORT, etc.
	// Example: CLUSTERS='[{"host":"dxfun.com","port":"8000","call":"MYCALL","loginPrompt":"login:","cluster":"DXCluster"},{"host":"cluster.sota.org.uk","port":"7300","call":"MYCALL","loginPrompt":"login:","cluster":"SOTACluster"}]'
	RawClustersJSON string          `env:"CLUSTERS"`
	Clusters        []ClusterConfig `env:"-"` // Will be populated from RawClustersJSON or individual vars

	// Individual DX Cluster (legacy/simple config, overridden by CLUSTERS JSON)
	DXCHost     string `env:"DXHOST" envDefault:"127.0.0.1"`
	DXCPort     string `env:"DXPORT" envDefault:"7300"`
	DXCCallsign string `env:"DXCALL"`
	DXCPassword string `env:"DXPASSWORD"`

	// POTA Integration
	EnablePOTA       bool          `env:"POTA_INTEGRATION" envDefault:"false"`
	POTAPollInterval time.Duration `env:"POTA_POLLING_INTERVAL" envDefault:"120s"`

	// DXCC Lookup Configuration
	DXCCUpdateInterval time.Duration `env:"DXCC_UPDATE_INTERVAL" envDefault:"168h"` // Weekly

	// LoTW Configuration
	LoTWUpdateInterval time.Duration `env:"LOTW_UPDATE_INTERVAL" envDefault:"168h"` // Weekly

	Redis RedisConfig
	// ClubLog API key (allows overriding via env)
	ClubLogAPIKey string `env:"CLUBLOG_API_KEY" envDefault:"608df94896cb9c5421ae748235492b43815610c9"`
	// Optional (test) overrides for DXCC source URLs. These are usually the package-level
	// defaults in production, but tests create Config literals expecting to override them.
	ClubLogAPIURL     string
	FallbackGitHubURL string
	// Default channel buffer for DX cluster clients (overridable per-cluster config)
	DXCChannelBuffer int `env:"DXC_CHANNEL_BUFFER" envDefault:"8"`
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %w", err)
	}

	// Parse Redis-specific options
	if err := env.Parse(&cfg.Redis); err != nil {
		return nil, fmt.Errorf("failed to parse Redis environment variables: %w", err)
	}

	// --- DX Cluster Configuration Logic ---
	// If CLUSTERS JSON is provided, use that. Otherwise, use individual DXC_ vars.
	if cfg.RawClustersJSON != "" {
		if err := json.Unmarshal([]byte(cfg.RawClustersJSON), &cfg.Clusters); err != nil {
			return nil, fmt.Errorf("failed to parse CLUSTERS JSON: %w", err)
		}
	} else {
		// Fallback to individual DXHOST/DXPORT/DXCALL/DXPASSWORD
		// Only add if CallSign is present, otherwise it's likely an incomplete config.
		if cfg.DXCCallsign != "" {
			cfg.Clusters = []ClusterConfig{
				{
					Host:        cfg.DXCHost,
					Port:        cfg.DXCPort,
					Callsign:    cfg.DXCCallsign,
					Password:    cfg.DXCPassword,
					LoginPrompt: "login:", // Default as seen in Node.js
					ClusterName: "DXCluster",
				},
			}
		}
	}

	// Ensure POTA poll interval is not below minimum
	if cfg.POTAPollInterval < MinPOTAPollInterval {
		cfg.POTAPollInterval = MinPOTAPollInterval
	}

	// Ensure DataDir exists (it's essential for SQLite)
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory %s: %w", cfg.DataDir, err)
	}

	return cfg, nil
}
