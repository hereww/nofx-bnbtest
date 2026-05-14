package api

import (
	"encoding/json"
	"testing"

	"nofx/crypto"
	"nofx/store"
)

func TestSafeExchangeConfigFromStoreIncludesCredentialPresenceFlags(t *testing.T) {
	cfg := &store.Exchange{
		ID:                      "ex-1",
		ExchangeType:            "okx",
		AccountName:             "OKX Main",
		Name:                    "OKX Main",
		Type:                    "cex",
		Enabled:                 true,
		APIKey:                  crypto.EncryptedString("api-test-123"),
		SecretKey:               crypto.EncryptedString("secret-test-123"),
		Passphrase:              crypto.EncryptedString("passphrase-test-123"),
		Testnet:                 true,
		AsterPrivateKey:         crypto.EncryptedString("aster-private-key"),
		LighterPrivateKey:       crypto.EncryptedString("lighter-private-key"),
		LighterAPIKeyPrivateKey: crypto.EncryptedString("lighter-api-key-private-key"),
	}

	safe := safeExchangeConfigFromStore(cfg)
	if !safe.HasAPIKey {
		t.Fatalf("expected has_api_key to be true")
	}
	if !safe.HasSecretKey {
		t.Fatalf("expected has_secret_key to be true")
	}
	if !safe.HasPassphrase {
		t.Fatalf("expected has_passphrase to be true")
	}
	if !safe.Testnet {
		t.Fatalf("expected testnet to be true")
	}
	if !safe.HasAsterPrivateKey {
		t.Fatalf("expected has_aster_private_key to be true")
	}
	if !safe.HasLighterPrivateKey {
		t.Fatalf("expected has_lighter_private_key to be true")
	}
	if !safe.HasLighterAPIKey {
		t.Fatalf("expected has_lighter_api_key_private_key to be true")
	}
}

func TestExchangeRequestsParseTestnetFlag(t *testing.T) {
	var createReq CreateExchangeRequest
	if err := json.Unmarshal([]byte(`{
		"exchange_type":"binance",
		"account_name":"Binance Testnet",
		"api_key":"api-test-123",
		"secret_key":"secret-test-123",
		"testnet":true
	}`), &createReq); err != nil {
		t.Fatalf("failed to unmarshal create request: %v", err)
	}
	if !createReq.Testnet {
		t.Fatalf("expected create request testnet to be true")
	}

	var updateReq UpdateExchangeConfigRequest
	if err := json.Unmarshal([]byte(`{
		"exchanges":{
			"exchange-1":{
				"enabled":true,
				"api_key":"api-test-123",
				"secret_key":"secret-test-123",
				"testnet":true
			}
		}
	}`), &updateReq); err != nil {
		t.Fatalf("failed to unmarshal update request: %v", err)
	}
	if !updateReq.Exchanges["exchange-1"].Testnet {
		t.Fatalf("expected update request testnet to be true")
	}
}
