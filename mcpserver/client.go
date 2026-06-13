package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxAPIResponseBytes = 8 << 20

type APIClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("nofx api status %d", e.StatusCode)
	}
	if len(body) > 500 {
		body = body[:500] + "..."
	}
	return fmt.Sprintf("nofx api status %d: %s", e.StatusCode, body)
}

func NewAPIClient(baseURL, token string, httpClient *http.Client) *APIClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &APIClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      strings.TrimSpace(token),
		httpClient: httpClient,
	}
}

func (c *APIClient) Get(ctx context.Context, path string, query map[string]string, timeout time.Duration) (any, error) {
	return c.call(ctx, http.MethodGet, path, query, nil, timeout)
}

func (c *APIClient) Post(ctx context.Context, path string, body any, timeout time.Duration) (any, error) {
	return c.call(ctx, http.MethodPost, path, nil, body, timeout)
}

func (c *APIClient) call(ctx context.Context, method, path string, query map[string]string, body any, timeout time.Duration) (any, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	u, err := url.Parse(c.baseURL + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("build nofx url: %w", err)
	}
	q := u.Query()
	for key, value := range query {
		if strings.TrimSpace(value) != "" {
			q.Set(key, value)
		}
	}
	u.RawQuery = q.Encode()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal nofx request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(callCtx, method, u.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("build nofx request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call nofx api: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read nofx response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: redactBody(raw)}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return string(raw), nil
	}
	return redactSensitive(decoded), nil
}

func redactBody(raw []byte) string {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return string(raw)
	}
	redacted, err := json.Marshal(redactSensitive(decoded))
	if err != nil {
		return string(raw)
	}
	return string(redacted)
}
