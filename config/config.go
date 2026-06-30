package config

import (
	"nofx/mcp"
	"nofx/telemetry"
	"os"
	"strconv"
	"strings"
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
	AlpacaAPIKey     string // Alpaca API key for US stocks
	AlpacaSecretKey  string // Alpaca secret key
	TwelveDataKey    string // TwelveData API key for forex & metals
	DataGatewayURL   string // Self-hosted NofxOS-compatible data gateway
	DataGatewayToken string // Optional token for the self-hosted data gateway
	MarketHTTPProxy  string // Optional explicit proxy for market/on-chain data APIs

	// On-chain token analysis
	OnchainIndexerEnabled                 bool
	OnchainIndexerURL                     string
	OnchainIndexerAddr                    string
	OnchainIndexerDBPath                  string
	OnchainBSCArchiveRPCURL               string
	OnchainArchiveHTTPProxy               string
	OnchainIndexerPollIntervalSeconds     int
	OnchainIndexerBatchBlocks             int
	OnchainFreeIndexerEnabled             bool
	OnchainFreeRPCURLs                    []string
	OnchainFreeBackfillMaxRequestsPerTick int
	OnchainFreeBackfillTargetSeeds        int
	OnchainFreeLogSource                  string
	OnchainGeckoTradesLimit               int
	OnchainEtherscanAPIKey                string
	OnchainEtherscanBaseURL               string
	OnchainFreeLogsRPS                    int
	OnchainFreeLogsDailyBudget            int
	OnchainLogPageSize                    int
	OnchainEarlyWindowBlocks              int64
}

// Init initializes global configuration (from .env)
func Init() {
	cfg := &Config{
		APIServerPort:         8080,
		ExperienceImprovement: true, // Default: enabled to help improve the product
		// Database defaults
		DBType:                                "sqlite",
		DBPath:                                "data/data.db",
		DBHost:                                "localhost",
		DBPort:                                5432,
		DBUser:                                "postgres",
		DBName:                                "nofx",
		DBSSLMode:                             "disable",
		OnchainIndexerPollIntervalSeconds:     15,
		OnchainIndexerBatchBlocks:             100,
		OnchainIndexerAddr:                    ":8091",
		OnchainIndexerDBPath:                  "data/onchain-indexer.db",
		OnchainFreeIndexerEnabled:             true,
		OnchainFreeRPCURLs:                    []string{"https://bsc-rpc.publicnode.com", "https://binance.llamarpc.com", "https://bsc-dataseed.binance.org", "https://bsc-dataseed1.binance.org", "https://bsc-dataseed2.binance.org"},
		OnchainFreeBackfillMaxRequestsPerTick: 200,
		OnchainFreeBackfillTargetSeeds:        100,
		OnchainFreeLogSource:                  "etherscan,gecko",
		OnchainGeckoTradesLimit:               300,
		OnchainEtherscanBaseURL:               "https://api.etherscan.io/v2/api",
		OnchainFreeLogsRPS:                    2,
		OnchainFreeLogsDailyBudget:            90000,
		OnchainLogPageSize:                    1000,
		OnchainEarlyWindowBlocks:              100000,
	}

	// Load from environment variables
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.JWTSecret = strings.TrimSpace(v)
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "default-jwt-secret-change-in-production"
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
	cfg.MarketHTTPProxy = strings.TrimSpace(os.Getenv("MARKET_HTTP_PROXY"))
	if v := os.Getenv("ONCHAIN_INDEXER_ENABLED"); v != "" {
		cfg.OnchainIndexerEnabled = strings.ToLower(strings.TrimSpace(v)) == "true"
	}
	cfg.OnchainIndexerURL = strings.TrimRight(strings.TrimSpace(os.Getenv("ONCHAIN_INDEXER_URL")), "/")
	if v := strings.TrimSpace(os.Getenv("ONCHAIN_INDEXER_ADDR")); v != "" {
		cfg.OnchainIndexerAddr = v
	}
	if v := strings.TrimSpace(os.Getenv("ONCHAIN_INDEXER_DB_PATH")); v != "" {
		cfg.OnchainIndexerDBPath = v
	}
	cfg.OnchainBSCArchiveRPCURL = strings.TrimSpace(os.Getenv("ONCHAIN_BSC_ARCHIVE_RPC_URL"))
	cfg.OnchainArchiveHTTPProxy = strings.TrimSpace(os.Getenv("ONCHAIN_ARCHIVE_HTTP_PROXY"))
	if v := os.Getenv("ONCHAIN_FREE_INDEXER_ENABLED"); v != "" {
		cfg.OnchainFreeIndexerEnabled = strings.ToLower(strings.TrimSpace(v)) == "true"
	}
	if v := strings.TrimSpace(os.Getenv("ONCHAIN_FREE_RPC_URLS")); v != "" {
		cfg.OnchainFreeRPCURLs = splitCSVEnv(v)
	}
	if v := os.Getenv("ONCHAIN_INDEXER_POLL_INTERVAL_SECONDS"); v != "" {
		if seconds, err := strconv.Atoi(v); err == nil && seconds > 0 {
			cfg.OnchainIndexerPollIntervalSeconds = seconds
		}
	}
	if v := os.Getenv("ONCHAIN_INDEXER_BATCH_BLOCKS"); v != "" {
		if blocks, err := strconv.Atoi(v); err == nil && blocks > 0 {
			cfg.OnchainIndexerBatchBlocks = blocks
		}
	}
	if v := os.Getenv("ONCHAIN_FREE_BACKFILL_MAX_REQUESTS_PER_TICK"); v != "" {
		if requests, err := strconv.Atoi(v); err == nil && requests > 0 {
			cfg.OnchainFreeBackfillMaxRequestsPerTick = requests
		}
	}
	if v := os.Getenv("ONCHAIN_FREE_BACKFILL_TARGET_SEEDS"); v != "" {
		if seeds, err := strconv.Atoi(v); err == nil && seeds > 0 {
			cfg.OnchainFreeBackfillTargetSeeds = seeds
		}
	}
	if v := strings.TrimSpace(os.Getenv("ONCHAIN_FREE_LOG_SOURCE")); v != "" {
		cfg.OnchainFreeLogSource = strings.ToLower(v)
	}
	if v := os.Getenv("ONCHAIN_GECKO_TRADES_LIMIT"); v != "" {
		if limit, err := strconv.Atoi(v); err == nil && limit > 0 {
			cfg.OnchainGeckoTradesLimit = limit
		}
	}
	cfg.OnchainEtherscanAPIKey = strings.TrimSpace(os.Getenv("ONCHAIN_ETHERSCAN_API_KEY"))
	if v := strings.TrimSpace(os.Getenv("ONCHAIN_ETHERSCAN_BASE_URL")); v != "" {
		cfg.OnchainEtherscanBaseURL = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("ONCHAIN_FREE_LOGS_RPS"); v != "" {
		if rps, err := strconv.Atoi(v); err == nil && rps > 0 {
			cfg.OnchainFreeLogsRPS = rps
		}
	}
	if v := os.Getenv("ONCHAIN_FREE_LOGS_DAILY_BUDGET"); v != "" {
		if budget, err := strconv.Atoi(v); err == nil && budget > 0 {
			cfg.OnchainFreeLogsDailyBudget = budget
		}
	}
	if v := os.Getenv("ONCHAIN_LOG_PAGE_SIZE"); v != "" {
		if pageSize, err := strconv.Atoi(v); err == nil && pageSize > 0 {
			cfg.OnchainLogPageSize = pageSize
		}
	}
	if v := os.Getenv("ONCHAIN_EARLY_WINDOW_BLOCKS"); v != "" {
		if blocks, err := strconv.ParseInt(v, 10, 64); err == nil && blocks > 0 {
			cfg.OnchainEarlyWindowBlocks = blocks
		}
	}

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
}

// Get returns the global configuration
func Get() *Config {
	if global == nil {
		Init()
	}
	return global
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func splitCSVEnv(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}
