package netclient

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NewProxyAwareHTTPClient returns an HTTP client that uses the explicit proxy
// when configured. With an empty proxy, Go's default transport is preserved,
// including standard HTTP_PROXY/HTTPS_PROXY environment handling.
func NewProxyAwareHTTPClient(timeout time.Duration, proxyRaw string) (*http.Client, error) {
	client := &http.Client{Timeout: timeout}

	proxyRaw = strings.TrimSpace(proxyRaw)
	if proxyRaw == "" {
		return client, nil
	}

	proxyURL, err := url.Parse(proxyRaw)
	if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
		return nil, fmt.Errorf("invalid proxy URL")
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("unsupported proxy scheme")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	client.Transport = transport
	return client, nil
}
