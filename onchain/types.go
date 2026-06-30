package onchain

type TokenAnalysisRequest struct {
	Chain      string `json:"chain"`
	Address    string `json:"address"`
	Depth      string `json:"depth"`
	StartBlock int64  `json:"start_block,omitempty"`
}

type TokenAnalysisResponse struct {
	Success         bool                     `json:"success"`
	Chain           string                   `json:"chain"`
	Address         string                   `json:"address"`
	Depth           string                   `json:"depth"`
	Status          string                   `json:"status"`
	Completeness    string                   `json:"completeness"`
	Message         string                   `json:"message,omitempty"`
	Token           TokenProfile             `json:"token"`
	Security        *TokenSecurity           `json:"security,omitempty"`
	Pools           []PoolSnapshot           `json:"pools,omitempty"`
	Recent          *RecentTradeAnalysis     `json:"recent,omitempty"`
	Full            *FullHistoryAnalysis     `json:"full,omitempty"`
	DealerFlow      *DealerFlowAnalysis      `json:"dealer_flow,omitempty"`
	EarlyWalletFlow *EarlyWalletFlowResponse `json:"early_wallet_flow,omitempty"`
	RiskFlags       []string                 `json:"risk_flags,omitempty"`
	Source          []string                 `json:"source,omitempty"`
	Error           string                   `json:"error,omitempty"`
}

type TokenProfile struct {
	Name        string  `json:"name,omitempty"`
	Symbol      string  `json:"symbol,omitempty"`
	Decimals    int     `json:"decimals,omitempty"`
	TotalSupply string  `json:"total_supply,omitempty"`
	PriceUSD    string  `json:"price_usd,omitempty"`
	FDV         float64 `json:"fdv,omitempty"`
	MarketCap   float64 `json:"market_cap,omitempty"`
}

type TokenSecurity struct {
	HolderCount        int              `json:"holder_count,omitempty"`
	IsOpenSource       string           `json:"is_open_source,omitempty"`
	IsHoneypot         string           `json:"is_honeypot,omitempty"`
	IsMintable         string           `json:"is_mintable,omitempty"`
	TransferPausable   string           `json:"transfer_pausable,omitempty"`
	SlippageModifiable string           `json:"slippage_modifiable,omitempty"`
	BuyTax             string           `json:"buy_tax,omitempty"`
	SellTax            string           `json:"sell_tax,omitempty"`
	OwnerAddress       string           `json:"owner_address,omitempty"`
	CreatorAddress     string           `json:"creator_address,omitempty"`
	TopHolders         []HolderSnapshot `json:"top_holders,omitempty"`
	LPHolders          []HolderSnapshot `json:"lp_holders,omitempty"`
}

type HolderSnapshot struct {
	Address    string  `json:"address"`
	Tag        string  `json:"tag,omitempty"`
	IsContract bool    `json:"is_contract,omitempty"`
	IsLocked   bool    `json:"is_locked,omitempty"`
	Balance    string  `json:"balance,omitempty"`
	Percent    float64 `json:"percent,omitempty"`
}

type PoolSnapshot struct {
	ChainID      string  `json:"chain_id,omitempty"`
	DexID        string  `json:"dex_id,omitempty"`
	Address      string  `json:"address"`
	Name         string  `json:"name,omitempty"`
	BaseToken    string  `json:"base_token,omitempty"`
	BaseSymbol   string  `json:"base_symbol,omitempty"`
	QuoteToken   string  `json:"quote_token,omitempty"`
	QuoteSymbol  string  `json:"quote_symbol,omitempty"`
	TokenName    string  `json:"token_name,omitempty"`
	PriceUSD     string  `json:"price_usd,omitempty"`
	LiquidityUSD float64 `json:"liquidity_usd,omitempty"`
	Volume24hUSD float64 `json:"volume_24h_usd,omitempty"`
	PairURL      string  `json:"pair_url,omitempty"`
	CreatedAtMS  int64   `json:"created_at_ms,omitempty"`
	CreatedBlock int64   `json:"created_block,omitempty"`
}

type RecentTradeAnalysis struct {
	WindowStart           string           `json:"window_start,omitempty"`
	WindowEnd             string           `json:"window_end,omitempty"`
	TradeCount            int              `json:"trade_count"`
	BuyCount              int              `json:"buy_count"`
	SellCount             int              `json:"sell_count"`
	VolumeUSD             float64          `json:"volume_usd"`
	UniqueAddresses       int              `json:"unique_addresses"`
	UniqueBuyers          int              `json:"unique_buyers"`
	UniqueSellers         int              `json:"unique_sellers"`
	FirstBuyBuckets       []TimeBucket     `json:"first_buy_buckets,omitempty"`
	TopAccumulators       []WalletAnalysis `json:"top_accumulators,omitempty"`
	TopSellers            []WalletAnalysis `json:"top_sellers,omitempty"`
	RelatedWalletClusters []WalletAnalysis `json:"related_wallet_clusters,omitempty"`
}

type FullHistoryAnalysis struct {
	IndexedBlock          int64            `json:"indexed_block,omitempty"`
	StartBlock            int64            `json:"start_block,omitempty"`
	EndBlock              int64            `json:"end_block,omitempty"`
	LastUpdated           int64            `json:"last_updated,omitempty"`
	WalletCount           int              `json:"wallet_count"`
	FirstBuyBuckets       []TimeBucket     `json:"first_buy_buckets,omitempty"`
	TopAccumulators       []WalletAnalysis `json:"top_accumulators,omitempty"`
	TopSellers            []WalletAnalysis `json:"top_sellers,omitempty"`
	RelatedWalletClusters []WalletAnalysis `json:"related_wallet_clusters,omitempty"`
}

type TimeBucket struct {
	Bucket     string  `json:"bucket"`
	BuyerCount int     `json:"buyer_count"`
	BuyAmount  float64 `json:"buy_amount"`
	SellAmount float64 `json:"sell_amount"`
	NetAmount  float64 `json:"net_amount"`
}

type WalletAnalysis struct {
	Address          string  `json:"address"`
	WalletType       string  `json:"wallet_type"`
	FirstBuyTime     int64   `json:"first_buy_time,omitempty"`
	FirstBuyAt       string  `json:"first_buy_at,omitempty"`
	BuyCount         int     `json:"buy_count"`
	SellCount        int     `json:"sell_count"`
	BuyAmount        float64 `json:"buy_amount"`
	SellAmount       float64 `json:"sell_amount"`
	NetBoughtAmount  float64 `json:"net_bought_amount"`
	CurrentBalance   float64 `json:"current_balance,omitempty"`
	PoolTouchCount   int     `json:"pool_touch_count,omitempty"`
	EstimatedUSDFlow float64 `json:"estimated_usd_flow,omitempty"`
}

type DealerFlowAnalysis struct {
	Direction       string            `json:"direction"`
	Score           int               `json:"score"`
	Confidence      string            `json:"confidence"`
	Summary         string            `json:"summary"`
	Reasons         []string          `json:"reasons,omitempty"`
	MonitoringPlan  []string          `json:"monitoring_plan,omitempty"`
	EvidenceDepth   string            `json:"evidence_depth"`
	SignalBreakdown DealerFlowSignals `json:"signal_breakdown"`
}

type DealerFlowSignals struct {
	TradeCount            int     `json:"trade_count"`
	BuyCount              int     `json:"buy_count"`
	SellCount             int     `json:"sell_count"`
	UniqueBuyers          int     `json:"unique_buyers"`
	UniqueSellers         int     `json:"unique_sellers"`
	WalletCount           int     `json:"wallet_count,omitempty"`
	AccumulatorCount      int     `json:"accumulator_count"`
	SellerCount           int     `json:"seller_count"`
	RelatedWalletCount    int     `json:"related_wallet_count"`
	AccumulatorNetAmount  float64 `json:"accumulator_net_amount"`
	SellerNetAmount       float64 `json:"seller_net_amount"`
	NetAmount             float64 `json:"net_amount"`
	TopAccumulatorNet     float64 `json:"top_accumulator_net,omitempty"`
	TopSellerNet          float64 `json:"top_seller_net,omitempty"`
	FirstBuyBucketCount   int     `json:"first_buy_bucket_count,omitempty"`
	FirstBuyConcentration float64 `json:"first_buy_concentration,omitempty"`
	TopHolderPercent      float64 `json:"top_holder_percent,omitempty"`
	TopFiveHolderPercent  float64 `json:"top_five_holder_percent,omitempty"`
	OwnerPresent          bool    `json:"owner_present,omitempty"`
	CreatorPresent        bool    `json:"creator_present,omitempty"`
	HighRiskFlagCount     int     `json:"high_risk_flag_count,omitempty"`
}

type IndexTokenRequest struct {
	Chain       string `json:"chain"`
	Address     string `json:"address"`
	Scope       string `json:"scope,omitempty"`
	IndexSource string `json:"index_source,omitempty"`
	StartBlock  int64  `json:"start_block,omitempty"`
	EndBlock    int64  `json:"end_block,omitempty"`
}

type IndexStatusResponse struct {
	Success                bool   `json:"success"`
	Chain                  string `json:"chain"`
	Address                string `json:"address"`
	Status                 string `json:"status"`
	Scope                  string `json:"scope,omitempty"`
	IndexSource            string `json:"index_source,omitempty"`
	Phase                  string `json:"phase,omitempty"`
	Provider               string `json:"provider,omitempty"`
	RequestBudgetRemaining int    `json:"request_budget_remaining,omitempty"`
	Message                string `json:"message,omitempty"`
	ProgressMessage        string `json:"progress_message,omitempty"`
	StartBlock             int64  `json:"start_block,omitempty"`
	EndBlock               int64  `json:"end_block,omitempty"`
	LastBlock              int64  `json:"last_block,omitempty"`
	SeedWallets            int64  `json:"seed_wallets,omitempty"`
	UpdatedAtMS            int64  `json:"updated_at_ms,omitempty"`
	ErrorMessage           string `json:"error_message,omitempty"`
}

type WalletGraphRequest struct {
	Chain   string `json:"chain"`
	Address string `json:"address"`
	Depth   string `json:"depth"`
	Limit   int    `json:"limit,omitempty"`
}

type WalletGraphResponse struct {
	Success    bool                `json:"success"`
	Chain      string              `json:"chain"`
	Address    string              `json:"address"`
	Depth      string              `json:"depth"`
	Status     string              `json:"status"`
	Message    string              `json:"message,omitempty"`
	Token      TokenProfile        `json:"token"`
	DealerFlow *DealerFlowAnalysis `json:"dealer_flow,omitempty"`
	Nodes      []WalletGraphNode   `json:"nodes,omitempty"`
	Edges      []WalletGraphEdge   `json:"edges,omitempty"`
	Error      string              `json:"error,omitempty"`
}

type WalletGraphNode struct {
	ID              string  `json:"id"`
	Label           string  `json:"label"`
	NodeType        string  `json:"node_type"`
	Address         string  `json:"address,omitempty"`
	WalletType      string  `json:"wallet_type,omitempty"`
	Value           float64 `json:"value,omitempty"`
	Percent         float64 `json:"percent,omitempty"`
	BuyCount        int     `json:"buy_count,omitempty"`
	SellCount       int     `json:"sell_count,omitempty"`
	BuyAmount       float64 `json:"buy_amount,omitempty"`
	SellAmount      float64 `json:"sell_amount,omitempty"`
	NetBoughtAmount float64 `json:"net_bought_amount,omitempty"`
	FirstBuyAt      string  `json:"first_buy_at,omitempty"`
}

type WalletGraphEdge struct {
	Source   string  `json:"source"`
	Target   string  `json:"target"`
	Relation string  `json:"relation"`
	Amount   float64 `json:"amount,omitempty"`
	Weight   float64 `json:"weight,omitempty"`
	TxHash   string  `json:"tx_hash,omitempty"`
}

type EarlyWalletFlowRequest struct {
	Chain       string `json:"chain"`
	Address     string `json:"address"`
	IndexSource string `json:"index_source,omitempty"`
	SeedCount   int    `json:"seed_count,omitempty"`
	MaxDepth    int    `json:"max_depth,omitempty"`
}

type EarlyWalletFlowResponse struct {
	Success                bool                    `json:"success"`
	Chain                  string                  `json:"chain"`
	Address                string                  `json:"address"`
	Status                 string                  `json:"status"`
	Completeness           string                  `json:"completeness"`
	Message                string                  `json:"message,omitempty"`
	IndexSource            string                  `json:"index_source,omitempty"`
	Phase                  string                  `json:"phase,omitempty"`
	Provider               string                  `json:"provider,omitempty"`
	RequestBudgetRemaining int                     `json:"request_budget_remaining,omitempty"`
	ProgressMessage        string                  `json:"progress_message,omitempty"`
	StartBlock             int64                   `json:"start_block,omitempty"`
	EndBlock               int64                   `json:"end_block,omitempty"`
	LastBlock              int64                   `json:"last_block,omitempty"`
	SeedWallets            int64                   `json:"seed_wallets,omitempty"`
	UpdatedAtMS            int64                   `json:"updated_at_ms,omitempty"`
	Token                  TokenProfile            `json:"token"`
	SeedCount              int                     `json:"seed_count"`
	MaxDepth               int                     `json:"max_depth"`
	QuoteToken             string                  `json:"quote_token,omitempty"`
	QuoteSymbol            string                  `json:"quote_symbol,omitempty"`
	Summary                EarlyWalletFlowSummary  `json:"summary"`
	Seeds                  []EarlyWalletSeed       `json:"seeds,omitempty"`
	Wallets                []EarlyWalletFlowWallet `json:"wallets,omitempty"`
	Edges                  []EarlyWalletFlowEdge   `json:"edges,omitempty"`
	Error                  string                  `json:"error,omitempty"`
}

type EarlyWalletFlowSummary struct {
	Direction             string   `json:"direction"`
	Confidence            string   `json:"confidence"`
	SeedWalletCount       int      `json:"seed_wallet_count"`
	TrackedWalletCount    int      `json:"tracked_wallet_count"`
	MaxObservedDepth      int      `json:"max_observed_depth"`
	TotalInitialBuyAmount float64  `json:"total_initial_buy_amount"`
	TotalInitialCost      float64  `json:"total_initial_cost"`
	SeedOwnSellAmount     float64  `json:"seed_own_sell_amount"`
	SeedOwnSellValue      float64  `json:"seed_own_sell_value"`
	DescendantSellAmount  float64  `json:"descendant_sell_amount"`
	DescendantSellValue   float64  `json:"descendant_sell_value"`
	TotalSellValue        float64  `json:"total_sell_value"`
	RealizedPnL           float64  `json:"realized_pnl"`
	RealizedPnLPct        float64  `json:"realized_pnl_pct"`
	RemainingAmount       float64  `json:"remaining_amount"`
	RemainingCost         float64  `json:"remaining_cost"`
	TransferOutAmount     float64  `json:"transfer_out_amount"`
	CostCoveragePct       float64  `json:"cost_coverage_pct"`
	MissingSwapPriceCount int      `json:"missing_swap_price_count"`
	IncompleteReasons     []string `json:"incomplete_reasons,omitempty"`
}

type EarlyWalletSeed struct {
	Rank                  int     `json:"rank"`
	Address               string  `json:"address"`
	FirstBuyTime          int64   `json:"first_buy_time,omitempty"`
	FirstBuyAt            string  `json:"first_buy_at,omitempty"`
	FirstBuyBlock         int64   `json:"first_buy_block,omitempty"`
	BuyCount              int     `json:"buy_count"`
	BuyAmount             float64 `json:"buy_amount"`
	BuyValue              float64 `json:"buy_value"`
	AvgBuyPrice           float64 `json:"avg_buy_price"`
	OwnSellAmount         float64 `json:"own_sell_amount"`
	OwnSellValue          float64 `json:"own_sell_value"`
	TransferOutAmount     float64 `json:"transfer_out_amount"`
	CurrentBalance        float64 `json:"current_balance"`
	RemainingCost         float64 `json:"remaining_cost"`
	RealizedPnL           float64 `json:"realized_pnl"`
	DescendantSellAmount  float64 `json:"descendant_sell_amount"`
	DescendantSellValue   float64 `json:"descendant_sell_value"`
	DescendantRealizedPnL float64 `json:"descendant_realized_pnl"`
	TotalRealizedPnL      float64 `json:"total_realized_pnl"`
	ChildCount            int     `json:"child_count"`
}

type EarlyWalletFlowWallet struct {
	Address           string  `json:"address"`
	RootAddress       string  `json:"root_address,omitempty"`
	ParentAddress     string  `json:"parent_address,omitempty"`
	Depth             int     `json:"depth"`
	FirstSeenTime     int64   `json:"first_seen_time,omitempty"`
	FirstSeenAt       string  `json:"first_seen_at,omitempty"`
	BuyCount          int     `json:"buy_count"`
	SellCount         int     `json:"sell_count"`
	BuyAmount         float64 `json:"buy_amount"`
	BuyValue          float64 `json:"buy_value"`
	AvgBuyPrice       float64 `json:"avg_buy_price"`
	SellAmount        float64 `json:"sell_amount"`
	SellValue         float64 `json:"sell_value"`
	TransferInAmount  float64 `json:"transfer_in_amount"`
	TransferOutAmount float64 `json:"transfer_out_amount"`
	CurrentBalance    float64 `json:"current_balance"`
	AllocatedCost     float64 `json:"allocated_cost"`
	RemainingCost     float64 `json:"remaining_cost"`
	RealizedPnL       float64 `json:"realized_pnl"`
	RealizedPnLPct    float64 `json:"realized_pnl_pct"`
	CostCoveragePct   float64 `json:"cost_coverage_pct"`
	EventCount        int     `json:"event_count"`
	IncompletePricing int     `json:"incomplete_pricing"`
	IsSeed            bool    `json:"is_seed"`
}

type EarlyWalletFlowEdge struct {
	Source      string  `json:"source"`
	Target      string  `json:"target"`
	RootAddress string  `json:"root_address,omitempty"`
	Depth       int     `json:"depth"`
	Amount      float64 `json:"amount"`
	Cost        float64 `json:"cost,omitempty"`
	TxHash      string  `json:"tx_hash,omitempty"`
	BlockNumber int64   `json:"block_number,omitempty"`
	BlockTime   int64   `json:"block_time,omitempty"`
	BlockAt     string  `json:"block_at,omitempty"`
}
