package onchain

import (
	"strings"
	"testing"

	"nofx/store"
)

func TestParsePoolEventSupportsSwapMintAndBurn(t *testing.T) {
	token := "0x812fc5119b772c6c7a66249a559f3614623f4444"
	quote := "0x55d398326f99059ff775485246999027b3197955"
	pool := store.OnchainPool{
		Chain:          "bsc",
		TokenAddress:   token,
		PoolAddress:    "0x1111111111111111111111111111111111111111",
		Token0:         token,
		Token1:         quote,
		Token0Decimals: 18,
		Token1Decimals: 18,
	}
	trader := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	swap, ok := parsePoolEvent("bsc", token, pool, rpcLog{
		Address:         pool.PoolAddress,
		Topics:          []string{swapTopic, topicAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), topicAddress(trader)},
		Data:            words(0, 10, 1000, 0),
		BlockNumber:     "0x1",
		TransactionHash: "0xabc",
		LogIndex:        "0x0",
	}, 1000)
	if !ok || swap.EventType != "swap" || swap.Side != "buy" || swap.TraderAddress != trader {
		t.Fatalf("unexpected swap parse: ok=%v swap=%+v", ok, swap)
	}

	mint, ok := parsePoolEvent("bsc", token, pool, rpcLog{
		Address:         pool.PoolAddress,
		Topics:          []string{mintTopic, topicAddress(trader)},
		Data:            words(500, 5),
		BlockNumber:     "0x2",
		TransactionHash: "0xdef",
		LogIndex:        "0x1",
	}, 2000)
	if !ok || mint.EventType != "mint" || mint.Side != "lp_event" || mint.TraderAddress != trader {
		t.Fatalf("unexpected mint parse: ok=%v mint=%+v", ok, mint)
	}

	burn, ok := parsePoolEvent("bsc", token, pool, rpcLog{
		Address:         pool.PoolAddress,
		Topics:          []string{burnTopic, topicAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"), topicAddress(trader)},
		Data:            words(250, 3),
		BlockNumber:     "0x3",
		TransactionHash: "0x123",
		LogIndex:        "0x2",
	}, 3000)
	if !ok || burn.EventType != "burn" || burn.Side != "lp_event" || burn.TraderAddress != trader {
		t.Fatalf("unexpected burn parse: ok=%v burn=%+v", ok, burn)
	}
}

func words(values ...int64) string {
	var b strings.Builder
	b.WriteString("0x")
	for _, value := range values {
		b.WriteString(hexWord(value))
	}
	return b.String()
}

func hexWord(value int64) string {
	hex := strings.TrimPrefix(hexBlock(value), "0x")
	return strings.Repeat("0", 64-len(hex)) + hex
}

func topicAddress(address string) string {
	return "0x" + strings.Repeat("0", 24) + strings.TrimPrefix(address, "0x")
}
