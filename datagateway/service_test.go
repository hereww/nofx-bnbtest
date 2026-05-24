package datagateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeSymbol(t *testing.T) {
	cases := map[string]string{
		"btc":      "BTCUSDT",
		"ethusdt":  "ETHUSDT",
		"sol-usdt": "SOLUSDT",
		"ada_usdt": "ADAUSDT",
	}
	for input, want := range cases {
		if got := normalizeSymbol(input); got != want {
			t.Fatalf("normalizeSymbol(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAI500ScoreSortAndFilter(t *testing.T) {
	now := time.Now()
	items := aggregateSnapshots([]MarketSnapshot{
		{Exchange: "binance", MarketType: MarketTypeFuture, Symbol: "BTCUSDT", Base: "BTC", Price: 100, PriceChange1h: 2, PriceChange4h: 5, PriceChange24h: 12, QuoteVolume24h: 100_000_000, OpenInterestValue: 50_000_000, OpenInterestDelta: 2_000_000, FetchedAt: now},
		{Exchange: "binance", MarketType: MarketTypeFuture, Symbol: "LOWUSDT", Base: "LOW", Price: 1, QuoteVolume24h: 10_000, OpenInterestValue: 0, FetchedAt: now},
	})
	var list []AggregatedSymbol
	for _, item := range items {
		list = append(list, item)
	}
	sortAggregatedByScore(list)
	if list[0].Symbol != "BTCUSDT" || list[0].Score <= 0 {
		t.Fatalf("expected BTCUSDT top scored item, got %+v", list)
	}
	if items["LOWUSDT"].Score != 0 {
		t.Fatalf("expected low-liquidity item score 0, got %.2f", items["LOWUSDT"].Score)
	}
}

func TestAI500DoesNotRequireOI(t *testing.T) {
	item := AggregatedSymbol{
		Symbol: "SOLUSDT", Base: "SOL", Price: 100, PriceChange24h: 8,
		QuoteVolume24h: 100_000_000, HasFutureData: true, ExchangeCount: 2,
	}
	if scoreAI500(item) <= 0 {
		t.Fatal("expected liquid future market to be AI500-eligible even when OI is unavailable")
	}
}

func TestAI500ExcludesStableAndLeveragedTokens(t *testing.T) {
	stable := AggregatedSymbol{
		Symbol: "USDCUSDT", Base: "USDC", Price: 1, QuoteVolume24h: 100_000_000,
		OpenInterestValue: 10_000_000, HasFutureData: true,
	}
	leveraged := AggregatedSymbol{
		Symbol: "BTCUPUSDT", Base: "BTCUP", Price: 10, QuoteVolume24h: 100_000_000,
		OpenInterestValue: 10_000_000, HasFutureData: true,
	}
	if scoreAI500(stable) != 0 {
		t.Fatal("expected stablecoin to be excluded from AI500")
	}
	if scoreAI500(leveraged) != 0 {
		t.Fatal("expected leveraged token to be excluded from AI500")
	}
}

func TestOIRankingDirection(t *testing.T) {
	items := []AggregatedSymbol{
		{Symbol: "AUSDT", OpenInterestValue: 10_000_000, OpenInterestDelta: 2_000_000},
		{Symbol: "BUSDT", OpenInterestValue: 10_000_000, OpenInterestDelta: -3_000_000},
		{Symbol: "CUSDT", OpenInterestValue: 10_000_000, OpenInterestDelta: 1_000_000},
	}
	sortAggregatedByOIDelta(items, true)
	if items[0].Symbol != "AUSDT" {
		t.Fatalf("top OI ranking first = %s", items[0].Symbol)
	}
	sortAggregatedByOIDelta(items, false)
	if items[0].Symbol != "BUSDT" {
		t.Fatalf("low OI ranking first = %s", items[0].Symbol)
	}
}

func TestOIRankingUsesFallbackOnlyWhenNoDirectionalDeltaExists(t *testing.T) {
	withLowDelta := []AggregatedSymbol{
		{Symbol: "AUSDT", OpenInterestValue: 100_000_000, OpenInterestDelta: 0},
		{Symbol: "BUSDT", OpenInterestValue: 10_000_000, OpenInterestDelta: -10_000},
	}
	useFallback := shouldUseOIFallback(withLowDelta, "low")
	if useFallback {
		t.Fatal("expected real negative OI delta to disable low-ranking fallback")
	}
	sortOIRanking(withLowDelta, "low", useFallback)
	if withLowDelta[0].Symbol != "BUSDT" {
		t.Fatalf("expected real negative delta first, got %+v", withLowDelta)
	}

	withoutDelta := []AggregatedSymbol{
		{Symbol: "AUSDT", OpenInterestValue: 100_000_000, OpenInterestDelta: 0, PriceChange24h: -1},
		{Symbol: "BUSDT", OpenInterestValue: 10_000_000, OpenInterestDelta: 0, PriceChange24h: -5},
	}
	useFallback = shouldUseOIFallback(withoutDelta, "low")
	if !useFallback {
		t.Fatal("expected fallback when no negative OI delta exists")
	}
	sortOIRanking(withoutDelta, "low", useFallback)
	if withoutDelta[0].Symbol == "" {
		t.Fatalf("expected fallback ranking to keep candidates: %+v", withoutDelta)
	}
}

func TestNetFlowEstimateDirection(t *testing.T) {
	inflow := AggregatedSymbol{Symbol: "AUSDT", FutureQuoteVolume24h: 100_000_000, PriceChange1h: 2, OpenInterestDelta: 1_000_000}
	outflow := AggregatedSymbol{Symbol: "BUSDT", FutureQuoteVolume24h: 100_000_000, PriceChange1h: -2, OpenInterestDelta: -1_000_000}
	if estimateNetFlow(inflow, "institution", "future") <= 0 {
		t.Fatal("expected positive estimated institution future flow")
	}
	if estimateNetFlow(outflow, "institution", "future") >= 0 {
		t.Fatal("expected negative estimated institution future flow")
	}
}

func TestAPIAuthAndShape(t *testing.T) {
	svc, closeStore := testServiceWithMarketData(t)
	defer closeStore()

	router := NewRouter(svc, "secret")
	req := httptest.NewRequest(http.MethodGet, "/api/ai500/list?limit=5", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status without token = %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/ai500/list?limit=5", nil)
	req.Header.Set("X-Gateway-Token", "secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status with token = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"success":true`) || !strings.Contains(w.Body.String(), `"coins"`) {
		t.Fatalf("unexpected ai500 shape: %s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/coin/BTC?include=netflow,oi,price,ai500&auth=secret", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"netflow"`) || !strings.Contains(w.Body.String(), `"ai500"`) {
		t.Fatalf("unexpected coin response: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIOpenAccessWhenTokenIsEmpty(t *testing.T) {
	svc, closeStore := testServiceWithMarketData(t)
	defer closeStore()

	router := NewRouter(svc, "")
	req := httptest.NewRequest(http.MethodGet, "/api/ai500/list?limit=1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status without configured token = %d body=%s", w.Code, w.Body.String())
	}
}

func TestCompatibleEndpointsShape(t *testing.T) {
	svc, closeStore := testServiceWithMarketData(t)
	defer closeStore()

	router := NewRouter(svc, "")
	endpoints := map[string][]string{
		"/health":                 {`"service":"nofx-data-gateway"`},
		"/api/ai500/list?limit=5": {`"success":true`, `"coins"`},
		"/api/ai500/BTC":          {`"success":true`, `"info"`, `"current_price"`},
		"/api/ai500/stats":        {`"success":true`, `"statistics"`, `"top_coins"`},
		"/api/oi/top-ranking?duration=1h&limit=5": {`"success":true`, `"positions"`, `"rank_type":"top"`},
		"/api/oi/low-ranking?duration=1h&limit=5": {`"success":true`, `"positions"`, `"rank_type":"low"`},
		"/api/oi/top": {`"success":true`, `"positions"`},
		"/api/netflow/top-ranking?duration=1h&limit=5": {`"success":true`, `"netflows"`, `"estimated":true`},
		"/api/netflow/low-ranking?duration=1h&limit=5": {`"success":true`, `"netflows"`, `"estimated":true`},
		"/api/netflow/top": {`"success":true`, `"netflows"`},
		"/api/price/ranking?duration=1h,4h,24h&limit=5":           {`"success":true`, `"durations"`, `"1h"`, `"24h"`},
		"/api/coin/BTC?include=netflow,oi,price,ai500":            {`"success":true`, `"price_change"`, `"ai500"`},
		"/api/netflow/top-ranking?type=personal&trade=spot":       {`"success":true`, `"type":"personal"`, `"trade":"spot"`},
		"/api/netflow/top-ranking?type=institution&trade=futures": {`"success":true`, `"trade":"future"`},
	}

	for endpoint, fragments := range endpoints {
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", endpoint, w.Code, w.Body.String())
		}
		for _, fragment := range fragments {
			if !strings.Contains(w.Body.String(), fragment) {
				t.Fatalf("%s missing %s in body=%s", endpoint, fragment, w.Body.String())
			}
		}
	}
}

func TestAPIParameterValidation(t *testing.T) {
	svc, closeStore := testServiceWithMarketData(t)
	defer closeStore()

	router := NewRouter(svc, "")
	for _, endpoint := range []string{
		"/api/oi/top-ranking?duration=2h",
		"/api/oi/top-ranking?limit=abc",
		"/api/netflow/top-ranking?type=whale",
		"/api/netflow/top-ranking?trade=margin",
		"/api/price/ranking?duration=1h,2h",
		"/api/price/ranking?limit=0",
	} {
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s, want 400", endpoint, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"success":false`) {
			t.Fatalf("%s unexpected error shape: %s", endpoint, w.Body.String())
		}
	}
}

func TestOKXFuturesEnrichment(t *testing.T) {
	previousOKXBaseURL := okxBaseURL
	previousHTTPClient := defaultHTTPClient
	defer func() {
		okxBaseURL = previousOKXBaseURL
		defaultHTTPClient = previousHTTPClient
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v5/public/open-interest":
			if got := r.URL.Query().Get("instId"); got != "BTC-USDT-SWAP" {
				t.Fatalf("unexpected instId for OI: %s", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"oiUsd":"1234567.89"}]}`))
		case "/api/v5/public/funding-rate":
			if got := r.URL.Query().Get("instId"); got != "BTC-USDT-SWAP" {
				t.Fatalf("unexpected instId for funding: %s", got)
			}
			_, _ = w.Write([]byte(`{"data":[{"fundingRate":"0.00012"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	okxBaseURL = server.URL
	defaultHTTPClient = server.Client()

	body := []byte(`{"data":[{"instId":"BTC-USDT-SWAP","last":"100","open24h":"90","volCcy24h":"5000000","vol24h":"50000"}]}`)
	items, err := parseOKX(MarketTypeFuture)(body, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one OKX item, got %d", len(items))
	}
	if items[0].OpenInterestValue != 1234567.89 {
		t.Fatalf("unexpected OI value: %+v", items[0])
	}
	if items[0].FundingRate != 0.00012 {
		t.Fatalf("unexpected funding rate: %+v", items[0])
	}
}

func testServiceWithMarketData(t *testing.T) (*Service, func()) {
	t.Helper()
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(Config{}, store)
	now := time.Now()
	svc.snapshots = []MarketSnapshot{}
	svc.aggregated = aggregateSnapshots([]MarketSnapshot{
		{Exchange: "binance", MarketType: MarketTypeFuture, Symbol: "BTCUSDT", Base: "BTC", Price: 100, PriceChange1h: 1, PriceChange4h: 3, PriceChange24h: 8, QuoteVolume24h: 200_000_000, OpenInterestValue: 60_000_000, OpenInterestDelta: 3_000_000, FetchedAt: now},
		{Exchange: "okx", MarketType: MarketTypeFuture, Symbol: "ETHUSDT", Base: "ETH", Price: 50, PriceChange1h: -2, PriceChange4h: -3, PriceChange24h: -5, QuoteVolume24h: 150_000_000, OpenInterestValue: 40_000_000, OpenInterestDelta: -2_000_000, FetchedAt: now},
		{Exchange: "gate", MarketType: MarketTypeSpot, Symbol: "BTCUSDT", Base: "BTC", Price: 101, PriceChange1h: 0.8, PriceChange4h: 2.5, PriceChange24h: 7, QuoteVolume24h: 50_000_000, FetchedAt: now},
	})
	return svc, func() { _ = store.Close() }
}

func assertJSONSuccess(t *testing.T, body string) {
	t.Helper()
	var payload struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("invalid json: %v body=%s", err, body)
	}
	if !payload.Success {
		t.Fatalf("expected success body=%s", body)
	}
}
