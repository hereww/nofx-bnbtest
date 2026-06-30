package onchain

import (
	"context"
	"strings"
	"testing"

	"nofx/config"
	"nofx/store"
)

func TestEarlyWalletFlowWeightedCostTransfersAcrossDepths(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "http://archive.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	chain := "bsc"
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	pool := "0x1111111111111111111111111111111111111111"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	seed := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	child := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	grandchild := "0xcccccccccccccccccccccccccccccccccccccccc"

	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: chain, Address: token, Symbol: "FPHX", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain: chain, TokenAddress: token, PoolAddress: pool, DexID: "pancakeswap",
		Token0: token, Token1: quote, Token0Decimals: 18, Token1Decimals: 18,
	}}); err != nil {
		t.Fatalf("upsert pools: %v", err)
	}
	swaps := []store.OnchainSwap{
		{Chain: chain, TokenAddress: token, PoolAddress: pool, TxHash: "0xbuy1", LogIndex: 0, BlockNumber: 1, BlockTime: 1000, EventType: "swap", TraderAddress: seed, Side: "buy", TokenAmount: 1000, QuoteAmount: 100, QuoteToken: quote},
		{Chain: chain, TokenAddress: token, PoolAddress: pool, TxHash: "0xbuy2", LogIndex: 0, BlockNumber: 2, BlockTime: 2000, EventType: "swap", TraderAddress: seed, Side: "buy", TokenAmount: 1000, QuoteAmount: 300, QuoteToken: quote},
		{Chain: chain, TokenAddress: token, PoolAddress: pool, TxHash: "0xsell1", LogIndex: 0, BlockNumber: 5, BlockTime: 5000, EventType: "swap", TraderAddress: grandchild, Side: "sell", TokenAmount: 500, QuoteAmount: 150, QuoteToken: quote},
		{Chain: chain, TokenAddress: token, PoolAddress: pool, TxHash: "0xsell2", LogIndex: 0, BlockNumber: 6, BlockTime: 6000, EventType: "swap", TraderAddress: seed, Side: "sell", TokenAmount: 300, QuoteAmount: 120, QuoteToken: quote},
	}
	if err := st.Onchain().InsertSwaps(swaps); err != nil {
		t.Fatalf("insert swaps: %v", err)
	}
	transfers := []store.OnchainTokenTransfer{
		{Chain: chain, TokenAddress: token, TxHash: "0xt1", LogIndex: 0, BlockNumber: 3, BlockTime: 3000, FromAddress: seed, ToAddress: child, Amount: 1000},
		{Chain: chain, TokenAddress: token, TxHash: "0xt2", LogIndex: 0, BlockNumber: 4, BlockTime: 4000, FromAddress: child, ToAddress: grandchild, Amount: 500},
	}
	if err := st.Onchain().InsertTransfers(transfers); err != nil {
		t.Fatalf("insert transfers: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{Chain: chain, TokenAddress: token, Status: store.OnchainJobStatusCompleted, StartBlock: 1, EndBlock: 6, LastBlock: 6}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	resp, err := NewService(st).EarlyWalletFlow(context.Background(), EarlyWalletFlowRequest{Chain: chain, Address: token, SeedCount: 100, MaxDepth: 4})
	if err != nil {
		t.Fatalf("EarlyWalletFlow error: %v", err)
	}
	if resp.Status != StatusOK {
		t.Fatalf("status = %s message=%s", resp.Status, resp.Message)
	}
	if len(resp.Seeds) != 1 {
		t.Fatalf("seeds = %d, want 1", len(resp.Seeds))
	}
	assertFloatNear(t, resp.Seeds[0].BuyValue, 400, "seed buy value")
	assertFloatNear(t, resp.Seeds[0].AvgBuyPrice, 0.2, "seed avg price")
	assertFloatNear(t, resp.Seeds[0].TransferOutAmount, 1000, "seed transfer out")
	assertFloatNear(t, resp.Seeds[0].RealizedPnL, 60, "seed realized pnl")
	assertFloatNear(t, resp.Seeds[0].DescendantRealizedPnL, 50, "descendant realized pnl")
	assertFloatNear(t, resp.Summary.RealizedPnL, 110, "summary realized pnl")
	if resp.Summary.MaxObservedDepth != 2 {
		t.Fatalf("max depth = %d, want 2", resp.Summary.MaxObservedDepth)
	}
}

func TestEarlyWalletFlowDefaultsToFreeLocalIndexRequiredWithoutArchiveRPC(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	resp, err := NewService(st).EarlyWalletFlow(context.Background(), EarlyWalletFlowRequest{
		Chain:   "bsc",
		Address: "0x812fc5119b772c6c7a66249a559f3614623f4444",
	})
	if err != nil {
		t.Fatalf("EarlyWalletFlow error: %v", err)
	}
	if resp.Status != StatusIndexRequired {
		t.Fatalf("status = %s, want %s", resp.Status, StatusIndexRequired)
	}
	if resp.IndexSource != store.OnchainIndexSourceFreeLocal {
		t.Fatalf("index source = %s, want free_local", resp.IndexSource)
	}
}

func TestEarlyWalletFlowArchiveSourceReportsMissingArchiveRPC(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = ""
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	resp, err := NewService(st).EarlyWalletFlow(context.Background(), EarlyWalletFlowRequest{
		Chain:       "bsc",
		Address:     "0x812fc5119b772c6c7a66249a559f3614623f4444",
		IndexSource: store.OnchainIndexSourceArchiveRPC,
	})
	if err != nil {
		t.Fatalf("EarlyWalletFlow error: %v", err)
	}
	if resp.Status != StatusArchiveRPCMissing {
		t.Fatalf("status = %s, want %s", resp.Status, StatusArchiveRPCMissing)
	}
}

func TestEarlyWalletFlowIndexingReturnsProgressWithoutPartialConclusions(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "http://archive.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	chain := "bsc"
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: chain, Address: token, Symbol: "FPHX", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        chain,
		TokenAddress: token,
		Status:       store.OnchainJobStatusIndexing,
		StartBlock:   100,
		EndBlock:     200,
		LastBlock:    150,
		UpdatedAtMS:  123456,
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	resp, err := NewService(st).EarlyWalletFlow(context.Background(), EarlyWalletFlowRequest{Chain: chain, Address: token})
	if err != nil {
		t.Fatalf("EarlyWalletFlow error: %v", err)
	}
	if resp.Status != StatusIndexing {
		t.Fatalf("status = %s, want %s", resp.Status, StatusIndexing)
	}
	if resp.LastBlock != 150 || resp.EndBlock != 200 {
		t.Fatalf("progress last/end = %d/%d, want 150/200", resp.LastBlock, resp.EndBlock)
	}
	if len(resp.Summary.IncompleteReasons) != 1 || resp.Summary.IncompleteReasons[0] != "indexing" {
		t.Fatalf("incomplete reasons = %#v, want [indexing]", resp.Summary.IncompleteReasons)
	}
	if resp.Message == "No early buy wallets found from indexed swaps." {
		t.Fatalf("message should not report partial early-wallet conclusions while indexing")
	}
}

func TestEarlyWalletFlowIndexingReportsRateLimitRetry(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "http://archive.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	chain := "bsc"
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: chain, Address: token, Symbol: "FPHX", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{
		Chain:        chain,
		TokenAddress: token,
		Status:       store.OnchainJobStatusQueued,
		StartBlock:   100,
		EndBlock:     200,
		LastBlock:    150,
		UpdatedAtMS:  123456,
		ErrorMessage: "rpc rate limited; retrying",
	}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	resp, err := NewService(st).EarlyWalletFlow(context.Background(), EarlyWalletFlowRequest{Chain: chain, Address: token})
	if err != nil {
		t.Fatalf("EarlyWalletFlow error: %v", err)
	}
	if resp.Status != StatusIndexing {
		t.Fatalf("status = %s, want %s", resp.Status, StatusIndexing)
	}
	if !strings.Contains(resp.Message, "rate limited") {
		t.Fatalf("message = %q, want rate limited", resp.Message)
	}
	found := false
	for _, reason := range resp.Summary.IncompleteReasons {
		if reason == "rpc_rate_limited" {
			found = true
		}
	}
	if !found {
		t.Fatalf("incomplete reasons = %#v, want rpc_rate_limited", resp.Summary.IncompleteReasons)
	}
}

func TestEarlyWalletFlowMissingSellPriceKeepsQuantityButExcludesPnL(t *testing.T) {
	config.Init()
	config.Get().OnchainBSCArchiveRPCURL = "http://archive.example"

	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	chain := "bsc"
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	pool := "0x1111111111111111111111111111111111111111"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	seed := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := st.Onchain().UpsertToken(&store.OnchainToken{Chain: chain, Address: token, Symbol: "FPHX", Decimals: 18}); err != nil {
		t.Fatalf("upsert token: %v", err)
	}
	if err := st.Onchain().UpsertPools([]store.OnchainPool{{
		Chain: chain, TokenAddress: token, PoolAddress: pool, DexID: "pancakeswap",
		Token0: token, Token1: quote, Token0Decimals: 18, Token1Decimals: 18,
	}}); err != nil {
		t.Fatalf("upsert pools: %v", err)
	}
	if err := st.Onchain().InsertSwaps([]store.OnchainSwap{
		{Chain: chain, TokenAddress: token, PoolAddress: pool, TxHash: "0xbuy", LogIndex: 0, BlockNumber: 1, BlockTime: 1000, EventType: "swap", TraderAddress: seed, Side: "buy", TokenAmount: 1000, QuoteAmount: 100, QuoteToken: quote},
		{Chain: chain, TokenAddress: token, PoolAddress: pool, TxHash: "0xsell_missing_price", LogIndex: 0, BlockNumber: 2, BlockTime: 2000, EventType: "swap", TraderAddress: seed, Side: "sell", TokenAmount: 400, QuoteAmount: 0, QuoteToken: quote},
	}); err != nil {
		t.Fatalf("insert swaps: %v", err)
	}
	if err := st.Onchain().UpsertJob(&store.OnchainIndexJob{Chain: chain, TokenAddress: token, Status: store.OnchainJobStatusCompleted, StartBlock: 1, EndBlock: 2, LastBlock: 2}); err != nil {
		t.Fatalf("upsert job: %v", err)
	}

	resp, err := NewService(st).EarlyWalletFlow(context.Background(), EarlyWalletFlowRequest{Chain: chain, Address: token})
	if err != nil {
		t.Fatalf("EarlyWalletFlow error: %v", err)
	}
	if resp.Status != StatusPartialData {
		t.Fatalf("status = %s, want %s", resp.Status, StatusPartialData)
	}
	if resp.Summary.MissingSwapPriceCount != 1 {
		t.Fatalf("missing price count = %d, want 1", resp.Summary.MissingSwapPriceCount)
	}
	assertFloatNear(t, resp.Seeds[0].OwnSellAmount, 400, "seed own sell amount")
	assertFloatNear(t, resp.Seeds[0].OwnSellValue, 0, "seed own sell value")
	assertFloatNear(t, resp.Seeds[0].RealizedPnL, 0, "seed realized pnl")
	assertFloatNear(t, resp.Seeds[0].CurrentBalance, 600, "seed current balance")
	assertFloatNear(t, resp.Seeds[0].RemainingCost, 60, "seed remaining cost")
}

func assertFloatNear(t *testing.T, got, want float64, label string) {
	t.Helper()
	if got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}
