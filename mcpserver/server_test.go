package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMCPInitializeAndToolsList(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream should not be called for initialize/tools/list")
	}))
	defer upstream.Close()

	rec, _ := callMCP(t, newTestMCPServer(upstream.URL), "test-mcp-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var initResp map[string]any
	decodeJSON(t, rec.Body.Bytes(), &initResp)
	result := initResp["result"].(map[string]any)
	if result["protocolVersion"] != protocolVersion {
		t.Fatalf("protocolVersion = %v", result["protocolVersion"])
	}
	capabilities := result["capabilities"].(map[string]any)
	if _, ok := capabilities["tools"]; !ok {
		t.Fatalf("initialize capabilities missing tools: %#v", capabilities)
	}

	rec, _ = callMCP(t, newTestMCPServer(upstream.URL), "test-mcp-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
	})
	var listResp map[string]any
	decodeJSON(t, rec.Body.Bytes(), &listResp)
	listResult := listResp["result"].(map[string]any)
	tools := listResult["tools"].([]any)
	if len(tools) != 20 {
		t.Fatalf("tool count = %d, want 20", len(tools))
	}
	if !toolListContains(tools, "nofx_strategy_test_run") || !toolListContains(tools, "nofx_get_custom_tokens") {
		t.Fatalf("expected core tools in list: %#v", tools)
	}
	if toolListContains(tools, "nofx_analyze_bsc_token") || toolListContains(tools, "nofx_get_bsc_wallet_graph") {
		t.Fatalf("on-chain analysis tools must not be exposed: %#v", tools)
	}
	if toolListContains(tools, "execute_trade") || toolListContains(tools, "nofx_start_trader") {
		t.Fatalf("high-risk tools must not be exposed: %#v", tools)
	}
}

func TestMCPUnauthorized(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()
	server := newTestMCPServer(upstream.URL)

	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMCPUnknownToolRejected(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream should not be called for unknown tool")
	}))
	defer upstream.Close()

	rec, _ := callMCP(t, newTestMCPServer(upstream.URL), "test-mcp-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "nofx_start_trader",
			"arguments": map[string]any{"trader_id": "t1"},
		},
	})
	var resp map[string]any
	decodeJSON(t, rec.Body.Bytes(), &resp)
	errObj := resp["error"].(map[string]any)
	if !strings.Contains(errObj["message"].(string), "not allowed") {
		t.Fatalf("error = %#v", errObj)
	}
}

func TestToolCallsForwardToNOFXAPI(t *testing.T) {
	type seenRequest struct {
		Method string
		Path   string
		Auth   string
		Query  map[string]string
		Body   map[string]any
	}
	var seen []seenRequest

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		item := seenRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Auth:   r.Header.Get("Authorization"),
			Query:  map[string]string{},
		}
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				item.Query[key] = values[0]
			}
		}
		if r.Body != nil && r.Header.Get("Content-Type") == "application/json" {
			_ = json.NewDecoder(r.Body).Decode(&item.Body)
		}
		seen = append(seen, item)

		switch r.URL.Path {
		case "/api/my-traders":
			writeJSON(w, http.StatusOK, []map[string]any{{"trader_id": "t1", "trader_name": "bot"}})
		case "/api/positions":
			writeJSON(w, http.StatusOK, []map[string]any{{"symbol": "BTCUSDT", "api_key": "secret-value"}})
		case "/api/strategies/test-run":
			writeJSON(w, http.StatusOK, map[string]any{"note": "simulation", "private_key": "hidden"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	server := newTestMCPServer(upstream.URL)
	_ = callTool(t, server, "nofx_list_traders", map[string]any{})
	positions := callTool(t, server, "nofx_get_positions", map[string]any{"trader_id": "t1"})
	testRun := callTool(t, server, "nofx_strategy_test_run", map[string]any{
		"config":         map[string]any{"coin_source": map[string]any{"source_type": "static"}},
		"prompt_variant": "balanced",
		"run_real_ai":    false,
	})

	if len(seen) != 3 {
		t.Fatalf("seen requests = %d, want 3", len(seen))
	}
	if seen[0].Path != "/api/my-traders" || seen[0].Auth != "Bearer test-nofx-token" {
		t.Fatalf("unexpected list request: %#v", seen[0])
	}
	if seen[1].Path != "/api/positions" || seen[1].Query["trader_id"] != "t1" {
		t.Fatalf("unexpected positions request: %#v", seen[1])
	}
	if strings.Contains(positions, "secret-value") || !strings.Contains(positions, redactedValue) {
		t.Fatalf("positions response was not redacted: %s", positions)
	}
	if seen[2].Path != "/api/strategies/test-run" || seen[2].Method != http.MethodPost {
		t.Fatalf("unexpected strategy test-run request: %#v", seen[2])
	}
	if seen[2].Body["run_real_ai"] != false {
		t.Fatalf("run_real_ai body = %#v", seen[2].Body)
	}
	if strings.Contains(testRun, "hidden") || !strings.Contains(testRun, redactedValue) {
		t.Fatalf("test-run response was not redacted: %s", testRun)
	}
}

func TestAPIClientNon2xxBecomesAPIErrorAndRedacts(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "bad", "token": "must-hide"})
	}))
	defer upstream.Close()

	client := NewAPIClient(upstream.URL, "jwt", upstream.Client())
	_, err := client.Get(testContext(t), "/api/protected", nil, time.Second)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
	if strings.Contains(apiErr.Body, "must-hide") || !strings.Contains(apiErr.Body, redactedValue) {
		t.Fatalf("body was not redacted: %s", apiErr.Body)
	}
}

func TestRedactSensitiveNestedFields(t *testing.T) {
	input := map[string]any{
		"apiKey": "a",
		"nested": map[string]any{
			"secret_key": "b",
			"items": []any{
				map[string]any{"privateKey": "c", "safe": "ok"},
			},
		},
	}
	out := redactSensitive(input).(map[string]any)
	if out["apiKey"] != redactedValue {
		t.Fatalf("apiKey not redacted: %#v", out)
	}
	nested := out["nested"].(map[string]any)
	if nested["secret_key"] != redactedValue {
		t.Fatalf("secret_key not redacted: %#v", nested)
	}
	item := nested["items"].([]any)[0].(map[string]any)
	if item["privateKey"] != redactedValue || item["safe"] != "ok" {
		t.Fatalf("nested item redaction failed: %#v", item)
	}
}

func newTestMCPServer(baseURL string) *Server {
	cfg := Config{
		Addr:              "127.0.0.1:0",
		BaseURL:           baseURL,
		NOFXAPIToken:      "test-nofx-token",
		MCPToken:          "test-mcp-token",
		DefaultTimeout:    time.Second,
		LongActionTimeout: time.Second,
	}
	return NewServer(cfg, NewAPIClient(cfg.BaseURL, cfg.NOFXAPIToken, http.DefaultClient))
}

func callTool(t *testing.T, server *Server, name string, args map[string]any) string {
	t.Helper()
	rec, _ := callMCP(t, server, "test-mcp-token", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	decodeJSON(t, rec.Body.Bytes(), &resp)
	if resp["error"] != nil {
		t.Fatalf("tool call error: %#v", resp["error"])
	}
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	return content[0].(map[string]any)["text"].(string)
}

func callMCP(t *testing.T, server *Server, token string, payload map[string]any) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec, req
}

func decodeJSON(t *testing.T, raw []byte, out any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		t.Fatalf("decode json %s: %v", string(raw), err)
	}
}

func toolListContains(tools []any, name string) bool {
	for _, item := range tools {
		tool := item.(map[string]any)
		if tool["name"] == name {
			return true
		}
	}
	return false
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
