package onchain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"nofx/store"
)

type dexScreenerTokenResponse struct {
	Pairs []dexScreenerPair `json:"pairs"`
}

type dexScreenerPair struct {
	ChainID       string `json:"chainId"`
	DexID         string `json:"dexId"`
	URL           string `json:"url"`
	PairAddress   string `json:"pairAddress"`
	PairCreatedAt int64  `json:"pairCreatedAt"`
	BaseToken     struct {
		Address string `json:"address"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
	} `json:"baseToken"`
	QuoteToken struct {
		Address string `json:"address"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
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

type goPlusResponse struct {
	Code    int                          `json:"code"`
	Message string                       `json:"message"`
	Result  map[string]goPlusTokenResult `json:"result"`
}

type goPlusTokenResult struct {
	TokenName          string         `json:"token_name"`
	TokenSymbol        string         `json:"token_symbol"`
	TotalSupply        string         `json:"total_supply"`
	HolderCount        string         `json:"holder_count"`
	IsOpenSource       string         `json:"is_open_source"`
	IsHoneypot         string         `json:"is_honeypot"`
	IsMintable         string         `json:"is_mintable"`
	TransferPausable   string         `json:"transfer_pausable"`
	SlippageModifiable string         `json:"slippage_modifiable"`
	BuyTax             string         `json:"buy_tax"`
	SellTax            string         `json:"sell_tax"`
	OwnerAddress       string         `json:"owner_address"`
	CreatorAddress     string         `json:"creator_address"`
	Holders            []goPlusHolder `json:"holders"`
	LPHolders          []goPlusHolder `json:"lp_holders"`
}

type goPlusHolder struct {
	Address    string `json:"address"`
	Tag        string `json:"tag"`
	IsContract int    `json:"is_contract"`
	IsLocked   int    `json:"is_locked"`
	Balance    string `json:"balance"`
	Percent    string `json:"percent"`
}

type geckoTradesResponse struct {
	Data []geckoTrade `json:"data"`
}

type geckoTrade struct {
	Attributes geckoTradeAttrs `json:"attributes"`
}

type geckoTradeAttrs struct {
	BlockNumber      int64  `json:"block_number"`
	TxHash           string `json:"tx_hash"`
	TxFromAddress    string `json:"tx_from_address"`
	FromTokenAmount  string `json:"from_token_amount"`
	ToTokenAmount    string `json:"to_token_amount"`
	FromTokenAddress string `json:"from_token_address"`
	ToTokenAddress   string `json:"to_token_address"`
	BlockTimestamp   string `json:"block_timestamp"`
	Kind             string `json:"kind"`
	VolumeUSD        string `json:"volume_in_usd"`
}

func (s *Service) buildRecentAnalysis(ctx context.Context, chain, address string) (*TokenAnalysisResponse, error) {
	resp := &TokenAnalysisResponse{
		Success:      true,
		Chain:        chain,
		Address:      address,
		Depth:        DepthRecent,
		Status:       StatusOK,
		Completeness: DepthRecent,
		Source:       []string{"dexscreener", "goplus", "geckoterminal"},
	}

	pools, err := s.discoverPools(ctx, chain, address)
	if err != nil {
		return resp, err
	}
	resp.Pools = pools
	if len(pools) > 0 {
		best := pools[0]
		resp.Token.Name = strings.TrimSpace(best.TokenName)
		if resp.Token.Name == "" {
			resp.Token.Name = strings.TrimSpace(best.Name)
		}
		resp.Token.Symbol = strings.TrimSpace(best.BaseSymbol)
		resp.Token.PriceUSD = best.PriceUSD
		resp.Token.FDV = 0
		resp.Token.MarketCap = 0
	}

	security, tokenProfile, riskFlags, _ := s.fetchGoPlusSecurity(ctx, address)
	if tokenProfile.Name != "" || tokenProfile.Symbol != "" {
		resp.Token.Name = tokenProfile.Name
		resp.Token.Symbol = tokenProfile.Symbol
		resp.Token.TotalSupply = tokenProfile.TotalSupply
	}
	resp.Security = security
	resp.RiskFlags = append(resp.RiskFlags, riskFlags...)

	recent := s.fetchRecentTrades(ctx, address, pools)
	resp.Recent = recent
	return resp, nil
}

func (s *Service) discoverPools(ctx context.Context, chain, address string) ([]PoolSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://api.dexscreener.com/token-pairs/v1/%s/%s", chain, address), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	pairs, err := decodeDexScreenerPairs(resp, 4<<20)
	if err != nil {
		return nil, err
	}

	out := make([]PoolSnapshot, 0, len(pairs))
	for _, pair := range pairs {
		baseIsTarget := strings.EqualFold(pair.BaseToken.Address, address)
		quoteIsTarget := strings.EqualFold(pair.QuoteToken.Address, address)
		if !strings.EqualFold(pair.ChainID, chain) || (!baseIsTarget && !quoteIsTarget) {
			continue
		}
		if !strings.Contains(strings.ToLower(pair.DexID), "pancake") {
			continue
		}
		tokenName := pair.BaseToken.Name
		tokenSymbol := pair.BaseToken.Symbol
		quoteSymbol := pair.QuoteToken.Symbol
		if quoteIsTarget {
			tokenName = pair.QuoteToken.Name
			tokenSymbol = pair.QuoteToken.Symbol
			quoteSymbol = pair.BaseToken.Symbol
		}
		out = append(out, PoolSnapshot{
			ChainID:      pair.ChainID,
			DexID:        pair.DexID,
			Address:      normalizeAddress(pair.PairAddress),
			Name:         strings.TrimSpace(pair.BaseToken.Symbol + " / " + pair.QuoteToken.Symbol),
			BaseToken:    normalizeAddress(pair.BaseToken.Address),
			BaseSymbol:   tokenSymbol,
			QuoteToken:   normalizeAddress(pair.QuoteToken.Address),
			QuoteSymbol:  quoteSymbol,
			TokenName:    tokenName,
			PriceUSD:     pair.PriceUSD,
			LiquidityUSD: pair.Liquidity.USD,
			Volume24hUSD: pair.Volume.H24,
			PairURL:      pair.URL,
			CreatedAtMS:  pair.PairCreatedAt,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LiquidityUSD > out[j].LiquidityUSD
	})
	return out, nil
}

func poolsToStore(pools []PoolSnapshot, chain, tokenAddress string) []store.OnchainPool {
	out := make([]store.OnchainPool, 0, len(pools))
	for _, pool := range pools {
		out = append(out, store.OnchainPool{
			Chain:        chain,
			TokenAddress: tokenAddress,
			PoolAddress:  pool.Address,
			DexID:        pool.DexID,
			Name:         pool.Name,
			BaseToken:    pool.BaseToken,
			QuoteToken:   pool.QuoteToken,
			CreatedBlock: pool.CreatedBlock,
			CreatedAtMS:  pool.CreatedAtMS,
			LiquidityUSD: pool.LiquidityUSD,
			PairURL:      pool.PairURL,
		})
	}
	return out
}

func decodeDexScreenerPairs(resp *http.Response, limit int64) ([]dexScreenerPair, error) {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, err
	}
	var pairs []dexScreenerPair
	if err := json.Unmarshal(raw, &pairs); err == nil {
		return pairs, nil
	}
	var wrapped dexScreenerTokenResponse
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Pairs, nil
}

func (s *Service) fetchGoPlusSecurity(ctx context.Context, address string) (*TokenSecurity, TokenProfile, []string, error) {
	url := fmt.Sprintf("https://api.gopluslabs.io/api/v1/token_security/%s?contract_addresses=%s", bscChainID, address)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, TokenProfile{}, nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, TokenProfile{}, nil, err
	}
	var decoded goPlusResponse
	if err := decodeJSONLimited(resp, &decoded, 4<<20); err != nil {
		return nil, TokenProfile{}, nil, err
	}
	item, ok := decoded.Result[address]
	if !ok {
		for _, value := range decoded.Result {
			item = value
			ok = true
			break
		}
	}
	if !ok {
		return nil, TokenProfile{}, nil, fmt.Errorf("no goplus security result")
	}
	security := &TokenSecurity{
		HolderCount:        parseIntString(item.HolderCount),
		IsOpenSource:       item.IsOpenSource,
		IsHoneypot:         item.IsHoneypot,
		IsMintable:         item.IsMintable,
		TransferPausable:   item.TransferPausable,
		SlippageModifiable: item.SlippageModifiable,
		BuyTax:             item.BuyTax,
		SellTax:            item.SellTax,
		OwnerAddress:       normalizeAddress(item.OwnerAddress),
		CreatorAddress:     normalizeAddress(item.CreatorAddress),
		TopHolders:         convertGoPlusHolders(item.Holders),
		LPHolders:          convertGoPlusHolders(item.LPHolders),
	}
	profile := TokenProfile{Name: item.TokenName, Symbol: item.TokenSymbol, TotalSupply: item.TotalSupply}
	var riskFlags []string
	if item.IsHoneypot == "1" {
		riskFlags = append(riskFlags, "honeypot")
	}
	if item.IsMintable == "1" {
		riskFlags = append(riskFlags, "mintable")
	}
	if item.TransferPausable == "1" {
		riskFlags = append(riskFlags, "transfer_pausable")
	}
	if item.SlippageModifiable == "1" {
		riskFlags = append(riskFlags, "slippage_modifiable")
	}
	return security, profile, riskFlags, nil
}

func convertGoPlusHolders(items []goPlusHolder) []HolderSnapshot {
	out := make([]HolderSnapshot, 0, len(items))
	for _, item := range items {
		out = append(out, HolderSnapshot{
			Address:    normalizeAddress(item.Address),
			Tag:        item.Tag,
			IsContract: item.IsContract == 1,
			IsLocked:   item.IsLocked == 1,
			Balance:    item.Balance,
			Percent:    parseFloatString(item.Percent),
		})
	}
	return out
}

func (s *Service) fetchRecentTrades(ctx context.Context, tokenAddress string, pools []PoolSnapshot) *RecentTradeAnalysis {
	type acc struct {
		firstBuy   int64
		lastTrade  int64
		buyCount   int
		sellCount  int
		buyAmount  float64
		sellAmount float64
		buyUSD     float64
		sellUSD    float64
		pools      map[string]bool
	}
	accounts := map[string]*acc{}
	get := func(addr string) *acc {
		addr = normalizeAddress(addr)
		if accounts[addr] == nil {
			accounts[addr] = &acc{pools: make(map[string]bool)}
		}
		return accounts[addr]
	}

	result := &RecentTradeAnalysis{}
	buyers := map[string]bool{}
	sellers := map[string]bool{}
	for _, pool := range pools {
		if pool.Address == "" {
			continue
		}
		url := fmt.Sprintf("https://api.geckoterminal.com/api/v2/networks/bsc/pools/%s/trades", pool.Address)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "application/json")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			continue
		}
		var decoded geckoTradesResponse
		if err := decodeJSONLimited(resp, &decoded, 4<<20); err != nil {
			continue
		}
		for _, trade := range decoded.Data {
			a := trade.Attributes
			fromToken := normalizeAddress(a.FromTokenAddress)
			toToken := normalizeAddress(a.ToTokenAddress)
			addr := normalizeAddress(a.TxFromAddress)
			ts := msFromISO(a.BlockTimestamp)
			volume := parseFloatString(a.VolumeUSD)
			w := get(addr)
			w.lastTrade = maxInt64(w.lastTrade, ts)
			w.pools[pool.Address] = true
			result.TradeCount++
			result.VolumeUSD += volume
			if ts > 0 && (result.WindowStart == "" || a.BlockTimestamp < result.WindowStart) {
				result.WindowStart = a.BlockTimestamp
			}
			if ts > 0 && (result.WindowEnd == "" || a.BlockTimestamp > result.WindowEnd) {
				result.WindowEnd = a.BlockTimestamp
			}

			if toToken == tokenAddress {
				amount := parseFloatString(a.ToTokenAmount)
				w.buyCount++
				w.buyAmount += amount
				w.buyUSD += volume
				if ts > 0 && (w.firstBuy == 0 || ts < w.firstBuy) {
					w.firstBuy = ts
				}
				buyers[addr] = true
				result.BuyCount++
			} else if fromToken == tokenAddress {
				amount := parseFloatString(a.FromTokenAmount)
				w.sellCount++
				w.sellAmount += amount
				w.sellUSD += volume
				sellers[addr] = true
				result.SellCount++
			}
		}
	}

	result.UniqueAddresses = len(accounts)
	result.UniqueBuyers = len(buyers)
	result.UniqueSellers = len(sellers)
	wallets := make([]WalletAnalysis, 0, len(accounts))
	buckets := map[string]*TimeBucket{}
	for addr, a := range accounts {
		net := a.buyAmount - a.sellAmount
		total := a.buyAmount + a.sellAmount
		walletType := walletTypeFromStats(a.buyCount, a.sellCount, len(a.pools), net, total)
		wallet := WalletAnalysis{
			Address:          addr,
			WalletType:       walletType,
			FirstBuyTime:     a.firstBuy,
			FirstBuyAt:       isoFromMS(a.firstBuy),
			BuyCount:         a.buyCount,
			SellCount:        a.sellCount,
			BuyAmount:        a.buyAmount,
			SellAmount:       a.sellAmount,
			NetBoughtAmount:  net,
			PoolTouchCount:   len(a.pools),
			EstimatedUSDFlow: a.sellUSD - a.buyUSD,
		}
		wallets = append(wallets, wallet)
		if a.firstBuy > 0 {
			bucketKey := time.UnixMilli(a.firstBuy).UTC().Format("2006-01-02 15:00")
			b := buckets[bucketKey]
			if b == nil {
				b = &TimeBucket{Bucket: bucketKey}
				buckets[bucketKey] = b
			}
			b.BuyerCount++
			b.BuyAmount += a.buyAmount
			b.SellAmount += a.sellAmount
			b.NetAmount += net
		}
	}
	result.TopAccumulators = capWallets(sortedWalletsByNet(wallets, true), 15)
	result.TopSellers = capWallets(sortedWalletsByNet(wallets, false), 15)
	for _, wallet := range wallets {
		if wallet.WalletType == store.OnchainWalletArbBot {
			result.RelatedWalletClusters = append(result.RelatedWalletClusters, wallet)
		}
	}
	result.RelatedWalletClusters = capWallets(result.RelatedWalletClusters, 15)
	for _, bucket := range buckets {
		result.FirstBuyBuckets = append(result.FirstBuyBuckets, *bucket)
	}
	sort.SliceStable(result.FirstBuyBuckets, func(i, j int) bool {
		return result.FirstBuyBuckets[i].Bucket < result.FirstBuyBuckets[j].Bucket
	})
	return result
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
