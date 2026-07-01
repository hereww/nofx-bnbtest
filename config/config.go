package config

import (
	"fmt"
	"nofx/mcp"
	"nofx/telemetry"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	insecureDefaultJWTSecret = "default-jwt-secret-change-in-production"
	minJWTSecretLength       = 32
)

// Global configuration instance
var global *Config

// Config is the global configuration (loaded from .env)
// Only contains truly global config, trading related config is at trader/strategy level
type Config struct {
	// Service configuration
	APIServerPort int
	JWTSecret     string

	// Database configuration
	DBType     string // sqlite or postgres
	DBPath     string // SQLite database file path
	DBHost     string // PostgreSQL host
	DBPort     int    // PostgreSQL port
	DBUser     string // PostgreSQL user
	DBPassword string // PostgreSQL password
	DBName     string // PostgreSQL database name
	DBSSLMode  string // PostgreSQL SSL mode

	// Security configuration
	// TransportEncryption enables browser-side encryption for API keys
	// Requires HTTPS or localhost. Set to false for HTTP access via IP.
	TransportEncryption bool

	// Experience improvement (anonymous usage statistics)
	// Helps us understand product usage and improve the experience
	// Set EXPERIENCE_IMPROVEMENT=false to disable
	ExperienceImprovement bool

	// Market data provider API keys
	AlpacaAPIKey               string        // Alpaca API key for US stocks
	AlpacaSecretKey            string        // Alpaca secret key
	TwelveDataKey              string        // TwelveData API key for forex & metals
	DataGatewayURL             string        // Self-hosted NofxOS-compatible data gateway
	DataGatewayToken           string        // Optional token for the self-hosted data gateway
	DataGatewayEmbedded        bool          // Run the NofxOS-compatible data gateway inside the NOFX backend
	DataGatewayDBPath          string        // SQLite database file for the embedded data gateway
	DataGatewayRefreshInterval time.Duration // Refresh interval for the embedded data gateway
	MarketHTTPProxy            string        // Optional explicit proxy for market data APIs
}

// MustInit initializes global configuration or panics. Use it from main so the
// server refuses to start under a known or weak JWT signing secret.
func MustInit() {
	if err := initConfig(true); err != nil {
		panic(fmt.Sprintf("config: %v", err))
	}
}

// Init initializes global configuration (from .env). It preserves historical
// fail-soft behavior for tests and local tools; main uses MustInit.
func Init() {
	if err := initConfig(false); err != nil {
		fmt.Fprintf(os.Stderr, "config init failed: %v\n", err)
	}
}

func initConfig(strictJWT bool) error {
	cfg := &Config{
		APIServerPort:              8080,
		ExperienceImprovement:      true, // Default: enabled to help improve the product
		DataGatewayEmbedded:        true,
		DataGatewayDBPath:          "data/data-gateway.db",
		DataGatewayRefreshInterval: time.Minute,
		// Database defaults
		DBType:    "sqlite",
		DBPath:    "data/data.db",
		DBHost:    "localhost",
		DBPort:    5432,
		DBUser:    "postgres",
		DBName:    "nofx",
		DBSSLMode: "disable",
	}

	// Load from environment variables
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.JWTSecret = strings.TrimSpace(v)
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = insecureDefaultJWTSecret
	}
	if strictJWT {
		if cfg.JWTSecret == insecureDefaultJWTSecret {
			return fmt.Errorf("JWT_SECRET is required and must not use the insecure default")
		}
		if len(cfg.JWTSecret) < minJWTSecretLength {
			return fmt.Errorf("JWT_SECRET must be at least %d bytes", minJWTSecretLength)
		}
	}

	if v := os.Getenv("API_SERVER_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.APIServerPort = port
		}
	}

	// Transport encryption: default false for easier deployment
	// Set TRANSPORT_ENCRYPTION=true to enable (requires HTTPS or localhost)
	if v := os.Getenv("TRANSPORT_ENCRYPTION"); v != "" {
		cfg.TransportEncryption = strings.ToLower(v) == "true"
	}

	// Experience improvement: anonymous usage statistics
	// Default enabled, set EXPERIENCE_IMPROVEMENT=false to disable
	if v := os.Getenv("EXPERIENCE_IMPROVEMENT"); v != "" {
		cfg.ExperienceImprovement = strings.ToLower(v) != "false"
	}

	// Market data provider API keys
	cfg.AlpacaAPIKey = os.Getenv("ALPACA_API_KEY")
	cfg.AlpacaSecretKey = os.Getenv("ALPACA_SECRET_KEY")
	cfg.TwelveDataKey = os.Getenv("TWELVEDATA_API_KEY")
	cfg.DataGatewayURL = strings.TrimRight(os.Getenv("DATA_GATEWAY_URL"), "/")
	if cfg.DataGatewayURL == "" {
		cfg.DataGatewayURL = "http://127.0.0.1:8090"
	}
	cfg.DataGatewayToken = strings.TrimSpace(os.Getenv("DATA_GATEWAY_TOKEN"))
	if v := os.Getenv("DATA_GATEWAY_EMBEDDED"); v != "" {
		cfg.DataGatewayEmbedded = strings.ToLower(strings.TrimSpace(v)) != "false"
	}
	if v := strings.TrimSpace(os.Getenv("DATA_GATEWAY_DB_PATH")); v != "" {
		cfg.DataGatewayDBPath = v
	}
	if v := strings.TrimSpace(os.Getenv("DATA_GATEWAY_REFRESH_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.DataGatewayRefreshInterval = d
		}
	}
	cfg.MarketHTTPProxy = strings.TrimSpace(os.Getenv("MARKET_HTTP_PROXY"))

	// Database configuration
	if v := os.Getenv("DB_TYPE"); v != "" {
		cfg.DBType = strings.ToLower(v)
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("DB_HOST"); v != "" {
		cfg.DBHost = v
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil && port > 0 {
			cfg.DBPort = port
		}
	}
	if v := os.Getenv("DB_USER"); v != "" {
		cfg.DBUser = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		cfg.DBPassword = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		cfg.DBName = v
	}
	if v := os.Getenv("DB_SSLMODE"); v != "" {
		cfg.DBSSLMode = v
	}

	global = cfg

	// Initialize experience improvement (installation ID will be set after database init)
	telemetry.Init(cfg.ExperienceImprovement, "")

	// Set up AI token usage tracking callback
	mcp.TokenUsageCallback = func(usage mcp.TokenUsage) {
		telemetry.TrackAIUsage(telemetry.AIUsageEvent{
			ModelProvider: usage.Provider,
			ModelName:     usage.Model,
			Channel:       usage.Channel(),
			InputTokens:   usage.PromptTokens,
			OutputTokens:  usage.CompletionTokens,
		})
	}
	return nil
}

// Get returns the global configuration
func Get() *Config {
	if global == nil {
		Init()
	}
	return global
}
