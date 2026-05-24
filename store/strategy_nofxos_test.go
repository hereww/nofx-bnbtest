package store

import "testing"

func TestDefaultStrategyDoesNotUseDeprecatedNofxOSPublicKey(t *testing.T) {
	cfg := GetDefaultStrategyConfig("zh")

	if cfg.Indicators.NofxOSAPIKey == "cm_568c67eae410d912c54c" {
		t.Fatal("default strategy must not use the deprecated NofxOS public key")
	}
	if cfg.Indicators.NofxOSAPIKey != "" {
		t.Fatalf("expected default NofxOS API key to be empty, got %q", cfg.Indicators.NofxOSAPIKey)
	}
}
