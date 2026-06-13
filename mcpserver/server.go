package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const protocolVersion = "2025-06-18"

type Server struct {
	cfg   Config
	tools map[string]ToolSpec
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type toolListItem struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type toolCallResult struct {
	Content []toolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type toolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewServer(cfg Config, client *APIClient) *Server {
	return &Server{
		cfg:   cfg,
		tools: buildTools(client, cfg),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/mcp", s.handleMCP)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "server": "nofx-mcp"})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req rpcRequest
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "parse error"},
		})
		return
	}

	resp := s.dispatch(r.Context(), req)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) authorized(r *http.Request) bool {
	expected := strings.TrimSpace(s.cfg.MCPToken)
	if expected == "" {
		return false
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	return auth == "Bearer "+expected
}

func (s *Server) dispatch(ctx context.Context, req rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		resp.Error = &rpcError{Code: -32600, Message: "invalid request"}
		return resp
	}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "nofx-mcp", "version": "0.1.0"},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.listTools()}
	case "tools/call":
		result, err := s.callTool(ctx, req.Params)
		if err != nil {
			resp.Error = jsonRPCErrorFrom(err)
			return resp
		}
		resp.Result = result
	default:
		resp.Error = &rpcError{Code: -32601, Message: fmt.Sprintf("method %q not found", req.Method)}
	}
	return resp
}

func (s *Server) listTools() []toolListItem {
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]toolListItem, 0, len(names))
	for _, name := range names {
		tool := s.tools[name]
		out = append(out, toolListItem{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return out
}

func (s *Server) callTool(ctx context.Context, rawParams json.RawMessage) (toolCallResult, error) {
	var params toolCallParams
	if len(rawParams) > 0 {
		decoder := json.NewDecoder(strings.NewReader(string(rawParams)))
		decoder.UseNumber()
		if err := decoder.Decode(&params); err != nil {
			return toolCallResult{}, &toolError{code: -32602, message: "invalid tools/call params"}
		}
	}
	params.Name = strings.TrimSpace(params.Name)
	if params.Name == "" {
		return toolCallResult{}, &toolError{code: -32602, message: "tool name is required"}
	}
	tool, ok := s.tools[params.Name]
	if !ok {
		return toolCallResult{}, &toolError{code: -32602, message: fmt.Sprintf("tool %q is not allowed", params.Name)}
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	result, err := tool.Handler(ctx, params.Arguments)
	if err != nil {
		return toolCallResult{}, err
	}
	text, err := marshalToolText(result)
	if err != nil {
		return toolCallResult{}, err
	}
	return toolCallResult{
		Content: []toolContent{{Type: "text", Text: text}},
	}, nil
}

type toolError struct {
	code    int
	message string
	data    any
}

func (e *toolError) Error() string { return e.message }

func jsonRPCErrorFrom(err error) *rpcError {
	if e, ok := err.(*toolError); ok {
		return &rpcError{Code: e.code, Message: e.message, Data: e.data}
	}
	if apiErr, ok := err.(*APIError); ok {
		return &rpcError{
			Code:    -32000,
			Message: apiErr.Error(),
			Data:    map[string]any{"status_code": apiErr.StatusCode},
		}
	}
	return &rpcError{Code: -32000, Message: err.Error()}
}

func marshalToolText(result any) (string, error) {
	payload, err := json.MarshalIndent(redactSensitive(result), "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal tool result: %w", err)
	}
	return string(payload), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
