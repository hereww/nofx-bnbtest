package api

import (
	"fmt"
	"strings"
	"testing"
)

func TestMaskSensitiveString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty", "", ""},
		{"short", "short", "****"},
		{"api key", "sk-1234567890abcdefghijklmnopqrstuvwxyz", "sk-1****wxyz"},
		{"private key", "0x1234567890abcdef1234567890abcdef12345678", "0x12****5678"},
		{"nine chars", "123456789", "1234****6789"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskSensitiveString(tt.input); got != tt.expected {
				t.Fatalf("MaskSensitiveString() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSanitizeExchangeConfigForLogNoPlaintextSecrets(t *testing.T) {
	secrets := map[string]string{
		"api_key":                     "binance_api_key_1234567890abcdef",
		"secret_key":                  "binance_secret_key_1234567890abcdef",
		"passphrase":                  "okx_passphrase_supersecret_value",
		"aster_private_key":           "aster_private_key_1234567890abcdef",
		"lighter_private_key":         "lighter_private_key_1234567890abcdef",
		"lighter_api_key_private_key": "lighter_api_key_private_key_1234567890abcdef",
	}

	exchanges := map[string]struct {
		Enabled                 bool   `json:"enabled"`
		APIKey                  string `json:"api_key"`
		SecretKey               string `json:"secret_key"`
		Passphrase              string `json:"passphrase"`
		Testnet                 bool   `json:"testnet"`
		HyperliquidWalletAddr   string `json:"hyperliquid_wallet_addr"`
		AsterUser               string `json:"aster_user"`
		AsterSigner             string `json:"aster_signer"`
		AsterPrivateKey         string `json:"aster_private_key"`
		LighterWalletAddr       string `json:"lighter_wallet_addr"`
		LighterPrivateKey       string `json:"lighter_private_key"`
		LighterAPIKeyPrivateKey string `json:"lighter_api_key_private_key"`
	}{
		"okx": {
			Enabled:                 true,
			APIKey:                  secrets["api_key"],
			SecretKey:               secrets["secret_key"],
			Passphrase:              secrets["passphrase"],
			AsterPrivateKey:         secrets["aster_private_key"],
			LighterPrivateKey:       secrets["lighter_private_key"],
			LighterAPIKeyPrivateKey: secrets["lighter_api_key_private_key"],
		},
	}

	rendered := fmt.Sprintf("%+v", SanitizeExchangeConfigForLog(exchanges))
	for field, secret := range secrets {
		if strings.Contains(rendered, secret) {
			t.Fatalf("sanitized log leaked plaintext %s: %q in %q", field, secret, rendered)
		}
	}
}
