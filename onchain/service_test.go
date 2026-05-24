package onchain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"nofx/config"
	"nofx/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestAnalyzeTokenFullWithoutArchiveRPCReturnsExplicitStatus(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st)
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{"pairs": []any{}})
	})}

	resp, err := svc.AnalyzeToken(context.Background(), TokenAnalysisRequest{
		Chain:   "bsc",
		Address: "0x812fc5119b772c6c7a66249a559f3614623f4444",
		Depth:   "full",
	})
	if err != nil {
		t.Fatalf("AnalyzeToken returned error: %v", err)
	}
	if resp.Status != StatusArchiveRPCMissing {
		t.Fatalf("status = %q, want %q", resp.Status, StatusArchiveRPCMissing)
	}
	if resp.Full != nil {
		t.Fatalf("full analysis should not be fabricated without archive RPC")
	}
}

func TestNewServiceUsesOnchainHTTPProxy(t *testing.T) {
	t.Setenv("ONCHAIN_HTTP_PROXY", "http://user:pass@proxy.example:10811")
	config.Init()

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st)
	transport, ok := svc.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", svc.httpClient.Transport)
	}
	req := mustRequest(t, "https://api.dexscreener.com/token-pairs/v1/bsc/0x812fc5119b772c6c7a66249a559f3614623f4444")
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy returned error: %v", err)
	}
	if proxyURL == nil || proxyURL.Host != "proxy.example:10811" {
		t.Fatalf("proxy URL = %v", proxyURL)
	}
}

func TestAnalyzeTokenRecentAggregatesMockedSources(t *testing.T) {
	config.Init()

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	tokenAddress := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	poolAddress := "0x1111111111111111111111111111111111111111"
	buyer := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	seller := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	svc := NewService(st)
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "api.dexscreener.com":
			return jsonResponse(http.StatusOK, map[string]any{
				"pairs": []any{
					map[string]any{
						"chainId":       "bsc",
						"dexId":         "pancakeswap",
						"url":           "https://dexscreener.com/bsc/" + poolAddress,
						"pairAddress":   poolAddress,
						"pairCreatedAt": float64(1700000000000),
						"baseToken": map[string]any{
							"address": tokenAddress,
							"name":    "Fire Phoenix",
							"symbol":  "FPHX",
						},
						"quoteToken": map[string]any{
							"address": "0x55d398326f99059ff775485246999027b3197955",
							"name":    "Tether USD",
							"symbol":  "USDT",
						},
						"priceUsd": "0.00000123",
						"liquidity": map[string]any{
							"usd": float64(12000),
						},
						"volume": map[string]any{
							"h24": float64(3400),
						},
					},
				},
			})
		case "api.gopluslabs.io":
			return jsonResponse(http.StatusOK, map[string]any{
				"code":    1,
				"message": "OK",
				"result": map[string]any{
					tokenAddress: map[string]any{
						"token_name":          "Fire Phoenix",
						"token_symbol":        "FPHX",
						"total_supply":        "1000000000",
						"holder_count":        "123",
						"is_open_source":      "1",
						"is_honeypot":         "0",
						"is_mintable":         "1",
						"transfer_pausable":   "0",
						"slippage_modifiable": "0",
						"buy_tax":             "0.01",
						"sell_tax":            "0.02",
						"owner_address":       "0x0000000000000000000000000000000000000000",
						"creator_address":     "0xcccccccccccccccccccccccccccccccccccccccc",
						"holders":             []any{map[string]any{"address": buyer, "balance": "1000", "percent": "0.1"}},
						"lp_holders":          []any{},
					},
				},
			})
		case "api.geckoterminal.com":
			return jsonResponse(http.StatusOK, map[string]any{
				"data": []any{
					map[string]any{"attributes": map[string]any{
						"block_number":       float64(100),
						"tx_hash":            "0xabc",
						"tx_from_address":    buyer,
						"from_token_amount":  "10",
						"to_token_amount":    "1000",
						"from_token_address": "0x55d398326f99059ff775485246999027b3197955",
						"to_token_address":   tokenAddress,
						"block_timestamp":    "2026-01-01T01:00:00Z",
						"kind":               "buy",
						"volume_in_usd":      "10",
					}},
					map[string]any{"attributes": map[string]any{
						"block_number":       float64(101),
						"tx_hash":            "0xdef",
						"tx_from_address":    seller,
						"from_token_amount":  "250",
						"to_token_amount":    "3",
						"from_token_address": tokenAddress,
						"to_token_address":   "0x55d398326f99059ff775485246999027b3197955",
						"block_timestamp":    "2026-01-01T02:00:00Z",
						"kind":               "sell",
						"volume_in_usd":      "3",
					}},
				},
			})
		default:
			t.Fatalf("unexpected upstream host: %s", req.URL.Host)
		}
		return nil, nil
	})}

	resp, err := svc.AnalyzeToken(context.Background(), TokenAnalysisRequest{
		Chain:   "bsc",
		Address: tokenAddress,
		Depth:   "recent",
	})
	if err != nil {
		t.Fatalf("AnalyzeToken returned error: %v", err)
	}
	if resp.Status != StatusOK {
		t.Fatalf("status = %q, want %q", resp.Status, StatusOK)
	}
	if resp.Token.Symbol != "FPHX" {
		t.Fatalf("symbol = %q, want FPHX", resp.Token.Symbol)
	}
	if resp.Recent == nil || resp.Recent.BuyCount != 1 || resp.Recent.SellCount != 1 {
		t.Fatalf("unexpected recent stats: %+v", resp.Recent)
	}
	if len(resp.RiskFlags) != 1 || resp.RiskFlags[0] != "mintable" {
		t.Fatalf("risk flags = %v, want [mintable]", resp.RiskFlags)
	}
	if resp.DealerFlow == nil {
		t.Fatalf("dealer flow should be present")
	}
	if resp.DealerFlow.Direction == "" {
		t.Fatalf("dealer flow direction should not be empty")
	}
}

func TestDealerFlowDirections(t *testing.T) {
	tests := []struct {
		name string
		resp *TokenAnalysisResponse
		want string
	}{
		{
			name: "accumulating",
			resp: &TokenAnalysisResponse{Success: true, Depth: DepthRecent, Recent: &RecentTradeAnalysis{
				TradeCount: 12, BuyCount: 10, SellCount: 2, UniqueBuyers: 8, UniqueSellers: 2,
				TopAccumulators: []WalletAnalysis{{Address: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BuyCount: 4, BuyAmount: 1000, NetBoughtAmount: 1000}},
				TopSellers:      []WalletAnalysis{{Address: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SellCount: 1, SellAmount: 100, NetBoughtAmount: -100}},
			}},
			want: DealerDirectionAccumulating,
		},
		{
			name: "distributing",
			resp: &TokenAnalysisResponse{Success: true, Depth: DepthRecent, Recent: &RecentTradeAnalysis{
				TradeCount: 12, BuyCount: 2, SellCount: 10, UniqueBuyers: 2, UniqueSellers: 8,
				TopAccumulators: []WalletAnalysis{{Address: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BuyCount: 1, BuyAmount: 100, NetBoughtAmount: 100}},
				TopSellers:      []WalletAnalysis{{Address: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SellCount: 4, SellAmount: 1000, NetBoughtAmount: -1000}},
			}},
			want: DealerDirectionDistributing,
		},
		{
			name: "mixed",
			resp: &TokenAnalysisResponse{Success: true, Depth: DepthRecent, Recent: &RecentTradeAnalysis{
				TradeCount: 20, BuyCount: 10, SellCount: 10, UniqueBuyers: 5, UniqueSellers: 5,
				TopAccumulators: []WalletAnalysis{{Address: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", BuyCount: 2, BuyAmount: 500, NetBoughtAmount: 500}},
				TopSellers:      []WalletAnalysis{{Address: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SellCount: 2, SellAmount: 500, NetBoughtAmount: -500}},
			}},
			want: DealerDirectionMixed,
		},
		{
			name: "insufficient",
			resp: &TokenAnalysisResponse{Success: true, Depth: DepthRecent, Recent: &RecentTradeAnalysis{TradeCount: 1, BuyCount: 1}},
			want: DealerDirectionInsufficientData,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDealerFlowAnalysis(tt.resp)
			if got == nil || got.Direction != tt.want {
				t.Fatalf("direction = %+v, want %s", got, tt.want)
			}
		})
	}
}

func TestWalletGraphFullIncludesTransferEdges(t *testing.T) {
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
	walletA := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	walletB := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	if err := st.Onchain().UpsertPools([]store.OnchainPool{{Chain: chain, TokenAddress: token, PoolAddress: pool, DexID: "pancakeswap", LiquidityUSD: 1000}}); err != nil {
		t.Fatalf("upsert pools: %v", err)
	}
	transfers := []store.OnchainTokenTransfer{
		{Chain: chain, TokenAddress: token, TxHash: "0x1", LogIndex: 0, BlockNumber: 1, BlockTime: 1000, FromAddress: pool, ToAddress: walletA, Amount: 1000},
		{Chain: chain, TokenAddress: token, TxHash: "0x2", LogIndex: 0, BlockNumber: 2, BlockTime: 2000, FromAddress: walletA, ToAddress: walletB, Amount: 250},
		{Chain: chain, TokenAddress: token, TxHash: "0x3", LogIndex: 0, BlockNumber: 3, BlockTime: 3000, FromAddress: walletB, ToAddress: pool, Amount: 100},
	}
	if err := st.Onchain().InsertTransfers(transfers); err != nil {
		t.Fatalf("insert transfers: %v", err)
	}
	if err := st.Onchain().RecomputeWalletSnapshots(chain, token); err != nil {
		t.Fatalf("recompute snapshots: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{Chain: chain, TokenAddress: token, Status: store.OnchainJobStatusCompleted, StartBlock: 1, EndBlock: 3, LastBlock: 3}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	svc := NewService(st)
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "api.dexscreener.com":
			return jsonResponse(http.StatusOK, map[string]any{"pairs": []any{map[string]any{
				"chainId": "bsc", "dexId": "pancakeswap", "url": "https://dexscreener.com/bsc/" + pool,
				"pairAddress": pool, "pairCreatedAt": float64(1700000000000),
				"baseToken":  map[string]any{"address": token, "name": "Demo", "symbol": "DEMO"},
				"quoteToken": map[string]any{"address": "0x55d398326f99059ff775485246999027b3197955", "name": "Tether", "symbol": "USDT"},
				"priceUsd":   "1", "liquidity": map[string]any{"usd": float64(1000)}, "volume": map[string]any{"h24": float64(100)},
			}}})
		case "api.gopluslabs.io":
			return jsonResponse(http.StatusOK, map[string]any{"code": 1, "result": map[string]any{token: map[string]any{"token_name": "Demo", "token_symbol": "DEMO", "holder_count": "2"}}})
		case "api.geckoterminal.com":
			return jsonResponse(http.StatusOK, map[string]any{"data": []any{}})
		default:
			t.Fatalf("unexpected host %s", req.URL.Host)
		}
		return nil, nil
	})}

	graph, err := svc.WalletGraph(context.Background(), WalletGraphRequest{Chain: chain, Address: token, Depth: DepthFull, Limit: 80})
	if err != nil {
		t.Fatalf("WalletGraph error: %v", err)
	}
	for _, edge := range graph.Edges {
		if edge.Relation == "transfer" && edge.TxHash == "0x2" {
			return
		}
	}
	t.Fatalf("expected transfer edge in graph: %+v", graph.Edges)
}

func jsonResponse(status int, body any) (*http.Response, error) {
	payload, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(payload))),
	}, nil
}

func mustRequest(t *testing.T, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return req
}
