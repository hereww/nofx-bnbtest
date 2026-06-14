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

func TestOnchainEarlyWalletFlowRejectsInvalidAddress(t *testing.T) {
	config.Init()
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	srv := NewServer(nil, st, nil, 0)
	req := httptest.NewRequest(http.MethodGet, "/api/onchain/early-wallet-flow?chain=bsc&address=not-an-address", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	exportReq := httptest.NewRequest(http.MethodGet, "/api/onchain/early-wallet-flow/export?chain=bsc&address=not-an-address", nil)
	exportRec := httptest.NewRecorder()
	srv.router.ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusBadRequest {
		t.Fatalf("export status = %d body=%s", exportRec.Code, exportRec.Body.String())
	}
	if contentType := exportRec.Header().Get("Content-Type"); strings.Contains(contentType, "text/csv") {
		t.Fatalf("invalid export returned csv content type: %s", contentType)
	}
}

func TestOnchainEarlyWalletFlowAPIAndExport(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "http://archive.example"
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	chain := "bsc"
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	pool := "0x1111111111111111111111111111111111111111"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	seed := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	child := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{Chain: chain, TokenAddress: token, PoolAddress: pool, Token0: token, Token1: quote, Token0Decimals: 18, Token1Decimals: 18}}); err != nil {
		t.Fatalf("upsert pools: %v", err)
	}
	if err := st.Onchain().InsertSwaps([]store.OnchainSwap{
		{Chain: chain, TokenAddress: token, PoolAddress: pool, TxHash: "0xbuy", LogIndex: 0, BlockNumber: 1, BlockTime: 1000, EventType: "swap", TraderAddress: seed, Side: "buy", TokenAmount: 1000, QuoteAmount: 100, QuoteToken: quote},
		{Chain: chain, TokenAddress: token, PoolAddress: pool, TxHash: "0xsell", LogIndex: 0, BlockNumber: 3, BlockTime: 3000, EventType: "swap", TraderAddress: child, Side: "sell", TokenAmount: 100, QuoteAmount: 20, QuoteToken: quote},
	}); err != nil {
		t.Fatalf("insert swaps: %v", err)
	}
	if err := st.Onchain().InsertTransfers([]store.OnchainTokenTransfer{{Chain: chain, TokenAddress: token, TxHash: "0xt", LogIndex: 0, BlockNumber: 2, BlockTime: 2000, FromAddress: seed, ToAddress: child, Amount: 200}}); err != nil {
		t.Fatalf("insert transfers: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{Chain: chain, TokenAddress: token, Status: store.OnchainJobStatusCompleted, StartBlock: 1, EndBlock: 3, LastBlock: 3}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	srv := NewServer(nil, st, nil, 0)
	req := httptest.NewRequest(http.MethodGet, "/api/onchain/early-wallet-flow?chain=bsc&address="+token, nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"summary"`, `"seeds"`, `"wallets"`, `"edges"`, `"realized_pnl"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in body: %s", want, body)
		}
	}

	exportReq := httptest.NewRequest(http.MethodGet, "/api/onchain/early-wallet-flow/export?chain=bsc&address="+token, nil)
	exportRec := httptest.NewRecorder()
	srv.router.ServeHTTP(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", exportRec.Code, exportRec.Body.String())
	}
	csvBody := exportRec.Body.String()
	for _, want := range []string{"seed_rank,address", "depth,root_address", "realized_pnl"} {
		if !strings.Contains(csvBody, want) {
			t.Fatalf("expected csv %q in body: %s", want, csvBody)
		}
	}
}
