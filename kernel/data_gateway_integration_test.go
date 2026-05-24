//go:build integration

package kernel

import (
	"os"
	"testing"

	"nofx/store"
)

func TestDataGatewayCandidateCoinSources(t *testing.T) {
	if os.Getenv("RUN_DATA_GATEWAY_INTEGRATION") != "1" {
		t.Skip("set RUN_DATA_GATEWAY_INTEGRATION=1 with DATA_GATEWAY_URL pointing at a running gateway")
	}

	tests := []struct {
		name       string
		configure  func(*store.StrategyConfig)
		wantSource string
	}{
		{
			name: "ai500",
			configure: func(cfg *store.StrategyConfig) {
				cfg.CoinSource.SourceType = "ai500"
				cfg.CoinSource.UseAI500 = true
				cfg.CoinSource.AI500Limit = 3
			},
			wantSource: "ai500",
		},
		{
			name: "oi_top",
			configure: func(cfg *store.StrategyConfig) {
				cfg.CoinSource.SourceType = "oi_top"
				cfg.CoinSource.UseOITop = true
				cfg.CoinSource.OITopLimit = 3
			},
			wantSource: "oi_top",
		},
		{
			name: "oi_low",
			configure: func(cfg *store.StrategyConfig) {
				cfg.CoinSource.SourceType = "oi_low"
				cfg.CoinSource.UseOILow = true
				cfg.CoinSource.OILowLimit = 3
			},
			wantSource: "oi_low",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := store.GetDefaultStrategyConfig("zh")
			tc.configure(&cfg)

			engine := NewStrategyEngine(&cfg)
			candidates, err := engine.GetCandidateCoins()
			if err != nil {
				t.Fatalf("GetCandidateCoins returned error: %v", err)
			}
			if len(candidates) == 0 {
				t.Fatalf("expected non-empty candidates for %s", tc.name)
			}
			for _, candidate := range candidates {
				if candidate.Symbol == "" {
					t.Fatalf("empty symbol in candidates: %+v", candidates)
				}
				if !containsSource(candidate.Sources, tc.wantSource) {
					t.Fatalf("candidate %+v missing source %q", candidate, tc.wantSource)
				}
			}
		})
	}
}

func containsSource(sources []string, want string) bool {
	for _, source := range sources {
		if source == want {
			return true
		}
	}
	return false
}
