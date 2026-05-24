package datagateway

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

func normalizeSymbol(symbol string) string {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	symbol = strings.ReplaceAll(symbol, "-", "")
	symbol = strings.ReplaceAll(symbol, "_", "")
	if symbol == "" {
		return ""
	}
	if !strings.HasSuffix(symbol, "USDT") {
		symbol += "USDT"
	}
	return symbol
}

func isUSDTMarket(symbol string) bool {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	switch {
	case strings.Contains(symbol, "-USDT"):
		return true
	case strings.Contains(symbol, "_USDT"):
		return true
	case strings.HasSuffix(symbol, "USDT"):
		return true
	case strings.HasSuffix(symbol, "USDTM"):
		return true
	default:
		return false
	}
}

func baseFromSymbol(symbol string) string {
	symbol = normalizeSymbol(symbol)
	return strings.TrimSuffix(symbol, "USDT")
}

func isExcludedAI500Base(base string) bool {
	base = strings.ToUpper(strings.TrimSpace(base))
	excluded := map[string]bool{
		"USDT": true, "USDC": true, "FDUSD": true, "BUSD": true, "DAI": true,
		"TUSD": true, "USDE": true, "USDP": true, "USD1": true,
		"EUR": true, "EURI": true, "TRY": true, "BRL": true, "GBP": true,
		"UP": true, "DOWN": true, "BULL": true, "BEAR": true,
	}
	if excluded[base] {
		return true
	}
	for _, suffix := range []string{"UP", "DOWN", "BULL", "BEAR", "3L", "3S", "5L", "5S"} {
		if strings.HasSuffix(base, suffix) && len(base) > len(suffix)+1 {
			return true
		}
	}
	return false
}

func parseFloat(v string) float64 {
	f, _ := strconv.ParseFloat(v, 64)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func pctDelta(current, previous float64) float64 {
	if previous <= 0 || current <= 0 {
		return 0
	}
	return (current - previous) / previous * 100
}

func durationKey(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "4h", "4hour", "4hours":
		return "4h"
	case "24h", "1d", "day":
		return "24h"
	default:
		return "1h"
	}
}

func durationAgo(key string) time.Duration {
	switch durationKey(key) {
	case "4h":
		return 4 * time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		return time.Hour
	}
}

func limitInt(v string, def, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		n = def
	}
	if max > 0 && n > max {
		n = max
	}
	return n
}

func sortAggregatedByScore(items []AggregatedSymbol) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].QuoteVolume24h > items[j].QuoteVolume24h
		}
		return items[i].Score > items[j].Score
	})
}

func sortAggregatedByOIDelta(items []AggregatedSymbol, desc bool) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].OpenInterestDelta == items[j].OpenInterestDelta {
			return items[i].OpenInterestValue > items[j].OpenInterestValue
		}
		if desc {
			return items[i].OpenInterestDelta > items[j].OpenInterestDelta
		}
		return items[i].OpenInterestDelta < items[j].OpenInterestDelta
	})
}

func sortAggregatedByPriceDelta(items []AggregatedSymbol, key string, desc bool) {
	sort.SliceStable(items, func(i, j int) bool {
		di := priceDeltaFor(items[i], key)
		dj := priceDeltaFor(items[j], key)
		if di == dj {
			return items[i].QuoteVolume24h > items[j].QuoteVolume24h
		}
		if desc {
			return di > dj
		}
		return di < dj
	})
}

func priceDeltaFor(item AggregatedSymbol, key string) float64 {
	switch durationKey(key) {
	case "4h":
		return item.PriceChange4h
	case "24h":
		return item.PriceChange24h
	default:
		return item.PriceChange1h
	}
}
