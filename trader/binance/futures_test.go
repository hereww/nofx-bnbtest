package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adshao/go-binance/v2/common"
	"github.com/adshao/go-binance/v2/futures"
	"github.com/stretchr/testify/assert"
	"nofx/trader/testutil"
	"nofx/trader/types"
)

// ============================================================
// 1. BinanceFuturesTestSuite - Inherits base test suite
// ============================================================

// BinanceFuturesTestSuite Binance Futures trader test suite
// Inherits TraderTestSuite and adds Binance Futures specific mock logic
type BinanceFuturesTestSuite struct {
	*testutil.TraderTestSuite // Embeds base test suite
	mockServer                *httptest.Server
}

func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// NewBinanceFuturesTestSuite Creates Binance Futures test suite
func NewBinanceFuturesTestSuite(t *testing.T) *BinanceFuturesTestSuite {
	// Create mock HTTP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return different mock responses based on URL path
		path := r.URL.Path

		var respBody interface{}

		switch {
		// Mock GetBalance - /fapi/v2/balance
		case path == "/fapi/v2/balance":
			respBody = []map[string]interface{}{
				{
					"accountAlias":       "test",
					"asset":              "USDT",
					"balance":            "10000.00",
					"crossWalletBalance": "10000.00",
					"crossUnPnl":         "100.50",
					"availableBalance":   "8000.00",
					"maxWithdrawAmount":  "8000.00",
				},
			}

		// Mock GetAccount - /fapi/v2/account
		case path == "/fapi/v2/account":
			respBody = map[string]interface{}{
				"totalWalletBalance":    "10000.00",
				"availableBalance":      "8000.00",
				"totalUnrealizedProfit": "100.50",
				"assets": []map[string]interface{}{
					{
						"asset":                  "USDT",
						"walletBalance":          "10000.00",
						"unrealizedProfit":       "100.50",
						"marginBalance":          "10100.50",
						"maintMargin":            "200.00",
						"initialMargin":          "2000.00",
						"positionInitialMargin":  "2000.00",
						"openOrderInitialMargin": "0.00",
						"crossWalletBalance":     "10000.00",
						"crossUnPnl":             "100.50",
						"availableBalance":       "8000.00",
						"maxWithdrawAmount":      "8000.00",
					},
				},
			}

		// Mock GetPositions - /fapi/v2/positionRisk
		case path == "/fapi/v2/positionRisk":
			respBody = []map[string]interface{}{
				{
					"symbol":           "BTCUSDT",
					"positionAmt":      "0.5",
					"entryPrice":       "50000.00",
					"markPrice":        "50500.00",
					"unRealizedProfit": "250.00",
					"liquidationPrice": "45000.00",
					"leverage":         "10",
					"positionSide":     "LONG",
				},
			}

		// Mock GetMarketPrice - /fapi/v1/ticker/price and /fapi/v2/ticker/price
		case path == "/fapi/v1/ticker/price" || path == "/fapi/v2/ticker/price":
			symbol := r.URL.Query().Get("symbol")
			if symbol == "" {
				// Return all prices
				respBody = []map[string]interface{}{
					{"Symbol": "BTCUSDT", "Price": "50000.00", "Time": 1234567890},
					{"Symbol": "ETHUSDT", "Price": "3000.00", "Time": 1234567890},
				}
			} else if symbol == "INVALIDUSDT" {
				// Return error
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"code": -1121,
					"msg":  "Invalid symbol.",
				})
				return
			} else {
				// Return single price (note: even with symbol parameter, return array)
				price := "50000.00"
				if symbol == "ETHUSDT" {
					price = "3000.00"
				}
				respBody = []map[string]interface{}{
					{
						"Symbol": symbol,
						"Price":  price,
						"Time":   1234567890,
					},
				}
			}

		// Mock ExchangeInfo - /fapi/v1/exchangeInfo
		case path == "/fapi/v1/exchangeInfo":
			respBody = map[string]interface{}{
				"symbols": []map[string]interface{}{
					{
						"symbol":             "BTCUSDT",
						"status":             "TRADING",
						"baseAsset":          "BTC",
						"quoteAsset":         "USDT",
						"pricePrecision":     2,
						"quantityPrecision":  3,
						"baseAssetPrecision": 8,
						"quotePrecision":     8,
						"filters": []map[string]interface{}{
							{
								"filterType": "PRICE_FILTER",
								"minPrice":   "0.01",
								"maxPrice":   "1000000",
								"tickSize":   "0.01",
							},
							{
								"filterType": "LOT_SIZE",
								"minQty":     "0.001",
								"maxQty":     "10000",
								"stepSize":   "0.001",
							},
						},
					},
					{
						"symbol":             "ETHUSDT",
						"status":             "TRADING",
						"baseAsset":          "ETH",
						"quoteAsset":         "USDT",
						"pricePrecision":     2,
						"quantityPrecision":  3,
						"baseAssetPrecision": 8,
						"quotePrecision":     8,
						"filters": []map[string]interface{}{
							{
								"filterType": "PRICE_FILTER",
								"minPrice":   "0.01",
								"maxPrice":   "100000",
								"tickSize":   "0.01",
							},
							{
								"filterType": "LOT_SIZE",
								"minQty":     "0.001",
								"maxQty":     "10000",
								"stepSize":   "0.001",
							},
						},
					},
				},
			}

		// Mock CreateOrder - /fapi/v1/order (POST)
		case path == "/fapi/v1/order" && r.Method == "POST":
			symbol := r.FormValue("symbol")
			if symbol == "" {
				symbol = "BTCUSDT"
			}
			respBody = map[string]interface{}{
				"orderId":       123456,
				"symbol":        symbol,
				"status":        "FILLED",
				"clientOrderId": r.FormValue("newClientOrderId"),
				"price":         r.FormValue("price"),
				"avgPrice":      r.FormValue("price"),
				"origQty":       r.FormValue("quantity"),
				"executedQty":   r.FormValue("quantity"),
				"cumQty":        r.FormValue("quantity"),
				"cumQuote":      "1000.00",
				"timeInForce":   r.FormValue("timeInForce"),
				"type":          r.FormValue("type"),
				"reduceOnly":    r.FormValue("reduceOnly") == "true",
				"side":          r.FormValue("side"),
				"positionSide":  r.FormValue("positionSide"),
				"stopPrice":     r.FormValue("stopPrice"),
				"workingType":   r.FormValue("workingType"),
			}

		// Mock CancelOrder - /fapi/v1/order (DELETE)
		case path == "/fapi/v1/order" && r.Method == "DELETE":
			respBody = map[string]interface{}{
				"orderId": 123456,
				"symbol":  r.URL.Query().Get("symbol"),
				"status":  "CANCELED",
			}

		// Mock ListOpenOrders - /fapi/v1/openOrders
		case path == "/fapi/v1/openOrders":
			respBody = []map[string]interface{}{}

		// Mock CancelAllOrders - /fapi/v1/allOpenOrders (DELETE)
		case path == "/fapi/v1/allOpenOrders" && r.Method == "DELETE":
			respBody = map[string]interface{}{
				"code": 200,
				"msg":  "The operation of cancel all open order is done.",
			}

		// Mock SetLeverage - /fapi/v1/leverage
		case path == "/fapi/v1/leverage":
			// Convert string to integer
			leverageStr := r.FormValue("leverage")
			leverage := 10 // default value
			if leverageStr != "" {
				// Note: here we return an integer directly, not a string
				fmt.Sscanf(leverageStr, "%d", &leverage)
			}
			respBody = map[string]interface{}{
				"leverage":         leverage,
				"maxNotionalValue": "1000000",
				"symbol":           r.FormValue("symbol"),
			}

		// Mock SetMarginType - /fapi/v1/marginType
		case path == "/fapi/v1/marginType":
			respBody = map[string]interface{}{
				"code": 200,
				"msg":  "success",
			}

		// Mock ChangePositionMode - /fapi/v1/positionSide/dual
		case path == "/fapi/v1/positionSide/dual":
			respBody = map[string]interface{}{
				"code": 200,
				"msg":  "success",
			}

		// Mock ServerTime - /fapi/v1/time
		case path == "/fapi/v1/time":
			respBody = map[string]interface{}{
				"serverTime": 1234567890000,
			}

		// Default: empty response
		default:
			respBody = map[string]interface{}{}
		}

		// Serialize response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respBody)
	}))

	// Create futures.Client and configure to use mock server
	client := futures.NewClient("test_api_key", "test_secret_key")
	client.BaseURL = mockServer.URL
	client.HTTPClient = mockServer.Client()
	installBinanceRateLimitTransport(&client.HTTPClient)

	// Create FuturesTrader
	traderInstance := &FuturesTrader{
		client:        client,
		cacheDuration: 0, // disable cache for testing
	}

	// Create base suite
	baseSuite := testutil.NewTraderTestSuite(t, traderInstance)

	return &BinanceFuturesTestSuite{
		TraderTestSuite: baseSuite,
		mockServer:      mockServer,
	}
}

// Cleanup cleans up resources
func (s *BinanceFuturesTestSuite) Cleanup() {
	if s.mockServer != nil {
		s.mockServer.Close()
	}
	s.TraderTestSuite.Cleanup()
}

// ============================================================
// 2. Run common tests using BinanceFuturesTestSuite
// ============================================================

// TestFuturesTrader_InterfaceCompliance tests interface compliance
func TestFuturesTrader_InterfaceCompliance(t *testing.T) {
	var _ types.Trader = (*FuturesTrader)(nil)
}

// TestFuturesTrader_CommonInterface runs all common interface tests using test suite
func TestFuturesTrader_CommonInterface(t *testing.T) {
	// Create test suite
	suite := NewBinanceFuturesTestSuite(t)
	defer suite.Cleanup()

	// Run all common interface tests
	suite.RunAllTests()
}

// ============================================================
// 3. Binance Futures specific unit tests
// ============================================================

// TestNewFuturesTrader tests creating Binance Futures trader
func TestNewFuturesTrader(t *testing.T) {
	// Create mock HTTP server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		var respBody interface{}

		switch path {
		case "/fapi/v1/time":
			respBody = map[string]interface{}{
				"serverTime": 1234567890000,
			}
		case "/fapi/v1/positionSide/dual":
			respBody = map[string]interface{}{
				"code": 200,
				"msg":  "success",
			}
		default:
			respBody = map[string]interface{}{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(respBody)
	}))
	defer mockServer.Close()

	// Test successful creation
	t1 := NewFuturesTrader("test_api_key", "test_secret_key", "test_user")

	// Modify client to use mock server
	t1.client.BaseURL = mockServer.URL
	t1.client.HTTPClient = mockServer.Client()

	assert.NotNil(t, t1)
	assert.NotNil(t, t1.client)
	assert.Equal(t, 60*time.Second, t1.cacheDuration)
}

func TestNewFuturesClientSelectsNetworkEndpoint(t *testing.T) {
	mainnetClient := newFuturesClient("test_api_key", "test_secret_key", false)
	assert.Equal(t, futures.BaseApiMainUrl, mainnetClient.BaseURL)

	testnetClient := newFuturesClient("test_api_key", "test_secret_key", true)
	assert.Equal(t, futures.BaseApiTestnetUrl, testnetClient.BaseURL)
}

func TestGetBalanceUsesWideRecvWindow(t *testing.T) {
	resetBinanceRateLimitForTest()

	var gotRecvWindow string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v2/account" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotRecvWindow = r.URL.Query().Get("recvWindow")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"totalWalletBalance":          "100.00",
			"availableBalance":            "95.00",
			"totalUnrealizedProfit":       "5.00",
			"totalInitialMargin":          "0",
			"totalMaintMargin":            "0",
			"totalMarginBalance":          "100.00",
			"totalPositionInitialMargin":  "0",
			"totalOpenOrderInitialMargin": "0",
			"totalCrossWalletBalance":     "100.00",
			"totalCrossUnPnl":             "0",
			"maxWithdrawAmount":           "95.00",
			"assets":                      []interface{}{},
			"positions":                   []interface{}{},
		})
	}))
	defer mockServer.Close()

	client := futures.NewClient("test_api_key", "test_secret_key")
	client.BaseURL = mockServer.URL
	client.HTTPClient = mockServer.Client()
	installBinanceRateLimitTransport(&client.HTTPClient)
	traderInstance := &FuturesTrader{
		client:        client,
		cacheDuration: 0,
	}

	_, err := traderInstance.GetBalance()
	assert.NoError(t, err)
	assert.Equal(t, "60000", gotRecvWindow)
}

func TestBinanceRateLimitCooldownBlocksBalanceRequest(t *testing.T) {
	resetBinanceRateLimitForTest()
	defer resetBinanceRateLimitForTest()

	banUntil := time.Now().Add(90 * time.Second).UnixMilli()
	err := recordBinanceAPIError(&common.APIError{
		Code:    -1003,
		Message: fmt.Sprintf("Way too many requests; IP(8.219.206.21) banned until %d. Please use the websocket for live updates to avoid bans.", banUntil),
	})
	assert.Error(t, err)

	var hits atomic.Int32
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	client := futures.NewClient("test_api_key", "test_secret_key")
	client.BaseURL = mockServer.URL
	client.HTTPClient = mockServer.Client()
	installBinanceRateLimitTransport(&client.HTTPClient)
	traderInstance := &FuturesTrader{
		client:        client,
		cacheDuration: 0,
	}

	_, err = traderInstance.GetBalance()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "rate-limit cooldown active")
	assert.Equal(t, int32(0), hits.Load())
}

func TestSyncBinanceServerTimeUsesLowestRTTSample(t *testing.T) {
	nowMs := time.Now().UnixMilli()
	var calls atomic.Int32
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fapi/v1/time" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		call := calls.Add(1)
		if call == 1 {
			time.Sleep(80 * time.Millisecond)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"serverTime": nowMs - 35000})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"serverTime": time.Now().UnixMilli()})
	}))
	defer mockServer.Close()

	client := futures.NewClient("test_api_key", "test_secret_key")
	client.BaseURL = mockServer.URL
	client.HTTPClient = mockServer.Client()

	syncBinanceServerTime(client)
	assert.Less(t, absInt64(client.TimeOffset), int64(5000))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := client.NewServerTimeService().Do(ctx)
	assert.NoError(t, err)
}

// TestCalculatePositionSize tests position size calculation
func TestCalculatePositionSize(t *testing.T) {
	ft := &FuturesTrader{}

	tests := []struct {
		name         string
		balance      float64
		riskPercent  float64
		price        float64
		leverage     int
		wantQuantity float64
	}{
		{
			name:         "normal calculation",
			balance:      10000,
			riskPercent:  2,
			price:        50000,
			leverage:     10,
			wantQuantity: 0.04, // (10000 * 0.02 * 10) / 50000 = 0.04
		},
		{
			name:         "high leverage",
			balance:      10000,
			riskPercent:  1,
			price:        3000,
			leverage:     20,
			wantQuantity: 0.6667, // (10000 * 0.01 * 20) / 3000 = 0.6667
		},
		{
			name:         "low risk",
			balance:      5000,
			riskPercent:  0.5,
			price:        50000,
			leverage:     5,
			wantQuantity: 0.0025, // (5000 * 0.005 * 5) / 50000 = 0.0025
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quantity := ft.CalculatePositionSize(tt.balance, tt.riskPercent, tt.price, tt.leverage)
			assert.InDelta(t, tt.wantQuantity, quantity, 0.0001, "calculated position size is incorrect")
		})
	}
}

// TestGetBrOrderID tests order ID generation
func TestGetBrOrderID(t *testing.T) {
	// Test 3 times to ensure each generated ID is unique
	ids := make(map[string]bool)
	for i := 0; i < 3; i++ {
		id := getBrOrderID()

		// Check format
		assert.True(t, strings.HasPrefix(id, "x-KzrpZaP9"), "order ID should start with x-KzrpZaP9")

		// Check length (should be <= 32)
		assert.LessOrEqual(t, len(id), 32, "order ID length should not exceed 32 characters")

		// Check uniqueness
		assert.False(t, ids[id], "order ID should be unique")
		ids[id] = true
	}
}
