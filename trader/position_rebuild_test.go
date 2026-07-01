package trader

import (
	"math"
	"testing"
	"time"
)

func closeFloat(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestRebuildPositionsFromTradesPartialCloseDoesNotDoubleCountEntryFee(t *testing.T) {
	baseTime := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	trades := []TradeRecord{
		{
			TradeID:  "open",
			Symbol:   "ETHUSDT",
			Side:     "BUY",
			Price:    100,
			Quantity: 2,
			Fee:      0.4,
			Time:     baseTime,
		},
		{
			TradeID:     "close-1",
			Symbol:      "ETHUSDT",
			Side:        "SELL",
			Price:       110,
			Quantity:    1,
			RealizedPnL: 10,
			Fee:         0.1,
			Time:        baseTime.Add(time.Minute),
		},
		{
			TradeID:     "close-2",
			Symbol:      "ETHUSDT",
			Side:        "SELL",
			Price:       120,
			Quantity:    1,
			RealizedPnL: 20,
			Fee:         0.1,
			Time:        baseTime.Add(2 * time.Minute),
		},
	}

	records := RebuildPositionsFromTrades(trades)
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}

	if !closeFloat(records[0].Fee, 0.3) {
		t.Fatalf("first close fee = %v, want 0.3", records[0].Fee)
	}
	if !closeFloat(records[1].Fee, 0.3) {
		t.Fatalf("second close fee = %v, want 0.3", records[1].Fee)
	}

	attributedEntryFee := records[0].Fee + records[1].Fee - 0.2
	if !closeFloat(attributedEntryFee, 0.4) {
		t.Fatalf("attributed entry fee = %v, want 0.4", attributedEntryFee)
	}
}
