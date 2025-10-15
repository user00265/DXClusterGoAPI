package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
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

	// Default LoTW URL - can be overridden via environment variable
	LoTWActivityURL = "https://lotw.arrl.org/lotw-user-activity.csv"
	POTAAPIEndpoint = "https://api.pota.app/spot/activator"
)

// FlexiblePort is a type that can unmarshal both string and integer port values from JSON
type FlexiblePort string

// UnmarshalJSON implements custom JSON unmarshaling for FlexiblePort
func (fp *FlexiblePort) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*fp = FlexiblePort(s)
		return nil
	}

	// Try to unmarshal as integer
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*fp = FlexiblePort(strconv.Itoa(i))
		return nil
	}

	return fmt.Errorf("port must be either a string or integer")
}

// String returns the port as a string
func (fp FlexiblePort) String() string {
	return string(fp)
}

// ClusterConfig holds connection details for a single DX cluster.
type ClusterConfig struct {
	Host          string       `json:"host"`          // Required: hostname or IP
	Port          FlexiblePort `json:"port"`          // Required: port number (string or int)
	Callsign      string       `json:"call"`          // Optional: callsign (uses global CALLSIGN if not set)
	LoginPrompt   string       `json:"loginPrompt"`   // Optional: login prompt to expect (default "login:")
	SOTA          bool         `json:"sota"`          // Optional: is this a SOTA cluster? (default false)
	ChannelBuffer int          `json:"channelBuffer"` // Optional: channel buffer size (default 32)
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
	SpotExpiry         time.Duration `env:"REDIS_SPOT_EXPIRY" envDefault:"600s"` // Default 10 minutes
}

// Config holds all application configuration.
type Config struct {
	WebPort  int    `env:"WEBPORT" envDefault:"8192"`
	BaseURL  string `env:"WEBURL" envDefault:"/"`
	MaxCache int    `env:"MAXCACHE" envDefault:"100"`
	DataDir  string `env:"DATA_DIR" envDefault:"/data"` // Directory for SQLite files

	// DX Cluster(s) Configuration
	// CLUSTERS env var contains JSON array of cluster configs
	// Example: CLUSTERS='[{"host":"dx.n9jr.com","port":"7300","call":"MYCALL"},{"host":"cluster.sota.org.uk","port":7300,"sota":true}]'
	// Note: Port can be either string ("7300") or integer (7300)
	RawClustersJSON string          `env:"CLUSTERS"`
	Clusters        []ClusterConfig `env:"-"` // Will be populated from RawClustersJSON

	// Global CALLSIGN - used for all clusters that don't specify their own
	Callsign string `env:"CALLSIGN"`

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
	DXCChannelBuffer int `env:"DXC_CHANNEL_BUFFER" envDefault:"32"`
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
	// Only use CLUSTERS JSON - no more legacy support
	if cfg.RawClustersJSON != "" {
		// Use CLUSTERS JSON if provided
		if err := json.Unmarshal([]byte(cfg.RawClustersJSON), &cfg.Clusters); err != nil {
			return nil, fmt.Errorf("failed to parse CLUSTERS JSON: %w", err)
		}
	}

	// Apply global callsign to clusters that don't have one
	globalCallsign := cfg.Callsign
	for i := range cfg.Clusters {
		if cfg.Clusters[i].Callsign == "" {
			cfg.Clusters[i].Callsign = globalCallsign
		}
		// Set default login prompt if not specified
		if cfg.Clusters[i].LoginPrompt == "" {
			cfg.Clusters[i].LoginPrompt = "login:"
		}
		// Set default channel buffer if not specified
		if cfg.Clusters[i].ChannelBuffer <= 0 {
			cfg.Clusters[i].ChannelBuffer = cfg.DXCChannelBuffer
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
