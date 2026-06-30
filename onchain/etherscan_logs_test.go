package onchain

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"nofx/config"
)

func TestEtherscanLogsClientBuildsV2GetLogsURL(t *testing.T) {
	client := &freeLogsClient{
		baseURL:  "https://api.etherscan.io/v2/api",
		apiKey:   "test-key",
		pageSize: 1000,
	}
	rawURL, err := client.buildGetLogsURL("0x1111111111111111111111111111111111111111", map[string]string{"topic0": swapTopic}, 10, 20, 2)
	if err != nil {
		t.Fatalf("build URL: %v", err)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	q := u.Query()
	assertQuery(t, q, "chainid", "56")
	assertQuery(t, q, "module", "logs")
	assertQuery(t, q, "action", "getLogs")
	assertQuery(t, q, "address", "0x1111111111111111111111111111111111111111")
	assertQuery(t, q, "fromBlock", "10")
	assertQuery(t, q, "toBlock", "20")
	assertQuery(t, q, "topic0", swapTopic)
	assertQuery(t, q, "page", "2")
	assertQuery(t, q, "offset", "1000")
	assertQuery(t, q, "apikey", "test-key")
}

func TestEtherscanLogsClientConvertsResponseToRPCLogs(t *testing.T) {
	config.Init()
	requests := 0
	client := &freeLogsClient{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if !strings.Contains(req.URL.RawQuery, "chainid=56") {
				t.Fatalf("missing chainid in %s", req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"status":"1",
					"message":"OK",
					"result":[{
						"address":"0x1111111111111111111111111111111111111111",
						"topics":["` + swapTopic + `"],
						"data":"0x",
						"blockNumber":"10",
						"transactionHash":"0xabc",
						"logIndex":"1"
					}]
				}`)),
			}, nil
		})},
		baseURL:    "https://api.etherscan.io/v2/api",
		apiKey:     "test-key",
		rps:        1000,
		dailyLimit: 10,
		pageSize:   1000,
	}
	logs, err := client.getLogs(context.Background(), "0x1111111111111111111111111111111111111111", []string{swapTopic}, 10, 20)
	if err != nil {
		t.Fatalf("getLogs: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if len(logs) != 1 {
		t.Fatalf("logs len = %d, want 1", len(logs))
	}
	if logs[0].TransactionHash != "0xabc" || logs[0].LogIndex != "0x1" || logs[0].BlockNumber != "0xa" {
		t.Fatalf("unexpected log: %+v", logs[0])
	}
}

func TestEtherscanLogsClientRateLimitError(t *testing.T) {
	client := &freeLogsClient{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`rate limited`)),
			}, nil
		})},
		baseURL:    "https://api.etherscan.io/v2/api",
		apiKey:     "test-key",
		rps:        1000,
		dailyLimit: 10,
		pageSize:   1000,
	}
	_, err := client.getLogs(context.Background(), "0x1111111111111111111111111111111111111111", []string{swapTopic}, 10, 20)
	if err != errRPCRateLimited {
		t.Fatalf("err = %v, want errRPCRateLimited", err)
	}
}

func assertQuery(t *testing.T, values url.Values, key, want string) {
	t.Helper()
	if got := values.Get(key); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}
