package mcpserver

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultAddr    = "0.0.0.0:8099"
	defaultBaseURL = "http://127.0.0.1:8080"
)

// Config controls the HTTP MCP server and its upstream NOFX API connection.
type Config struct {
	Addr              string
	BaseURL           string
	NOFXAPIToken      string
	MCPToken          string
	DefaultTimeout    time.Duration
	LongActionTimeout time.Duration
}

// ConfigFromEnv builds Config from environment variables.
func ConfigFromEnv() Config {
	return Config{
		Addr:              envOr("NOFX_MCP_ADDR", defaultAddr),
		BaseURL:           strings.TrimRight(envOr("NOFX_BASE_URL", defaultBaseURL), "/"),
		NOFXAPIToken:      strings.TrimSpace(os.Getenv("NOFX_API_TOKEN")),
		MCPToken:          strings.TrimSpace(os.Getenv("NOFX_MCP_TOKEN")),
		DefaultTimeout:    30 * time.Second,
		LongActionTimeout: 90 * time.Second,
	}
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.MCPToken) == "" {
		return fmt.Errorf("NOFX_MCP_TOKEN is required")
	}
	if strings.TrimSpace(c.NOFXAPIToken) == "" {
		return fmt.Errorf("NOFX_API_TOKEN is required")
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("NOFX_BASE_URL is required")
	}
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("NOFX_MCP_ADDR is required")
	}
	return nil
}

func envOr(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
