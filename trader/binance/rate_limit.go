package binance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/common"
)

const binanceGenericRateLimitCooldown = 2 * time.Minute

var (
	binanceBanUntilPattern = regexp.MustCompile(`banned until\s+(\d{10,})`)

	binanceRateLimitMu     sync.Mutex
	binanceRateLimitUntil  time.Time
	binanceRateLimitReason string
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
	if err := checkBinanceRateLimitCooldown(); err != nil {
		return nil, err
	}

	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	res, err := base.RoundTrip(req)
	if err != nil || res == nil || res.StatusCode < http.StatusBadRequest || res.Body == nil {
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

	return res, nil
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

func recordBinanceAPIError(err error) error {
	if err == nil {
		return nil
	}

	until, reason, ok := parseBinanceRateLimitError(err)
	if !ok {
		return err
	}

	binanceRateLimitMu.Lock()
	if until.After(binanceRateLimitUntil) {
		binanceRateLimitUntil = until
		binanceRateLimitReason = reason
	}
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
}
