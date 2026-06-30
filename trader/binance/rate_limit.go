package binance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/common"
)

const (
	binanceGenericRateLimitCooldown     = 2 * time.Minute
	binanceDefaultRESTMinInterval       = 600 * time.Millisecond
	binanceDefaultUsedWeight1MSoftLimit = 1800
	binanceDefaultUsedWeight1MHardLimit = 2200
)

var (
	binanceBanUntilPattern = regexp.MustCompile(`banned until\s+(\d{10,})`)

	binanceRateLimitMu     sync.Mutex
	binanceRateLimitUntil  time.Time
	binanceRateLimitReason string

	binanceRESTThrottleMu    sync.Mutex
	binanceRESTNextAllowedAt time.Time
)

type binanceRateLimitTransport struct {
	base http.RoundTripper
}

func installBinanceRateLimitTransport(clientHTTP **http.Client) {
	if clientHTTP == nil {
		return
	}

	if *clientHTTP == nil {
		*clientHTTP = &http.Client{}
	}

	current := *clientHTTP
	base := current.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	if _, ok := base.(*binanceRateLimitTransport); ok {
		return
	}

	*clientHTTP = &http.Client{
		Transport:     &binanceRateLimitTransport{base: base},
		CheckRedirect: current.CheckRedirect,
		Jar:           current.Jar,
		Timeout:       current.Timeout,
	}
}

func (t *binanceRateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := waitBinanceRESTSlot(req); err != nil {
		return nil, err
	}

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	res, err := base.RoundTrip(req)
	if err != nil || res == nil || res.StatusCode < http.StatusBadRequest || res.Body == nil {
		recordBinanceUsedWeightHeaders(res)
		return res, err
	}

	body, readErr := io.ReadAll(res.Body)
	closeErr := res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(body))
	if readErr != nil {
		return res, nil
	}
	if closeErr != nil {
		return res, nil
	}

	apiErr := new(common.APIError)
	if jsonErr := json.Unmarshal(body, apiErr); jsonErr == nil && apiErr.IsValid() {
		_ = recordBinanceAPIError(apiErr)
	}
	recordBinanceUsedWeightHeaders(res)

	return res, nil
}

func waitBinanceRESTSlot(req *http.Request) error {
	if err := checkBinanceRateLimitCooldown(); err != nil {
		return err
	}
	if req != nil && isLocalBinanceTestRequest(req) {
		return nil
	}

	interval := binanceRESTMinInterval()
	if interval <= 0 {
		return nil
	}

	for {
		binanceRESTThrottleMu.Lock()
		now := time.Now()
		if binanceRESTNextAllowedAt.IsZero() || !now.Before(binanceRESTNextAllowedAt) {
			binanceRESTNextAllowedAt = now.Add(interval)
			binanceRESTThrottleMu.Unlock()
			return checkBinanceRateLimitCooldown()
		}
		wait := time.Until(binanceRESTNextAllowedAt)
		binanceRESTThrottleMu.Unlock()

		if wait > 0 {
			time.Sleep(wait)
		}
		if err := checkBinanceRateLimitCooldown(); err != nil {
			return err
		}
	}
}

func isLocalBinanceTestRequest(req *http.Request) bool {
	host := strings.ToLower(req.URL.Hostname())
	return host == "127.0.0.1" || host == "localhost" || strings.HasSuffix(host, ".local")
}

func binanceRESTMinInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("BINANCE_REST_MIN_INTERVAL_MS"))
	if raw == "" {
		return binanceDefaultRESTMinInterval
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 0 {
		return binanceDefaultRESTMinInterval
	}
	return time.Duration(ms) * time.Millisecond
}

func recordBinanceUsedWeightHeaders(res *http.Response) {
	if res == nil {
		return
	}
	used, ok := parseBinanceUsedWeight1M(res.Header)
	if !ok {
		return
	}

	soft := binanceUsedWeightLimitFromEnv("BINANCE_USED_WEIGHT_1M_SOFT_LIMIT", binanceDefaultUsedWeight1MSoftLimit)
	hard := binanceUsedWeightLimitFromEnv("BINANCE_USED_WEIGHT_1M_HARD_LIMIT", binanceDefaultUsedWeight1MHardLimit)
	switch {
	case hard > 0 && used >= hard:
		setBinanceRateLimitCooldown(time.Now().Add(60*time.Second), fmt.Sprintf("used weight 1m is high: %d >= %d", used, hard))
	case soft > 0 && used >= soft:
		setBinanceRateLimitCooldown(time.Now().Add(15*time.Second), fmt.Sprintf("used weight 1m is elevated: %d >= %d", used, soft))
	}
}

func parseBinanceUsedWeight1M(header http.Header) (int, bool) {
	for key, values := range header {
		if !strings.EqualFold(key, "X-MBX-USED-WEIGHT-1M") || len(values) == 0 {
			continue
		}
		used, err := strconv.Atoi(strings.TrimSpace(values[0]))
		if err != nil {
			return 0, false
		}
		return used, true
	}
	return 0, false
}

func binanceUsedWeightLimitFromEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func checkBinanceRateLimitCooldown() error {
	binanceRateLimitMu.Lock()
	defer binanceRateLimitMu.Unlock()

	if binanceRateLimitUntil.IsZero() || time.Now().After(binanceRateLimitUntil) {
		return nil
	}

	return fmt.Errorf(
		"Binance API rate-limit cooldown active until %s: %s",
		binanceRateLimitUntil.Local().Format("2006-01-02 15:04:05 MST"),
		binanceRateLimitReason,
	)
}

func setBinanceRateLimitCooldown(until time.Time, reason string) {
	if until.IsZero() {
		return
	}

	binanceRateLimitMu.Lock()
	if until.After(binanceRateLimitUntil) {
		binanceRateLimitUntil = until
		binanceRateLimitReason = reason
	}
	binanceRateLimitMu.Unlock()
}

func recordBinanceAPIError(err error) error {
	if err == nil {
		return nil
	}

	until, reason, ok := parseBinanceRateLimitError(err)
	if !ok {
		return err
	}

	setBinanceRateLimitCooldown(until, reason)

	binanceRateLimitMu.Lock()
	activeUntil := binanceRateLimitUntil
	activeReason := binanceRateLimitReason
	binanceRateLimitMu.Unlock()

	return fmt.Errorf(
		"Binance API rate limited; cooling down until %s: %s: %w",
		activeUntil.Local().Format("2006-01-02 15:04:05 MST"),
		activeReason,
		err,
	)
}

func parseBinanceRateLimitError(err error) (time.Time, string, bool) {
	var apiErr *common.APIError
	if !errors.As(err, &apiErr) {
		return time.Time{}, "", false
	}

	msg := strings.TrimSpace(apiErr.Message)
	lower := strings.ToLower(msg)
	if apiErr.Code != -1003 && !strings.Contains(lower, "too many requests") && !strings.Contains(lower, "rate limit") {
		return time.Time{}, "", false
	}

	if match := binanceBanUntilPattern.FindStringSubmatch(lower); len(match) == 2 {
		ms, parseErr := strconv.ParseInt(match[1], 10, 64)
		if parseErr == nil && ms > 0 {
			return time.UnixMilli(ms), msg, true
		}
	}

	return time.Now().Add(binanceGenericRateLimitCooldown), msg, true
}

func resetBinanceRateLimitForTest() {
	binanceRateLimitMu.Lock()
	binanceRateLimitUntil = time.Time{}
	binanceRateLimitReason = ""
	binanceRateLimitMu.Unlock()

	binanceRESTThrottleMu.Lock()
	binanceRESTNextAllowedAt = time.Time{}
	binanceRESTThrottleMu.Unlock()
}
