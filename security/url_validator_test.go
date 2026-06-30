package security

import (
	"net"
	"testing"
)

func TestTrustedProxyDialTargetsFromEnvironmentAllowsConfiguredDockerProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://user:pass@nofx-proxy:18080")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")
	t.Setenv("all_proxy", "")

	targets := trustedProxyDialTargetsFromEnvironment()
	if !isTrustedProxyDialTarget("nofx-proxy:18080", targets) {
		t.Fatal("configured Docker proxy should be a trusted dial target")
	}
}

func TestTrustedProxyDialTargetsDoNotTrustOtherPrivateServices(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://nofx-proxy:18080")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")
	t.Setenv("all_proxy", "")

	targets := trustedProxyDialTargetsFromEnvironment()
	if isTrustedProxyDialTarget("database:5432", targets) {
		t.Fatal("unconfigured private service must not be trusted")
	}
	if !isPrivateIP(net.ParseIP("172.22.0.6")) {
		t.Fatal("Docker private IP must remain classified as private")
	}
}

func TestTrustedProxyDialTargetsUseDefaultPorts(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy.internal")
	t.Setenv("HTTPS_PROXY", "https://secure-proxy.internal")
	t.Setenv("ALL_PROXY", "socks5://socks-proxy.internal")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")
	t.Setenv("all_proxy", "")

	targets := trustedProxyDialTargetsFromEnvironment()
	for _, addr := range []string{
		"proxy.internal:80",
		"secure-proxy.internal:443",
		"socks-proxy.internal:1080",
	} {
		if !isTrustedProxyDialTarget(addr, targets) {
			t.Fatalf("expected %s to be trusted", addr)
		}
	}
}
