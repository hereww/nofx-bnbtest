package onchain

import (
	"context"
	"encoding/json"
	"errors"
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

func TestQueueIndexEarlyWindowDoesNotIndexToLatestBlock(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "http://archive.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	svc := NewService(st)
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "0x63",
		})
	})}

	resp, err := svc.QueueIndex(context.Background(), IndexTokenRequest{
		Chain:       "bsc",
		Address:     token,
		Scope:       store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource: store.OnchainIndexSourceArchiveRPC,
		StartBlock:  100,
	})
	if err != nil {
		t.Fatalf("QueueIndex error: %v", err)
	}
	if resp.EndBlock != 100+defaultEarlyWalletIndexWindowBlocks-1 {
		t.Fatalf("early window end block = %d, want %d", resp.EndBlock, 100+defaultEarlyWalletIndexWindowBlocks-1)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Scope != store.OnchainIndexScopeEarlyWalletWindow {
		t.Fatalf("scope = %q, want %q", job.Scope, store.OnchainIndexScopeEarlyWalletWindow)
	}
}

func TestQueueIndexEarlyWindowDefaultsToFreeLocalWithoutArchiveRPC(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeIndexerEnabled = true

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	svc := NewService(st)
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, []any{})
	})}

	resp, err := svc.QueueIndex(context.Background(), IndexTokenRequest{
		Chain:   "bsc",
		Address: token,
		Scope:   store.OnchainIndexScopeEarlyWalletWindow,
	})
	if err != nil {
		t.Fatalf("QueueIndex error: %v", err)
	}
	if !resp.Success || (resp.Status != store.OnchainJobStatusQueued && resp.Status != store.OnchainJobStatusCompleted && resp.Status != store.OnchainJobStatusIndexing) {
		t.Fatalf("response status/success = %s/%v body=%+v", resp.Status, resp.Success, resp)
	}
	if resp.IndexSource != store.OnchainIndexSourceFreeLocal {
		t.Fatalf("index source = %q, want free_local", resp.IndexSource)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job == nil || job.IndexSource != store.OnchainIndexSourceFreeLocal {
		t.Fatalf("job = %+v, want free_local job", job)
	}
	task, err := st.Onchain().GetLocalIndexTask("bsc", token)
	if err != nil {
		t.Fatalf("get local task: %v", err)
	}
	if task == nil || !task.Enabled {
		t.Fatalf("task = %+v, want enabled local task", task)
	}
}

func TestQueueFreeLocalEarlyIndexResetsOldCursor(t *testing.T) {
	config.Init()
	config.Get().OnchainFreeIndexerEnabled = true
	config.Get().OnchainIndexerBatchBlocks = 100

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		StartBlock:   123,
		EndBlock:     456,
		LastBlock:    104783077,
		BatchSize:    5,
	}); err != nil {
		t.Fatalf("upsert old job: %v", err)
	}

	svc := NewService(st)
	if _, err := svc.queueFreeLocalEarlyIndex(context.Background(), "bsc", token, IndexTokenRequest{
		Chain:      "bsc",
		Address:    token,
		StartBlock: 123,
		EndBlock:   456,
	}); err != nil {
		t.Fatalf("queue free local: %v", err)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.LastBlock != 122 {
		t.Fatalf("last block = %d, want reset to 122", job.LastBlock)
	}
	if job.BatchSize != 100 {
		t.Fatalf("batch size = %d, want 100", job.BatchSize)
	}
}

func TestQueueFreeLocalEarlyIndexDoesNotReuseOldStartWithoutExplicitStart(t *testing.T) {
	config.Init()
	config.Get().OnchainFreeIndexerEnabled = true
	config.Get().OnchainIndexerBatchBlocks = 100

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		StartBlock:   104738691,
		EndBlock:     104838690,
		LastBlock:    104738766,
		BatchSize:    1,
	}); err != nil {
		t.Fatalf("upsert old job: %v", err)
	}

	svc := NewService(st)
	if _, err := svc.queueFreeLocalEarlyIndex(context.Background(), "bsc", token, IndexTokenRequest{
		Chain:   "bsc",
		Address: token,
	}); err != nil {
		t.Fatalf("queue free local: %v", err)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.StartBlock != 0 || job.EndBlock != 0 || job.LastBlock != 0 {
		t.Fatalf("start/end/last = %d/%d/%d, want reset to 0/0/0", job.StartBlock, job.EndBlock, job.LastBlock)
	}
	if job.BatchSize != 100 {
		t.Fatalf("batch size = %d, want 100", job.BatchSize)
	}
}

func TestNewServiceUsesMarketHTTPProxy(t *testing.T) {
	t.Setenv("MARKET_HTTP_PROXY", "http://user:pass@proxy.example:10811")
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

func TestNewServiceUsesDirectOnchainHTTPClientByDefault(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://user:pass@proxy.example:10811")
	t.Setenv("HTTPS_PROXY", "http://user:pass@proxy.example:10811")
	t.Setenv("MARKET_HTTP_PROXY", "")
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
	if transport.Proxy != nil {
		t.Fatalf("onchain data transport should not use environment proxy by default")
	}
}

func TestNewServiceUsesDirectArchiveRPCClientByDefault(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://user:pass@proxy.example:10811")
	t.Setenv("ONCHAIN_ARCHIVE_HTTP_PROXY", "")
	config.Init()

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st)
	transport, ok := svc.rpcHTTPClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", svc.rpcHTTPClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatalf("archive RPC transport should not use environment proxy")
	}
}

func TestPoolsToStorePersistsPairTokenAddresses(t *testing.T) {
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	rows := poolsToStore([]PoolSnapshot{{
		ChainID:    "bsc",
		Address:    "0x1111111111111111111111111111111111111111",
		BaseToken:  token,
		QuoteToken: quote,
	}}, "bsc", token)
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Token0 != token || rows[0].Token1 != quote {
		t.Fatalf("token0/token1 = %s/%s, want %s/%s", rows[0].Token0, rows[0].Token1, token, quote)
	}

	rows = poolsToStore([]PoolSnapshot{{
		ChainID:    "bsc",
		Address:    "0x2222222222222222222222222222222222222222",
		BaseToken:  quote,
		QuoteToken: token,
	}}, "bsc", token)
	if rows[0].Token0 != token || rows[0].Token1 != quote {
		t.Fatalf("inverted token0/token1 = %s/%s, want %s/%s", rows[0].Token0, rows[0].Token1, token, quote)
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

func TestAnalyzeTokenRecentFallsBackToGeckoPoolsWhenDexScreenerFails(t *testing.T) {
	config.Init()

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	tokenAddress := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	poolAddress := "0x4271ea806625f27f056d0724cb6fd5baf5353ead"
	quoteAddress := "0x6d8d8df799279e761a4f49edc319b3bd50f14444"
	buyer := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	svc := NewService(st)
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "api.dexscreener.com":
			return nil, errors.New("context deadline exceeded (Client.Timeout exceeded while awaiting headers)")
		case "api.geckoterminal.com":
			if strings.Contains(req.URL.Path, "/tokens/") {
				return jsonResponse(http.StatusOK, map[string]any{
					"data": []any{
						map[string]any{
							"id":   "bsc_" + poolAddress,
							"type": "pool",
							"attributes": map[string]any{
								"address":         poolAddress,
								"name":            "Fire Phoenix / Future",
								"pool_created_at": "2026-03-11T03:05:10Z",
								"token_price_usd": "0.09711340025",
								"reserve_in_usd":  "81257.4125",
								"volume_usd":      map[string]any{"h24": "3556.4446396086"},
							},
							"relationships": map[string]any{
								"base_token":  map[string]any{"data": map[string]any{"id": "bsc_" + tokenAddress, "type": "token"}},
								"quote_token": map[string]any{"data": map[string]any{"id": "bsc_" + quoteAddress, "type": "token"}},
								"dex":         map[string]any{"data": map[string]any{"id": "pancakeswap_v2", "type": "dex"}},
							},
						},
					},
					"included": []any{
						map[string]any{"id": "bsc_" + tokenAddress, "type": "token", "attributes": map[string]any{"address": tokenAddress, "name": "Fire Phoenix", "symbol": "FPHX"}},
						map[string]any{"id": "bsc_" + quoteAddress, "type": "token", "attributes": map[string]any{"address": quoteAddress, "name": "Future", "symbol": "FUTURE"}},
					},
				})
			}
			return jsonResponse(http.StatusOK, map[string]any{
				"data": []any{
					map[string]any{"attributes": map[string]any{
						"block_number":       float64(100),
						"tx_hash":            "0xabc",
						"tx_from_address":    buyer,
						"from_token_amount":  "10",
						"to_token_amount":    "1000",
						"from_token_address": quoteAddress,
						"to_token_address":   tokenAddress,
						"block_timestamp":    "2026-01-01T01:00:00Z",
						"kind":               "buy",
						"volume_in_usd":      "10",
					}},
				},
			})
		case "api.gopluslabs.io":
			return jsonResponse(http.StatusOK, map[string]any{
				"code": 1,
				"result": map[string]any{
					tokenAddress: map[string]any{"token_name": "Fire Phoenix", "token_symbol": "FPHX", "holder_count": "123"},
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
		t.Fatalf("status = %q, want %q; message=%q", resp.Status, StatusOK, resp.Message)
	}
	if len(resp.Pools) != 1 || resp.Pools[0].Address != poolAddress {
		t.Fatalf("unexpected pools: %+v", resp.Pools)
	}
	if resp.Token.Symbol != "FPHX" {
		t.Fatalf("symbol = %q, want FPHX", resp.Token.Symbol)
	}
	if resp.Recent == nil || resp.Recent.BuyCount != 1 {
		t.Fatalf("unexpected recent stats: %+v", resp.Recent)
	}
}

func TestAnalyzeTokenRecentFallsBackToLocalIndexerWhenPublicSourcesTimeout(t *testing.T) {
	config.Init()

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	tokenAddress := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	poolAddress := "0xc81a9d722a41a5d76aaca07f8e88188beaf9221a"
	trader := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := st.Onchain().UpsertToken(&store.OnchainToken{
		Chain:       "bsc",
		Address:     tokenAddress,
		Name:        "Fire Phoenix",
		Symbol:      "FPHX",
		Decimals:    18,
		IndexStatus: store.OnchainJobStatusIndexing,
	}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain:        "bsc",
		TokenAddress: tokenAddress,
		PoolAddress:  poolAddress,
		DexID:        "pancakeswap_v2",
		BaseToken:    tokenAddress,
		QuoteToken:   "0x55d398326f99059ff775485246999027b3197955",
		Token0:       tokenAddress,
		Token1:       "0x55d398326f99059ff775485246999027b3197955",
		CreatedBlock: 86282308,
	}}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().InsertSwaps([]store.OnchainSwap{{
		Chain:         "bsc",
		TokenAddress:  tokenAddress,
		PoolAddress:   poolAddress,
		TxHash:        "0xabc",
		LogIndex:      0,
		BlockNumber:   86282308,
		BlockTime:     1781740000000,
		EventType:     "swap",
		TraderAddress: trader,
		Side:          "buy",
		TokenAmount:   100,
		QuoteAmount:   1.5,
	}}); err != nil {
		t.Fatalf("insert swaps: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: tokenAddress,
		Status:       store.OnchainJobStatusIndexing,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		Phase:        PhaseScanningSeeds,
		StartBlock:   86282308,
		EndBlock:     86382307,
		LastBlock:    86282307,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	svc := NewService(st)
	svc.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "api.dexscreener.com":
			return nil, errors.New("dial tcp 185.45.6.57:443: connect: connection refused")
		case "api.geckoterminal.com":
			return nil, context.DeadlineExceeded
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
	if resp.Status != StatusPartialData {
		t.Fatalf("status = %q, want %q", resp.Status, StatusPartialData)
	}
	if strings.Contains(resp.Message, "dial tcp") || strings.Contains(resp.Message, "context deadline") {
		t.Fatalf("message leaked transport details: %q", resp.Message)
	}
	if resp.Token.Symbol != "FPHX" {
		t.Fatalf("symbol = %q, want FPHX", resp.Token.Symbol)
	}
	if len(resp.Pools) != 1 || resp.Pools[0].CreatedBlock != 86282308 {
		t.Fatalf("pools = %+v, want local created block", resp.Pools)
	}
	if resp.Recent == nil || resp.Recent.BuyCount != 1 || resp.Recent.TradeCount != 1 {
		t.Fatalf("recent = %+v, want local swap stats", resp.Recent)
	}
	if resp.DealerFlow == nil {
		t.Fatalf("dealer flow should be present")
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
