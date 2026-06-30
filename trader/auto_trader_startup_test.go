package trader

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nofx/hook"
	"nofx/store"

	"github.com/adshao/go-binance/v2/futures"
)

func TestNewAutoTraderAllowsZeroInitialBalanceWhenExchangeEquityIsZero(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fapi/v1/time":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"serverTime": 1234567890000})
		case "/fapi/v1/positionSide/dual":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "msg": "success"})
		case "/fapi/v2/account":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"totalWalletBalance":    "0.00",
				"availableBalance":      "0.00",
				"totalUnrealizedProfit": "0.00",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{})
		}
	}))
	defer mockServer.Close()

	originalHook, hadOriginalHook := hook.Hooks[hook.NEW_BINANCE_TRADER]
	hook.RegisterHook(hook.NEW_BINANCE_TRADER, func(args ...any) any {
		client := futures.NewClient("test_api_key", "test_secret_key")
		client.BaseURL = mockServer.URL
		client.HTTPClient = mockServer.Client()
		return &hook.NewBinanceTraderResult{Client: client}
	})
	defer func() {
		if hadOriginalHook {
			hook.Hooks[hook.NEW_BINANCE_TRADER] = originalHook
		} else {
			delete(hook.Hooks, hook.NEW_BINANCE_TRADER)
		}
	}()

	cfg := AutoTraderConfig{
		ID:               "zero-balance-trader",
		Name:             "Zero Balance Trader",
		AIModel:          "deepseek",
		Exchange:         "binance",
		BinanceAPIKey:    "test_api_key",
		BinanceSecretKey: "test_secret_key",
		ScanInterval:     3 * time.Minute,
		InitialBalance:   0,
		IsCrossMargin:    true,
		StrategyConfig:   defaultStartupTestStrategyConfig(),
	}

	at, err := NewAutoTrader(cfg, nil, "test-user")
	if err != nil {
		t.Fatalf("expected zero exchange equity to allow trader construction, got error: %v", err)
	}
	if at == nil {
		t.Fatalf("expected trader instance")
	}
	if got := at.GetStatus()["initial_balance"]; got != float64(0) {
		t.Fatalf("expected initial_balance to remain 0, got %#v", got)
	}
}

func defaultStartupTestStrategyConfig() *store.StrategyConfig {
	cfg := store.GetDefaultStrategyConfig("en")
	return &cfg
}
