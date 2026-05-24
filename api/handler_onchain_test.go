package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nofx/config"
	"nofx/store"
)

func TestOnchainTokenAnalysisRejectsInvalidAddress(t *testing.T) {
	config.Init()
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(nil, st, nil, 0)
	req := httptest.NewRequest(http.MethodGet, "/api/onchain/token-analysis?chain=bsc&address=not-an-address", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOnchainAIReportRequiresConfiguredModel(t *testing.T) {
	config.Init()
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	if err := st.User().EnsureAdmin(); err != nil {
		t.Fatalf("ensure admin: %v", err)
	}

	srv := NewServer(nil, st, nil, 0)
	body := `{
		"chain":"bsc",
		"address":"0x812fc5119b772c6c7a66249a559f3614623f4444",
		"depth":"recent",
		"language":"zh",
		"analysis":{
			"success":true,
			"chain":"bsc",
			"address":"0x812fc5119b772c6c7a66249a559f3614623f4444",
			"depth":"recent",
			"status":"ok",
			"completeness":"recent",
			"token":{"name":"Demo","symbol":"DEMO"}
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/onchain/ai-report", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "AI") && !strings.Contains(body, "模型") {
		t.Fatalf("expected model configuration error, got %s", body)
	}
}

func TestOnchainAIReportPreviewReturnsPrompts(t *testing.T) {
	config.Init()
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(nil, st, nil, 0)
	body := `{
		"chain":"bsc",
		"address":"0x812fc5119b772c6c7a66249a559f3614623f4444",
		"depth":"recent",
		"language":"zh",
		"agent":{
			"report_style":"deep",
			"risk_profile":"defensive",
			"focus":"重点看持仓集中度",
			"custom_prompt":"输出后续监控项"
		},
		"analysis":{
			"success":true,
			"chain":"bsc",
			"address":"0x812fc5119b772c6c7a66249a559f3614623f4444",
			"depth":"recent",
			"status":"ok",
			"completeness":"recent",
			"token":{"name":"Demo","symbol":"DEMO"},
			"risk_flags":["transfer_pausable"]
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/onchain/ai-report/preview", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	bodyText := rec.Body.String()
	for _, want := range []string{"system_prompt", "user_prompt", "分析 Agent 设置", "重点看持仓集中度"} {
		if !strings.Contains(bodyText, want) {
			t.Fatalf("expected %q in body, got %s", want, bodyText)
		}
	}
}

func TestOnchainTokenAnalysisFullWithoutArchiveRPC(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(nil, st, nil, 0)
	req := httptest.NewRequest(http.MethodGet, "/api/onchain/token-analysis?chain=bsc&address=0x812fc5119b772c6c7a66249a559f3614623f4444&depth=full", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"status":"archive_rpc_missing"`) {
		t.Fatalf("expected archive_rpc_missing body, got %s", body)
	}
}

func TestOnchainWalletGraphRejectsInvalidAddress(t *testing.T) {
	config.Init()
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(nil, st, nil, 0)
	req := httptest.NewRequest(http.MethodGet, "/api/onchain/wallet-graph?chain=bsc&address=not-an-address", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
