// Package nofxos provides access to the NofxOS-compatible local data gateway
// for quantitative trading data including AI500 scores, OI rankings,
// fund flow (NetFlow), price rankings, and coin details.
package nofxos

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"nofx/security"
	"os"
	"strings"
	"sync"
	"time"
)

// Default configuration
const (
	DefaultBaseURL = "http://127.0.0.1:8090"
	DefaultTimeout = 30 * time.Second
	DefaultAuthKey = ""
)

// Client is the NofxOS API client
type Client struct {
	BaseURL string
	AuthKey string
	Timeout time.Duration
	mu      sync.RWMutex
}

var (
	defaultClient          *Client
	clientOnce             sync.Once
	embeddedGatewayHandler http.Handler
	embeddedGatewayMu      sync.RWMutex
)

// SetEmbeddedGatewayHandler routes NofxOS-compatible requests directly to an
// in-process data-gateway handler. Passing nil restores normal HTTP transport.
func SetEmbeddedGatewayHandler(handler http.Handler) {
	embeddedGatewayMu.Lock()
	defer embeddedGatewayMu.Unlock()
	embeddedGatewayHandler = handler
}

// DefaultClient returns the singleton default client
func DefaultClient() *Client {
	clientOnce.Do(func() {
		defaultClient = &Client{
			BaseURL: DefaultBaseURL,
			AuthKey: DefaultAuthKey,
			Timeout: DefaultTimeout,
		}
	})
	return defaultClient
}

// NewClient creates a new NofxOS API client
func NewClient(baseURL, authKey string) *Client {
	if baseURL == "" {
		baseURL = os.Getenv("DATA_GATEWAY_URL")
		if baseURL == "" {
			baseURL = DefaultBaseURL
		}
	}
	if authKey == "" {
		authKey = os.Getenv("DATA_GATEWAY_TOKEN")
		if authKey == "" {
			authKey = DefaultAuthKey
		}
	}
	return &Client{
		BaseURL: baseURL,
		AuthKey: authKey,
		Timeout: DefaultTimeout,
	}
}

// SetConfig updates client configuration
func (c *Client) SetConfig(baseURL, authKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if baseURL != "" {
		c.BaseURL = baseURL
	}
	if authKey != "" {
		c.AuthKey = authKey
	}
}

// GetBaseURL returns the current base URL
func (c *Client) GetBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BaseURL
}

// GetAuthKey returns the current auth key
func (c *Client) GetAuthKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.AuthKey
}

// doRequest performs an HTTP GET request with optional authentication.
func (c *Client) doRequest(endpoint string) ([]byte, error) {
	c.mu.RLock()
	baseURL := c.BaseURL
	authKey := c.AuthKey
	timeout := c.Timeout
	c.mu.RUnlock()

	if body, handled, err := doEmbeddedRequest(endpoint, authKey); handled {
		return body, err
	}

	target := baseURL + endpoint
	if err := validateGatewayURL(target); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(authKey) != "" {
		req.Header.Set("X-Gateway-Token", strings.TrimSpace(authKey))
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return body, &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	return body, nil
}

func doEmbeddedRequest(endpoint, authKey string) ([]byte, bool, error) {
	embeddedGatewayMu.RLock()
	handler := embeddedGatewayHandler
	embeddedGatewayMu.RUnlock()
	if handler == nil {
		return nil, false, nil
	}
	if endpoint == "" {
		endpoint = "/"
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}

	req := httptest.NewRequest(http.MethodGet, endpoint, nil)
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(authKey) != "" {
		req.Header.Set("X-Gateway-Token", strings.TrimSpace(authKey))
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	body := rec.Body.Bytes()
	if rec.Code != http.StatusOK {
		return body, true, &APIError{
			StatusCode: rec.Code,
			Message:    string(body),
		}
	}
	return body, true, nil
}

// failureMessage extracts the API-provided error message from a NofxOS response.
func failureMessage(body []byte) string {
	var payload struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return strings.TrimSpace(string(body))
	}
	if payload.Error != "" {
		return payload.Error
	}
	if payload.Message != "" {
		return payload.Message
	}
	if payload.Code != 0 {
		return fmt.Sprintf("API returned error code: %d", payload.Code)
	}
	return strings.TrimSpace(string(body))
}

// APIError represents an API error response
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return e.Message
}

// ExtractAuthKey extracts auth key from a URL string
func ExtractAuthKey(url string) string {
	parsed, err := urlpkgParse(url)
	if err == nil {
		return parsed.Query().Get("auth")
	}
	return ""
}

var urlpkgParse = url.Parse

func validateGatewayURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return security.ValidateURL(rawURL)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported data gateway scheme: %s", scheme)
	}
	path := parsed.EscapedPath()
	if path != "/health" && !strings.HasPrefix(path, "/api/") {
		return fmt.Errorf("unsupported data gateway path: %s", parsed.Path)
	}
	if isTrustedGatewayHost(parsed.Hostname()) {
		return nil
	}
	return security.ValidateURL(rawURL)
}

func isTrustedGatewayHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if host == "" {
		return false
	}
	switch host {
	case "localhost", "127.0.0.1", "::1", "nofx-data-gateway":
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
