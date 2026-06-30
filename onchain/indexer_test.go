package onchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"nofx/config"
	"nofx/store"
)

func TestParsePoolEventSupportsSwapMintAndBurn(t *testing.T) {
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	pool := store.OnchainPool{
		Chain:          "bsc",
		TokenAddress:   token,
		PoolAddress:    "0x1111111111111111111111111111111111111111",
		Token0:         token,
		Token1:         quote,
		Token0Decimals: 18,
		Token1Decimals: 18,
	}
	trader := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	swap, ok := parsePoolEvent("bsc", token, pool, rpcLog{
		Address:         pool.PoolAddress,
		Topics:          []string{swapTopic, topicAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), topicAddress(trader)},
		Data:            words(0, 10, 1000, 0),
		BlockNumber:     "0x1",
		TransactionHash: "0xabc",
		LogIndex:        "0x0",
	}, 1000)
	if !ok || swap.EventType != "swap" || swap.Side != "buy" || swap.TraderAddress != trader {
		t.Fatalf("unexpected swap parse: ok=%v swap=%+v", ok, swap)
	}

	mint, ok := parsePoolEvent("bsc", token, pool, rpcLog{
		Address:         pool.PoolAddress,
		Topics:          []string{mintTopic, topicAddress(trader)},
		Data:            words(500, 5),
		BlockNumber:     "0x2",
		TransactionHash: "0xdef",
		LogIndex:        "0x1",
	}, 2000)
	if !ok || mint.EventType != "mint" || mint.Side != "lp_event" || mint.TraderAddress != trader {
		t.Fatalf("unexpected mint parse: ok=%v mint=%+v", ok, mint)
	}

	burn, ok := parsePoolEvent("bsc", token, pool, rpcLog{
		Address:         pool.PoolAddress,
		Topics:          []string{burnTopic, topicAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), topicAddress(trader)},
		Data:            words(250, 3),
		BlockNumber:     "0x3",
		TransactionHash: "0x123",
		LogIndex:        "0x2",
	}, 3000)
	if !ok || burn.EventType != "burn" || burn.Side != "lp_event" || burn.TraderAddress != trader {
		t.Fatalf("unexpected burn parse: ok=%v burn=%+v", ok, burn)
	}
}

func TestParsePoolEventSupportsV3Swap(t *testing.T) {
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	pool := store.OnchainPool{
		Chain:          "bsc",
		TokenAddress:   token,
		PoolAddress:    "0x1111111111111111111111111111111111111111",
		Token0:         token,
		Token1:         quote,
		Token0Decimals: 18,
		Token1Decimals: 18,
	}
	trader := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	swap, ok := parsePoolEvent("bsc", token, pool, rpcLog{
		Address:         pool.PoolAddress,
		Topics:          []string{swapV3Topic, topicAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), topicAddress(trader)},
		Data:            wordsBig(rawUnits(-1000), rawUnits(10), big.NewInt(0), big.NewInt(0), big.NewInt(0)),
		BlockNumber:     "0x1",
		TransactionHash: "0xabc",
		LogIndex:        "0x0",
	}, 1000)
	if !ok || swap.EventType != "swap" || swap.Side != "buy" || swap.TraderAddress != trader {
		t.Fatalf("unexpected v3 buy parse: ok=%v swap=%+v", ok, swap)
	}
	if swap.TokenAmount != 1000 || swap.QuoteAmount != 10 {
		t.Fatalf("v3 amounts = token=%v quote=%v, want 1000/10", swap.TokenAmount, swap.QuoteAmount)
	}
}

func TestRPCCallRetriesRateLimit(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "https://archive.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	attempts := 0
	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts < 3 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":"0x64"}`)),
		}, nil
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	var result string
	if err := svc.rpcCall(context.Background(), "eth_blockNumber", []any{}, &result); err != nil {
		t.Fatalf("rpcCall returned error: %v", err)
	}
	if result != "0x64" {
		t.Fatalf("result = %q, want 0x64", result)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRPCCallTreatsLogRangeLimitAsPayloadTooLarge(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "https://archive.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    -32615,
				"message": "eth_getLogs is limited to a 5 range",
			},
		})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	var logs []rpcLog
	err = svc.rpcCall(context.Background(), "eth_getLogs", []any{map[string]any{}}, &logs)
	if !errors.Is(err, errRPCPayloadTooLarge) {
		t.Fatalf("err = %v, want errRPCPayloadTooLarge", err)
	}
}

func TestRPCCallTreatsLimitExceededAsPayloadTooLarge(t *testing.T) {
	if err := classifyRPCError(-32005, "limit exceeded"); !errors.Is(err, errRPCPayloadTooLarge) {
		t.Fatalf("err = %v, want errRPCPayloadTooLarge", err)
	}
}

func TestRPCCallTreatsPlanUsageLimitAsRateLimited(t *testing.T) {
	err := classifyRPCError(-32001, "You've reached the usage limit for your current plan. To continue with higher limits and uninterrupted access, please upgrade here: https://www.1rpc.io/#pricing")
	if !errors.Is(err, errRPCRateLimited) {
		t.Fatalf("err = %v, want errRPCRateLimited", err)
	}
}

func TestRPCCallTreatsMissingHistoricalStateAsArchiveUnsupported(t *testing.T) {
	for _, message := range []string{
		"missing trie node",
		"historical state abc is not available",
		"header not found",
	} {
		if err := classifyRPCError(-32000, message); !errors.Is(err, errRPCArchiveUnsupported) {
			t.Fatalf("message %q err = %v, want errRPCArchiveUnsupported", message, err)
		}
	}
}

func TestRPCCallFallsBackToNextEndpointOnUsageLimit(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st)
	firstCalls := 0
	secondCalls := 0
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "limited.example":
			firstCalls++
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"error": map[string]any{
					"code":    -32001,
					"message": "You've reached the usage limit for your current plan. To continue with higher limits and uninterrupted access, please upgrade here: https://www.1rpc.io/#pricing",
				},
			})
		case "backup.example":
			secondCalls++
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  "0x64",
			})
		default:
			t.Fatalf("unexpected endpoint: %s", req.URL.String())
			return nil, nil
		}
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	ctx := context.WithValue(context.Background(), rpcEndpointContextKey{}, []string{
		"https://limited.example",
		"https://backup.example",
	})
	var result string
	if err := svc.rpcCall(ctx, "eth_blockNumber", []any{}, &result); err != nil {
		t.Fatalf("rpcCall returned error: %v", err)
	}
	if result != "0x64" {
		t.Fatalf("result = %q, want 0x64", result)
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("calls first/second = %d/%d, want 1/1", firstCalls, secondCalls)
	}
}

func TestRPCCallReturnsRetryableTransportErrorAfterEndpointTimeouts(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, context.DeadlineExceeded
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	ctx := context.WithValue(context.Background(), rpcEndpointContextKey{}, []string{
		"https://timeout-one.example",
		"https://timeout-two.example",
	})
	var result string
	err = svc.rpcCall(ctx, "eth_blockNumber", []any{}, &result)
	if !errors.Is(err, errRPCTransportRetryable) {
		t.Fatalf("err = %v, want errRPCTransportRetryable", err)
	}
}

func TestRPCCallFallsBackToNextEndpointOnPrunedLogs(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st)
	firstCalls := 0
	secondCalls := 0
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "first.example":
			firstCalls++
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"error": map[string]any{
					"code":    -32701,
					"message": "History has been pruned for this block range",
				},
			})
		case "second.example":
			secondCalls++
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  []any{},
			})
		default:
			t.Fatalf("unexpected endpoint: %s", req.URL.String())
			return nil, nil
		}
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	ctx := context.WithValue(context.Background(), rpcEndpointContextKey{}, []string{
		"https://first.example",
		"https://second.example",
	})
	var logs []rpcLog
	if err := svc.rpcCall(ctx, "eth_getLogs", []any{map[string]any{}}, &logs); err != nil {
		t.Fatalf("rpcCall returned error: %v", err)
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("calls first/second = %d/%d, want 1/1", firstCalls, secondCalls)
	}
}

func TestRPCCallFallsBackToNextEndpointWhenGetLogsUnsupported(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st)
	firstCalls := 0
	secondCalls := 0
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "first.example":
			firstCalls++
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"error": map[string]any{
					"code":    -32000,
					"message": "The method eth_getLogs is not supported.",
				},
			})
		case "second.example":
			secondCalls++
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  []any{},
			})
		default:
			t.Fatalf("unexpected endpoint: %s", req.URL.String())
			return nil, nil
		}
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	ctx := context.WithValue(context.Background(), rpcEndpointContextKey{}, []string{
		"https://first.example",
		"https://second.example",
	})
	var logs []rpcLog
	if err := svc.rpcCall(ctx, "eth_getLogs", []any{map[string]any{}}, &logs); err != nil {
		t.Fatalf("rpcCall returned error: %v", err)
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("calls first/second = %d/%d, want 1/1", firstCalls, secondCalls)
	}
}

func TestRPCCallTreatsHTTPArchivePlanErrorAsUnsupported(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "https://archive.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"error":{"code":-32002,"message":"Archive, Debug and Trace requests are not available on your current plan."}}`)),
		}, nil
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	var result string
	err = svc.rpcCall(context.Background(), "eth_getBlockByNumber", []any{"0x1", false}, &result)
	if !errors.Is(err, errRPCArchiveUnsupported) {
		t.Fatalf("err = %v, want errRPCArchiveUnsupported", err)
	}
}

func TestRunIndexJobStopsEarlyWhenSeedWalletsAreFound(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "https://archive.example"
	config.Get().OnchainIndexerBatchBlocks = 100

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	pool := "0x1111111111111111111111111111111111111111"
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: "bsc", Address: token, Symbol: "T", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain:          "bsc",
		TokenAddress:   token,
		PoolAddress:    pool,
		Token0:         token,
		Token1:         "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Token0Decimals: 18,
		Token1Decimals: 18,
	}}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		StartBlock:   1,
		EndBlock:     300,
		LastBlock:    0,
		BatchSize:    100,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return jsonHTTPResponse([]any{})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if strings.Contains(raw, "eth_getLogs") && strings.Contains(raw, strings.ToLower(pool)) && strings.Contains(raw, `"0x1"`) {
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  seedSwapLogs(pool, 100),
			})
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  emptyRPCResult(raw),
		})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusCompleted {
		t.Fatalf("status = %q, want completed", job.Status)
	}
	if job.LastBlock != 100 {
		t.Fatalf("last block = %d, want early stop at 100", job.LastBlock)
	}
	count, err := st.Onchain().CountEarlyBuyWallets("bsc", token, 100)
	if err != nil {
		t.Fatalf("count early buys: %v", err)
	}
	if count != 100 {
		t.Fatalf("early buy count = %d, want 100", count)
	}
}

func TestRunIndexJobSplitsEarlySeedRangesForSmallLogLimits(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "https://archive.example"
	config.Get().OnchainIndexerBatchBlocks = 20

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	pool := "0x1111111111111111111111111111111111111111"
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: "bsc", Address: token, Symbol: "T", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain:          "bsc",
		TokenAddress:   token,
		PoolAddress:    pool,
		Token0:         token,
		Token1:         "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Token0Decimals: 18,
		Token1Decimals: 18,
	}}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		StartBlock:   1,
		EndBlock:     20,
		LastBlock:    0,
		BatchSize:    20,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	svc := NewService(st)
	wideRangeErrors := 0
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return jsonHTTPResponse([]any{})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if strings.Contains(raw, "eth_getLogs") && strings.Contains(raw, strings.ToLower(pool)) {
			from, to := rpcLogRange(raw)
			if to-from+1 > 1 {
				wideRangeErrors++
				return jsonResponse(http.StatusOK, map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"error": map[string]any{
						"code":    -32615,
						"message": "limit exceeded",
					},
				})
			}
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  seedSwapLogsInRange(pool, from, to),
			})
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  emptyRPCResult(raw),
		})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	if wideRangeErrors == 0 {
		t.Fatalf("expected at least one wide range split")
	}
	count, err := st.Onchain().CountEarlyBuyWallets("bsc", token, 100)
	if err != nil {
		t.Fatalf("count early buys: %v", err)
	}
	if count != 20 {
		t.Fatalf("early buy count = %d, want 20", count)
	}
}

func TestRunFreeLocalIndexFallsBackToOneBlockLogRanges(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeRPCURLs = []string{"https://free.example"}
	config.Get().OnchainFreeLogSource = "rpc"
	config.Get().OnchainFreeBackfillMaxRequestsPerTick = 200
	config.Get().OnchainFreeBackfillTargetSeeds = 100
	config.Get().OnchainIndexerBatchBlocks = 20

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	pool := "0x1111111111111111111111111111111111111111"
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: "bsc", Address: token, Symbol: "T", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain:          "bsc",
		TokenAddress:   token,
		PoolAddress:    pool,
		Token0:         token,
		Token1:         "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Token0Decimals: 18,
		Token1Decimals: 18,
	}}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().UpsertLocalIndexTask(&store.OnchainLocalIndexTask{
		Chain:        "bsc",
		TokenAddress: token,
		Enabled:      true,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		StartBlock:   1,
		Status:       store.OnchainJobStatusQueued,
	}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		StartBlock:   1,
		EndBlock:     20,
		LastBlock:    0,
		BatchSize:    20,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	svc := NewService(st)
	wideRangeErrors := 0
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return jsonHTTPResponse([]any{})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if strings.Contains(raw, "eth_blockNumber") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x14"})
		}
		if strings.Contains(raw, "eth_getLogs") && strings.Contains(raw, strings.ToLower(pool)) {
			from, to := rpcLogRange(raw)
			if to-from+1 > 1 {
				wideRangeErrors++
				return jsonResponse(http.StatusOK, map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"error": map[string]any{
						"code":    -32005,
						"message": "limit exceeded",
					},
				})
			}
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  seedSwapLogsInRange(pool, from, to),
			})
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  emptyRPCResult(raw),
		})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	if wideRangeErrors == 0 {
		t.Fatalf("expected wide range request to hit one-block limit")
	}
	count, err := st.Onchain().CountEarlyBuyWallets("bsc", token, 100)
	if err != nil {
		t.Fatalf("count early buys: %v", err)
	}
	if count != 20 {
		t.Fatalf("early buy count = %d, want 20", count)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusCompleted {
		t.Fatalf("job status = %s, want completed", job.Status)
	}
	if job.IndexSource != store.OnchainIndexSourceFreeLocal {
		t.Fatalf("job source = %s, want free_local", job.IndexSource)
	}
	if !strings.Contains(job.ProgressMessage, "20/100") {
		t.Fatalf("progress message = %q, want 20/100", job.ProgressMessage)
	}
}

func TestParsePancakeV2PairCreatedLog(t *testing.T) {
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	pair := "0x1111111111111111111111111111111111111111"
	pool, ok := parsePancakeV2PairCreatedLog("bsc", token, rpcLog{
		Address:     pancakeV2FactoryAddress,
		Topics:      []string{pairCreatedTopic, topicAddress(quote), topicAddress(token)},
		Data:        topicAddress(pair) + strings.Repeat("0", 64),
		BlockNumber: "0x1234",
	})
	if !ok {
		t.Fatalf("expected pair log to parse")
	}
	if pool.PoolAddress != pair || pool.Token0 != quote || pool.Token1 != token || pool.QuoteToken != quote || pool.CreatedBlock != 0x1234 {
		t.Fatalf("unexpected pool: %+v", pool)
	}
}

func TestRunFreeLocalIndexDiscoversPancakePoolFromFactoryLogs(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeRPCURLs = []string{"https://free.example"}
	config.Get().OnchainFreeLogSource = "rpc"
	config.Get().OnchainFreeBackfillMaxRequestsPerTick = 20
	config.Get().OnchainFreeBackfillTargetSeeds = 3
	config.Get().OnchainIndexerBatchBlocks = 20
	config.Get().OnchainEarlyWindowBlocks = 20

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	pool := "0x1111111111111111111111111111111111111111"
	if err := st.Onchain().UpsertLocalIndexTask(&store.OnchainLocalIndexTask{
		Chain:        "bsc",
		TokenAddress: token,
		Enabled:      true,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		Status:       store.OnchainJobStatusQueued,
	}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	dexCalled := false
	factoryLogsCalled := false
	poolLogsCalled := false
	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			if req.URL.Host == "api.dexscreener.com" || req.URL.Host == "api.geckoterminal.com" {
				dexCalled = true
				return nil, errors.New("network unavailable")
			}
			return jsonHTTPResponse(map[string]any{"data": []any{}})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if strings.Contains(raw, "eth_blockNumber") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x6b6c23"})
		}
		if strings.Contains(raw, "eth_call") && strings.Contains(raw, pairToken0Selector) {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": topicAddress(quote)})
		}
		if strings.Contains(raw, "eth_call") && strings.Contains(raw, pairToken1Selector) {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": topicAddress(token)})
		}
		if strings.Contains(raw, "eth_call") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x12"})
		}
		if strings.Contains(raw, "eth_getLogs") && strings.Contains(raw, pancakeV2FactoryAddress) {
			factoryLogsCalled = true
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": []map[string]any{{
					"address":         pancakeV2FactoryAddress,
					"topics":          []string{pairCreatedTopic, topicAddress(quote), topicAddress(token)},
					"data":            topicAddress(pool) + strings.Repeat("0", 64),
					"blockNumber":     "0x6b6c20",
					"transactionHash": "0xabc",
					"logIndex":        "0x1",
				}},
			})
		}
		if strings.Contains(raw, "eth_getLogs") && strings.Contains(raw, pool) {
			poolLogsCalled = true
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  seedSwapLogsInRangeForToken1(pool, 0x6b6c20, 0x6b6c22),
			})
		}
		return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": emptyRPCResult(raw)})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	if dexCalled || !factoryLogsCalled || !poolLogsCalled {
		t.Fatalf("expected rpc factory/pool logs before Dex/Gecko, got dex=%v factory=%v pool=%v", dexCalled, factoryLogsCalled, poolLogsCalled)
	}
	pools, err := st.Onchain().ListPools("bsc", token)
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) != 1 || pools[0].PoolAddress != pool || pools[0].CreatedBlock != 0x6b6c20 {
		t.Fatalf("unexpected pools: %+v", pools)
	}
	count, err := st.Onchain().CountEarlyBuyWallets("bsc", token, 100)
	if err != nil {
		t.Fatalf("count early buys: %v", err)
	}
	if count != 3 {
		t.Fatalf("early buy count = %d, want 3", count)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusCompleted || job.Provider != "free_rpc" {
		t.Fatalf("job status/provider = %s/%s, want completed/free_rpc", job.Status, job.Provider)
	}
}

func TestDiscoverPancakePoolFromGetPairUsesPublicCreatedTime(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeRPCURLs = []string{"https://rpc.example"}
	config.Get().OnchainFreeLogSource = "rpc"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c"
	pair := "0xc81a9d722a41a5d76aaca07f8e88188beaf9221a"
	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Host == "api.dexscreener.com" {
			return jsonHTTPResponse([]map[string]any{{
				"chainId":       "bsc",
				"dexId":         "pancakeswap",
				"pairAddress":   pair,
				"pairCreatedAt": float64(1700000000000),
				"url":           "https://dexscreener.example/pair",
				"baseToken": map[string]any{
					"address": token,
					"name":    "Demo",
					"symbol":  "DEMO",
				},
				"quoteToken": map[string]any{
					"address": quote,
					"name":    "Wrapped BNB",
					"symbol":  "WBNB",
				},
				"priceUsd":  "0.01",
				"liquidity": map[string]any{"usd": 12345},
				"volume":    map[string]any{"h24": 100},
			}})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		switch {
		case strings.Contains(raw, "eth_blockNumber"):
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": hexBlock(1000)})
		case strings.Contains(raw, "eth_getBlockByNumber"):
			from, _ := rpcBlockArg(raw)
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  map[string]any{"timestamp": hexBlock(from)},
			})
		case strings.Contains(raw, pancakeV2GetPairSelector):
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": topicAddress(pair)})
		case strings.Contains(raw, pairToken0Selector):
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": topicAddress(token)})
		case strings.Contains(raw, pairToken1Selector):
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": topicAddress(quote)})
		default:
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x12"})
		}
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	pools, err := svc.discoverPoolsFromPancakeFactoryCalls(context.Background(), "bsc", token)
	if err != nil {
		t.Fatalf("discover pools: %v", err)
	}
	if len(pools) == 0 {
		t.Fatalf("expected getPair pool")
	}
	if pools[0].PoolAddress != pair {
		t.Fatalf("pool = %s, want %s", pools[0].PoolAddress, pair)
	}
	if pools[0].CreatedAtMS != 1700000000000 {
		t.Fatalf("created_at_ms = %d, want public metadata timestamp", pools[0].CreatedAtMS)
	}
	if pools[0].CreatedBlock <= 0 {
		t.Fatalf("created_block = %d, want populated block", pools[0].CreatedBlock)
	}
}

func TestPoolCreationBlockFromCodeSkipsUnsupportedHistoricalStateEndpoint(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	const pool = "0xc81a9d722a41a5d76aaca07f8e88188beaf9221a"
	svc := NewService(st)
	callsByHost := map[string]int{}
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		callsByHost[req.URL.Host]++
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if req.URL.Host == "bad.example" {
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"error": map[string]any{
					"code":    -32000,
					"message": "missing trie node",
				},
			})
		}
		if strings.Contains(raw, "eth_blockNumber") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": hexBlock(10)})
		}
		block, _ := rpcGetCodeBlockArg(raw)
		code := "0x"
		if block >= 6 {
			code = "0x6001"
		}
		return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": code})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	block, err := svc.poolCreationBlockFromCode(context.Background(), pool, []string{
		"https://bad.example",
		"https://good.example",
	})
	if err != nil {
		t.Fatalf("poolCreationBlockFromCode returned error: %v", err)
	}
	if block != 6 {
		t.Fatalf("block = %d, want 6", block)
	}
	if callsByHost["bad.example"] == 0 || callsByHost["good.example"] == 0 {
		t.Fatalf("calls by host = %+v, want both endpoints tried", callsByHost)
	}
}

func TestRunFreeLocalIndexBackfillsExistingPoolCreatedBlockFromEtherscan(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeRPCURLs = []string{"https://rpc.example"}
	config.Get().OnchainFreeLogSource = "etherscan,rpc"
	config.Get().OnchainEtherscanAPIKey = "test-key"
	config.Get().OnchainEtherscanBaseURL = "https://api.etherscan.io/v2/api"
	config.Get().OnchainFreeLogsRPS = 1000
	config.Get().OnchainFreeLogsDailyBudget = 100
	config.Get().OnchainIndexerBatchBlocks = 10
	config.Get().OnchainEarlyWindowBlocks = 20

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c"
	pool := "0xc81a9d722a41a5d76aaca07f8e88188beaf9221a"
	createdBlock := pancakeV2FactoryStartBlock + 100
	staleStartBlock := createdBlock + 90000000
	staleLastBlock := staleStartBlock + 45
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: "bsc", Address: token, Symbol: "T", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain:        "bsc",
		TokenAddress: token,
		PoolAddress:  pool,
		BaseToken:    token,
		QuoteToken:   quote,
		Token0:       token,
		Token1:       quote,
	}}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().UpsertLocalIndexTask(&store.OnchainLocalIndexTask{
		Chain:        "bsc",
		TokenAddress: token,
		Enabled:      true,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		StartBlock:   staleStartBlock,
		LastBlock:    staleLastBlock,
		Status:       store.OnchainJobStatusQueued,
	}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		StartBlock:   staleStartBlock,
		EndBlock:     staleStartBlock + 20,
		LastBlock:    staleLastBlock,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Host == "api.etherscan.io" {
			q := req.URL.Query()
			if strings.EqualFold(q.Get("address"), pancakeV2FactoryAddress) {
				return jsonHTTPResponse(map[string]any{
					"status":  "1",
					"message": "OK",
					"result": []map[string]any{{
						"address":         pancakeV2FactoryAddress,
						"topics":          []string{pairCreatedTopic, topicAddress(token), topicAddress(quote)},
						"data":            topicAddress(pool) + strings.Repeat("0", 64),
						"blockNumber":     strconv.FormatInt(createdBlock, 10),
						"transactionHash": "0xabc",
						"logIndex":        "1",
					}},
				})
			}
			return jsonHTTPResponse(map[string]any{"status": "0", "message": "No records found", "result": "No records found"})
		}
		if req.Method == http.MethodGet {
			return jsonHTTPResponse([]any{})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if strings.Contains(raw, "eth_blockNumber") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": hexBlock(createdBlock + 20)})
		}
		if strings.Contains(raw, "eth_getLogs") && strings.Contains(raw, pool) {
			from, to := rpcLogRange(raw)
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  seedSwapLogsInRange(pool, from, to),
			})
		}
		return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": emptyRPCResult(raw)})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient
	svc.freeLogs.httpClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.StartBlock != createdBlock {
		t.Fatalf("start block = %d, want %d", job.StartBlock, createdBlock)
	}
	if job.LastBlock < createdBlock {
		t.Fatalf("last block = %d, want >= %d", job.LastBlock, createdBlock)
	}
	pools, err := st.Onchain().ListPools("bsc", token)
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) == 0 || pools[0].CreatedBlock != createdBlock {
		t.Fatalf("pools = %+v, want created block %d", pools, createdBlock)
	}
}

func TestRunFreeLocalIndexBackfillsExistingPoolCreatedBlockFromHistoricalCode(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeRPCURLs = []string{"https://bad.example", "https://good.example"}
	config.Get().OnchainFreeLogSource = "etherscan,rpc"
	config.Get().OnchainEtherscanAPIKey = "test-key"
	config.Get().OnchainEtherscanBaseURL = "https://api.etherscan.io/v2/api"
	config.Get().OnchainFreeLogsRPS = 1000
	config.Get().OnchainFreeLogsDailyBudget = 100
	config.Get().OnchainIndexerBatchBlocks = 10
	config.Get().OnchainEarlyWindowBlocks = 20

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c"
	pool := "0xc81a9d722a41a5d76aaca07f8e88188beaf9221a"
	createdBlock := int64(1234)
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: "bsc", Address: token, Symbol: "T", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain:        "bsc",
		TokenAddress: token,
		PoolAddress:  pool,
		BaseToken:    token,
		QuoteToken:   quote,
		Token0:       token,
		Token1:       quote,
	}}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Host == "api.etherscan.io" {
			return nil, context.DeadlineExceeded
		}
		if req.Method == http.MethodGet {
			return jsonHTTPResponse([]any{})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if req.URL.Host == "bad.example" {
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"error": map[string]any{
					"code":    -32000,
					"message": "missing trie node",
				},
			})
		}
		switch {
		case strings.Contains(raw, "eth_blockNumber"):
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": hexBlock(createdBlock + 30)})
		case strings.Contains(raw, "eth_getCode"):
			block, _ := rpcGetCodeBlockArg(raw)
			code := "0x"
			if block >= createdBlock {
				code = "0x6001"
			}
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": code})
		case strings.Contains(raw, "eth_getLogs") && strings.Contains(raw, pool):
			from, to := rpcLogRange(raw)
			return jsonResponse(http.StatusOK, map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  seedSwapLogsInRange(pool, from, to),
			})
		default:
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": emptyRPCResult(raw)})
		}
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient
	svc.freeLogs.httpClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.StartBlock != createdBlock {
		t.Fatalf("start block = %d, want %d", job.StartBlock, createdBlock)
	}
	pools, err := st.Onchain().ListPools("bsc", token)
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	if len(pools) == 0 || pools[0].CreatedBlock != createdBlock {
		t.Fatalf("pools = %+v, want created block %d", pools, createdBlock)
	}
}

func TestRunFreeLocalIndexKeepsSeedPhaseQueuedWhenCreatedBlockKnownAndLogsTimeout(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeRPCURLs = []string{"https://rpc.example"}
	config.Get().OnchainFreeLogSource = "etherscan,gecko"
	config.Get().OnchainEtherscanAPIKey = "test-key"
	config.Get().OnchainEtherscanBaseURL = "https://api.etherscan.io/v2/api"
	config.Get().OnchainFreeLogsRPS = 1000
	config.Get().OnchainFreeLogsDailyBudget = 100
	config.Get().OnchainIndexerBatchBlocks = 10
	config.Get().OnchainEarlyWindowBlocks = 20

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c"
	pool := "0xc81a9d722a41a5d76aaca07f8e88188beaf9221a"
	createdBlock := int64(1234)
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: "bsc", Address: token, Symbol: "T", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain:        "bsc",
		TokenAddress: token,
		PoolAddress:  pool,
		BaseToken:    token,
		QuoteToken:   quote,
		Token0:       token,
		Token1:       quote,
		CreatedBlock: createdBlock,
	}}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		Phase:        PhaseDiscoveringPools,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Host == "api.etherscan.io" {
			return nil, context.DeadlineExceeded
		}
		if req.Method == http.MethodGet {
			return jsonHTTPResponse(map[string]any{"pairs": []any{}, "data": []any{}})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if strings.Contains(raw, "eth_blockNumber") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": hexBlock(createdBlock + 30)})
		}
		return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": emptyRPCResult(raw)})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient
	svc.freeLogs.httpClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusQueued || job.Phase != PhaseScanningSeeds {
		t.Fatalf("job status/phase = %s/%s, want queued/scanning_seed_window", job.Status, job.Phase)
	}
	if job.StartBlock != createdBlock {
		t.Fatalf("start block = %d, want %d", job.StartBlock, createdBlock)
	}
	if !strings.Contains(job.ErrorMessage, "rpc transport retrying") {
		t.Fatalf("error message = %q, want rpc transport retrying", job.ErrorMessage)
	}
}

func TestRunFreeLocalIndexQueuesWhenCreatedBlockMissing(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeRPCURLs = []string{"https://rpc.example"}
	config.Get().OnchainFreeLogSource = "etherscan,gecko"
	config.Get().OnchainEtherscanAPIKey = "test-key"
	config.Get().OnchainEtherscanBaseURL = "https://api.etherscan.io/v2/api"
	config.Get().OnchainFreeLogsRPS = 1000
	config.Get().OnchainFreeLogsDailyBudget = 100
	config.Get().OnchainIndexerBatchBlocks = 10
	config.Get().OnchainEarlyWindowBlocks = 20

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c"
	pool := "0xc81a9d722a41a5d76aaca07f8e88188beaf9221a"
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: "bsc", Address: token, Symbol: "T", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain:        "bsc",
		TokenAddress: token,
		PoolAddress:  pool,
		BaseToken:    token,
		QuoteToken:   quote,
		Token0:       token,
		Token1:       quote,
	}}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	rpcGetLogsCalled := false
	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Host == "api.etherscan.io" {
			return jsonHTTPResponse(map[string]any{"status": "0", "message": "No records found", "result": "No records found"})
		}
		if req.Method == http.MethodGet {
			return jsonHTTPResponse(map[string]any{"pairs": []any{}, "data": []any{}})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if strings.Contains(raw, "eth_getLogs") {
			rpcGetLogsCalled = true
		}
		if strings.Contains(raw, "eth_blockNumber") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": hexBlock(pancakeV2FactoryStartBlock + 200)})
		}
		if strings.Contains(raw, "eth_call") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x12"})
		}
		return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": emptyRPCResult(raw)})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient
	svc.freeLogs.httpClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	if rpcGetLogsCalled {
		t.Fatalf("expected missing created block not to trigger RPC eth_getLogs seed scan")
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusQueued || job.Phase != PhaseDiscoveringPools {
		t.Fatalf("job status/phase = %s/%s, want queued/discovering_pools", job.Status, job.Phase)
	}
	if !strings.Contains(job.ProgressMessage, "pool_created_block_pending") {
		t.Fatalf("progress message = %q, want pool_created_block_pending", job.ProgressMessage)
	}
	count, err := st.Onchain().CountEarlyBuyWallets("bsc", token, 100)
	if err != nil {
		t.Fatalf("count early buys: %v", err)
	}
	if count != 0 {
		t.Fatalf("early buy count = %d, want 0", count)
	}
}

func TestRunFreeLocalIndexKeepsFactoryDiscoveryQueuedWhenPoolNotFoundYet(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeRPCURLs = []string{"https://free.example"}
	config.Get().OnchainFreeLogSource = "rpc"
	config.Get().OnchainFreeBackfillMaxRequestsPerTick = 2

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	if err := st.Onchain().UpsertLocalIndexTask(&store.OnchainLocalIndexTask{
		Chain:        "bsc",
		TokenAddress: token,
		Enabled:      true,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		Status:       store.OnchainJobStatusQueued,
	}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	dexCalled := false
	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			if req.URL.Host == "api.dexscreener.com" || req.URL.Host == "api.geckoterminal.com" {
				dexCalled = true
			}
			return nil, errors.New("network unavailable")
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if strings.Contains(raw, "eth_blockNumber") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": hexBlock(pancakeV2FactoryStartBlock + 300000)})
		}
		if strings.Contains(raw, "eth_call") {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x12"})
		}
		if strings.Contains(raw, "eth_getLogs") && strings.Contains(raw, pancakeV2FactoryAddress) {
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": []any{}})
		}
		return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": emptyRPCResult(raw)})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	if dexCalled {
		t.Fatalf("expected rpc factory discovery to run before external Dex/Gecko discovery")
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusQueued || job.Phase != PhaseDiscoveringPools {
		t.Fatalf("job status/phase = %s/%s, want queued/discovering_pools", job.Status, job.Phase)
	}
	if !strings.Contains(job.ProgressMessage, "continuing next tick") {
		t.Fatalf("progress message = %q", job.ProgressMessage)
	}
}

func TestGeckoTradeToSwapConvertsBuyAndSell(t *testing.T) {
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	pool := store.OnchainPool{
		Chain:        "bsc",
		TokenAddress: token,
		PoolAddress:  "0x1111111111111111111111111111111111111111",
		Token0:       token,
		Token1:       quote,
	}
	buy, ok := geckoTradeToSwap("bsc", token, pool, geckoTrade{
		ID: "bsc_123_0xabc_9_1781716275",
		Attributes: geckoTradeAttrs{
			BlockNumber:      123,
			TxHash:           "0xabc",
			TxFromAddress:    "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			FromTokenAmount:  "1.5",
			ToTokenAmount:    "100",
			FromTokenAddress: quote,
			ToTokenAddress:   token,
			BlockTimestamp:   "2026-06-17T17:10:41Z",
			VolumeUSD:        "42.5",
		},
	}, 0)
	if !ok || buy.Side != "buy" || buy.TokenAmount != 100 || buy.QuoteAmount != 42.5 || buy.QuoteToken != quote || buy.LogIndex != 1781716275 {
		t.Fatalf("unexpected buy swap: ok=%v swap=%+v", ok, buy)
	}
	sell, ok := geckoTradeToSwap("bsc", token, pool, geckoTrade{
		ID: "bsc_124_0xdef_10_1781716280",
		Attributes: geckoTradeAttrs{
			BlockNumber:      124,
			TxHash:           "0xdef",
			TxFromAddress:    "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			FromTokenAmount:  "25",
			ToTokenAmount:    "0.4",
			FromTokenAddress: token,
			ToTokenAddress:   quote,
			BlockTimestamp:   "2026-06-17T17:10:50Z",
			VolumeUSD:        "10",
		},
	}, 1)
	if !ok || sell.Side != "sell" || sell.TokenAmount != 25 || sell.QuoteAmount != 10 || sell.QuoteToken != quote {
		t.Fatalf("unexpected sell swap: ok=%v swap=%+v", ok, sell)
	}
}

func TestRunFreeLocalIndexSeedsFromGeckoTerminalWithoutRPC(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	config.Get().OnchainFreeRPCURLs = nil
	config.Get().OnchainFreeLogSource = "geckoterminal"
	config.Get().OnchainGeckoTradesLimit = 300
	config.Get().OnchainFreeBackfillTargetSeeds = 100

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	pool := "0x1111111111111111111111111111111111111111"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: "bsc", Address: token, Symbol: "T", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain:          "bsc",
		TokenAddress:   token,
		PoolAddress:    pool,
		BaseToken:      token,
		QuoteToken:     quote,
		Token0:         token,
		Token1:         quote,
		Token0Decimals: 18,
		Token1Decimals: 18,
		CreatedBlock:   100,
	}}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().UpsertLocalIndexTask(&store.OnchainLocalIndexTask{
		Chain:        "bsc",
		TokenAddress: token,
		Enabled:      true,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
		Status:       store.OnchainJobStatusQueued,
	}); err != nil {
		t.Fatalf("upsert task: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:  store.OnchainIndexSourceFreeLocal,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	rpcCalled := false
	svc := NewService(st)
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			rpcCalled = true
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x1"})
		}
		if req.URL.Host == "api.geckoterminal.com" && strings.Contains(req.URL.Path, "/trades") {
			return jsonHTTPResponse(map[string]any{
				"data": []map[string]any{
					geckoTradeFixture(101, "0xaaa1", seedAddress(1), quote, token, "10", "100", "2026-06-17T17:10:41Z"),
					geckoTradeFixture(102, "0xaaa2", seedAddress(2), quote, token, "11", "110", "2026-06-17T17:10:45Z"),
				},
			})
		}
		return jsonHTTPResponse(map[string]any{"data": []any{}})
	})}
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	if err := svc.runIndexJob(context.Background(), "bsc", token); err != nil {
		t.Fatalf("runIndexJob error: %v", err)
	}
	if rpcCalled {
		t.Fatalf("expected GeckoTerminal seed path not to require RPC when no RPC URL is configured")
	}
	count, err := st.Onchain().CountEarlyBuyWallets("bsc", token, 100)
	if err != nil {
		t.Fatalf("count early buys: %v", err)
	}
	if count != 2 {
		t.Fatalf("early buy count = %d, want 2", count)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusCompleted || job.Provider != "geckoterminal_recent" {
		t.Fatalf("job status/provider = %s/%s, want completed/geckoterminal_recent", job.Status, job.Provider)
	}
	if !strings.Contains(job.ProgressMessage, "2/100") {
		t.Fatalf("progress message = %q, want 2/100", job.ProgressMessage)
	}
}

func TestDiscoverPoolsFromPancakeFactoryCallsUsesCommonQuotes(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "https://rpc.example"
	config.Get().OnchainFreeRPCURLs = []string{"https://rpc.example"}

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	pool := "0x1111111111111111111111111111111111111111"
	factoryLogCalled := false
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			return jsonHTTPResponse([]any{})
		}
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		if strings.Contains(raw, "eth_getLogs") && strings.Contains(raw, pancakeV2FactoryAddress) {
			factoryLogCalled = true
		}
		switch {
		case strings.Contains(raw, pancakeV2GetPairSelector) && strings.Contains(raw, strings.TrimPrefix(quote, "0x")):
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": topicAddress(pool)})
		case strings.Contains(raw, pairToken0Selector):
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": topicAddress(token)})
		case strings.Contains(raw, pairToken1Selector):
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": topicAddress(quote)})
		case strings.Contains(raw, "eth_call"):
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x12"})
		default:
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": 1, "result": "0x0"})
		}
	})}
	svc := NewService(st)
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient

	pools, err := svc.discoverPoolsFromPancakeFactoryCalls(context.Background(), "bsc", token)
	if err != nil {
		t.Fatalf("discover pools: %v", err)
	}
	if len(pools) != 1 || pools[0].PoolAddress != pool || pools[0].QuoteToken != quote {
		t.Fatalf("pools = %+v, want %s/%s", pools, pool, quote)
	}
	if factoryLogCalled {
		t.Fatalf("expected getPair discovery not to call factory eth_getLogs")
	}
}

func TestFreeLocalTransferEnrichmentQueriesTrackedWalletsOnly(t *testing.T) {
	config.Init()
	config.Get().OnchainFreeLogSource = "etherscan"
	config.Get().OnchainEtherscanAPIKey = "test-key"
	config.Get().OnchainEtherscanBaseURL = "https://api.etherscan.io/v2/api"
	config.Get().OnchainFreeLogsRPS = 1000
	config.Get().OnchainFreeLogsDailyBudget = 100

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	seed := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	child := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	pool := store.OnchainPool{
		Chain:          "bsc",
		TokenAddress:   token,
		PoolAddress:    "0x1111111111111111111111111111111111111111",
		Token0:         token,
		Token1:         "0x55d398326f99059ff775485246999027b3197955",
		Token0Decimals: 18,
		Token1Decimals: 18,
	}
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: "bsc", Address: token, Symbol: "T", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{pool}); err != nil {
		t.Fatalf("upsert pool: %v", err)
	}
	if err := st.Onchain().InsertSwaps([]store.OnchainSwap{{
		Chain:         "bsc",
		TokenAddress:  token,
		PoolAddress:   pool.PoolAddress,
		TxHash:        "0xabc",
		LogIndex:      1,
		BlockNumber:   10,
		EventType:     "swap",
		TraderAddress: seed,
		Side:          "buy",
		TokenAmount:   100,
		QuoteAmount:   1,
		QuoteToken:    pool.Token1,
	}}); err != nil {
		t.Fatalf("insert swap: %v", err)
	}
	job := &store.OnchainIndexJob{Chain: "bsc", TokenAddress: token, Status: store.OnchainJobStatusIndexing, IndexSource: store.OnchainIndexSourceFreeLocal, LastBlock: 10}
	task := &store.OnchainLocalIndexTask{Chain: "bsc", TokenAddress: token, Enabled: true}

	queriedTokenWide := false
	queriedWallet := false
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		if q.Get("module") == "logs" && q.Get("action") == "getLogs" {
			if q.Get("topic1") == "" && q.Get("topic2") == "" {
				queriedTokenWide = true
			}
			if strings.EqualFold(q.Get("topic1"), addressTopic(seed)) || strings.EqualFold(q.Get("topic2"), addressTopic(seed)) {
				queriedWallet = true
				return jsonHTTPResponse(map[string]any{
					"status":  "1",
					"message": "OK",
					"result": []map[string]any{{
						"address":         token,
						"topics":          []string{transferTopic, topicAddress(seed), topicAddress(child)},
						"data":            words(25),
						"blockNumber":     "11",
						"transactionHash": "0xdef",
						"logIndex":        "2",
					}},
				})
			}
			return jsonHTTPResponse(map[string]any{"status": "0", "message": "No records found", "result": "No records found"})
		}
		return jsonHTTPResponse(map[string]any{"status": "0", "message": "No records found", "result": "No records found"})
	})}
	svc := NewService(st)
	svc.httpClient = mockClient
	svc.freeLogs.httpClient = mockClient

	if err := svc.enrichFreeLocalSeedTransfers(context.Background(), "bsc", token, job, task, []store.OnchainPool{pool}, 1, 20, 100); err != nil {
		t.Fatalf("enrich transfers: %v", err)
	}
	if queriedTokenWide {
		t.Fatalf("expected enrichment not to issue token-wide transfer getLogs")
	}
	if !queriedWallet {
		t.Fatalf("expected enrichment to query tracked seed wallet")
	}
	transfers, err := st.Onchain().ListTransfers("bsc", token)
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	if len(transfers) != 1 || transfers[0].FromAddress != seed || transfers[0].ToAddress != child {
		t.Fatalf("unexpected transfers: %+v", transfers)
	}
}

func TestFreeLocalPoolDiscoveryUsesEtherscanBeforeRPC(t *testing.T) {
	config.Init()
	config.Get().OnchainFreeLogSource = "etherscan,rpc"
	config.Get().OnchainEtherscanAPIKey = "test-key"
	config.Get().OnchainEtherscanBaseURL = "https://api.etherscan.io/v2/api"
	config.Get().OnchainFreeRPCURLs = []string{"https://rpc.example"}
	config.Get().OnchainFreeLogsRPS = 1000
	config.Get().OnchainFreeLogsDailyBudget = 100

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	pair := "0x1111111111111111111111111111111111111111"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	rpcGetLogsCalled := false
	mockClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "api.etherscan.io" {
			q := req.URL.Query()
			if q.Get("module") == "logs" && q.Get("action") == "getLogs" && strings.EqualFold(q.Get("address"), pancakeV2FactoryAddress) {
				return jsonHTTPResponse(map[string]any{
					"status":  "1",
					"message": "OK",
					"result": []map[string]any{{
						"address":         pancakeV2FactoryAddress,
						"topics":          []string{pairCreatedTopic, topicAddress(token), topicAddress(quote)},
						"data":            topicAddress(pair) + strings.Repeat("0", 64),
						"blockNumber":     "7000000",
						"transactionHash": "0xabc",
						"logIndex":        "1",
					}},
				})
			}
			return jsonHTTPResponse(map[string]any{"status": "0", "message": "No records found", "result": "No records found"})
		}
		var body rpcRequest
		_ = json.NewDecoder(req.Body).Decode(&body)
		if body.Method == "eth_getLogs" {
			rpcGetLogsCalled = true
		}
		switch body.Method {
		case "eth_blockNumber":
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": "0x64"})
		case "eth_call":
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": "0x12"})
		default:
			return jsonResponse(http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": []any{}})
		}
	})}
	svc := NewService(st)
	svc.httpClient = mockClient
	svc.rpcHTTPClient = mockClient
	svc.freeLogs.httpClient = mockClient

	pools, _, complete, _, err := svc.discoverPoolsFromPancakeFactoryFreeLogs(context.Background(), "bsc", token, pancakeV2FactoryStartBlock, 8000000, 10)
	if err != nil {
		t.Fatalf("discover pools: %v", err)
	}
	if !complete {
		t.Fatalf("complete = false, want true")
	}
	if len(pools) != 1 || pools[0].PoolAddress != pair {
		t.Fatalf("pools = %+v, want pair %s", pools, pair)
	}
	if rpcGetLogsCalled {
		t.Fatalf("expected Etherscan pool discovery not to call RPC eth_getLogs")
	}
}

func TestDefaultFreeLogSourceDoesNotUseRPCLogs(t *testing.T) {
	config.Init()
	if shouldUseRPCSeeds() {
		t.Fatalf("default ONCHAIN_FREE_LOG_SOURCE should not enable RPC logs")
	}
	if !shouldUseEtherscanSeeds() || !shouldUseGeckoTerminalSeeds() {
		t.Fatalf("default ONCHAIN_FREE_LOG_SOURCE should enable etherscan and geckoterminal")
	}
}

func TestStartJobKeepsRateLimitedEarlyIndexQueued(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "https://archive.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	svc := NewService(st)
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        "bsc",
		TokenAddress: token,
		Status:       store.OnchainJobStatusIndexing,
		Scope:        store.OnchainIndexScopeEarlyWalletWindow,
		StartBlock:   100,
		EndBlock:     200,
		LastBlock:    120,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}
	if err := svc.markIndexJobFailure(context.Background(), "bsc", token, errRPCRateLimited); err != nil {
		t.Fatalf("mark failure: %v", err)
	}
	job, err := st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusQueued {
		t.Fatalf("status = %q, want queued", job.Status)
	}
	if !strings.Contains(job.ErrorMessage, "retrying") {
		t.Fatalf("error message = %q, want retrying", job.ErrorMessage)
	}
	if err := svc.markIndexJobFailure(context.Background(), "bsc", token, fmt.Errorf("%w: deadline exceeded", errRPCTransportRetryable)); err != nil {
		t.Fatalf("mark transport failure: %v", err)
	}
	job, err = st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusQueued {
		t.Fatalf("status = %q, want queued after transport retryable", job.Status)
	}
	if !strings.Contains(job.ErrorMessage, "rpc transport retrying") {
		t.Fatalf("error message = %q, want rpc transport retrying", job.ErrorMessage)
	}
	if err := svc.markIndexJobFailure(context.Background(), "bsc", token, errors.New("boom")); err != nil {
		t.Fatalf("mark non-rate failure: %v", err)
	}
	job, err = st.Onchain().GetJob("bsc", token)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != store.OnchainJobStatusFailed {
		t.Fatalf("status = %q, want failed", job.Status)
	}
}

func seedSwapLogs(pool string, count int) []map[string]any {
	logs := make([]map[string]any, 0, count)
	for i := 0; i < count; i++ {
		logs = append(logs, map[string]any{
			"address":         pool,
			"topics":          []string{swapTopic, topicAddress("0xcccccccccccccccccccccccccccccccccccccccc"), topicAddress(seedAddress(i))},
			"data":            words(0, 1, 10, 0),
			"blockNumber":     hexBlock(1 + int64(i)),
			"transactionHash": "0x" + strings.Repeat("0", 62) + hexByte(i),
			"logIndex":        hexBlock(int64(i)),
		})
	}
	return logs
}

func geckoTradeFixture(block int64, txHash, trader, fromToken, toToken, fromAmount, toAmount, ts string) map[string]any {
	return map[string]any{
		"id":   "bsc_" + strings.TrimPrefix(hexBlock(block), "0x") + "_" + txHash + "_1_" + strings.TrimPrefix(hexBlock(block+1000), "0x"),
		"type": "trade",
		"attributes": map[string]any{
			"block_number":       block,
			"tx_hash":            txHash,
			"tx_from_address":    trader,
			"from_token_amount":  fromAmount,
			"to_token_amount":    toAmount,
			"from_token_address": fromToken,
			"to_token_address":   toToken,
			"block_timestamp":    ts,
			"kind":               "buy",
			"volume_in_usd":      fromAmount,
		},
	}
}

func seedSwapLogsInRange(pool string, from, to int64) []map[string]any {
	logs := make([]map[string]any, 0, to-from+1)
	for block := from; block <= to; block++ {
		i := int(block)
		logs = append(logs, map[string]any{
			"address":         pool,
			"topics":          []string{swapTopic, topicAddress("0xcccccccccccccccccccccccccccccccccccccccc"), topicAddress(seedAddress(i))},
			"data":            words(0, 1, 10, 0),
			"blockNumber":     hexBlock(block),
			"transactionHash": "0x" + strings.Repeat("0", 62) + hexByte(i),
			"logIndex":        hexBlock(block),
		})
	}
	return logs
}

func seedSwapLogsInRangeForToken1(pool string, from, to int64) []map[string]any {
	logs := make([]map[string]any, 0, to-from+1)
	for block := from; block <= to; block++ {
		i := int(block)
		logs = append(logs, map[string]any{
			"address":         pool,
			"topics":          []string{swapTopic, topicAddress("0xcccccccccccccccccccccccccccccccccccccccc"), topicAddress(seedAddress(i))},
			"data":            words(1, 0, 0, 10),
			"blockNumber":     hexBlock(block),
			"transactionHash": "0x" + strings.Repeat("0", 62) + hexByte(i),
			"logIndex":        hexBlock(block),
		})
	}
	return logs
}

func rpcLogRange(raw string) (int64, int64) {
	from := int64(0)
	to := int64(0)
	for _, field := range []struct {
		name string
		dst  *int64
	}{
		{name: "fromBlock", dst: &from},
		{name: "toBlock", dst: &to},
	} {
		needle := `"` + field.name + `":"`
		idx := strings.Index(raw, needle)
		if idx < 0 {
			continue
		}
		start := idx + len(needle)
		end := strings.Index(raw[start:], `"`)
		if end < 0 {
			continue
		}
		*field.dst = hexToInt64(raw[start : start+end])
	}
	return from, to
}

func rpcBlockArg(raw string) (int64, bool) {
	needle := `"params":["`
	idx := strings.Index(raw, needle)
	if idx < 0 {
		return 0, false
	}
	start := idx + len(needle)
	end := strings.Index(raw[start:], `"`)
	if end < 0 {
		return 0, false
	}
	return hexToInt64(raw[start : start+end]), true
}

func rpcGetCodeBlockArg(raw string) (int64, bool) {
	var payload struct {
		Method string   `json:"method"`
		Params []string `json:"params"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return 0, false
	}
	if payload.Method != "eth_getCode" || len(payload.Params) < 2 {
		return 0, false
	}
	return hexToInt64(payload.Params[1]), true
}

func seedAddress(i int) string {
	return "0x" + strings.Repeat("0", 38) + hexByte(i)
}

func hexByte(i int) string {
	return strings.TrimPrefix(hexBlock(int64(i+1)), "0x")
}

func emptyRPCResult(raw string) any {
	if strings.Contains(raw, "eth_getLogs") {
		return []any{}
	}
	if strings.Contains(raw, "eth_getBlockByNumber") {
		return map[string]any{"timestamp": "0x1"}
	}
	if strings.Contains(raw, "eth_getCode") {
		return "0x"
	}
	return "0x1"
}

func words(values ...int64) string {
	var b strings.Builder
	b.WriteString("0x")
	for _, value := range values {
		b.WriteString(hexWord(value))
	}
	return b.String()
}

func wordsBig(values ...*big.Int) string {
	var b strings.Builder
	b.WriteString("0x")
	for _, value := range values {
		if value == nil {
			b.WriteString(hexWord(0))
			continue
		}
		if value.Sign() >= 0 {
			hex := value.Text(16)
			b.WriteString(strings.Repeat("0", 64-len(hex)) + hex)
			continue
		}
		n := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 256), value)
		hex := n.Text(16)
		b.WriteString(strings.Repeat("0", 64-len(hex)) + hex)
	}
	return b.String()
}

func rawUnits(value int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(value), big.NewInt(1_000_000_000_000_000_000))
}

func hexWord(value int64) string {
	hex := strings.TrimPrefix(hexBlock(value), "0x")
	return strings.Repeat("0", 64-len(hex)) + hex
}

func topicAddress(address string) string {
	return "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(address, "0x")
}

func jsonHTTPResponse(body any) (*http.Response, error) {
	raw, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(raw))),
	}, nil
}
