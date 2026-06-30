package onchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"nofx/config"
)

const (
	freeLogsProviderEtherscan = "etherscan_v2"
	etherscanBSCChainID       = "56"
)

var errFreeLogsNotConfigured = errors.New("free logs provider not configured")
var errFreeLogsBudgetExceeded = errors.New("free logs daily budget exceeded")

type freeLogsClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	rps        int
	dailyLimit int
	pageSize   int

	mu          sync.Mutex
	windowStart time.Time
	requests    int
}

type etherscanLogsResponse struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Result  json.RawMessage `json:"result"`
}

func (s *Service) newFreeLogsClient() *freeLogsClient {
	cfg := config.Get()
	return &freeLogsClient{
		httpClient: s.httpClient,
		baseURL:    strings.TrimRight(cfg.OnchainEtherscanBaseURL, "/"),
		apiKey:     strings.TrimSpace(cfg.OnchainEtherscanAPIKey),
		rps:        cfg.OnchainFreeLogsRPS,
		dailyLimit: cfg.OnchainFreeLogsDailyBudget,
		pageSize:   cfg.OnchainLogPageSize,
	}
}

func (c *freeLogsClient) configured() bool {
	return strings.TrimSpace(c.baseURL) != "" && strings.TrimSpace(c.apiKey) != ""
}

func (c *freeLogsClient) remainingBudget() int {
	if c == nil || c.dailyLimit <= 0 {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resetDailyWindowLocked(time.Now())
	remaining := c.dailyLimit - c.requests
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (c *freeLogsClient) getLogs(ctx context.Context, address string, topics []string, from, to int64) ([]rpcLog, error) {
	if c == nil || !c.configured() {
		return nil, errFreeLogsNotConfigured
	}
	c.ensureDefaults()

	var out []rpcLog
	for _, topic := range topics {
		page := 1
		for {
			logs, err := c.getLogsPage(ctx, address, topic, from, to, page)
			if err != nil {
				return out, err
			}
			out = append(out, logs...)
			if len(logs) < c.pageSize {
				break
			}
			page++
		}
	}
	return dedupeRPCLogs(out), nil
}

func (c *freeLogsClient) getTransferLogsForAddress(ctx context.Context, tokenAddress, walletAddress string, from, to int64) ([]rpcLog, error) {
	if c == nil || !c.configured() {
		return nil, errFreeLogsNotConfigured
	}
	out := make([]rpcLog, 0)
	for _, topicParam := range []struct {
		name  string
		value string
	}{
		{name: "topic1", value: addressTopic(walletAddress)},
		{name: "topic2", value: addressTopic(walletAddress)},
	} {
		page := 1
		for {
			logs, err := c.getLogsPageWithTopics(ctx, tokenAddress, map[string]string{
				"topic0":        transferTopic,
				topicParam.name: topicParam.value,
			}, from, to, page)
			if err != nil {
				return out, err
			}
			out = append(out, logs...)
			if len(logs) < c.pageSize {
				break
			}
			page++
		}
	}
	return dedupeRPCLogs(out), nil
}

func (c *freeLogsClient) getLogsPage(ctx context.Context, address, topic string, from, to int64, page int) ([]rpcLog, error) {
	return c.getLogsPageWithTopics(ctx, address, map[string]string{"topic0": topic}, from, to, page)
}

func (c *freeLogsClient) getLogsPageWithTopics(ctx context.Context, address string, topics map[string]string, from, to int64, page int) ([]rpcLog, error) {
	c.ensureDefaults()
	if err := c.reserveRequest(ctx); err != nil {
		return nil, err
	}
	u, err := c.buildGetLogsURL(address, topics, from, to, page)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NOFX-Onchain-FreeIndexer/1.0")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: etherscan logs request failed: %s", errRPCTransportRetryable, errWithoutURL(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errRPCRateLimited
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("etherscan logs status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded etherscanLogsResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	message := strings.ToLower(strings.TrimSpace(decoded.Message))
	if decoded.Status == "0" {
		if strings.Contains(message, "no records") || strings.Contains(strings.ToLower(string(decoded.Result)), "no records") {
			return nil, nil
		}
		if strings.Contains(message, "rate") || strings.Contains(message, "limit") || strings.Contains(strings.ToLower(string(decoded.Result)), "limit") {
			return nil, errRPCRateLimited
		}
		return nil, fmt.Errorf("etherscan logs error: %s %s", decoded.Message, strings.TrimSpace(string(decoded.Result)))
	}
	var logs []rpcLog
	if err := json.Unmarshal(decoded.Result, &logs); err != nil {
		return nil, err
	}
	for i := range logs {
		logs[i].BlockNumber = normalizeHexQuantity(logs[i].BlockNumber)
		logs[i].LogIndex = normalizeHexQuantity(logs[i].LogIndex)
	}
	return logs, nil
}

func errWithoutURL(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if idx := strings.Index(msg, "http"); idx >= 0 {
		msg = strings.TrimSpace(msg[:idx])
		msg = strings.TrimRight(msg, ":")
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "network error"
	}
	return msg
}

func (c *freeLogsClient) ensureDefaults() {
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	if c.rps <= 0 {
		c.rps = 2
	}
	if c.dailyLimit <= 0 {
		c.dailyLimit = 90000
	}
	if c.pageSize <= 0 || c.pageSize > 1000 {
		c.pageSize = 1000
	}
}

func (c *freeLogsClient) buildGetLogsURL(address string, topics map[string]string, from, to int64, page int) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("chainid", etherscanBSCChainID)
	q.Set("module", "logs")
	q.Set("action", "getLogs")
	q.Set("address", normalizeAddress(address))
	q.Set("fromBlock", strconv.FormatInt(from, 10))
	q.Set("toBlock", strconv.FormatInt(to, 10))
	for key, value := range topics {
		q.Set(key, strings.ToLower(strings.TrimSpace(value)))
	}
	q.Set("page", strconv.Itoa(page))
	q.Set("offset", strconv.Itoa(c.pageSize))
	q.Set("apikey", c.apiKey)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (c *freeLogsClient) reserveRequest(ctx context.Context) error {
	now := time.Now()
	c.mu.Lock()
	c.resetDailyWindowLocked(now)
	if c.requests >= c.dailyLimit {
		c.mu.Unlock()
		return errFreeLogsBudgetExceeded
	}
	c.requests++
	requests := c.requests
	rps := c.rps
	c.mu.Unlock()

	if requests > 1 && rps > 0 {
		delay := time.Second / time.Duration(rps)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (c *freeLogsClient) resetDailyWindowLocked(now time.Time) {
	if c.windowStart.IsZero() || now.Sub(c.windowStart) >= 24*time.Hour {
		c.windowStart = now
		c.requests = 0
	}
}

func dedupeRPCLogs(logs []rpcLog) []rpcLog {
	if len(logs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]rpcLog, 0, len(logs))
	for _, log := range logs {
		key := strings.ToLower(log.TransactionHash) + "|" + strings.ToLower(log.LogIndex)
		if key == "|" {
			key = strings.ToLower(log.BlockNumber) + "|" + strings.ToLower(log.Address) + "|" + strings.ToLower(log.Data)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, log)
	}
	return out
}

func normalizeHexQuantity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(strings.ToLower(value), "0x") {
		return strings.ToLower(value)
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return value
	}
	return hexBlock(n)
}

func addressTopic(address string) string {
	address = strings.TrimPrefix(normalizeAddress(address), "0x")
	if len(address) > 40 {
		address = address[len(address)-40:]
	}
	return "0x" + strings.Repeat("0", 64-len(address)) + address
}
