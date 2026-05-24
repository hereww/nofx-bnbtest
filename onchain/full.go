package onchain

import (
	"sort"
	"time"

	"nofx/store"
)

func (s *Service) buildFullAnalysis(chain, address string) (*FullHistoryAnalysis, string, string, error) {
	job, err := s.store.Onchain().GetJob(chain, address)
	if err != nil {
		return nil, StatusPartialData, "", err
	}
	if job == nil {
		return nil, StatusIndexRequired, "Full-history index has not been started for this token.", nil
	}

	full := &FullHistoryAnalysis{
		IndexedBlock: job.LastBlock,
		StartBlock:   job.StartBlock,
		EndBlock:     job.EndBlock,
		LastUpdated:  job.UpdatedAtMS,
	}
	if job.Status == store.OnchainJobStatusQueued || job.Status == store.OnchainJobStatusIndexing {
		return full, StatusIndexing, "Full-history index is still running.", nil
	}
	if job.Status == store.OnchainJobStatusFailed {
		return full, StatusPartialData, job.ErrorMessage, nil
	}

	accumulators, err := s.store.Onchain().ListWalletSnapshots(chain, address, store.OnchainWalletAccumulator, 15, "net_bought_amount")
	if err != nil {
		return nil, StatusPartialData, "", err
	}
	sellers, err := s.store.Onchain().ListWalletSnapshots(chain, address, store.OnchainWalletSeller, 15, "sell_amount")
	if err != nil {
		return nil, StatusPartialData, "", err
	}
	arbs, err := s.store.Onchain().ListWalletSnapshots(chain, address, store.OnchainWalletArbBot, 15, "net_bought_amount")
	if err != nil {
		return nil, StatusPartialData, "", err
	}
	walletCount, err := s.store.Onchain().CountWalletSnapshots(chain, address)
	if err != nil {
		return nil, StatusPartialData, "", err
	}
	bucketWallets, err := s.store.Onchain().FirstBuyBuckets(chain, address)
	if err != nil {
		return nil, StatusPartialData, "", err
	}
	full.WalletCount = int(walletCount)
	full.TopAccumulators = snapshotsToWallets(accumulators)
	full.TopSellers = snapshotsToWallets(sellers)
	full.RelatedWalletClusters = snapshotsToWallets(arbs)
	full.FirstBuyBuckets = bucketWalletSnapshots(bucketWallets)
	return full, StatusOK, "", nil
}

func snapshotsToWallets(items []store.OnchainWalletSnapshot) []WalletAnalysis {
	out := make([]WalletAnalysis, 0, len(items))
	for _, item := range items {
		out = append(out, WalletAnalysis{
			Address:         item.WalletAddress,
			WalletType:      item.WalletType,
			FirstBuyTime:    item.FirstBuyTime,
			FirstBuyAt:      isoFromMS(item.FirstBuyTime),
			BuyCount:        item.BuyCount,
			SellCount:       item.SellCount,
			BuyAmount:       item.BuyAmount,
			SellAmount:      item.SellAmount,
			NetBoughtAmount: item.NetBoughtAmount,
			CurrentBalance:  item.CurrentBalance,
			PoolTouchCount:  item.PoolTouchCount,
		})
	}
	return out
}

func bucketWalletSnapshots(items []store.OnchainWalletSnapshot) []TimeBucket {
	buckets := map[string]*TimeBucket{}
	for _, item := range items {
		if item.FirstBuyTime <= 0 {
			continue
		}
		key := time.UnixMilli(item.FirstBuyTime).UTC().Format("2006-01-02 15:00")
		bucket := buckets[key]
		if bucket == nil {
			bucket = &TimeBucket{Bucket: key}
			buckets[key] = bucket
		}
		bucket.BuyerCount++
		bucket.BuyAmount += item.BuyAmount
		bucket.SellAmount += item.SellAmount
		bucket.NetAmount += item.NetBoughtAmount
	}
	out := make([]TimeBucket, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, *bucket)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Bucket < out[j].Bucket
	})
	return out
}
