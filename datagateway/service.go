package datagateway

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
)

type Service struct {
	cfg        Config
	store      *Store
	collectors []Collector

	mu               sync.RWMutex
	snapshots        []MarketSnapshot
	aggregated       map[string]AggregatedSymbol
	lastRefresh      time.Time
	lastRefreshError string
}

func NewService(cfg Config, store *Store) *Service {
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = time.Minute
	}
	svc := &Service{
		cfg:        cfg,
		store:      store,
		collectors: defaultCollectors(),
		aggregated: make(map[string]AggregatedSymbol),
	}
	if err := svc.loadCachedSnapshots(24 * time.Hour); err != nil {
		log.Printf("data gateway warm cache load failed: %v", err)
	}
	return svc
}

func (s *Service) Start(ctx context.Context) {
	if err := s.Refresh(ctx); err != nil {
		log.Printf("data gateway initial refresh failed: %v", err)
	}
	ticker := time.NewTicker(s.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Refresh(ctx); err != nil {
				log.Printf("data gateway refresh failed: %v", err)
			}
		}
	}
}

func (s *Service) loadCachedSnapshots(maxAge time.Duration) error {
	items, err := s.store.LatestSnapshots(maxAge)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	s.mu.Lock()
	s.snapshots = items
	s.aggregated = aggregateSnapshots(items)
	s.lastRefresh = latestSnapshotTime(items)
	s.mu.Unlock()
	return nil
}

func (s *Service) Refresh(ctx context.Context) error {
	previous, _ := s.store.LatestSnapshots(48 * time.Hour)
	prevByKey := make(map[string]MarketSnapshot, len(previous))
	for _, item := range previous {
		prevByKey[item.Exchange+"|"+item.MarketType+"|"+item.Symbol] = item
	}

	items, errs := fetchAll(ctx, s.collectors)
	for i := range items {
		key := items[i].Exchange + "|" + items[i].MarketType + "|" + items[i].Symbol
		if items[i].OpenInterestDelta == 0 {
			if prev, ok := prevByKey[key]; ok {
				if items[i].OpenInterestValue > 0 && prev.OpenInterestValue > 0 {
					items[i].OpenInterestDelta = items[i].OpenInterestValue - prev.OpenInterestValue
				}
			}
		}
		if price, ok := s.store.HistoricalPrice(items[i].Symbol, time.Hour); ok {
			items[i].PriceChange1h = pctDelta(items[i].Price, price)
		}
		if price, ok := s.store.HistoricalPrice(items[i].Symbol, 4*time.Hour); ok {
			items[i].PriceChange4h = pctDelta(items[i].Price, price)
		}
	}

	if len(items) > 0 {
		if err := s.store.SaveSnapshots(items); err != nil {
			return err
		}
	} else {
		cached, err := s.store.LatestSnapshots(24 * time.Hour)
		if err != nil {
			return err
		}
		items = cached
	}
	if len(items) == 0 {
		return errors.New("all market data sources failed and no cache is available")
	}

	aggregated := aggregateSnapshots(items)
	s.mu.Lock()
	s.snapshots = items
	s.aggregated = aggregated
	s.lastRefresh = time.Now()
	s.lastRefreshError = joinErrors(errs)
	s.mu.Unlock()
	return nil
}

func latestSnapshotTime(items []MarketSnapshot) time.Time {
	var latest time.Time
	for _, item := range items {
		if item.FetchedAt.After(latest) {
			latest = item.FetchedAt
		}
	}
	return latest
}

func (s *Service) Health() healthPayload {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := "ok"
	if len(s.aggregated) == 0 {
		status = "degraded"
	}
	return healthPayload{
		Status: status, Service: "nofx-data-gateway",
		Symbols: len(s.aggregated), SnapshotCount: len(s.snapshots),
		LastRefresh: s.lastRefresh, LastRefreshError: s.lastRefreshError,
	}
}

func (s *Service) allAggregated() []AggregatedSymbol {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]AggregatedSymbol, 0, len(s.aggregated))
	for _, item := range s.aggregated {
		items = append(items, item)
	}
	return items
}

func (s *Service) find(symbol string) (AggregatedSymbol, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.aggregated[normalizeSymbol(symbol)]
	return item, ok
}

func aggregateSnapshots(items []MarketSnapshot) map[string]AggregatedSymbol {
	type bucket struct {
		item        AggregatedSymbol
		priceWeight float64
		exchanges   map[string]bool
	}
	buckets := make(map[string]*bucket)
	for _, snap := range items {
		if snap.Symbol == "" || snap.Price <= 0 {
			continue
		}
		b, ok := buckets[snap.Symbol]
		if !ok {
			b = &bucket{item: AggregatedSymbol{Symbol: snap.Symbol, Base: snap.Base, FetchedAt: snap.FetchedAt}, exchanges: map[string]bool{}}
			buckets[snap.Symbol] = b
		}
		weight := snap.QuoteVolume24h
		if weight <= 0 {
			weight = 1
		}
		b.item.Price += snap.Price * weight
		b.priceWeight += weight
		b.item.QuoteVolume24h += snap.QuoteVolume24h
		b.exchanges[snap.Exchange] = true
		if snap.MarketType == MarketTypeFuture {
			b.item.HasFutureData = true
			b.item.FutureQuoteVolume24h += snap.QuoteVolume24h
			if snap.OpenInterestValue > b.item.OpenInterestValue {
				b.item.OpenInterestValue = snap.OpenInterestValue
			}
			b.item.OpenInterestDelta += snap.OpenInterestDelta
			if math.Abs(snap.FundingRate) > math.Abs(b.item.FundingRate) {
				b.item.FundingRate = snap.FundingRate
			}
		}
		if snap.MarketType == MarketTypeSpot {
			b.item.HasSpotData = true
			b.item.SpotQuoteVolume24h += snap.QuoteVolume24h
		}
		if snap.QuoteVolume24h >= b.item.QuoteVolume24h/float64(len(b.exchanges)+1) {
			if snap.PriceChange1h != 0 {
				b.item.PriceChange1h = snap.PriceChange1h
			}
			if snap.PriceChange4h != 0 {
				b.item.PriceChange4h = snap.PriceChange4h
			}
			if snap.PriceChange24h != 0 {
				b.item.PriceChange24h = snap.PriceChange24h
			}
		}
		if snap.FetchedAt.After(b.item.FetchedAt) {
			b.item.FetchedAt = snap.FetchedAt
		}
	}

	out := make(map[string]AggregatedSymbol, len(buckets))
	for symbol, b := range buckets {
		if b.priceWeight > 0 {
			b.item.Price = b.item.Price / b.priceWeight
		}
		b.item.ExchangeCount = len(b.exchanges)
		if b.item.OpenInterestValue > 0 {
			b.item.OpenInterestDeltaPct = b.item.OpenInterestDelta / b.item.OpenInterestValue * 100
		}
		b.item.Score = scoreAI500(b.item)
		out[symbol] = b.item
	}
	return out
}

func scoreAI500(item AggregatedSymbol) float64 {
	if !eligibleForAI500(item) {
		return 0
	}
	score := 35.0
	score += clamp(item.PriceChange1h*2.2, -12, 18)
	score += clamp(item.PriceChange4h*1.3, -12, 18)
	score += clamp(item.PriceChange24h*0.55, -10, 16)
	score += clamp(math.Log10(item.QuoteVolume24h/1_000_000)*8, 0, 18)
	score += clamp(float64(item.ExchangeCount)*2.5, 0, 12)
	score += clamp(math.Log10(math.Max(item.OpenInterestValue, 1)/1_000_000)*7, 0, 18)
	score += clamp(item.OpenInterestDeltaPct*1.8, -10, 10)
	score -= clamp(math.Abs(item.FundingRate)*10000-1, 0, 10)
	if math.Abs(item.PriceChange24h) > 35 {
		score -= 8
	}
	return math.Round(clamp(score, 0, 100)*1000) / 1000
}

func eligibleForAI500(item AggregatedSymbol) bool {
	if !item.HasFutureData || item.Price <= 0 || item.QuoteVolume24h < 1_000_000 {
		return false
	}
	if item.Price < 0.000001 || math.Abs(item.PriceChange24h) > 250 {
		return false
	}
	if isExcludedAI500Base(item.Base) {
		return false
	}
	return true
}

func joinErrors(errs []error) string {
	if len(errs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			parts = append(parts, err.Error())
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

func (s *Service) SaveRanking(name string, payload any) {
	if err := s.store.SaveRanking(name, payload); err != nil {
		log.Printf("failed to save ranking %s: %v", name, err)
	}
}

func serviceError(message string) map[string]any {
	return map[string]any{"success": false, "error": message}
}

func ensureData(items []AggregatedSymbol) error {
	if len(items) == 0 {
		return fmt.Errorf("market data unavailable")
	}
	return nil
}
