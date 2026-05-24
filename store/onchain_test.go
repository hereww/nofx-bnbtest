package store

import "testing"

func TestOnchainInsertDedupesAndClassifiesWallets(t *testing.T) {
	st, err := New(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	chain := "bsc"
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	poolA := "0x1111111111111111111111111111111111111111"
	poolB := "0x2222222222222222222222222222222222222222"
	accumulator := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	seller := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	bot := "0xcccccccccccccccccccccccccccccccccccccccc"

	if err := st.Onchain().UpsertPools([]OnchainPool{
		{Chain: chain, TokenAddress: token, PoolAddress: poolA, DexID: "pancakeswap", LiquidityUSD: 1000},
		{Chain: chain, TokenAddress: token, PoolAddress: poolB, DexID: "pancakeswap", LiquidityUSD: 500},
	}); err != nil {
		t.Fatalf("upsert pools: %v", err)
	}

	transfers := []OnchainTokenTransfer{
		{Chain: chain, TokenAddress: token, TxHash: "0x1", LogIndex: 0, BlockNumber: 1, BlockTime: 1000, FromAddress: "0x0000000000000000000000000000000000000000", ToAddress: poolA, Amount: 10000},
		{Chain: chain, TokenAddress: token, TxHash: "0x2", LogIndex: 0, BlockNumber: 2, BlockTime: 2000, FromAddress: poolA, ToAddress: accumulator, Amount: 1000},
		{Chain: chain, TokenAddress: token, TxHash: "0x3", LogIndex: 0, BlockNumber: 3, BlockTime: 3000, FromAddress: poolA, ToAddress: seller, Amount: 1000},
		{Chain: chain, TokenAddress: token, TxHash: "0x4", LogIndex: 0, BlockNumber: 4, BlockTime: 4000, FromAddress: seller, ToAddress: poolA, Amount: 1200},
	}
	for i := 0; i < 5; i++ {
		transfers = append(transfers,
			OnchainTokenTransfer{Chain: chain, TokenAddress: token, TxHash: "0xb0" + string(rune('a'+i)), LogIndex: 0, BlockNumber: int64(10 + i*2), BlockTime: int64(10000 + i*2), FromAddress: poolA, ToAddress: bot, Amount: 100},
			OnchainTokenTransfer{Chain: chain, TokenAddress: token, TxHash: "0xc0" + string(rune('a'+i)), LogIndex: 0, BlockNumber: int64(11 + i*2), BlockTime: int64(10001 + i*2), FromAddress: bot, ToAddress: poolB, Amount: 100},
		)
	}

	if err := st.Onchain().InsertTransfers(transfers); err != nil {
		t.Fatalf("insert transfers: %v", err)
	}
	if err := st.Onchain().InsertTransfers(transfers); err != nil {
		t.Fatalf("insert duplicate transfers: %v", err)
	}
	if err := st.Onchain().RecomputeWalletSnapshots(chain, token); err != nil {
		t.Fatalf("recompute snapshots: %v", err)
	}

	count, err := st.Onchain().CountWalletSnapshots(chain, token)
	if err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if count < 4 {
		t.Fatalf("wallet count = %d, want at least 4", count)
	}

	acc, err := st.Onchain().ListWalletSnapshots(chain, token, OnchainWalletAccumulator, 10, "net_bought_amount")
	if err != nil {
		t.Fatalf("list accumulators: %v", err)
	}
	if len(acc) == 0 || acc[0].WalletAddress != accumulator {
		t.Fatalf("accumulator classification unexpected: %+v", acc)
	}

	sellers, err := st.Onchain().ListWalletSnapshots(chain, token, OnchainWalletSeller, 10, "sell_amount")
	if err != nil {
		t.Fatalf("list sellers: %v", err)
	}
	if len(sellers) == 0 || sellers[0].WalletAddress != seller {
		t.Fatalf("seller classification unexpected: %+v", sellers)
	}

	arbs, err := st.Onchain().ListWalletSnapshots(chain, token, OnchainWalletArbBot, 10, "net_bought_amount")
	if err != nil {
		t.Fatalf("list arb bots: %v", err)
	}
	if len(arbs) == 0 || arbs[0].WalletAddress != bot {
		t.Fatalf("arb classification unexpected: %+v", arbs)
	}
}
