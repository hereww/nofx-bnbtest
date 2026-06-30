package onchain

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"nofx/config"
	"nofx/store"
)

const (
	defaultEarlySeedCount = 100
	defaultEarlyMaxDepth  = 4
	maxEarlySeedCount     = 100
	maxEarlyDepth         = 6

	EarlyFlowDirectionAccumulating     = "accumulating"
	EarlyFlowDirectionDistributing     = "distributing"
	EarlyFlowDirectionMixed            = "mixed"
	EarlyFlowDirectionInsufficientData = "insufficient_data"
)

type earlyFlowEvent struct {
	Kind        string
	Address     string
	From        string
	To          string
	TxHash      string
	BlockNumber int64
	LogIndex    int64
	BlockTime   int64
	TokenAmount float64
	QuoteAmount float64
	QuoteToken  string
	PoolAddress string
	PriceKnown  bool
}

type earlyWalletState struct {
	address           string
	root              string
	parent            string
	depth             int
	firstSeenTime     int64
	isSeed            bool
	buyCount          int
	sellCount         int
	buyAmount         float64
	buyValue          float64
	sellAmount        float64
	sellValue         float64
	transferInAmount  float64
	transferOutAmount float64
	currentBalance    float64
	allocatedCost     float64
	remainingCost     float64
	realizedPnL       float64
	eventCount        int
	incompletePricing int
}

type earlySeedCandidate struct {
	address       string
	firstBuyTime  int64
	firstBuyBlock int64
	firstBuyIndex int64
	buyCount      int
	buyAmount     float64
	buyValue      float64
}

func normalizeEarlyWalletFlowRequest(req EarlyWalletFlowRequest) EarlyWalletFlowRequest {
	req.Chain = normalizeChain(req.Chain)
	req.Address = normalizeAddress(req.Address)
	req.IndexSource = normalizeIndexSource(req.IndexSource, store.OnchainIndexScopeEarlyWalletWindow)
	if req.SeedCount <= 0 || req.SeedCount > maxEarlySeedCount {
		req.SeedCount = defaultEarlySeedCount
	}
	if req.MaxDepth <= 0 {
		req.MaxDepth = defaultEarlyMaxDepth
	}
	if req.MaxDepth > maxEarlyDepth {
		req.MaxDepth = maxEarlyDepth
	}
	return req
}

func (s *Service) EarlyWalletFlow(ctx context.Context, req EarlyWalletFlowRequest) (*EarlyWalletFlowResponse, error) {
	req = normalizeEarlyWalletFlowRequest(req)
	if err := validateRequest(req.Chain, req.Address); err != nil {
		return &EarlyWalletFlowResponse{
			Success:   false,
			Chain:     req.Chain,
			Address:   req.Address,
			Status:    StatusInvalidRequest,
			SeedCount: req.SeedCount,
			MaxDepth:  req.MaxDepth,
			Error:     err.Error(),
		}, nil
	}
	resp := &EarlyWalletFlowResponse{
		Success:      true,
		Chain:        req.Chain,
		Address:      req.Address,
		Status:       StatusOK,
		Completeness: "full",
		IndexSource:  req.IndexSource,
		SeedCount:    req.SeedCount,
		MaxDepth:     req.MaxDepth,
	}
	if req.IndexSource == store.OnchainIndexSourceArchiveRPC && strings.TrimSpace(config.Get().OnchainBSCArchiveRPCURL) == "" {
		resp.Status = StatusArchiveRPCMissing
		resp.Completeness = "unavailable"
		resp.Message = "Early-wallet flow requires ONCHAIN_BSC_ARCHIVE_RPC_URL and early-window indexed data."
		resp.Summary.Direction = EarlyFlowDirectionInsufficientData
		resp.Summary.Confidence = "low"
		resp.Summary.IncompleteReasons = []string{"archive_rpc_missing"}
		return resp, nil
	}

	token, _ := s.store.Onchain().GetToken(req.Chain, req.Address)
	if token != nil {
		resp.Token = TokenProfile{
			Name:        token.Name,
			Symbol:      token.Symbol,
			Decimals:    token.Decimals,
			TotalSupply: token.TotalSupply,
		}
	}
	job, err := s.store.Onchain().GetJob(req.Chain, req.Address)
	if err != nil {
		return nil, err
	}
	if job == nil {
		resp.Status = StatusIndexRequired
		resp.Completeness = "unavailable"
		resp.IndexSource = store.OnchainIndexSourceFreeLocal
		resp.ProgressMessage = "Click Start Index to register the free local early-wallet index. Historical backfill may be slow and partial."
		resp.Message = "Early-window index has not been started for this token."
		resp.Summary.Direction = EarlyFlowDirectionInsufficientData
		resp.Summary.Confidence = "low"
		resp.Summary.IncompleteReasons = []string{"index_required"}
		return resp, nil
	}
	resp.IndexSource = job.IndexSource
	resp.Phase = job.Phase
	resp.Provider = job.Provider
	resp.RequestBudgetRemaining = job.RequestBudget
	resp.ProgressMessage = job.ProgressMessage
	resp.StartBlock = job.StartBlock
	resp.EndBlock = job.EndBlock
	resp.LastBlock = job.LastBlock
	resp.UpdatedAtMS = job.UpdatedAtMS
	seedWallets, _ := s.store.Onchain().CountEarlyBuyWallets(req.Chain, req.Address, req.SeedCount)
	resp.SeedWallets = seedWallets
	if job.IndexSource == store.OnchainIndexSourceArchiveRPC && isArchiveRPCUnsupportedError(job.ErrorMessage) {
		resp.Status = StatusArchiveRPCUnsupported
		resp.Completeness = "unavailable"
		resp.Message = "Archive RPC does not support historical eth_getLogs. Upgrade the RPC plan or use an archive-capable BSC endpoint, then restart the early-window index."
		resp.Summary.Direction = EarlyFlowDirectionInsufficientData
		resp.Summary.Confidence = "low"
		resp.Summary.IncompleteReasons = []string{"archive_rpc_unsupported"}
		return resp, nil
	}
	if job.Status == store.OnchainJobStatusQueued || job.Status == store.OnchainJobStatusIndexing {
		resp.Status = StatusIndexing
		resp.Completeness = "partial"
		if job.IndexSource == store.OnchainIndexSourceFreeLocal {
			resp.Message = "Free local early-window index is still running; current partial data can be viewed, but conclusions may change as backfill continues."
		} else {
			resp.Message = "Early-window index is still running. Wait for it to complete before calculating early-wallet flow."
		}
		resp.Summary.Direction = EarlyFlowDirectionInsufficientData
		resp.Summary.Confidence = "low"
		resp.Summary.IncompleteReasons = []string{"indexing"}
		errorMessage := strings.ToLower(job.ErrorMessage)
		progressMessage := strings.ToLower(job.ProgressMessage)
		if strings.Contains(errorMessage, "rate limited") {
			resp.Message = "RPC is rate limited; the early-window index is queued for retry."
			resp.Summary.IncompleteReasons = appendMissingReason(resp.Summary.IncompleteReasons, "rpc_rate_limited")
		}
		if strings.Contains(errorMessage, "free logs daily budget") {
			resp.Message = "Free Etherscan log request budget is exhausted; the early-window index is queued for retry."
			resp.Summary.IncompleteReasons = appendMissingReason(resp.Summary.IncompleteReasons, "free_logs_budget_exhausted")
		}
		if strings.Contains(errorMessage, "rpc transport retrying") {
			resp.Message = "Free RPC transport is temporarily unavailable; the early-window index is queued for retry."
			resp.Summary.IncompleteReasons = appendMissingReason(resp.Summary.IncompleteReasons, "rpc_transport_retrying")
		}
		if strings.Contains(progressMessage, "pool_created_block_pending") || strings.Contains(errorMessage, "pool_created_block_pending") {
			resp.Message = "Pool discovery is waiting for a reliable created block before scanning the early-wallet window."
			resp.Summary.IncompleteReasons = appendMissingReason(resp.Summary.IncompleteReasons, "pool_created_block_pending")
		}
		if job.IndexSource != store.OnchainIndexSourceFreeLocal || resp.SeedWallets == 0 {
			return resp, nil
		}
	}
	if job.Status == store.OnchainJobStatusFailed {
		resp.Status = StatusPartialData
		resp.Completeness = "partial"
		resp.Message = job.ErrorMessage
		resp.Summary.IncompleteReasons = append(resp.Summary.IncompleteReasons, "index_failed")
	}
	if job.IndexSource == store.OnchainIndexSourceFreeLocal && strings.Contains(strings.ToLower(job.Provider), "geckoterminal") && resp.Message == "" {
		resp.Message = "Free public GeckoTerminal data is being used; old-token history may be recent-window limited, but no paid Archive RPC is required."
	}

	pools, err := s.store.Onchain().ListPools(req.Chain, req.Address)
	if err != nil {
		return nil, err
	}
	swaps, err := s.store.Onchain().ListSwaps(req.Chain, req.Address)
	if err != nil {
		return nil, err
	}
	seedAddresses, err := s.store.Onchain().ListEarlyBuyWallets(req.Chain, req.Address, req.SeedCount)
	if err != nil {
		return nil, err
	}
	transfers, err := s.listSeedGraphTransfers(req.Chain, req.Address, seedAddresses, req.MaxDepth)
	if err != nil {
		return nil, err
	}
	buildEarlyWalletFlow(resp, req, pools, transfers, swaps)
	if job.IndexSource == store.OnchainIndexSourceFreeLocal && resp.Status == StatusOK && tokenAnalysisIsPartial(token) {
		resp.Completeness = "partial"
	}
	if len(transfers) == 0 && hasEarlyWalletFlowData(resp) {
		resp.Status = mergeEarlyStatus(resp.Status, StatusPartialData)
		resp.Completeness = "partial"
		resp.Summary.IncompleteReasons = appendMissingReason(resp.Summary.IncompleteReasons, "missing_transfer_graph")
		if resp.Message == "" {
			resp.Message = "Early buy wallets were found. Transfer graph enrichment is limited on the free public-data path."
		}
	}
	if len(swaps) == 0 {
		resp.Status = mergeEarlyStatus(resp.Status, StatusPartialData)
		resp.Completeness = "partial"
		resp.Summary.IncompleteReasons = appendMissingReason(resp.Summary.IncompleteReasons, "missing_swap_prices")
		if resp.Message == "" {
			resp.Message = "No swap price records found; costs and PnL cannot be fully calculated."
		}
	}
	return resp, nil
}

func isArchiveRPCUnsupportedError(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(message, "archive rpc unsupported") ||
		(strings.Contains(message, "archive") && strings.Contains(message, "eth_getlogs") && strings.Contains(message, "not allow")) ||
		(strings.Contains(message, "archive") && strings.Contains(message, "not available"))
}

func tokenAnalysisIsPartial(token *store.OnchainToken) bool {
	return token != nil && strings.EqualFold(strings.TrimSpace(token.AnalysisCompleteness), "partial")
}

func hasEarlyWalletFlowData(resp *EarlyWalletFlowResponse) bool {
	return resp != nil && resp.Summary.SeedWalletCount > 0
}

func (s *Service) listSeedGraphTransfers(chain, tokenAddress string, seeds []string, maxDepth int) ([]store.OnchainTokenTransfer, error) {
	if maxDepth <= 0 || len(seeds) == 0 {
		return nil, nil
	}
	known := map[string]bool{}
	frontier := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		seed = normalizeAddress(seed)
		if seed == "" || known[seed] {
			continue
		}
		known[seed] = true
		frontier = append(frontier, seed)
	}
	var out []store.OnchainTokenTransfer
	seenTransfer := map[string]bool{}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		batch, err := s.store.Onchain().ListTransfersForAddresses(chain, tokenAddress, frontier)
		if err != nil {
			return nil, err
		}
		next := []string{}
		for _, tr := range batch {
			key := strings.ToLower(tr.TxHash) + "|" + strconv.FormatInt(tr.LogIndex, 10)
			if !seenTransfer[key] {
				seenTransfer[key] = true
				out = append(out, tr)
			}
			from := normalizeAddress(tr.FromAddress)
			to := normalizeAddress(tr.ToAddress)
			if known[from] && to != "" && !known[to] {
				known[to] = true
				next = append(next, to)
			}
		}
		frontier = next
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].BlockNumber != out[j].BlockNumber {
			return out[i].BlockNumber < out[j].BlockNumber
		}
		return out[i].LogIndex < out[j].LogIndex
	})
	return out, nil
}

func mergeEarlyStatus(current, next string) string {
	if current == "" || current == StatusOK {
		return next
	}
	return current
}

func appendMissingReason(items []string, reason string) []string {
	for _, item := range items {
		if item == reason {
			return items
		}
	}
	return append(items, reason)
}

func buildEarlyWalletFlow(resp *EarlyWalletFlowResponse, req EarlyWalletFlowRequest, pools []store.OnchainPool, transfers []store.OnchainTokenTransfer, swaps []store.OnchainSwap) {
	system := earlySystemAddressSet(req.Address, pools)
	swapEvents, quoteToken := earlySwapEvents(swaps, system)
	quoteSymbol := quoteSymbolForToken(quoteToken, pools)
	resp.QuoteToken = quoteToken
	resp.QuoteSymbol = quoteSymbol
	seeds := selectEarlySeedCandidates(swapEvents, req.SeedCount)
	if len(seeds) == 0 {
		resp.Status = mergeEarlyStatus(resp.Status, StatusPartialData)
		resp.Completeness = "partial"
		resp.Message = "No early buy wallets found from indexed swaps."
		resp.Summary.Direction = EarlyFlowDirectionInsufficientData
		resp.Summary.Confidence = "low"
		resp.Summary.IncompleteReasons = appendMissingReason(resp.Summary.IncompleteReasons, "no_seed_wallets")
		return
	}

	seedSet := map[string]bool{}
	seedRank := map[string]int{}
	seedCandidateByAddress := map[string]earlySeedCandidate{}
	for i, seed := range seeds {
		seedSet[seed.address] = true
		seedRank[seed.address] = i + 1
		seedCandidateByAddress[seed.address] = seed
	}
	transferEvents := earlyTransferEvents(transfers, system)
	tracked, walletMeta, edges := buildTrackedAddressSet(seedSet, transferEvents, req.MaxDepth)
	for seed := range seedSet {
		tracked[seed] = true
		if walletMeta[seed] == nil {
			walletMeta[seed] = &earlyWalletState{address: seed, root: seed, depth: 0, isSeed: true}
		}
	}

	states := map[string]*earlyWalletState{}
	getState := func(address string) *earlyWalletState {
		address = normalizeAddress(address)
		if states[address] == nil {
			meta := walletMeta[address]
			if meta == nil {
				meta = &earlyWalletState{address: address, root: address}
			}
			states[address] = &earlyWalletState{
				address:       address,
				root:          meta.root,
				parent:        meta.parent,
				depth:         meta.depth,
				firstSeenTime: meta.firstSeenTime,
				isSeed:        seedSet[address],
			}
		}
		return states[address]
	}

	events := combineEarlyEvents(swapEvents, transferEvents)
	for _, ev := range events {
		switch ev.Kind {
		case "buy":
			if !tracked[ev.Address] {
				continue
			}
			st := getState(ev.Address)
			st.buyCount++
			st.buyAmount += ev.TokenAmount
			st.buyValue += ev.QuoteAmount
			st.currentBalance += ev.TokenAmount
			if ev.PriceKnown {
				st.allocatedCost += ev.QuoteAmount
				st.remainingCost += ev.QuoteAmount
			} else {
				st.incompletePricing++
			}
			st.eventCount++
			if st.firstSeenTime == 0 || (ev.BlockTime > 0 && ev.BlockTime < st.firstSeenTime) {
				st.firstSeenTime = ev.BlockTime
			}
		case "sell":
			if !tracked[ev.Address] {
				continue
			}
			st := getState(ev.Address)
			cost := proportionalCost(st.remainingCost, st.currentBalance, ev.TokenAmount)
			st.sellCount++
			st.sellAmount += ev.TokenAmount
			st.currentBalance -= ev.TokenAmount
			if ev.PriceKnown {
				st.sellValue += ev.QuoteAmount
				st.realizedPnL += ev.QuoteAmount - cost
			} else {
				st.incompletePricing++
			}
			st.remainingCost -= cost
			st.eventCount++
			if st.firstSeenTime == 0 || (ev.BlockTime > 0 && ev.BlockTime < st.firstSeenTime) {
				st.firstSeenTime = ev.BlockTime
			}
		case "transfer":
			if !tracked[ev.From] || !tracked[ev.To] {
				continue
			}
			from := getState(ev.From)
			to := getState(ev.To)
			cost := proportionalCost(from.remainingCost, from.currentBalance, ev.TokenAmount)
			from.transferOutAmount += ev.TokenAmount
			from.currentBalance -= ev.TokenAmount
			from.remainingCost -= cost
			from.eventCount++
			to.transferInAmount += ev.TokenAmount
			to.currentBalance += ev.TokenAmount
			to.allocatedCost += cost
			to.remainingCost += cost
			to.eventCount++
			if to.firstSeenTime == 0 || (ev.BlockTime > 0 && ev.BlockTime < to.firstSeenTime) {
				to.firstSeenTime = ev.BlockTime
			}
			for i := range edges {
				if edges[i].Source == ev.From && edges[i].Target == ev.To && edges[i].TxHash == ev.TxHash {
					edges[i].Cost = cost
					break
				}
			}
		}
	}

	seedRows := make([]EarlyWalletSeed, 0, len(seeds))
	childrenByRoot := map[string]int{}
	for _, edge := range edges {
		if edge.Depth == 1 {
			childrenByRoot[edge.RootAddress]++
		}
	}
	descendantByRoot := map[string]*EarlyWalletSeed{}
	for _, st := range states {
		if st.root == "" || st.root == st.address {
			continue
		}
		row := descendantByRoot[st.root]
		if row == nil {
			row = &EarlyWalletSeed{}
			descendantByRoot[st.root] = row
		}
		row.DescendantSellAmount += st.sellAmount
		row.DescendantSellValue += st.sellValue
		row.DescendantRealizedPnL += st.realizedPnL
	}

	for _, seed := range seeds {
		st := getState(seed.address)
		desc := descendantByRoot[seed.address]
		row := EarlyWalletSeed{
			Rank:              seedRank[seed.address],
			Address:           seed.address,
			FirstBuyTime:      seed.firstBuyTime,
			FirstBuyAt:        isoFromMS(seed.firstBuyTime),
			FirstBuyBlock:     seed.firstBuyBlock,
			BuyCount:          st.buyCount,
			BuyAmount:         st.buyAmount,
			BuyValue:          st.buyValue,
			AvgBuyPrice:       ratio(st.buyValue, st.buyAmount),
			OwnSellAmount:     st.sellAmount,
			OwnSellValue:      st.sellValue,
			TransferOutAmount: st.transferOutAmount,
			CurrentBalance:    st.currentBalance,
			RemainingCost:     st.remainingCost,
			RealizedPnL:       st.realizedPnL,
			ChildCount:        childrenByRoot[seed.address],
		}
		if desc != nil {
			row.DescendantSellAmount = desc.DescendantSellAmount
			row.DescendantSellValue = desc.DescendantSellValue
			row.DescendantRealizedPnL = desc.DescendantRealizedPnL
		}
		row.TotalRealizedPnL = row.RealizedPnL + row.DescendantRealizedPnL
		seedRows = append(seedRows, row)
	}
	sort.SliceStable(seedRows, func(i, j int) bool { return seedRows[i].Rank < seedRows[j].Rank })
	resp.Seeds = seedRows

	walletRows := make([]EarlyWalletFlowWallet, 0, len(states))
	for _, st := range states {
		if !tracked[st.address] {
			continue
		}
		row := EarlyWalletFlowWallet{
			Address:           st.address,
			RootAddress:       st.root,
			ParentAddress:     st.parent,
			Depth:             st.depth,
			FirstSeenTime:     st.firstSeenTime,
			FirstSeenAt:       isoFromMS(st.firstSeenTime),
			BuyCount:          st.buyCount,
			SellCount:         st.sellCount,
			BuyAmount:         st.buyAmount,
			BuyValue:          st.buyValue,
			AvgBuyPrice:       ratio(st.buyValue, st.buyAmount),
			SellAmount:        st.sellAmount,
			SellValue:         st.sellValue,
			TransferInAmount:  st.transferInAmount,
			TransferOutAmount: st.transferOutAmount,
			CurrentBalance:    st.currentBalance,
			AllocatedCost:     st.allocatedCost,
			RemainingCost:     st.remainingCost,
			RealizedPnL:       st.realizedPnL,
			RealizedPnLPct:    pct(st.realizedPnL, st.allocatedCost),
			CostCoveragePct:   pct(st.allocatedCost, st.buyValue+st.transferInAmount),
			EventCount:        st.eventCount,
			IncompletePricing: st.incompletePricing,
			IsSeed:            st.isSeed,
		}
		walletRows = append(walletRows, row)
	}
	sort.SliceStable(walletRows, func(i, j int) bool {
		if walletRows[i].Depth != walletRows[j].Depth {
			return walletRows[i].Depth < walletRows[j].Depth
		}
		if walletRows[i].RootAddress != walletRows[j].RootAddress {
			return walletRows[i].RootAddress < walletRows[j].RootAddress
		}
		return walletRows[i].Address < walletRows[j].Address
	})
	resp.Wallets = walletRows
	sort.SliceStable(edges, func(i, j int) bool {
		if edges[i].Depth != edges[j].Depth {
			return edges[i].Depth < edges[j].Depth
		}
		if edges[i].BlockNumber != edges[j].BlockNumber {
			return edges[i].BlockNumber < edges[j].BlockNumber
		}
		return edges[i].TxHash < edges[j].TxHash
	})
	resp.Edges = edges
	resp.Summary = summarizeEarlyWalletFlow(resp.Summary.IncompleteReasons, seedRows, walletRows)
	if resp.Summary.MissingSwapPriceCount > 0 {
		resp.Status = mergeEarlyStatus(resp.Status, StatusPartialData)
		resp.Completeness = "partial"
		if resp.Message == "" {
			resp.Message = "Some swaps are missing quote prices; quantities are included but those swaps are excluded from realized PnL."
		}
	}
}

func earlySystemAddressSet(tokenAddress string, pools []store.OnchainPool) map[string]bool {
	system := map[string]bool{
		"":                             true,
		normalizeAddress(tokenAddress): true,
		"0x0000000000000000000000000000000000000000": true,
		"0x000000000000000000000000000000000000dead": true,
		"0x05ff2b0d3c5d22a9ed84c8b6f1d957a2fdd6d571": true,
		"0x10ed43c718714eb63d5aa57b78b54704e256024e": true,
		"0x13f4ea83d0bd40e75c8222255bc855a974568dd4": true,
	}
	for _, pool := range pools {
		system[normalizeAddress(pool.PoolAddress)] = true
		system[normalizeAddress(pool.Token0)] = true
		system[normalizeAddress(pool.Token1)] = true
	}
	return system
}

func earlySwapEvents(swaps []store.OnchainSwap, system map[string]bool) ([]earlyFlowEvent, string) {
	events := make([]earlyFlowEvent, 0, len(swaps))
	quoteToken := ""
	for _, swap := range swaps {
		if swap.EventType != "swap" || swap.TokenAmount <= 0 || swap.TraderAddress == "" || system[normalizeAddress(swap.TraderAddress)] {
			continue
		}
		if quoteToken == "" && swap.QuoteToken != "" {
			quoteToken = normalizeAddress(swap.QuoteToken)
		}
		kind := swap.Side
		if kind != "buy" && kind != "sell" {
			continue
		}
		events = append(events, earlyFlowEvent{
			Kind:        kind,
			Address:     normalizeAddress(swap.TraderAddress),
			TxHash:      normalizeAddress(swap.TxHash),
			BlockNumber: swap.BlockNumber,
			LogIndex:    swap.LogIndex,
			BlockTime:   swap.BlockTime,
			TokenAmount: swap.TokenAmount,
			QuoteAmount: swap.QuoteAmount,
			QuoteToken:  normalizeAddress(swap.QuoteToken),
			PoolAddress: normalizeAddress(swap.PoolAddress),
			PriceKnown:  swap.QuoteAmount > 0 && swap.QuoteToken != "",
		})
	}
	return events, quoteToken
}

func earlyTransferEvents(transfers []store.OnchainTokenTransfer, system map[string]bool) []earlyFlowEvent {
	events := make([]earlyFlowEvent, 0, len(transfers))
	for _, transfer := range transfers {
		from := normalizeAddress(transfer.FromAddress)
		to := normalizeAddress(transfer.ToAddress)
		if transfer.Amount <= 0 || system[from] || system[to] || from == to {
			continue
		}
		events = append(events, earlyFlowEvent{
			Kind:        "transfer",
			From:        from,
			To:          to,
			TxHash:      normalizeAddress(transfer.TxHash),
			BlockNumber: transfer.BlockNumber,
			LogIndex:    transfer.LogIndex,
			BlockTime:   transfer.BlockTime,
			TokenAmount: transfer.Amount,
		})
	}
	return events
}

func selectEarlySeedCandidates(events []earlyFlowEvent, limit int) []earlySeedCandidate {
	byAddress := map[string]*earlySeedCandidate{}
	for _, ev := range events {
		if ev.Kind != "buy" || ev.TokenAmount <= 0 {
			continue
		}
		item := byAddress[ev.Address]
		if item == nil {
			item = &earlySeedCandidate{
				address:       ev.Address,
				firstBuyTime:  ev.BlockTime,
				firstBuyBlock: ev.BlockNumber,
				firstBuyIndex: ev.LogIndex,
			}
			byAddress[ev.Address] = item
		}
		if item.firstBuyTime == 0 || (ev.BlockTime > 0 && ev.BlockTime < item.firstBuyTime) ||
			(ev.BlockTime == item.firstBuyTime && ev.BlockNumber < item.firstBuyBlock) {
			item.firstBuyTime = ev.BlockTime
			item.firstBuyBlock = ev.BlockNumber
			item.firstBuyIndex = ev.LogIndex
		}
		item.buyCount++
		item.buyAmount += ev.TokenAmount
		item.buyValue += ev.QuoteAmount
	}
	out := make([]earlySeedCandidate, 0, len(byAddress))
	for _, item := range byAddress {
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].firstBuyTime != out[j].firstBuyTime {
			return out[i].firstBuyTime < out[j].firstBuyTime
		}
		if out[i].firstBuyBlock != out[j].firstBuyBlock {
			return out[i].firstBuyBlock < out[j].firstBuyBlock
		}
		return out[i].address < out[j].address
	})
	if len(out) > limit {
		return out[:limit]
	}
	return out
}

func buildTrackedAddressSet(seedSet map[string]bool, transfers []earlyFlowEvent, maxDepth int) (map[string]bool, map[string]*earlyWalletState, []EarlyWalletFlowEdge) {
	tracked := map[string]bool{}
	meta := map[string]*earlyWalletState{}
	queue := make([]string, 0, len(seedSet))
	for seed := range seedSet {
		tracked[seed] = true
		meta[seed] = &earlyWalletState{address: seed, root: seed, depth: 0, isSeed: true}
		queue = append(queue, seed)
	}
	outgoing := map[string][]earlyFlowEvent{}
	for _, tr := range transfers {
		outgoing[tr.From] = append(outgoing[tr.From], tr)
	}
	sort.Strings(queue)
	edges := []EarlyWalletFlowEdge{}
	edgeSeen := map[string]bool{}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		currentMeta := meta[current]
		if currentMeta == nil || currentMeta.depth >= maxDepth {
			continue
		}
		for _, tr := range outgoing[current] {
			childDepth := currentMeta.depth + 1
			if childDepth > maxDepth {
				continue
			}
			if meta[tr.To] == nil || childDepth < meta[tr.To].depth {
				meta[tr.To] = &earlyWalletState{
					address:       tr.To,
					root:          currentMeta.root,
					parent:        current,
					depth:         childDepth,
					firstSeenTime: tr.BlockTime,
				}
			}
			if !tracked[tr.To] {
				tracked[tr.To] = true
				queue = append(queue, tr.To)
			}
			key := tr.TxHash + "|" + tr.From + "|" + tr.To
			if !edgeSeen[key] {
				edgeSeen[key] = true
				edges = append(edges, EarlyWalletFlowEdge{
					Source:      tr.From,
					Target:      tr.To,
					RootAddress: currentMeta.root,
					Depth:       childDepth,
					Amount:      tr.TokenAmount,
					TxHash:      tr.TxHash,
					BlockNumber: tr.BlockNumber,
					BlockTime:   tr.BlockTime,
					BlockAt:     isoFromMS(tr.BlockTime),
				})
			}
		}
	}
	return tracked, meta, edges
}

func combineEarlyEvents(swaps, transfers []earlyFlowEvent) []earlyFlowEvent {
	events := make([]earlyFlowEvent, 0, len(swaps)+len(transfers))
	events = append(events, swaps...)
	events = append(events, transfers...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].BlockNumber != events[j].BlockNumber {
			return events[i].BlockNumber < events[j].BlockNumber
		}
		if events[i].LogIndex != events[j].LogIndex {
			return events[i].LogIndex < events[j].LogIndex
		}
		return eventKindOrder(events[i].Kind) < eventKindOrder(events[j].Kind)
	})
	return events
}

func eventKindOrder(kind string) int {
	switch kind {
	case "buy":
		return 1
	case "transfer":
		return 2
	case "sell":
		return 3
	default:
		return 9
	}
}

func proportionalCost(remainingCost, balance, amount float64) float64 {
	if remainingCost <= 0 || balance <= 0 || amount <= 0 {
		return 0
	}
	if amount >= balance {
		return remainingCost
	}
	cost := remainingCost * (amount / balance)
	if cost > remainingCost {
		return remainingCost
	}
	return cost
}

func summarizeEarlyWalletFlow(reasons []string, seeds []EarlyWalletSeed, wallets []EarlyWalletFlowWallet) EarlyWalletFlowSummary {
	summary := EarlyWalletFlowSummary{
		SeedWalletCount:    len(seeds),
		TrackedWalletCount: len(wallets),
		IncompleteReasons:  reasons,
	}
	for _, seed := range seeds {
		summary.TotalInitialBuyAmount += seed.BuyAmount
		summary.TotalInitialCost += seed.BuyValue
		summary.SeedOwnSellAmount += seed.OwnSellAmount
		summary.SeedOwnSellValue += seed.OwnSellValue
		summary.DescendantSellAmount += seed.DescendantSellAmount
		summary.DescendantSellValue += seed.DescendantSellValue
		summary.TransferOutAmount += seed.TransferOutAmount
	}
	for _, wallet := range wallets {
		if wallet.Depth > summary.MaxObservedDepth {
			summary.MaxObservedDepth = wallet.Depth
		}
		summary.RemainingAmount += wallet.CurrentBalance
		summary.RemainingCost += wallet.RemainingCost
		summary.RealizedPnL += wallet.RealizedPnL
		summary.MissingSwapPriceCount += wallet.IncompletePricing
	}
	if summary.MissingSwapPriceCount > 0 {
		summary.IncompleteReasons = appendMissingReason(summary.IncompleteReasons, "missing_swap_prices")
	}
	summary.TotalSellValue = summary.SeedOwnSellValue + summary.DescendantSellValue
	summary.RealizedPnLPct = pct(summary.RealizedPnL, summary.TotalInitialCost)
	summary.CostCoveragePct = pct(summary.TotalInitialCost, summary.TotalInitialCost+summary.RemainingCost)
	summary.Direction, summary.Confidence = classifyEarlyFlow(summary)
	return summary
}

func classifyEarlyFlow(summary EarlyWalletFlowSummary) (string, string) {
	if summary.SeedWalletCount == 0 || summary.TotalInitialCost <= 0 || len(summary.IncompleteReasons) > 0 && summary.TotalSellValue == 0 {
		return EarlyFlowDirectionInsufficientData, "low"
	}
	soldRatio := ratio(summary.TotalSellValue, summary.TotalInitialCost)
	remainingRatio := ratio(summary.RemainingCost, summary.TotalInitialCost)
	descendantSellRatio := ratio(summary.DescendantSellValue, summary.TotalSellValue)
	direction := EarlyFlowDirectionMixed
	if remainingRatio >= 0.55 && soldRatio < 0.45 {
		direction = EarlyFlowDirectionAccumulating
	} else if soldRatio >= 0.65 || (soldRatio >= 0.45 && descendantSellRatio >= 0.35) {
		direction = EarlyFlowDirectionDistributing
	}
	confidence := "low"
	if summary.TrackedWalletCount >= 30 && math.Abs(soldRatio-remainingRatio) >= 0.25 {
		confidence = "medium"
	}
	if summary.TrackedWalletCount >= 80 && math.Abs(soldRatio-remainingRatio) >= 0.4 {
		confidence = "high"
	}
	return direction, confidence
}

func quoteSymbolForToken(quoteToken string, pools []store.OnchainPool) string {
	quoteToken = normalizeAddress(quoteToken)
	if quoteToken == "" {
		return ""
	}
	for _, pool := range pools {
		if normalizeAddress(pool.QuoteToken) == quoteToken && pool.Name != "" {
			parts := strings.Split(pool.Name, "/")
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[len(parts)-1])
			}
		}
	}
	switch quoteToken {
	case "0x55d398326f99059ff775485246999027b3197955":
		return "USDT"
	case "0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d":
		return "USDC"
	case "0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c":
		return "WBNB"
	default:
		return shortenForGraph(quoteToken)
	}
}

func ratio(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func pct(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator * 100
}

func (s *Service) WriteEarlyWalletFlowCSV(ctx context.Context, w io.Writer, req EarlyWalletFlowRequest) error {
	resp, err := s.EarlyWalletFlow(ctx, req)
	if err != nil {
		return err
	}
	return WriteEarlyWalletFlowResponseCSV(w, resp)
}

func WriteEarlyWalletFlowResponseCSV(w io.Writer, resp *EarlyWalletFlowResponse) error {
	if resp == nil {
		return fmt.Errorf("missing early wallet flow response")
	}
	cw := csv.NewWriter(w)
	defer cw.Flush()
	write := func(record []string) error {
		return cw.Write(record)
	}
	if err := write([]string{"section", "metric", "value"}); err != nil {
		return err
	}
	summary := resp.Summary
	for _, record := range [][]string{
		{"summary", "status", resp.Status},
		{"summary", "completeness", resp.Completeness},
		{"summary", "direction", summary.Direction},
		{"summary", "confidence", summary.Confidence},
		{"summary", "seed_wallet_count", strconv.Itoa(summary.SeedWalletCount)},
		{"summary", "tracked_wallet_count", strconv.Itoa(summary.TrackedWalletCount)},
		{"summary", "total_initial_cost", floatString(summary.TotalInitialCost)},
		{"summary", "total_sell_value", floatString(summary.TotalSellValue)},
		{"summary", "realized_pnl", floatString(summary.RealizedPnL)},
		{"summary", "remaining_amount", floatString(summary.RemainingAmount)},
	} {
		if err := write(record); err != nil {
			return err
		}
	}
	if err := write([]string{}); err != nil {
		return err
	}
	if err := write([]string{"seed_rank", "address", "first_buy_at", "buy_amount", "buy_value", "avg_buy_price", "own_sell_amount", "own_sell_value", "transfer_out_amount", "current_balance", "remaining_cost", "realized_pnl", "descendant_sell_value", "descendant_realized_pnl", "total_realized_pnl", "child_count"}); err != nil {
		return err
	}
	for _, seed := range resp.Seeds {
		if err := write([]string{
			strconv.Itoa(seed.Rank),
			seed.Address,
			seed.FirstBuyAt,
			floatString(seed.BuyAmount),
			floatString(seed.BuyValue),
			floatString(seed.AvgBuyPrice),
			floatString(seed.OwnSellAmount),
			floatString(seed.OwnSellValue),
			floatString(seed.TransferOutAmount),
			floatString(seed.CurrentBalance),
			floatString(seed.RemainingCost),
			floatString(seed.RealizedPnL),
			floatString(seed.DescendantSellValue),
			floatString(seed.DescendantRealizedPnL),
			floatString(seed.TotalRealizedPnL),
			strconv.Itoa(seed.ChildCount),
		}); err != nil {
			return err
		}
	}
	if err := write([]string{}); err != nil {
		return err
	}
	if err := write([]string{"depth", "root_address", "parent_address", "address", "first_seen_at", "buy_amount", "buy_value", "sell_amount", "sell_value", "transfer_in_amount", "transfer_out_amount", "current_balance", "allocated_cost", "remaining_cost", "realized_pnl", "realized_pnl_pct", "event_count", "incomplete_pricing", "is_seed"}); err != nil {
		return err
	}
	for _, wallet := range resp.Wallets {
		if err := write([]string{
			strconv.Itoa(wallet.Depth),
			wallet.RootAddress,
			wallet.ParentAddress,
			wallet.Address,
			wallet.FirstSeenAt,
			floatString(wallet.BuyAmount),
			floatString(wallet.BuyValue),
			floatString(wallet.SellAmount),
			floatString(wallet.SellValue),
			floatString(wallet.TransferInAmount),
			floatString(wallet.TransferOutAmount),
			floatString(wallet.CurrentBalance),
			floatString(wallet.AllocatedCost),
			floatString(wallet.RemainingCost),
			floatString(wallet.RealizedPnL),
			floatString(wallet.RealizedPnLPct),
			strconv.Itoa(wallet.EventCount),
			strconv.Itoa(wallet.IncompletePricing),
			strconv.FormatBool(wallet.IsSeed),
		}); err != nil {
			return err
		}
	}
	if err := cw.Error(); err != nil {
		return fmt.Errorf("write csv: %w", err)
	}
	return nil
}

func floatString(value float64) string {
	if math.Abs(value) < 1e-12 {
		value = 0
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}
