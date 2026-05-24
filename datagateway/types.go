package datagateway

import "time"

const (
	MarketTypeSpot   = "spot"
	MarketTypeFuture = "future"
)

type Config struct {
	Addr            string
	Token           string
	DBPath          string
	RefreshInterval time.Duration
}

type MarketSnapshot struct {
	Exchange          string
	MarketType        string
	Symbol            string
	Base              string
	Price             float64
	PriceChange1h     float64
	PriceChange4h     float64
	PriceChange24h    float64
	QuoteVolume24h    float64
	OpenInterestValue float64
	OpenInterestDelta float64
	FundingRate       float64
	FetchedAt         time.Time
}

type AggregatedSymbol struct {
	Symbol               string
	Base                 string
	Price                float64
	PriceChange1h        float64
	PriceChange4h        float64
	PriceChange24h       float64
	QuoteVolume24h       float64
	FutureQuoteVolume24h float64
	SpotQuoteVolume24h   float64
	OpenInterestValue    float64
	OpenInterestDelta    float64
	OpenInterestDeltaPct float64
	FundingRate          float64
	ExchangeCount        int
	HasFutureData        bool
	HasSpotData          bool
	Score                float64
	FetchedAt            time.Time
}

type AI500Coin struct {
	Pair            string  `json:"pair"`
	Symbol          string  `json:"symbol,omitempty"`
	Score           float64 `json:"score"`
	IsActive        bool    `json:"is_active,omitempty"`
	StartTime       int64   `json:"start_time"`
	StartPrice      float64 `json:"start_price"`
	LastScore       float64 `json:"last_score"`
	MaxScore        float64 `json:"max_score"`
	MaxPrice        float64 `json:"max_price"`
	IncreasePercent float64 `json:"increase_percent"`
}

type OIPosition struct {
	Symbol            string  `json:"symbol"`
	Rank              int     `json:"rank"`
	Price             float64 `json:"price"`
	CurrentOI         float64 `json:"current_oi"`
	OIDelta           float64 `json:"oi_delta"`
	OIDeltaPercent    float64 `json:"oi_delta_percent"`
	OIDeltaValue      float64 `json:"oi_delta_value"`
	PriceDeltaPercent float64 `json:"price_delta_percent"`
	NetLong           float64 `json:"net_long"`
	NetShort          float64 `json:"net_short"`
}

type NetFlowPosition struct {
	Rank   int     `json:"rank"`
	Symbol string  `json:"symbol"`
	Amount float64 `json:"amount"`
	Price  float64 `json:"price"`
}

type PriceRankingItem struct {
	Pair         string  `json:"pair"`
	Symbol       string  `json:"symbol"`
	PriceDelta   float64 `json:"price_delta"`
	Price        float64 `json:"price"`
	FutureFlow   float64 `json:"future_flow"`
	SpotFlow     float64 `json:"spot_flow"`
	OI           float64 `json:"oi"`
	OIDelta      float64 `json:"oi_delta"`
	OIDeltaValue float64 `json:"oi_delta_value"`
}

type PriceRankingDuration struct {
	Top []PriceRankingItem `json:"top"`
	Low []PriceRankingItem `json:"low"`
}

type QuantData struct {
	Symbol      string             `json:"symbol"`
	Price       float64            `json:"price"`
	Netflow     *NetflowData       `json:"netflow,omitempty"`
	OI          map[string]*OIData `json:"oi,omitempty"`
	PriceChange map[string]float64 `json:"price_change,omitempty"`
	AI500       *AI500Coin         `json:"ai500,omitempty"`
}

type NetflowData struct {
	Institution *FlowTypeData `json:"institution,omitempty"`
	Personal    *FlowTypeData `json:"personal,omitempty"`
}

type FlowTypeData struct {
	Future map[string]float64 `json:"future,omitempty"`
	Spot   map[string]float64 `json:"spot,omitempty"`
}

type OIData struct {
	CurrentOI float64                 `json:"current_oi"`
	NetLong   float64                 `json:"net_long"`
	NetShort  float64                 `json:"net_short"`
	Delta     map[string]*OIDeltaData `json:"delta,omitempty"`
}

type OIDeltaData struct {
	OIDelta        float64 `json:"oi_delta"`
	OIDeltaValue   float64 `json:"oi_delta_value"`
	OIDeltaPercent float64 `json:"oi_delta_percent"`
}

type healthPayload struct {
	Status           string    `json:"status"`
	Service          string    `json:"service"`
	Symbols          int       `json:"symbols"`
	SnapshotCount    int       `json:"snapshot_count"`
	LastRefresh      time.Time `json:"last_refresh"`
	LastRefreshError string    `json:"last_refresh_error,omitempty"`
}
