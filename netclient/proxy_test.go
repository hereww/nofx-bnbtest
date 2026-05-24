package netclient

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestNewProxyAwareHTTPClientUsesExplicitProxy(t *testing.T) {
	client, err := NewProxyAwareHTTPClient(5*time.Second, "http://user:pass@proxy.example:10811")
	if err != nil {
		t.Fatalf("NewProxyAwareHTTPClient returned error: %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	reqURL, _ := url.Parse("https://api.dexscreener.com/token-pairs/v1/bsc/0x812fc5119b772c6c7a66249a559f3614623f4444")
	req := &http.Request{URL: reqURL}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy returned error: %v", err)
	}
	if proxyURL == nil {
		t.Fatalf("proxy URL is nil")
	}
	if got := proxyURL.String(); got != "http://user:pass@proxy.example:10811" {
		t.Fatalf("proxy URL = %q", got)
	}
}

func TestNewProxyAwareHTTPClientRejectsInvalidProxy(t *testing.T) {
	if _, err := NewProxyAwareHTTPClient(5*time.Second, "://bad-proxy"); err == nil {
		t.Fatalf("expected invalid proxy error")
	}
}
