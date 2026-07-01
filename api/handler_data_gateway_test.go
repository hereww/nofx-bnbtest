package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"nofx/config"
	"nofx/datagateway"
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

func TestDataGatewayProxyUsesEmbeddedGatewayWhenConfigured(t *testing.T) {
	config.Init()
	config.Get().DataGatewayToken = "embedded-secret"
	config.Get().DataGatewayURL = "http://unreachable.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	dgStore, err := datagateway.OpenStore(t.TempDir() + "/data-gateway.db")
	if err != nil {
		t.Fatalf("open data gateway store: %v", err)
	}
	dgSvc := datagateway.NewService(datagateway.Config{Token: "embedded-secret"}, dgStore)
	handler := datagateway.NewRouter(dgSvc, "embedded-secret")

	_, cancel := context.WithCancel(context.Background())
	srv := NewServer(nil, st, nil, 0)
	srv.EnableEmbeddedDataGateway(dgStore, dgSvc, handler, cancel)
	defer func() {
		if err := srv.Shutdown(); err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/data-gateway/health", nil)
	rec := httptest.NewRecorder()
	srv.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("embedded gateway status = %d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"service":"nofx-data-gateway"`) {
		t.Fatalf("unexpected embedded gateway body: %s", body)
	}
}
