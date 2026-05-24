package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"nofx/config"
	"nofx/store"
)

func TestBuildDataGatewayURL(t *testing.T) {
	config.Init()
	config.Get().DataGatewayURL = "http://gateway.local:8090/base/"

	got, err := buildDataGatewayURL("/api/ai500/list", "limit=6")
	if err != nil {
		t.Fatalf("buildDataGatewayURL returned error: %v", err)
	}
	want := "http://gateway.local:8090/base/api/ai500/list?limit=6"
	if got != want {
		t.Fatalf("url mismatch: got %q want %q", got, want)
	}

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("proxy url should parse: %v", err)
	}
	if parsed.RawQuery != "limit=6" {
		t.Fatalf("query not preserved: %q", parsed.RawQuery)
	}
}

func TestBuildDataGatewayURLRejectsUnexpectedPath(t *testing.T) {
	config.Init()
	config.Get().DataGatewayURL = "http://gateway.local:8090"

	if _, err := buildDataGatewayURL("/admin", ""); err == nil {
		t.Fatal("expected unsupported path error")
	}
}

func TestDataGatewayProxyForwardsToken(t *testing.T) {
	var gotToken string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Gateway-Token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer upstream.Close()

	config.Init()
	config.Get().DataGatewayURL = upstream.URL
	config.Get().DataGatewayToken = "proxy-secret"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()
	srv := NewServer(nil, st, nil, 0)

	req := httptest.NewRequest(http.MethodGet, "/api/data-gateway/api/ai500/list", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotToken != "proxy-secret" {
		t.Fatalf("token not forwarded: %q", gotToken)
	}
}
