package datagateway

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func NewRouter(svc *Service, token string) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), authMiddleware(token))

	healthHandler := func(c *gin.Context) {
		c.JSON(http.StatusOK, svc.Health())
	}
	r.GET("/health", healthHandler)
	r.HEAD("/health", healthHandler)
	api := r.Group("/api")
	api.GET("/ai500/list", svc.handleAI500List)
	api.GET("/ai500/stats", svc.handleAI500Stats)
	api.GET("/ai500/:symbol", svc.handleAI500Symbol)
	api.GET("/oi/top-ranking", svc.handleOIRanking("top"))
	api.GET("/oi/low-ranking", svc.handleOIRanking("low"))
	api.GET("/oi/top", svc.handleOILegacyTop)
	api.GET("/netflow/top-ranking", svc.handleNetFlowRanking("top"))
	api.GET("/netflow/low-ranking", svc.handleNetFlowRanking("low"))
	api.GET("/netflow/top", svc.handleNetFlowLegacyTop)
	api.GET("/price/ranking", svc.handlePriceRanking)
	api.GET("/coin/:symbol", svc.handleCoin)
	return r
}

func authMiddleware(token string) gin.HandlerFunc {
	token = strings.TrimSpace(token)
	return func(c *gin.Context) {
		if token == "" || c.Request.URL.Path == "/health" {
			c.Next()
			return
		}
		got := strings.TrimSpace(c.GetHeader("X-Gateway-Token"))
		if got == "" {
			got = strings.TrimSpace(c.Query("auth"))
		}
		if got != token {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "invalid api key"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Service) handleAI500List(c *gin.Context) {
	limit, err := parseLimitParam(c.Query("limit"), 20, 500)
	if err != nil {
		abortBadRequest(c, err)
		return
	}
	items := s.allAggregated()
	if err := ensureData(items); err != nil {
		c.JSON(http.StatusServiceUnavailable, serviceError(err.Error()))
		return
	}
	sortAggregatedByScore(items)
	coins := make([]AI500Coin, 0, limit)
	for _, item := range items {
		if item.Score < 45 || !item.HasFutureData {
			continue
		}
		coins = append(coins, ai500FromAggregated(item))
		if len(coins) >= limit {
			break
		}
	}
	payload := gin.H{"success": true, "data": gin.H{"count": len(coins), "coins": coins}}
	s.SaveRanking("ai500:list", payload)
	c.JSON(http.StatusOK, payload)
}

func (s *Service) handleAI500Symbol(c *gin.Context) {
	item, ok := s.find(c.Param("symbol"))
	if !ok {
		c.JSON(http.StatusNotFound, serviceError("symbol not found"))
		return
	}
	coin := ai500FromAggregated(item)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"info":             coin,
		"current_price":    item.Price,
		"score":            item.Score,
		"pair":             coin.Pair,
		"symbol":           coin.Symbol,
		"is_active":        coin.IsActive,
		"start_time":       coin.StartTime,
		"start_price":      coin.StartPrice,
		"last_score":       coin.LastScore,
		"max_score":        coin.MaxScore,
		"max_price":        coin.MaxPrice,
		"increase_percent": coin.IncreasePercent,
	}})
}

func (s *Service) handleAI500Stats(c *gin.Context) {
	items := s.allAggregated()
	if err := ensureData(items); err != nil {
		c.JSON(http.StatusServiceUnavailable, serviceError(err.Error()))
		return
	}
	total := len(items)
	available := 0
	var avgScore, avgIncrease, maxScore, minScore float64
	minScore = 101
	for _, item := range items {
		if item.Score > 0 {
			available++
			avgScore += item.Score
			avgIncrease += item.PriceChange24h
			if item.Score > maxScore {
				maxScore = item.Score
			}
			if item.Score < minScore {
				minScore = item.Score
			}
		}
	}
	if available > 0 {
		avgScore = avgScore / float64(available)
		avgIncrease = avgIncrease / float64(available)
	} else {
		minScore = 0
	}

	sortAggregatedByScore(items)
	topCoins := make([]AI500Coin, 0, 10)
	for _, item := range items {
		if item.Score <= 0 {
			continue
		}
		topCoins = append(topCoins, ai500FromAggregated(item))
		if len(topCoins) >= 10 {
			break
		}
	}
	bottomCoins := make([]AI500Coin, 0, 10)
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Score <= 0 {
			continue
		}
		bottomCoins = append(bottomCoins, ai500FromAggregated(items[i]))
		if len(bottomCoins) >= 10 {
			break
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"statistics": gin.H{
			"total_count":      total,
			"available_count":  available,
			"average_score":    avgScore,
			"max_score":        maxScore,
			"min_score":        minScore,
			"average_increase": avgIncrease,
		},
		"top_coins": topCoins, "bottom_coins": bottomCoins,
		"total": total, "available": available, "average_score": avgScore,
		"updated_at": time.Now().Unix(),
	}})
}

func ai500FromAggregated(item AggregatedSymbol) AI500Coin {
	startPrice := item.Price / (1 + item.PriceChange24h/100)
	if startPrice <= 0 {
		startPrice = item.Price
	}
	return AI500Coin{
		Pair: item.Symbol, Symbol: item.Symbol, Score: item.Score, IsActive: item.Score > 0,
		StartTime:  item.FetchedAt.Add(-24 * time.Hour).Unix(),
		StartPrice: startPrice, LastScore: item.Score,
		MaxScore: item.Score, MaxPrice: item.Price,
		IncreasePercent: item.PriceChange24h,
	}
}

func (s *Service) handleOIRanking(rankType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		duration, err := parseDurationParam(c.Query("duration"), "1h")
		if err != nil {
			abortBadRequest(c, err)
			return
		}
		limit, err := parseLimitParam(c.Query("limit"), 20, 100)
		if err != nil {
			abortBadRequest(c, err)
			return
		}
		allItems := s.allAggregated()
		if err := ensureData(allItems); err != nil {
			c.JSON(http.StatusServiceUnavailable, serviceError(err.Error()))
			return
		}
		items := filterFutureOI(allItems)
		useFallback := shouldUseOIFallback(items, rankType)
		sortOIRanking(items, rankType, useFallback)
		positions := make([]OIPosition, 0, limit)
		for _, item := range items {
			if len(positions) >= limit {
				break
			}
			rankDelta := oiRankingValue(item, rankType, useFallback)
			if rankType == "top" && rankDelta <= 0 {
				continue
			}
			if rankType == "low" && rankDelta >= 0 {
				continue
			}
			positions = append(positions, oiPositionFromAggregated(item, len(positions)+1))
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "code": 0, "data": gin.H{
			"positions": positions, "count": len(positions), "exchange": "aggregated",
			"time_range": duration, "time_range_param": duration, "rank_type": rankType, "limit": limit,
		}})
	}
}

func (s *Service) handleOILegacyTop(c *gin.Context) {
	allItems := s.allAggregated()
	if err := ensureData(allItems); err != nil {
		c.JSON(http.StatusServiceUnavailable, serviceError(err.Error()))
		return
	}
	items := filterFutureOI(allItems)
	useFallback := shouldUseOIFallback(items, "top")
	sortOIRanking(items, "top", useFallback)
	positions := make([]OIPosition, 0, 20)
	for _, item := range items {
		if oiRankingValue(item, "top", useFallback) <= 0 {
			continue
		}
		positions = append(positions, oiPositionFromAggregated(item, len(positions)+1))
		if len(positions) >= 20 {
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"positions": positions, "count": len(positions)}})
}

func filterFutureOI(items []AggregatedSymbol) []AggregatedSymbol {
	out := make([]AggregatedSymbol, 0, len(items))
	for _, item := range items {
		if item.HasFutureData && item.OpenInterestValue > 0 {
			out = append(out, item)
		}
	}
	return out
}

func oiPositionFromAggregated(item AggregatedSymbol, rank int) OIPosition {
	delta := item.OpenInterestDelta
	deltaPct := item.OpenInterestDeltaPct
	if item.OpenInterestDelta == 0 && item.OpenInterestValue > 0 {
		deltaPct = 0
	}
	return OIPosition{
		Symbol: item.Symbol, Rank: rank, Price: item.Price,
		CurrentOI: item.OpenInterestValue, OIDelta: delta,
		OIDeltaPercent: deltaPct, OIDeltaValue: delta,
		PriceDeltaPercent: item.PriceChange1h,
		NetLong:           positive(delta), NetShort: positive(-delta),
	}
}

func sortOIRanking(items []AggregatedSymbol, rankType string, useFallback bool) {
	desc := rankType == "top"
	sortAggregatedByOIDelta(items, desc)
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			vi := oiRankingValue(items[i], rankType, useFallback)
			vj := oiRankingValue(items[j], rankType, useFallback)
			if desc && vi < vj || !desc && vi > vj {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func shouldUseOIFallback(items []AggregatedSymbol, rankType string) bool {
	for _, item := range items {
		if rankType == "top" && item.OpenInterestDelta > 0 {
			return false
		}
		if rankType == "low" && item.OpenInterestDelta < 0 {
			return false
		}
	}
	return true
}

func oiRankingValue(item AggregatedSymbol, rankType string, useFallback bool) float64 {
	if item.OpenInterestDelta != 0 {
		return item.OpenInterestDelta
	}
	if !useFallback {
		return 0
	}
	if rankType == "low" {
		momentum := item.PriceChange1h
		if momentum == 0 {
			momentum = item.PriceChange24h
		}
		weaknessBoost := 1 + clamp(-momentum, 0, 20)/100
		return -item.OpenInterestValue * weaknessBoost
	}
	return item.OpenInterestValue
}

func (s *Service) handleNetFlowRanking(rankType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit, err := parseLimitParam(c.Query("limit"), 20, 100)
		if err != nil {
			abortBadRequest(c, err)
			return
		}
		duration, err := parseDurationParam(c.Query("duration"), "1h")
		if err != nil {
			abortBadRequest(c, err)
			return
		}
		flowType, err := parseFlowType(c.Query("type"))
		if err != nil {
			abortBadRequest(c, err)
			return
		}
		trade, err := parseTradeType(c.Query("trade"))
		if err != nil {
			abortBadRequest(c, err)
			return
		}
		items := s.allAggregated()
		if err := ensureData(items); err != nil {
			c.JSON(http.StatusServiceUnavailable, serviceError(err.Error()))
			return
		}
		sortNetFlow(items, flowType, trade, rankType == "top")
		netflows := make([]NetFlowPosition, 0, limit)
		for _, item := range items {
			amount := estimateNetFlow(item, flowType, trade)
			if rankType == "top" && amount <= 0 {
				continue
			}
			if rankType == "low" && amount >= 0 {
				continue
			}
			netflows = append(netflows, NetFlowPosition{Rank: len(netflows) + 1, Symbol: item.Symbol, Amount: amount, Price: item.Price})
			if len(netflows) >= limit {
				break
			}
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
			"netflows": netflows, "count": len(netflows), "type": flowType, "trade": trade,
			"time_range": duration, "rank_type": rankType, "limit": limit,
			"estimated": true,
		}})
	}
}

func (s *Service) handleNetFlowLegacyTop(c *gin.Context) {
	items := s.allAggregated()
	if err := ensureData(items); err != nil {
		c.JSON(http.StatusServiceUnavailable, serviceError(err.Error()))
		return
	}
	sortNetFlow(items, "institution", "future", true)
	netflows := make([]NetFlowPosition, 0, 20)
	for _, item := range items {
		amount := estimateNetFlow(item, "institution", "future")
		if amount <= 0 {
			continue
		}
		netflows = append(netflows, NetFlowPosition{Rank: len(netflows) + 1, Symbol: item.Symbol, Amount: amount, Price: item.Price})
		if len(netflows) >= 20 {
			break
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"netflows": netflows, "count": len(netflows), "estimated": true}})
}

func sortNetFlow(items []AggregatedSymbol, flowType, trade string, desc bool) {
	sortAggregatedByPriceDelta(items, "1h", desc)
	for i := range items {
		for j := i + 1; j < len(items); j++ {
			vi := estimateNetFlow(items[i], flowType, trade)
			vj := estimateNetFlow(items[j], flowType, trade)
			if desc && vi < vj || !desc && vi > vj {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func estimateNetFlow(item AggregatedSymbol, flowType, trade string) float64 {
	volume := item.FutureQuoteVolume24h
	if trade == "spot" {
		volume = item.SpotQuoteVolume24h
	}
	if volume <= 0 {
		volume = item.QuoteVolume24h
	}
	momentum := item.PriceChange1h
	if momentum == 0 {
		momentum = item.PriceChange24h / 24
	}
	oiTerm := item.OpenInterestDelta
	if oiTerm == 0 {
		oiTerm = item.OpenInterestValue * item.OpenInterestDeltaPct / 100
	}
	amount := volume * clamp(momentum/100, -0.08, 0.08) * 0.35
	if trade != "spot" {
		amount += oiTerm * 0.65
	}
	if flowType == "personal" {
		amount = volume*clamp(momentum/100, -0.06, 0.06)*0.22 - oiTerm*0.25
	}
	return amount
}

func (s *Service) handlePriceRanking(c *gin.Context) {
	limit, err := parseLimitParam(c.Query("limit"), 20, 100)
	if err != nil {
		abortBadRequest(c, err)
		return
	}
	durations, err := parseDurationList(c.Query("duration"))
	if err != nil {
		abortBadRequest(c, err)
		return
	}
	result := map[string]PriceRankingDuration{}
	items := s.allAggregated()
	if err := ensureData(items); err != nil {
		c.JSON(http.StatusServiceUnavailable, serviceError(err.Error()))
		return
	}
	for _, key := range durations {
		topItems := append([]AggregatedSymbol(nil), items...)
		lowItems := append([]AggregatedSymbol(nil), items...)
		sortAggregatedByPriceDelta(topItems, key, true)
		sortAggregatedByPriceDelta(lowItems, key, false)
		result[key] = PriceRankingDuration{
			Top: priceItems(topItems, key, limit),
			Low: priceItems(lowItems, key, limit),
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"durations": durations, "limit": limit, "data": result}})
}

func priceItems(items []AggregatedSymbol, duration string, limit int) []PriceRankingItem {
	out := make([]PriceRankingItem, 0, limit)
	for _, item := range items {
		if item.Price <= 0 {
			continue
		}
		out = append(out, PriceRankingItem{
			Pair: item.Symbol, Symbol: item.Base, PriceDelta: priceDeltaFor(item, duration) / 100,
			Price: item.Price, FutureFlow: estimateNetFlow(item, "institution", "future"),
			SpotFlow: estimateNetFlow(item, "institution", "spot"), OI: item.OpenInterestValue,
			OIDelta: item.OpenInterestDeltaPct, OIDeltaValue: item.OpenInterestDelta,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (s *Service) handleCoin(c *gin.Context) {
	item, ok := s.find(c.Param("symbol"))
	if !ok {
		c.JSON(http.StatusNotFound, serviceError("symbol not found"))
		return
	}
	data := QuantData{
		Symbol: item.Symbol, Price: item.Price,
		PriceChange: map[string]float64{
			"1h":  item.PriceChange1h / 100,
			"4h":  item.PriceChange4h / 100,
			"24h": item.PriceChange24h / 100,
		},
	}
	include := strings.ToLower(c.DefaultQuery("include", "netflow,oi,price,ai500"))
	if strings.Contains(include, "ai500") {
		coin := ai500FromAggregated(item)
		data.AI500 = &coin
	}
	if strings.Contains(include, "oi") && item.OpenInterestValue > 0 {
		data.OI = map[string]*OIData{"aggregated": {
			CurrentOI: item.OpenInterestValue, NetLong: positive(item.OpenInterestDelta),
			NetShort: positive(-item.OpenInterestDelta),
			Delta: map[string]*OIDeltaData{"1h": {
				OIDelta: item.OpenInterestDelta, OIDeltaValue: item.OpenInterestDelta,
				OIDeltaPercent: item.OpenInterestDeltaPct,
			}},
		}}
	}
	if strings.Contains(include, "netflow") {
		data.Netflow = &NetflowData{
			Institution: &FlowTypeData{
				Future: map[string]float64{"1h": estimateNetFlow(item, "institution", "future")},
				Spot:   map[string]float64{"1h": estimateNetFlow(item, "institution", "spot")},
			},
			Personal: &FlowTypeData{
				Future: map[string]float64{"1h": estimateNetFlow(item, "personal", "future")},
				Spot:   map[string]float64{"1h": estimateNetFlow(item, "personal", "spot")},
			},
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "code": 0, "data": data})
}

func positive(v float64) float64 {
	if v > 0 {
		return v
	}
	return 0
}

func parseLimitParam(raw string, def, max int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid limit: must be a positive integer")
	}
	if max > 0 && n > max {
		return max, nil
	}
	return n, nil
}

func parseDurationParam(raw, def string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		raw = def
	}
	switch raw {
	case "1h", "1hour", "1hours":
		return "1h", nil
	case "4h", "4hour", "4hours":
		return "4h", nil
	case "24h", "1d", "day":
		return "24h", nil
	default:
		return "", fmt.Errorf("invalid duration: supported values are 1h, 4h, 24h")
	}
}

func parseDurationList(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{"1h"}, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		duration, err := parseDurationParam(part, "")
		if err != nil {
			return nil, err
		}
		if seen[duration] {
			continue
		}
		seen[duration] = true
		out = append(out, duration)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("invalid duration: supported values are 1h, 4h, 24h")
	}
	return out, nil
}

func parseFlowType(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "institution", nil
	}
	switch raw {
	case "institution", "personal":
		return raw, nil
	default:
		return "", fmt.Errorf("invalid type: supported values are institution, personal")
	}
}

func parseTradeType(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "future", nil
	}
	switch raw {
	case "future", "futures":
		return "future", nil
	case "spot":
		return "spot", nil
	default:
		return "", fmt.Errorf("invalid trade: supported values are future, spot")
	}
}

func abortBadRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, serviceError(err.Error()))
}
