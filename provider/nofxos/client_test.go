package nofxos

import "testing"

func TestDefaultAuthKeyIsNotDeprecatedPublicKey(t *testing.T) {
	if DefaultAuthKey == "cm_568c67eae410d912c54c" {
		t.Fatal("default NofxOS auth key must not use the deprecated public key")
	}
}

func TestFailureMessageExtractsGatewayError(t *testing.T) {
	body := []byte(`{"success":false,"error":"data gateway unavailable"}`)

	got := failureMessage(body)
	if got != "data gateway unavailable" {
		t.Fatalf("unexpected failure message: %q", got)
	}
}

func TestValidateGatewayURLAllowsLocalGatewayAPI(t *testing.T) {
	for _, rawURL := range []string{
		"http://127.0.0.1:8090/api/ai500/list",
		"http://localhost:8090/api/oi/top-ranking?limit=3",
		"http://nofx-data-gateway:8090/api/price/ranking",
		"http://127.0.0.1:8090/health",
	} {
		if err := validateGatewayURL(rawURL); err != nil {
			t.Fatalf("expected %s to be allowed: %v", rawURL, err)
		}
	}
}

func TestValidateGatewayURLRejectsUnexpectedPathAndScheme(t *testing.T) {
	for _, rawURL := range []string{
		"file:///tmp/data",
		"http://127.0.0.1:8090/admin",
		"http://127.0.0.1:8090/../../etc/passwd",
	} {
		if err := validateGatewayURL(rawURL); err == nil {
			t.Fatalf("expected %s to be rejected", rawURL)
		}
	}
}
