package datagateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Collector interface {
	Name() string
	Fetch(ctx context.Context) ([]MarketSnapshot, error)
}

type httpCollector struct {
	name       string
	marketType string
	url        string
	parse      func([]byte, time.Time) ([]MarketSnapshot, error)
}

func (c httpCollector) Name() string {
	return c.name + "-" + c.marketType
}

func (c httpCollector) Fetch(ctx context.Context) ([]MarketSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned status %d: %s", c.Name(), resp.StatusCode, string(body))
	}
	return c.parse(body, time.Now())
}

var defaultHTTPClient = &http.Client{Timeout: 18 * time.Second}

var (
	binanceFuturesBaseURL = "https://fapi.binance.com"
	binanceSpotBaseURL    = "https://api.binance.com"
	bybitBaseURL          = "https://api.bybit.com"
	okxBaseURL            = "https://www.okx.com"
	bitgetBaseURL         = "https://api.bitget.com"
	gateBaseURL           = "https://api.gateio.ws"
	kucoinFuturesBaseURL  = "https://api-futures.kucoin.com"
	kucoinSpotBaseURL     = "https://api.kucoin.com"
)

func defaultCollectors() []Collector {
	return []Collector{
		httpCollector{name: "binance", marketType: MarketTypeFuture, url: binanceFuturesBaseURL + "/fapi/v1/ticker/24hr", parse: parseBinanceFutures},
		httpCollector{name: "binance", marketType: MarketTypeSpot, url: binanceSpotBaseURL + "/api/v3/ticker/24hr", parse: parseBinanceSpot},
		httpCollector{name: "bybit", marketType: MarketTypeFuture, url: bybitBaseURL + "/v5/market/tickers?category=linear", parse: parseBybit(MarketTypeFuture)},
		httpCollector{name: "bybit", marketType: MarketTypeSpot, url: bybitBaseURL + "/v5/market/tickers?category=spot", parse: parseBybit(MarketTypeSpot)},
		httpCollector{name: "okx", marketType: MarketTypeFuture, url: okxBaseURL + "/api/v5/market/tickers?instType=SWAP", parse: parseOKX(MarketTypeFuture)},
		httpCollector{name: "okx", marketType: MarketTypeSpot, url: okxBaseURL + "/api/v5/market/tickers?instType=SPOT", parse: parseOKX(MarketTypeSpot)},
		httpCollector{name: "bitget", marketType: MarketTypeFuture, url: bitgetBaseURL + "/api/v2/mix/market/tickers?productType=USDT-FUTURES", parse: parseBitget(MarketTypeFuture)},
		httpCollector{name: "bitget", marketType: MarketTypeSpot, url: bitgetBaseURL + "/api/v2/spot/market/tickers", parse: parseBitget(MarketTypeSpot)},
		httpCollector{name: "gate", marketType: MarketTypeFuture, url: gateBaseURL + "/api/v4/futures/usdt/tickers", parse: parseGateFutures},
		httpCollector{name: "gate", marketType: MarketTypeSpot, url: gateBaseURL + "/api/v4/spot/tickers", parse: parseGateSpot},
		httpCollector{name: "kucoin", marketType: MarketTypeFuture, url: kucoinFuturesBaseURL + "/api/v1/contracts/active", parse: parseKuCoinFutures},
		httpCollector{name: "kucoin", marketType: MarketTypeSpot, url: kucoinSpotBaseURL + "/api/v1/market/allTickers", parse: parseKuCoinSpot},
	}
}

func fetchAll(ctx context.Context, collectors []Collector) ([]MarketSnapshot, []error) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var out []MarketSnapshot
	var errs []error
	for _, collector := range collectors {
		collector := collector
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := collector.Fetch(ctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", collector.Name(), err))
				return
			}
			out = append(out, items...)
		}()
	}
	wg.Wait()
	return out, errs
}

func parseBinanceFutures(body []byte, now time.Time) ([]MarketSnapshot, error) {
	var rows []struct {
		Symbol             string `json:"symbol"`
		LastPrice          string `json:"lastPrice"`
		PriceChangePercent string `json:"priceChangePercent"`
		QuoteVolume        string `json:"quoteVolume"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	items := make([]MarketSnapshot, 0, len(rows))
	for _, row := range rows {
		if !isUSDTMarket(row.Symbol) {
			continue
		}
		symbol := normalizeSymbol(row.Symbol)
		price := parseFloat(row.LastPrice)
		if price <= 0 {
			continue
		}
		items = append(items, MarketSnapshot{
			Exchange: "binance", MarketType: MarketTypeFuture, Symbol: symbol, Base: baseFromSymbol(symbol),
			Price: price, PriceChange24h: parseFloat(row.PriceChangePercent),
			QuoteVolume24h: parseFloat(row.QuoteVolume), FetchedAt: now,
		})
	}
	enrichBinanceFutures(context.Background(), items)
	return items, nil
}

func parseBinanceSpot(body []byte, now time.Time) ([]MarketSnapshot, error) {
	var rows []struct {
		Symbol             string `json:"symbol"`
		LastPrice          string `json:"lastPrice"`
		PriceChangePercent string `json:"priceChangePercent"`
		QuoteVolume        string `json:"quoteVolume"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	items := make([]MarketSnapshot, 0, len(rows))
	for _, row := range rows {
		if !isUSDTMarket(row.Symbol) {
			continue
		}
		symbol := normalizeSymbol(row.Symbol)
		price := parseFloat(row.LastPrice)
		if price <= 0 {
			continue
		}
		items = append(items, MarketSnapshot{
			Exchange: "binance", MarketType: MarketTypeSpot, Symbol: symbol, Base: baseFromSymbol(symbol),
			Price: price, PriceChange24h: parseFloat(row.PriceChangePercent),
			QuoteVolume24h: parseFloat(row.QuoteVolume), FetchedAt: now,
		})
	}
	return items, nil
}

func enrichBinanceFutures(ctx context.Context, items []MarketSnapshot) {
	limit := len(items)
	if limit > 120 {
		limit = 120
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		i := i
		if items[i].QuoteVolume24h < 2_000_000 {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			oi, oiDelta, funding := fetchBinanceOIAndFunding(ctx, items[i].Symbol, items[i].Price)
			items[i].OpenInterestValue = oi
			items[i].OpenInterestDelta = oiDelta
			items[i].FundingRate = funding
		}()
	}
	wg.Wait()
}

func fetchBinanceOIAndFunding(ctx context.Context, symbol string, price float64) (float64, float64, float64) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	oiURL := binanceFuturesBaseURL + "/fapi/v1/openInterest?symbol=" + symbol
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, oiURL, nil)
	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return 0, 0, 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var oiResp struct {
		OpenInterest string `json:"openInterest"`
	}
	_ = json.Unmarshal(body, &oiResp)
	oiValue := parseFloat(oiResp.OpenInterest) * price
	oiDelta := fetchBinanceOIDelta(ctx, symbol)

	fundingURL := binanceFuturesBaseURL + "/fapi/v1/premiumIndex?symbol=" + symbol
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, fundingURL, nil)
	resp, err = defaultHTTPClient.Do(req)
	if err != nil {
		return oiValue, oiDelta, 0
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var fundingResp struct {
		LastFundingRate string `json:"lastFundingRate"`
	}
	_ = json.Unmarshal(body, &fundingResp)
	return oiValue, oiDelta, parseFloat(fundingResp.LastFundingRate)
}

func fetchBinanceOIDelta(ctx context.Context, symbol string) float64 {
	histURL := binanceFuturesBaseURL + "/futures/data/openInterestHist?symbol=" + symbol + "&period=1h&limit=2"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, histURL, nil)
	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var rows []struct {
		SumOpenInterestValue string `json:"sumOpenInterestValue"`
		SumOpenInterest      string `json:"sumOpenInterest"`
	}
	if err := json.Unmarshal(body, &rows); err != nil || len(rows) < 2 {
		return 0
	}
	first := parseFloat(rows[0].SumOpenInterestValue)
	last := parseFloat(rows[len(rows)-1].SumOpenInterestValue)
	if first <= 0 || last <= 0 {
		return 0
	}
	return last - first
}

func parseBybit(marketType string) func([]byte, time.Time) ([]MarketSnapshot, error) {
	return func(body []byte, now time.Time) ([]MarketSnapshot, error) {
		var resp struct {
			Result struct {
				List []struct {
					Symbol            string `json:"symbol"`
					LastPrice         string `json:"lastPrice"`
					Price24hPcnt      string `json:"price24hPcnt"`
					Turnover24h       string `json:"turnover24h"`
					OpenInterestValue string `json:"openInterestValue"`
					FundingRate       string `json:"fundingRate"`
				} `json:"list"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		items := make([]MarketSnapshot, 0, len(resp.Result.List))
		for _, row := range resp.Result.List {
			if !isUSDTMarket(row.Symbol) {
				continue
			}
			symbol := normalizeSymbol(row.Symbol)
			price := parseFloat(row.LastPrice)
			if price <= 0 {
				continue
			}
			items = append(items, MarketSnapshot{
				Exchange: "bybit", MarketType: marketType, Symbol: symbol, Base: baseFromSymbol(symbol),
				Price: price, PriceChange24h: parseFloat(row.Price24hPcnt) * 100,
				QuoteVolume24h:    parseFloat(row.Turnover24h),
				OpenInterestValue: parseFloat(row.OpenInterestValue), FundingRate: parseFloat(row.FundingRate),
				FetchedAt: now,
			})
		}
		return items, nil
	}
}

func parseOKX(marketType string) func([]byte, time.Time) ([]MarketSnapshot, error) {
	return func(body []byte, now time.Time) ([]MarketSnapshot, error) {
		var resp struct {
			Data []struct {
				InstID    string `json:"instId"`
				Last      string `json:"last"`
				Open24h   string `json:"open24h"`
				VolCcy24h string `json:"volCcy24h"`
				Vol24h    string `json:"vol24h"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		items := make([]MarketSnapshot, 0, len(resp.Data))
		for _, row := range resp.Data {
			if !isUSDTMarket(row.InstID) {
				continue
			}
			symbol := strings.ReplaceAll(row.InstID, "-", "")
			symbol = strings.TrimSuffix(symbol, "SWAP")
			symbol = normalizeSymbol(symbol)
			price := parseFloat(row.Last)
			if price <= 0 {
				continue
			}
			quoteVol := parseFloat(row.VolCcy24h)
			if quoteVol <= 0 {
				quoteVol = parseFloat(row.Vol24h) * price
			}
			items = append(items, MarketSnapshot{
				Exchange: "okx", MarketType: marketType, Symbol: symbol, Base: baseFromSymbol(symbol),
				Price: price, PriceChange24h: pctDelta(price, parseFloat(row.Open24h)),
				QuoteVolume24h: quoteVol, FetchedAt: now,
			})
		}
		if marketType == MarketTypeFuture {
			enrichOKXFutures(context.Background(), items)
		}
		return items, nil
	}
}

func enrichOKXFutures(ctx context.Context, items []MarketSnapshot) {
	limit := len(items)
	if limit > 80 {
		limit = 80
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		i := i
		if items[i].QuoteVolume24h < 2_000_000 {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			oi, funding := fetchOKXOIAndFunding(ctx, items[i].Symbol)
			items[i].OpenInterestValue = oi
			items[i].FundingRate = funding
		}()
	}
	wg.Wait()
}

func fetchOKXOIAndFunding(ctx context.Context, symbol string) (float64, float64) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	instID := okxInstID(symbol)
	oiURL := okxBaseURL + "/api/v5/public/open-interest?instType=SWAP&instId=" + instID
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, oiURL, nil)
	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var oiResp struct {
		Data []struct {
			OIUSD string `json:"oiUsd"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &oiResp)
	var oiValue float64
	if len(oiResp.Data) > 0 {
		oiValue = parseFloat(oiResp.Data[0].OIUSD)
	}

	fundingURL := okxBaseURL + "/api/v5/public/funding-rate?instId=" + instID
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, fundingURL, nil)
	resp, err = defaultHTTPClient.Do(req)
	if err != nil {
		return oiValue, 0
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var fundingResp struct {
		Data []struct {
			FundingRate string `json:"fundingRate"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &fundingResp)
	if len(fundingResp.Data) == 0 {
		return oiValue, 0
	}
	return oiValue, parseFloat(fundingResp.Data[0].FundingRate)
}

func okxInstID(symbol string) string {
	base := strings.TrimSuffix(normalizeSymbol(symbol), "USDT")
	return base + "-USDT-SWAP"
}

func parseBitget(marketType string) func([]byte, time.Time) ([]MarketSnapshot, error) {
	return func(body []byte, now time.Time) ([]MarketSnapshot, error) {
		var resp struct {
			Data []struct {
				Symbol      string `json:"symbol"`
				LastPr      string `json:"lastPr"`
				Last        string `json:"last"`
				Change24h   string `json:"change24h"`
				QuoteVolume string `json:"quoteVolume"`
				UsdtVolume  string `json:"usdtVolume"`
				BaseVolume  string `json:"baseVolume"`
				FundingRate string `json:"fundingRate"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		items := make([]MarketSnapshot, 0, len(resp.Data))
		for _, row := range resp.Data {
			if !isUSDTMarket(row.Symbol) {
				continue
			}
			symbol := normalizeSymbol(row.Symbol)
			price := parseFloat(row.LastPr)
			if price <= 0 {
				price = parseFloat(row.Last)
			}
			if price <= 0 {
				continue
			}
			quoteVol := parseFloat(row.QuoteVolume)
			if quoteVol <= 0 {
				quoteVol = parseFloat(row.UsdtVolume)
			}
			if quoteVol <= 0 {
				quoteVol = parseFloat(row.BaseVolume) * price
			}
			items = append(items, MarketSnapshot{
				Exchange: "bitget", MarketType: marketType, Symbol: symbol, Base: baseFromSymbol(symbol),
				Price: price, PriceChange24h: parseFloat(row.Change24h) * 100,
				QuoteVolume24h: quoteVol, FundingRate: parseFloat(row.FundingRate), FetchedAt: now,
			})
		}
		return items, nil
	}
}

func parseGateFutures(body []byte, now time.Time) ([]MarketSnapshot, error) {
	var rows []struct {
		Contract       string `json:"contract"`
		Last           string `json:"last"`
		ChangePercent  string `json:"change_percentage"`
		Volume24hQuote string `json:"volume_24h_quote"`
		TotalSize      string `json:"total_size"`
		FundingRate    string `json:"funding_rate"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	items := make([]MarketSnapshot, 0, len(rows))
	for _, row := range rows {
		if !isUSDTMarket(row.Contract) {
			continue
		}
		symbol := normalizeSymbol(row.Contract)
		price := parseFloat(row.Last)
		if price <= 0 {
			continue
		}
		items = append(items, MarketSnapshot{
			Exchange: "gate", MarketType: MarketTypeFuture, Symbol: symbol, Base: baseFromSymbol(symbol),
			Price: price, PriceChange24h: parseFloat(row.ChangePercent),
			QuoteVolume24h: parseFloat(row.Volume24hQuote),
			FundingRate:    parseFloat(row.FundingRate), FetchedAt: now,
		})
	}
	return items, nil
}

func parseGateSpot(body []byte, now time.Time) ([]MarketSnapshot, error) {
	var rows []struct {
		CurrencyPair string `json:"currency_pair"`
		Last         string `json:"last"`
		Change       string `json:"change_percentage"`
		QuoteVolume  string `json:"quote_volume"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	items := make([]MarketSnapshot, 0, len(rows))
	for _, row := range rows {
		if !isUSDTMarket(row.CurrencyPair) {
			continue
		}
		symbol := normalizeSymbol(row.CurrencyPair)
		price := parseFloat(row.Last)
		if price <= 0 {
			continue
		}
		items = append(items, MarketSnapshot{
			Exchange: "gate", MarketType: MarketTypeSpot, Symbol: symbol, Base: baseFromSymbol(symbol),
			Price: price, PriceChange24h: parseFloat(row.Change),
			QuoteVolume24h: parseFloat(row.QuoteVolume), FetchedAt: now,
		})
	}
	return items, nil
}

func parseKuCoinFutures(body []byte, now time.Time) ([]MarketSnapshot, error) {
	var resp struct {
		Data []struct {
			Symbol         string  `json:"symbol"`
			LastTradePrice float64 `json:"lastTradePrice"`
			ChangeRate     float64 `json:"changeRate"`
			TurnoverOf24h  float64 `json:"turnoverOf24h"`
			OpenInterest   string  `json:"openInterest"`
			FundingFeeRate float64 `json:"fundingFeeRate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	items := make([]MarketSnapshot, 0, len(resp.Data))
	for _, row := range resp.Data {
		if !isUSDTMarket(row.Symbol) {
			continue
		}
		symbol := strings.TrimSuffix(row.Symbol, "M")
		symbol = normalizeSymbol(symbol)
		if row.LastTradePrice <= 0 {
			continue
		}
		items = append(items, MarketSnapshot{
			Exchange: "kucoin", MarketType: MarketTypeFuture, Symbol: symbol, Base: baseFromSymbol(symbol),
			Price: row.LastTradePrice, PriceChange24h: row.ChangeRate * 100,
			QuoteVolume24h: row.TurnoverOf24h,
			FundingRate:    row.FundingFeeRate, FetchedAt: now,
		})
	}
	return items, nil
}

func parseKuCoinSpot(body []byte, now time.Time) ([]MarketSnapshot, error) {
	var resp struct {
		Data struct {
			Ticker []struct {
				Symbol     string `json:"symbol"`
				Last       string `json:"last"`
				ChangeRate string `json:"changeRate"`
				VolValue   string `json:"volValue"`
			} `json:"ticker"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	items := make([]MarketSnapshot, 0, len(resp.Data.Ticker))
	for _, row := range resp.Data.Ticker {
		if !isUSDTMarket(row.Symbol) {
			continue
		}
		symbol := normalizeSymbol(row.Symbol)
		price := parseFloat(row.Last)
		if price <= 0 {
			continue
		}
		items = append(items, MarketSnapshot{
			Exchange: "kucoin", MarketType: MarketTypeSpot, Symbol: symbol, Base: baseFromSymbol(symbol),
			Price: price, PriceChange24h: parseFloat(row.ChangeRate) * 100,
			QuoteVolume24h: parseFloat(row.VolValue), FetchedAt: now,
		})
	}
	return items, nil
}
