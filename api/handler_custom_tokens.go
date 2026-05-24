package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"nofx/config"
	"nofx/logger"
	"nofx/netclient"

	"github.com/gin-gonic/gin"
)

var evmAddressRe = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

var defaultCustomTokenAddresses = []string{
	"0x600e3b55d5368c32a94f9372563318adb6a3f882",
	"0x812fc5119b772c6c7a66249a559f3614623f4444",
	"0x6d8d8df799279e761a4f49edc319b3bd50f14444",
	"0xa1ed61902f13e162305f59e1b2475e269e647777",
	"0x249130f5e2dd4cf278180c0df8273f3592ad1247",
	"0xd6e14208e929a38db47bae48f3ac8a420aff0999",
	"0x5f28b56a2f6e396a69fc912aec8d42d8afa17777",
}

type customTokenResponse struct {
	Address        string  `json:"address"`
	ChainID        string  `json:"chain_id,omitempty"`
	DexID          string  `json:"dex_id,omitempty"`
	Name           string  `json:"name,omitempty"`
	Symbol         string  `json:"symbol,omitempty"`
	QuoteSymbol    string  `json:"quote_symbol,omitempty"`
	PriceUSD       string  `json:"price_usd,omitempty"`
	LiquidityUSD   float64 `json:"liquidity_usd,omitempty"`
	Volume24hUSD   float64 `json:"volume_24h_usd,omitempty"`
	FDV            float64 `json:"fdv,omitempty"`
	MarketCap      float64 `json:"market_cap,omitempty"`
	PairURL        string  `json:"pair_url,omitempty"`
	PairAddress    string  `json:"pair_address,omitempty"`
	TradeSupported bool    `json:"trade_supported"`
	Reason         string  `json:"reason,omitempty"`
	Error          string  `json:"error,omitempty"`
}

type dexScreenerTokenResponse struct {
	Pairs []dexScreenerPair `json:"pairs"`
}

type dexScreenerPair struct {
	ChainID     string `json:"chainId"`
	DexID       string `json:"dexId"`
	URL         string `json:"url"`
	PairAddress string `json:"pairAddress"`
	BaseToken   struct {
		Address string `json:"address"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
	} `json:"baseToken"`
	QuoteToken struct {
		Symbol string `json:"symbol"`
	} `json:"quoteToken"`
	PriceUSD  string `json:"priceUsd"`
	Liquidity struct {
		USD float64 `json:"usd"`
	} `json:"liquidity"`
	Volume struct {
		H24 float64 `json:"h24"`
	} `json:"volume"`
	FDV       float64 `json:"fdv"`
	MarketCap float64 `json:"marketCap"`
}

// handleCustomTokens returns DEX market snapshots for configured EVM token addresses.
// These tokens are monitored as spot/DEX assets; they are not Binance Futures symbols.
func (s *Server) handleCustomTokens(c *gin.Context) {
	addresses := customTokenAddressesFromQuery(c.Query("addresses"))
	if len(addresses) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "no valid token addresses"})
		return
	}
	if len(addresses) > 20 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "max 20 token addresses"})
		return
	}

	tokens := make([]customTokenResponse, 0, len(addresses))
	for _, address := range addresses {
		tokens = append(tokens, fetchCustomTokenSnapshot(c.Request.Context(), address))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"source":  "dexscreener",
		"count":   len(tokens),
		"tokens":  tokens,
	})
}

func customTokenAddressesFromQuery(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return append([]string(nil), defaultCustomTokenAddresses...)
	}

	seen := make(map[string]bool)
	var out []string
	for _, part := range strings.Split(raw, ",") {
		address := strings.ToLower(strings.TrimSpace(part))
		if !evmAddressRe.MatchString(address) || seen[address] {
			continue
		}
		seen[address] = true
		out = append(out, address)
	}
	return out
}

func fetchCustomTokenSnapshot(ctx context.Context, address string) customTokenResponse {
	result := customTokenResponse{
		Address:        address,
		TradeSupported: false,
		Reason:         "DEX spot token; current trader execution supports exchange symbols such as BTCUSDT, not raw contract addresses",
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://api.dexscreener.com/latest/dex/tokens/%s", address), nil)
	if err != nil {
		result.Error = "failed to build request"
		return result
	}
	req.Header.Set("Accept", "application/json")

	resp, err := newCustomTokenHTTPClient().Do(req)
	if err != nil {
		result.Error = "token data unavailable"
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("upstream status %d", resp.StatusCode)
		return result
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		result.Error = "failed to read token data"
		return result
	}

	var decoded dexScreenerTokenResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		result.Error = "failed to parse token data"
		return result
	}

	pairs := make([]dexScreenerPair, 0, len(decoded.Pairs))
	for _, pair := range decoded.Pairs {
		if strings.EqualFold(pair.BaseToken.Address, address) {
			pairs = append(pairs, pair)
		}
	}
	if len(pairs) == 0 {
		result.Error = "no pair found"
		return result
	}

	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i].Liquidity.USD > pairs[j].Liquidity.USD
	})

	best := pairs[0]
	result.ChainID = best.ChainID
	result.DexID = best.DexID
	result.Name = best.BaseToken.Name
	result.Symbol = best.BaseToken.Symbol
	result.QuoteSymbol = best.QuoteToken.Symbol
	result.PriceUSD = best.PriceUSD
	result.LiquidityUSD = best.Liquidity.USD
	result.Volume24hUSD = best.Volume.H24
	result.FDV = best.FDV
	result.MarketCap = best.MarketCap
	result.PairURL = best.URL
	result.PairAddress = best.PairAddress
	return result
}

func newCustomTokenHTTPClient() *http.Client {
	client, err := netclient.NewProxyAwareHTTPClient(20*time.Second, config.Get().MarketHTTPProxy)
	if err != nil {
		logger.Warnf("invalid custom token HTTP proxy ignored: %v", err)
		return &http.Client{Timeout: 20 * time.Second}
	}
	return client
}
