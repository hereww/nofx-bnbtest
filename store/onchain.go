package store

import (
	"fmt"
	"math"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	OnchainJobStatusQueued    = "queued"
	OnchainJobStatusIndexing  = "indexing"
	OnchainJobStatusCompleted = "completed"
	OnchainJobStatusFailed    = "failed"

	OnchainWalletAccumulator  = "accumulator"
	OnchainWalletSeller       = "seller"
	OnchainWalletArbBot       = "arb_bot"
	OnchainWalletLPOrContract = "lp_or_contract"
	OnchainWalletUnknown      = "unknown_holder"
)

type OnchainToken struct {
	ID                   int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Chain                string    `gorm:"column:chain;not null;uniqueIndex:idx_onchain_token_chain_address,priority:1" json:"chain"`
	Address              string    `gorm:"column:address;not null;uniqueIndex:idx_onchain_token_chain_address,priority:2" json:"address"`
	Name                 string    `gorm:"column:name;default:''" json:"name"`
	Symbol               string    `gorm:"column:symbol;default:''" json:"symbol"`
	Decimals             int       `gorm:"column:decimals;default:18" json:"decimals"`
	TotalSupply          string    `gorm:"column:total_supply;default:''" json:"total_supply"`
	IndexStatus          string    `gorm:"column:index_status;default:'';index" json:"index_status"`
	LastIndexedBlock     int64     `gorm:"column:last_indexed_block;default:0" json:"last_indexed_block"`
	LastIndexedAt        int64     `gorm:"column:last_indexed_at;default:0" json:"last_indexed_at"`
	AnalysisCompleteness string    `gorm:"column:analysis_completeness;default:''" json:"analysis_completeness"`
	CreatedAt            time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (OnchainToken) TableName() string { return "onchain_tokens" }

type OnchainPool struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Chain          string    `gorm:"column:chain;not null;uniqueIndex:idx_onchain_pool_unique,priority:1" json:"chain"`
	TokenAddress   string    `gorm:"column:token_address;not null;uniqueIndex:idx_onchain_pool_unique,priority:2;index" json:"token_address"`
	PoolAddress    string    `gorm:"column:pool_address;not null;uniqueIndex:idx_onchain_pool_unique,priority:3;index" json:"pool_address"`
	DexID          string    `gorm:"column:dex_id;default:''" json:"dex_id"`
	Name           string    `gorm:"column:name;default:''" json:"name"`
	BaseToken      string    `gorm:"column:base_token;default:''" json:"base_token"`
	QuoteToken     string    `gorm:"column:quote_token;default:''" json:"quote_token"`
	Token0         string    `gorm:"column:token0;default:''" json:"token0"`
	Token1         string    `gorm:"column:token1;default:''" json:"token1"`
	Token0Decimals int       `gorm:"column:token0_decimals;default:18" json:"token0_decimals"`
	Token1Decimals int       `gorm:"column:token1_decimals;default:18" json:"token1_decimals"`
	CreatedBlock   int64     `gorm:"column:created_block;default:0;index" json:"created_block"`
	CreatedAtMS    int64     `gorm:"column:created_at_ms;default:0" json:"created_at_ms"`
	LiquidityUSD   float64   `gorm:"column:liquidity_usd;default:0" json:"liquidity_usd"`
	PairURL        string    `gorm:"column:pair_url;default:''" json:"pair_url"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (OnchainPool) TableName() string { return "onchain_pools" }

type OnchainTokenTransfer struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Chain        string    `gorm:"column:chain;not null;uniqueIndex:idx_onchain_transfer_unique,priority:1" json:"chain"`
	TokenAddress string    `gorm:"column:token_address;not null;uniqueIndex:idx_onchain_transfer_unique,priority:2;index" json:"token_address"`
	TxHash       string    `gorm:"column:tx_hash;not null;uniqueIndex:idx_onchain_transfer_unique,priority:3;index" json:"tx_hash"`
	LogIndex     int64     `gorm:"column:log_index;not null;uniqueIndex:idx_onchain_transfer_unique,priority:4" json:"log_index"`
	BlockNumber  int64     `gorm:"column:block_number;not null;index" json:"block_number"`
	BlockTime    int64     `gorm:"column:block_time;default:0;index" json:"block_time"`
	FromAddress  string    `gorm:"column:from_address;not null;index" json:"from_address"`
	ToAddress    string    `gorm:"column:to_address;not null;index" json:"to_address"`
	AmountRaw    string    `gorm:"column:amount_raw;default:''" json:"amount_raw"`
	Amount       float64   `gorm:"column:amount;default:0" json:"amount"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (OnchainTokenTransfer) TableName() string { return "onchain_token_transfers" }

type OnchainSwap struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Chain         string    `gorm:"column:chain;not null;uniqueIndex:idx_onchain_swap_unique,priority:1" json:"chain"`
	TokenAddress  string    `gorm:"column:token_address;not null;uniqueIndex:idx_onchain_swap_unique,priority:2;index" json:"token_address"`
	PoolAddress   string    `gorm:"column:pool_address;not null;index" json:"pool_address"`
	TxHash        string    `gorm:"column:tx_hash;not null;uniqueIndex:idx_onchain_swap_unique,priority:3;index" json:"tx_hash"`
	LogIndex      int64     `gorm:"column:log_index;not null;uniqueIndex:idx_onchain_swap_unique,priority:4" json:"log_index"`
	BlockNumber   int64     `gorm:"column:block_number;not null;index" json:"block_number"`
	BlockTime     int64     `gorm:"column:block_time;default:0;index" json:"block_time"`
	EventType     string    `gorm:"column:event_type;default:'swap';index" json:"event_type"`
	TraderAddress string    `gorm:"column:trader_address;default:'';index" json:"trader_address"`
	Side          string    `gorm:"column:side;default:'';index" json:"side"`
	TokenAmount   float64   `gorm:"column:token_amount;default:0" json:"token_amount"`
	QuoteAmount   float64   `gorm:"column:quote_amount;default:0" json:"quote_amount"`
	QuoteToken    string    `gorm:"column:quote_token;default:''" json:"quote_token"`
	EstimatedUSD  float64   `gorm:"column:estimated_usd;default:0" json:"estimated_usd"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (OnchainSwap) TableName() string { return "onchain_swaps" }

type OnchainWalletSnapshot struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Chain           string    `gorm:"column:chain;not null;uniqueIndex:idx_onchain_wallet_unique,priority:1" json:"chain"`
	TokenAddress    string    `gorm:"column:token_address;not null;uniqueIndex:idx_onchain_wallet_unique,priority:2;index" json:"token_address"`
	WalletAddress   string    `gorm:"column:wallet_address;not null;uniqueIndex:idx_onchain_wallet_unique,priority:3;index" json:"wallet_address"`
	CurrentBalance  float64   `gorm:"column:current_balance;default:0" json:"current_balance"`
	FirstBuyTime    int64     `gorm:"column:first_buy_time;default:0;index" json:"first_buy_time"`
	BuyCount        int       `gorm:"column:buy_count;default:0" json:"buy_count"`
	SellCount       int       `gorm:"column:sell_count;default:0" json:"sell_count"`
	BuyAmount       float64   `gorm:"column:buy_amount;default:0" json:"buy_amount"`
	SellAmount      float64   `gorm:"column:sell_amount;default:0" json:"sell_amount"`
	NetBoughtAmount float64   `gorm:"column:net_bought_amount;default:0" json:"net_bought_amount"`
	PoolTouchCount  int       `gorm:"column:pool_touch_count;default:0" json:"pool_touch_count"`
	WalletType      string    `gorm:"column:wallet_type;default:'unknown_holder';index" json:"wallet_type"`
	UpdatedAtMS     int64     `gorm:"column:updated_at_ms;default:0" json:"updated_at_ms"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (OnchainWalletSnapshot) TableName() string { return "onchain_wallet_snapshots" }

type OnchainIndexJob struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Chain         string    `gorm:"column:chain;not null;uniqueIndex:idx_onchain_job_unique,priority:1" json:"chain"`
	TokenAddress  string    `gorm:"column:token_address;not null;uniqueIndex:idx_onchain_job_unique,priority:2;index" json:"token_address"`
	Status        string    `gorm:"column:status;not null;default:'queued';index" json:"status"`
	ErrorMessage  string    `gorm:"column:error_message;default:''" json:"error_message"`
	StartBlock    int64     `gorm:"column:start_block;default:0" json:"start_block"`
	EndBlock      int64     `gorm:"column:end_block;default:0" json:"end_block"`
	LastBlock     int64     `gorm:"column:last_block;default:0" json:"last_block"`
	BatchSize     int       `gorm:"column:batch_size;default:0" json:"batch_size"`
	StartedAtMS   int64     `gorm:"column:started_at_ms;default:0" json:"started_at_ms"`
	CompletedAtMS int64     `gorm:"column:completed_at_ms;default:0" json:"completed_at_ms"`
	UpdatedAtMS   int64     `gorm:"column:updated_at_ms;default:0" json:"updated_at_ms"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (OnchainIndexJob) TableName() string { return "onchain_index_jobs" }

type OnchainStore struct {
	db *gorm.DB
}

func NewOnchainStore(db *gorm.DB) *OnchainStore {
	return &OnchainStore{db: db}
}

func (s *OnchainStore) initTables() error {
	if err := s.db.AutoMigrate(
		&OnchainToken{},
		&OnchainPool{},
		&OnchainTokenTransfer{},
		&OnchainSwap{},
		&OnchainWalletSnapshot{},
		&OnchainIndexJob{},
	); err != nil {
		return fmt.Errorf("failed to migrate onchain tables: %w", err)
	}
	return nil
}

func (s *OnchainStore) UpsertToken(token *OnchainToken) error {
	normalizeOnchainToken(token)
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "chain"}, {Name: "address"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "symbol", "decimals", "total_supply", "index_status",
			"last_indexed_block", "last_indexed_at", "analysis_completeness", "updated_at",
		}),
	}).Create(token).Error
}

func (s *OnchainStore) GetToken(chain, address string) (*OnchainToken, error) {
	var token OnchainToken
	err := s.db.Where("chain = ? AND address = ?", normalizeChain(chain), normalizeAddress(address)).First(&token).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &token, nil
}

func (s *OnchainStore) UpsertPools(pools []OnchainPool) error {
	if len(pools) == 0 {
		return nil
	}
	for i := range pools {
		pools[i].Chain = normalizeChain(pools[i].Chain)
		pools[i].TokenAddress = normalizeAddress(pools[i].TokenAddress)
		pools[i].PoolAddress = normalizeAddress(pools[i].PoolAddress)
		pools[i].BaseToken = normalizeAddress(pools[i].BaseToken)
		pools[i].QuoteToken = normalizeAddress(pools[i].QuoteToken)
		pools[i].Token0 = normalizeAddress(pools[i].Token0)
		pools[i].Token1 = normalizeAddress(pools[i].Token1)
		if pools[i].Token0Decimals == 0 {
			pools[i].Token0Decimals = 18
		}
		if pools[i].Token1Decimals == 0 {
			pools[i].Token1Decimals = 18
		}
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "chain"}, {Name: "token_address"}, {Name: "pool_address"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"dex_id", "name", "base_token", "quote_token", "token0", "token1",
			"token0_decimals", "token1_decimals", "created_block", "created_at_ms",
			"liquidity_usd", "pair_url", "updated_at",
		}),
	}).Create(&pools).Error
}

func (s *OnchainStore) ListPools(chain, tokenAddress string) ([]OnchainPool, error) {
	var pools []OnchainPool
	err := s.db.Where("chain = ? AND token_address = ?", normalizeChain(chain), normalizeAddress(tokenAddress)).
		Order("liquidity_usd DESC").Find(&pools).Error
	return pools, err
}

func (s *OnchainStore) InsertTransfers(transfers []OnchainTokenTransfer) error {
	if len(transfers) == 0 {
		return nil
	}
	for i := range transfers {
		transfers[i].Chain = normalizeChain(transfers[i].Chain)
		transfers[i].TokenAddress = normalizeAddress(transfers[i].TokenAddress)
		transfers[i].FromAddress = normalizeAddress(transfers[i].FromAddress)
		transfers[i].ToAddress = normalizeAddress(transfers[i].ToAddress)
		transfers[i].TxHash = strings.ToLower(strings.TrimSpace(transfers[i].TxHash))
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&transfers).Error
}

func (s *OnchainStore) InsertSwaps(swaps []OnchainSwap) error {
	if len(swaps) == 0 {
		return nil
	}
	for i := range swaps {
		swaps[i].Chain = normalizeChain(swaps[i].Chain)
		swaps[i].TokenAddress = normalizeAddress(swaps[i].TokenAddress)
		swaps[i].PoolAddress = normalizeAddress(swaps[i].PoolAddress)
		swaps[i].TraderAddress = normalizeAddress(swaps[i].TraderAddress)
		swaps[i].QuoteToken = normalizeAddress(swaps[i].QuoteToken)
		swaps[i].TxHash = strings.ToLower(strings.TrimSpace(swaps[i].TxHash))
	}
	return s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&swaps).Error
}

func (s *OnchainStore) UpsertJob(job *OnchainIndexJob) error {
	job.Chain = normalizeChain(job.Chain)
	job.TokenAddress = normalizeAddress(job.TokenAddress)
	now := time.Now().UTC().UnixMilli()
	if job.UpdatedAtMS == 0 {
		job.UpdatedAtMS = now
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "chain"}, {Name: "token_address"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"status", "error_message", "start_block", "end_block", "last_block",
			"batch_size", "started_at_ms", "completed_at_ms", "updated_at_ms", "updated_at",
		}),
	}).Create(job).Error
}

func (s *OnchainStore) GetJob(chain, tokenAddress string) (*OnchainIndexJob, error) {
	var job OnchainIndexJob
	err := s.db.Where("chain = ? AND token_address = ?", normalizeChain(chain), normalizeAddress(tokenAddress)).First(&job).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *OnchainStore) ListActiveJobs() ([]OnchainIndexJob, error) {
	var jobs []OnchainIndexJob
	err := s.db.Where("status IN ?", []string{OnchainJobStatusQueued, OnchainJobStatusIndexing}).
		Order("updated_at_ms ASC").Find(&jobs).Error
	return jobs, err
}

func (s *OnchainStore) ListWalletSnapshots(chain, tokenAddress, walletType string, limit int, orderBy string) ([]OnchainWalletSnapshot, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query := s.db.Where("chain = ? AND token_address = ?", normalizeChain(chain), normalizeAddress(tokenAddress))
	if walletType != "" {
		query = query.Where("wallet_type = ?", walletType)
	}
	switch orderBy {
	case "sell_amount":
		query = query.Order("sell_amount DESC")
	case "first_buy_time":
		query = query.Order("first_buy_time ASC")
	default:
		query = query.Order("net_bought_amount DESC")
	}
	var wallets []OnchainWalletSnapshot
	err := query.Limit(limit).Find(&wallets).Error
	return wallets, err
}

func (s *OnchainStore) CountWalletSnapshots(chain, tokenAddress string) (int64, error) {
	var count int64
	err := s.db.Model(&OnchainWalletSnapshot{}).
		Where("chain = ? AND token_address = ?", normalizeChain(chain), normalizeAddress(tokenAddress)).
		Count(&count).Error
	return count, err
}

func (s *OnchainStore) FirstBuyBuckets(chain, tokenAddress string) ([]OnchainWalletSnapshot, error) {
	var wallets []OnchainWalletSnapshot
	err := s.db.
		Where("chain = ? AND token_address = ? AND first_buy_time > 0", normalizeChain(chain), normalizeAddress(tokenAddress)).
		Order("first_buy_time ASC").
		Find(&wallets).Error
	return wallets, err
}

func (s *OnchainStore) ListTransfersForGraph(chain, tokenAddress string, addresses []string, limit int) ([]OnchainTokenTransfer, error) {
	if limit <= 0 || limit > 500 {
		limit = 160
	}
	chain = normalizeChain(chain)
	tokenAddress = normalizeAddress(tokenAddress)
	cleanAddresses := make([]string, 0, len(addresses))
	seen := map[string]bool{}
	for _, address := range addresses {
		address = normalizeAddress(address)
		if address == "" || seen[address] {
			continue
		}
		seen[address] = true
		cleanAddresses = append(cleanAddresses, address)
	}
	if len(cleanAddresses) == 0 {
		return nil, nil
	}
	var transfers []OnchainTokenTransfer
	err := s.db.Where("chain = ? AND token_address = ? AND from_address IN ? AND to_address IN ?", chain, tokenAddress, cleanAddresses, cleanAddresses).
		Order("block_number DESC, log_index DESC").
		Limit(limit).
		Find(&transfers).Error
	return transfers, err
}

func (s *OnchainStore) RecomputeWalletSnapshots(chain, tokenAddress string) error {
	chain = normalizeChain(chain)
	tokenAddress = normalizeAddress(tokenAddress)

	pools, err := s.ListPools(chain, tokenAddress)
	if err != nil {
		return err
	}
	poolSet := map[string]bool{
		"0x0000000000000000000000000000000000000000": true,
		"0x000000000000000000000000000000000000dead": true,
		tokenAddress: true,
	}
	for _, pool := range pools {
		poolSet[pool.PoolAddress] = true
	}

	var transfers []OnchainTokenTransfer
	if err := s.db.Where("chain = ? AND token_address = ?", chain, tokenAddress).
		Order("block_number ASC, log_index ASC").Find(&transfers).Error; err != nil {
		return err
	}

	type acc struct {
		balance    float64
		firstBuy   int64
		buyCount   int
		sellCount  int
		buyAmount  float64
		sellAmount float64
		pools      map[string]bool
	}
	accounts := make(map[string]*acc)
	get := func(address string) *acc {
		address = normalizeAddress(address)
		if address == "" {
			address = "unknown"
		}
		if accounts[address] == nil {
			accounts[address] = &acc{pools: make(map[string]bool)}
		}
		return accounts[address]
	}

	for _, tr := range transfers {
		from := normalizeAddress(tr.FromAddress)
		to := normalizeAddress(tr.ToAddress)
		amount := tr.Amount
		if amount == 0 {
			continue
		}
		get(from).balance -= amount
		get(to).balance += amount

		if poolSet[from] && !poolSet[to] {
			w := get(to)
			w.buyCount++
			w.buyAmount += amount
			w.pools[from] = true
			if tr.BlockTime > 0 && (w.firstBuy == 0 || tr.BlockTime < w.firstBuy) {
				w.firstBuy = tr.BlockTime
			}
		}
		if poolSet[to] && !poolSet[from] {
			w := get(from)
			w.sellCount++
			w.sellAmount += amount
			w.pools[to] = true
		}
	}

	snapshots := make([]OnchainWalletSnapshot, 0, len(accounts))
	now := time.Now().UTC().UnixMilli()
	for address, account := range accounts {
		if math.Abs(account.balance) < 1e-12 && account.buyCount == 0 && account.sellCount == 0 {
			continue
		}
		net := account.buyAmount - account.sellAmount
		totalPoolFlow := account.buyAmount + account.sellAmount
		walletType := classifyOnchainWallet(address, poolSet[address], account.buyCount, account.sellCount, net, totalPoolFlow, len(account.pools))
		snapshots = append(snapshots, OnchainWalletSnapshot{
			Chain:           chain,
			TokenAddress:    tokenAddress,
			WalletAddress:   address,
			CurrentBalance:  account.balance,
			FirstBuyTime:    account.firstBuy,
			BuyCount:        account.buyCount,
			SellCount:       account.sellCount,
			BuyAmount:       account.buyAmount,
			SellAmount:      account.sellAmount,
			NetBoughtAmount: net,
			PoolTouchCount:  len(account.pools),
			WalletType:      walletType,
			UpdatedAtMS:     now,
		})
	}
	if len(snapshots) == 0 {
		return nil
	}
	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "chain"}, {Name: "token_address"}, {Name: "wallet_address"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"current_balance", "first_buy_time", "buy_count", "sell_count",
			"buy_amount", "sell_amount", "net_bought_amount", "pool_touch_count",
			"wallet_type", "updated_at_ms", "updated_at",
		}),
	}).Create(&snapshots).Error
}

func classifyOnchainWallet(address string, isKnownPoolOrContract bool, buyCount, sellCount int, net, totalPoolFlow float64, poolCount int) string {
	address = normalizeAddress(address)
	if isKnownPoolOrContract || address == "0x0000000000000000000000000000000000000000" || address == "0x000000000000000000000000000000000000dead" {
		return OnchainWalletLPOrContract
	}
	if buyCount+sellCount >= 10 && poolCount >= 2 && totalPoolFlow > 0 && math.Abs(net) <= math.Max(totalPoolFlow*0.05, 1) {
		return OnchainWalletArbBot
	}
	if net > 0 {
		return OnchainWalletAccumulator
	}
	if net < 0 || sellCount > buyCount {
		return OnchainWalletSeller
	}
	return OnchainWalletUnknown
}

func normalizeOnchainToken(token *OnchainToken) {
	if token == nil {
		return
	}
	token.Chain = normalizeChain(token.Chain)
	token.Address = normalizeAddress(token.Address)
	if token.Decimals == 0 {
		token.Decimals = 18
	}
}

func normalizeChain(chain string) string {
	chain = strings.ToLower(strings.TrimSpace(chain))
	if chain == "" {
		return "bsc"
	}
	return chain
}

func normalizeAddress(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}
