package manager

import (
	"testing"

	"nofx/crypto"
	"nofx/store"
)

func TestBuildAutoTraderConfigMapsBinanceTestnet(t *testing.T) {
	exchangeCfg := &store.Exchange{
		ID:           "exchange-1",
		ExchangeType: "binance",
		APIKey:       crypto.EncryptedString("api-key"),
		SecretKey:    crypto.EncryptedString("secret-key"),
		Testnet:      true,
	}

	cfg := buildAutoTraderConfig(
		&store.Trader{
			ID:                  "trader-1",
			Name:                "Binance Testnet Trader",
			ScanIntervalMinutes: 3,
			InitialBalance:      100,
			IsCrossMargin:       true,
			ShowInCompetition:   true,
		},
		&store.AIModel{
			Provider: "deepseek",
		},
		exchangeCfg,
		nil,
	)

	if !cfg.BinanceTestnet {
		t.Fatalf("expected BinanceTestnet to be true")
	}
	if cfg.BinanceAPIKey != "api-key" {
		t.Fatalf("expected BinanceAPIKey to be mapped from exchange config")
	}
	if cfg.BinanceSecretKey != "secret-key" {
		t.Fatalf("expected BinanceSecretKey to be mapped from exchange config")
	}
}
