package onchain

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	DealerDirectionAccumulating     = "accumulating"
	DealerDirectionDistributing     = "distributing"
	DealerDirectionMixed            = "mixed"
	DealerDirectionInsufficientData = "insufficient_data"
)

func buildDealerFlowAnalysis(analysis *TokenAnalysisResponse) *DealerFlowAnalysis {
	if analysis == nil || !analysis.Success {
		return nil
	}

	depth := DepthRecent
	accumulators := []WalletAnalysis{}
	sellers := []WalletAnalysis{}
	related := []WalletAnalysis{}
	buckets := []TimeBucket{}
	signals := DealerFlowSignals{}

	if analysis.Full != nil && len(analysis.Full.TopAccumulators)+len(analysis.Full.TopSellers)+len(analysis.Full.RelatedWalletClusters) > 0 {
		depth = DepthFull
		accumulators = analysis.Full.TopAccumulators
		sellers = analysis.Full.TopSellers
		related = analysis.Full.RelatedWalletClusters
		buckets = analysis.Full.FirstBuyBuckets
		signals.WalletCount = analysis.Full.WalletCount
	} else if analysis.Recent != nil {
		accumulators = analysis.Recent.TopAccumulators
		sellers = analysis.Recent.TopSellers
		related = analysis.Recent.RelatedWalletClusters
		buckets = analysis.Recent.FirstBuyBuckets
		signals.TradeCount = analysis.Recent.TradeCount
		signals.BuyCount = analysis.Recent.BuyCount
		signals.SellCount = analysis.Recent.SellCount
		signals.UniqueBuyers = analysis.Recent.UniqueBuyers
		signals.UniqueSellers = analysis.Recent.UniqueSellers
	}

	if analysis.Recent != nil {
		signals.TradeCount = analysis.Recent.TradeCount
		signals.BuyCount = analysis.Recent.BuyCount
		signals.SellCount = analysis.Recent.SellCount
		signals.UniqueBuyers = analysis.Recent.UniqueBuyers
		signals.UniqueSellers = analysis.Recent.UniqueSellers
	}

	signals.AccumulatorCount = countPositiveWallets(accumulators)
	signals.SellerCount = countSellerWallets(sellers)
	signals.RelatedWalletCount = len(related)
	signals.AccumulatorNetAmount = positiveNetSum(accumulators)
	signals.SellerNetAmount = negativeNetSum(sellers)
	signals.NetAmount = signals.AccumulatorNetAmount + signals.SellerNetAmount
	signals.TopAccumulatorNet = topPositiveNet(accumulators)
	signals.TopSellerNet = topNegativeNet(sellers)
	signals.FirstBuyBucketCount = len(buckets)
	signals.FirstBuyConcentration = firstBuyConcentration(buckets)
	fillSecuritySignals(&signals, analysis.Security, analysis.RiskFlags)

	activity := signals.TradeCount + signals.AccumulatorCount + signals.SellerCount + signals.RelatedWalletCount
	if activity < 3 || (signals.AccumulatorNetAmount == 0 && signals.SellerNetAmount == 0 && signals.BuyCount+signals.SellCount < 3) {
		return &DealerFlowAnalysis{
			Direction:       DealerDirectionInsufficientData,
			Score:           0,
			Confidence:      "low",
			Summary:         "链上交易与钱包样本不足，暂时不能判断疑似主力是在进货还是出货。",
			Reasons:         []string{"有效买卖和钱包样本不足。", "建议等待更多交易数据或启动 full 索引后再判断。"},
			MonitoringPlan:  []string{"继续按当前监控间隔刷新。", "若准备交易，优先等待净买入/净卖出钱包数量拉开差距。"},
			EvidenceDepth:   depth,
			SignalBreakdown: signals,
		}
	}

	score := 0.0
	totalAbsNet := math.Abs(signals.AccumulatorNetAmount) + math.Abs(signals.SellerNetAmount)
	if totalAbsNet > 0 {
		score += (signals.NetAmount / totalAbsNet) * 46
	}
	if signals.BuyCount+signals.SellCount > 0 {
		score += (float64(signals.BuyCount-signals.SellCount) / float64(signals.BuyCount+signals.SellCount)) * 18
	}
	if signals.UniqueBuyers+signals.UniqueSellers > 0 {
		score += (float64(signals.UniqueBuyers-signals.UniqueSellers) / float64(signals.UniqueBuyers+signals.UniqueSellers)) * 12
	}
	if signals.AccumulatorCount+signals.SellerCount > 0 {
		score += (float64(signals.AccumulatorCount-signals.SellerCount) / float64(signals.AccumulatorCount+signals.SellerCount)) * 14
	}
	if signals.RelatedWalletCount >= 3 {
		if signals.NetAmount > 0 {
			score += 6
		} else if signals.NetAmount < 0 {
			score -= 6
		}
	}
	if signals.FirstBuyConcentration >= 0.45 && signals.NetAmount > 0 {
		score += 4
	}
	if signals.TopHolderPercent >= 0.12 || signals.TopFiveHolderPercent >= 0.35 || signals.OwnerPresent || signals.CreatorPresent {
		if signals.NetAmount < 0 {
			score -= 4
		}
	}
	if signals.HighRiskFlagCount > 0 && signals.NetAmount < 0 {
		score -= float64(minInt(signals.HighRiskFlagCount*2, 8))
	}

	score = math.Max(-100, math.Min(100, score))
	roundedScore := int(math.Round(score))
	direction := DealerDirectionMixed
	if roundedScore >= 22 {
		direction = DealerDirectionAccumulating
	} else if roundedScore <= -22 {
		direction = DealerDirectionDistributing
	}

	confidence := dealerConfidence(depth, roundedScore, signals)
	reasons := dealerReasons(direction, signals)
	return &DealerFlowAnalysis{
		Direction:       direction,
		Score:           roundedScore,
		Confidence:      confidence,
		Summary:         dealerSummary(direction, roundedScore, confidence),
		Reasons:         reasons,
		MonitoringPlan:  dealerMonitoringPlan(direction),
		EvidenceDepth:   depth,
		SignalBreakdown: signals,
	}
}

func dealerConfidence(depth string, score int, signals DealerFlowSignals) string {
	activity := signals.TradeCount + signals.AccumulatorCount + signals.SellerCount
	if depth == DepthFull && activity >= 20 && math.Abs(float64(score)) >= 35 {
		return "high"
	}
	if activity >= 10 && math.Abs(float64(score)) >= 24 {
		return "medium"
	}
	if depth == DepthFull && activity >= 10 {
		return "medium"
	}
	return "low"
}

func dealerSummary(direction string, score int, confidence string) string {
	switch direction {
	case DealerDirectionAccumulating:
		return fmt.Sprintf("疑似主力/大户资金流倾向进货，评分 %+d，信心 %s。", score, confidence)
	case DealerDirectionDistributing:
		return fmt.Sprintf("疑似主力/大户资金流倾向出货，评分 %+d，信心 %s。", score, confidence)
	case DealerDirectionMixed:
		return fmt.Sprintf("买卖力量接近或信号冲突，暂按多空混合处理，评分 %+d，信心 %s。", score, confidence)
	default:
		return "链上样本不足，暂时不能判断疑似主力方向。"
	}
}

func dealerReasons(direction string, signals DealerFlowSignals) []string {
	reasons := []string{}
	if signals.AccumulatorNetAmount > 0 {
		reasons = append(reasons, fmt.Sprintf("净买入钱包合计约 %.2f token。", signals.AccumulatorNetAmount))
	}
	if signals.SellerNetAmount < 0 {
		reasons = append(reasons, fmt.Sprintf("净卖出钱包合计约 %.2f token。", math.Abs(signals.SellerNetAmount)))
	}
	if signals.BuyCount+signals.SellCount > 0 {
		reasons = append(reasons, fmt.Sprintf("最近买/卖次数为 %d/%d。", signals.BuyCount, signals.SellCount))
	}
	if signals.UniqueBuyers+signals.UniqueSellers > 0 {
		reasons = append(reasons, fmt.Sprintf("最近独立买家/卖家为 %d/%d。", signals.UniqueBuyers, signals.UniqueSellers))
	}
	if signals.RelatedWalletCount > 0 {
		reasons = append(reasons, fmt.Sprintf("发现 %d 个疑似关联或套利钱包，需要结合关系网继续观察。", signals.RelatedWalletCount))
	}
	if signals.FirstBuyConcentration >= 0.45 {
		reasons = append(reasons, fmt.Sprintf("首次买入时间集中度 %.0f%%，存在集中建仓或同批地址行为。", signals.FirstBuyConcentration*100))
	}
	if signals.TopHolderPercent > 0 {
		reasons = append(reasons, fmt.Sprintf("最大持仓地址约占 %.2f%%。", signals.TopHolderPercent*100))
	}
	if len(reasons) == 0 || direction == DealerDirectionMixed {
		reasons = append(reasons, "核心资金流指标没有形成单边优势，需继续监控净流向变化。")
	}
	return capStrings(reasons, 6)
}

func dealerMonitoringPlan(direction string) []string {
	switch direction {
	case DealerDirectionAccumulating:
		return []string{
			"重点观察净买入钱包是否继续增加持仓，而不是快速转入池子卖出。",
			"如果关联钱包同步买入且卖出次数没有放大，可提高进货倾向权重。",
			"若 Top holder 或 owner 钱包开始向池子转账卖出，需要立即重新评估。",
		}
	case DealerDirectionDistributing:
		return []string{
			"重点观察主要卖出地址是否持续向池子转入并卖出。",
			"若净卖出扩大且买家分散承接，优先按出货风险处理。",
			"如果卖压停止且净买入钱包重新占优，再重新评估方向。",
		}
	case DealerDirectionMixed:
		return []string{
			"等待买卖次数、净流入和关联钱包行为进一步拉开差距。",
			"把监控间隔调短，观察连续两到三轮结果是否同向。",
			"避免把单次大额交易当作确定方向。",
		}
	default:
		return []string{
			"继续收集最近交易和钱包样本。",
			"必要时启动 full 索引以获取历史转账关系。",
		}
	}
}

func fillSecuritySignals(signals *DealerFlowSignals, security *TokenSecurity, riskFlags []string) {
	if signals == nil {
		return
	}
	signals.HighRiskFlagCount = len(riskFlags)
	if security == nil {
		return
	}
	signals.OwnerPresent = strings.TrimSpace(security.OwnerAddress) != "" && security.OwnerAddress != "0x0000000000000000000000000000000000000000"
	signals.CreatorPresent = strings.TrimSpace(security.CreatorAddress) != ""
	holders := append([]HolderSnapshot(nil), security.TopHolders...)
	sort.SliceStable(holders, func(i, j int) bool { return holders[i].Percent > holders[j].Percent })
	for i, holder := range holders {
		if i == 0 {
			signals.TopHolderPercent = holder.Percent
		}
		if i < 5 {
			signals.TopFiveHolderPercent += holder.Percent
		}
	}
}

func countPositiveWallets(wallets []WalletAnalysis) int {
	count := 0
	for _, wallet := range wallets {
		if wallet.NetBoughtAmount > 0 || wallet.BuyAmount > wallet.SellAmount {
			count++
		}
	}
	return count
}

func countSellerWallets(wallets []WalletAnalysis) int {
	count := 0
	for _, wallet := range wallets {
		if wallet.NetBoughtAmount < 0 || wallet.SellAmount > wallet.BuyAmount {
			count++
		}
	}
	return count
}

func positiveNetSum(wallets []WalletAnalysis) float64 {
	total := 0.0
	for _, wallet := range wallets {
		if wallet.NetBoughtAmount > 0 {
			total += wallet.NetBoughtAmount
		}
	}
	return total
}

func negativeNetSum(wallets []WalletAnalysis) float64 {
	total := 0.0
	for _, wallet := range wallets {
		if wallet.NetBoughtAmount < 0 {
			total += wallet.NetBoughtAmount
		}
	}
	return total
}

func topPositiveNet(wallets []WalletAnalysis) float64 {
	top := 0.0
	for _, wallet := range wallets {
		if wallet.NetBoughtAmount > top {
			top = wallet.NetBoughtAmount
		}
	}
	return top
}

func topNegativeNet(wallets []WalletAnalysis) float64 {
	top := 0.0
	for _, wallet := range wallets {
		if wallet.NetBoughtAmount < top {
			top = wallet.NetBoughtAmount
		}
	}
	return top
}

func firstBuyConcentration(buckets []TimeBucket) float64 {
	if len(buckets) == 0 {
		return 0
	}
	total := 0
	maxBucket := 0
	for _, bucket := range buckets {
		total += bucket.BuyerCount
		if bucket.BuyerCount > maxBucket {
			maxBucket = bucket.BuyerCount
		}
	}
	if total == 0 {
		return 0
	}
	return float64(maxBucket) / float64(total)
}

func capStrings(items []string, limit int) []string {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
