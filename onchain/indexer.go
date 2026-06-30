package onchain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"nofx/config"
	"nofx/store"

	"golang.org/x/crypto/sha3"
)

const (
	erc20NameSelector        = "0x06fdde03"
	erc20SymbolSelector      = "0x95d89b41"
	erc20DecimalsSelector    = "0x313ce567"
	erc20TotalSupplySelector = "0x18160ddd"
	pairToken0Selector       = "0x0dfe1681"
	pairToken1Selector       = "0xd21220a7"

	pancakeV2FactoryAddress    = "0xca143ce32fe78f1f7019d7d551a6402fc5350c73"
	pancakeV2DexID             = "pancakeswap_v2"
	pancakeV2FactoryStartBlock = int64(6800000)
	pancakeV2GetPairSelector   = "0xe6a43905"
	zeroAddress                = "0x0000000000000000000000000000000000000000"
)

var pancakeV2CommonQuoteTokens = []string{
	"0xbb4cdb9cbd36b01bd1cbaebf2de08d9173bc095c", // WBNB
	"0x55d398326f99059ff775485246999027b3197955", // USDT
	"0xe9e7cea3dedca5984780bafc599bd69add087d56", // BUSD
	"0x8ac76a51cc950d9822d68b83fe1ad97b32cd580d", // USDC
	"0x7130d2a12b9bcbfae4f2634d864a1ee1ce3ead9c", // BTCB
	"0x2170ed0880ac9a755fd29b2688956bd959f933f8", // ETH
	"0x0e09fabb73bd3ade0a17ecc321fd13a19e81ce82", // CAKE
	"0x8ff795a6f4d97e7887c79bea79aba5cc76444adf", // BCH
	"0x1af3f329e8be154074d8769d1ffa4ee058b1dbc3", // DAI
}

var (
	transferTopic    = eventTopic("Transfer(address,address,uint256)")
	swapTopic        = eventTopic("Swap(address,uint256,uint256,uint256,uint256,address)")
	swapV3Topic      = eventTopic("Swap(address,address,int256,int256,uint160,uint128,int24)")
	mintTopic        = eventTopic("Mint(address,uint256,uint256)")
	burnTopic        = eventTopic("Burn(address,uint256,uint256,address)")
	pairCreatedTopic = eventTopic("PairCreated(address,address,address,uint256)")
)

var errRPCPayloadTooLarge = errors.New("rpc payload too large")
var errRPCRateLimited = errors.New("rpc rate limited")
var errRPCArchiveUnsupported = errors.New("rpc archive unsupported")
var errRPCTransportRetryable = errors.New("rpc transport retryable")

const minIndexLogRangeBlocks = int64(1)
const maxFactoryLogRangeBlocks = int64(50000)

type rpcEndpointContextKey struct{}

const (
	freeLogSourceGeckoTerminal = "geckoterminal"
	freeLogSourceEtherscan     = "etherscan"
	freeLogSourceRPC           = "rpc"
)

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type rpcLog struct {
	Address         string   `json:"address"`
	Topics          []string `json:"topics"`
	Data            string   `json:"data"`
	BlockNumber     string   `json:"blockNumber"`
	TransactionHash string   `json:"transactionHash"`
	LogIndex        string   `json:"logIndex"`
}

func (s *Service) runIndexJob(ctx context.Context, chain, address string) error {
	job, err := s.store.Onchain().GetJob(chain, address)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("index job not found")
	}
	now := time.Now().UTC().UnixMilli()
	job.Status = store.OnchainJobStatusIndexing
	job.UpdatedAtMS = now
	if job.StartedAtMS == 0 {
		job.StartedAtMS = now
	}
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return err
	}
	if job.Scope == store.OnchainIndexScopeEarlyWalletWindow && job.IndexSource == store.OnchainIndexSourceFreeLocal {
		return s.runFreeLocalEarlyIndexJob(ctx, chain, address, job)
	}

	if err := s.ensureTokenMetadata(ctx, chain, address); err != nil {
		return err
	}
	pools, err := s.ensurePoolsForIndex(ctx, chain, address)
	if err != nil {
		return err
	}
	if len(pools) == 0 {
		return fmt.Errorf("no PancakeSwap V2 pools found for token")
	}

	batch := job.BatchSize
	if batch <= 0 {
		batch = config.Get().OnchainIndexerBatchBlocks
	}
	if batch <= 0 {
		batch = 2000
	}
	from := job.LastBlock + 1
	if from <= 0 || from < job.StartBlock {
		from = job.StartBlock
	}
	end := job.EndBlock
	if end <= 0 {
		end, err = s.rpcBlockNumber(ctx)
		if err != nil {
			return err
		}
	}
	for from <= end {
		to := from + int64(batch) - 1
		if to > end {
			to = end
		}
		if job.Scope == store.OnchainIndexScopeEarlyWalletWindow {
			if err := s.indexEarlySeedBlockRange(ctx, chain, address, pools, from, to); err != nil {
				return err
			}
		} else if err := s.indexBlockRange(ctx, chain, address, pools, from, to); err != nil {
			return err
		}
		job.LastBlock = to
		job.EndBlock = end
		job.UpdatedAtMS = time.Now().UTC().UnixMilli()
		if err := s.store.Onchain().UpsertJob(job); err != nil {
			return err
		}
		if job.Scope == store.OnchainIndexScopeEarlyWalletWindow {
			count, err := s.store.Onchain().CountEarlyBuyWallets(chain, address, defaultEarlySeedCount)
			if err != nil {
				return err
			}
			if int(count) >= defaultEarlySeedCount {
				break
			}
		}
		from = to + 1
	}
	if job.Scope == store.OnchainIndexScopeEarlyWalletWindow {
		// Early-window indexing is intentionally seed-first: swap logs identify
		// the first buyer set quickly. Transfer graph enrichment can be added by
		// a fuller index, but should not block the early-wallet model on RPCs
		// that limit eth_getLogs to tiny ranges.
	} else {
		if err := s.store.Onchain().RecomputeWalletSnapshots(chain, address); err != nil {
			return err
		}
	}
	completed := time.Now().UTC().UnixMilli()
	job.Status = store.OnchainJobStatusCompleted
	job.CompletedAtMS = completed
	job.UpdatedAtMS = completed
	job.ErrorMessage = ""
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return err
	}
	token, _ := s.store.Onchain().GetToken(chain, address)
	if token != nil {
		token.IndexStatus = store.OnchainJobStatusCompleted
		token.LastIndexedBlock = job.LastBlock
		token.LastIndexedAt = completed
		token.AnalysisCompleteness = "full"
		_ = s.store.Onchain().UpsertToken(token)
	}
	return nil
}

func (s *Service) runFreeLocalEarlyIndexJob(ctx context.Context, chain, address string, job *store.OnchainIndexJob) error {
	freeRPCs := freeRPCURLs()
	if len(freeRPCs) > 0 {
		ctx = context.WithValue(ctx, rpcEndpointContextKey{}, freeRPCs)
	}
	task, _ := s.store.Onchain().GetLocalIndexTask(chain, address)
	if task == nil {
		task = &store.OnchainLocalIndexTask{
			Chain:        chain,
			TokenAddress: address,
			Enabled:      true,
			IndexSource:  store.OnchainIndexSourceFreeLocal,
			Status:       store.OnchainJobStatusIndexing,
		}
	}
	_ = s.ensureTokenMetadata(ctx, chain, address)
	job.Phase = PhaseDiscoveringPools
	job.Provider = freeSeedProviderName(s.freeLogs)
	job.RequestBudget = freeLogsBudgetRemaining(s.freeLogs)
	job.ProgressMessage = "Discovering PancakeSwap pools for free local early-wallet index."
	job.UpdatedAtMS = time.Now().UTC().UnixMilli()
	_ = s.store.Onchain().UpsertJob(job)

	pools, err := s.store.Onchain().ListPools(chain, address)
	if err != nil {
		return err
	}
	needsStartDiscovery := job.StartBlock <= 0 && task.StartBlock <= 0
	needsCreatedMetadata := len(pools) > 0 && poolsNeedCreatedMetadata(pools)
	shouldDiscoverCreatedMetadata := needsCreatedMetadata && needsFreeLocalCreatedMetadata(job, task, pools)
	if shouldDiscoverCreatedMetadata {
		s.enrichPoolsWithPublicMetadata(ctx, chain, address, pools)
		pools, err = s.store.Onchain().ListPools(chain, address)
		if err != nil {
			return err
		}
		needsCreatedMetadata = len(pools) > 0 && poolsNeedCreatedMetadata(pools)
		shouldDiscoverCreatedMetadata = needsCreatedMetadata && needsFreeLocalCreatedMetadata(job, task, pools)
	}
	if shouldDiscoverCreatedMetadata && shouldUseEtherscanSeeds() && s.freeLogs != nil && s.freeLogs.configured() {
		maxRequests := config.Get().OnchainFreeBackfillMaxRequestsPerTick
		ethPools, _, _, _, ethErr := s.discoverPoolsFromPancakeFactoryFreeLogs(ctx, chain, address, pancakeV2FactoryStartBlock, 0, maxRequests)
		if ethErr == nil && len(ethPools) > 0 {
			if upsertErr := s.store.Onchain().UpsertPools(ethPools); upsertErr != nil {
				return upsertErr
			}
			pools, err = s.store.Onchain().ListPools(chain, address)
			if err != nil {
				return err
			}
			needsCreatedMetadata = len(pools) > 0 && poolsNeedCreatedMetadata(pools)
		}
	}
	if !needsCreatedMetadata && len(pools) > 0 && poolsHaveCreatedBlock(pools) {
		discoveredStart := earliestPoolCreatedBlock(pools)
		if discoveredStart > 0 && (needsStartDiscovery || job.StartBlock != discoveredStart) {
			job.StartBlock = 0
			job.EndBlock = 0
			job.LastBlock = 0
			task.StartBlock = 0
			task.LastBlock = 0
			job.Phase = PhaseScanningSeeds
		}
		job.Phase = PhaseScanningSeeds
		job.ProgressMessage = "Pool created block is known; scanning free early-wallet seed window."
		task.ProgressMessage = job.ProgressMessage
		_ = s.store.Onchain().UpsertJob(job)
		_ = s.store.Onchain().UpsertLocalIndexTask(task)
	}
	if len(pools) == 0 && shouldUseEtherscanSeeds() && s.freeLogs != nil && s.freeLogs.configured() {
		discoveryUpper := int64(0)
		if job.LastBlock >= pancakeV2FactoryStartBlock {
			discoveryUpper = job.LastBlock
		}
		maxRequests := config.Get().OnchainFreeBackfillMaxRequestsPerTick
		ethPools, nextDiscoveryUpper, discoveryComplete, _, ethErr := s.discoverPoolsFromPancakeFactoryFreeLogs(ctx, chain, address, pancakeV2FactoryStartBlock, discoveryUpper, maxRequests)
		if ethErr == nil && len(ethPools) > 0 {
			if upsertErr := s.store.Onchain().UpsertPools(ethPools); upsertErr != nil {
				return upsertErr
			}
			pools, err = s.store.Onchain().ListPools(chain, address)
			if err != nil {
				return err
			}
			job.Phase = PhaseScanningSeeds
			job.LastBlock = 0
			task.LastBlock = 0
		} else if ethErr == nil && !discoveryComplete {
			return s.queueFreeLocalPoolDiscovery(chain, address, job, task, nextDiscoveryUpper, fmt.Sprintf("Free Etherscan pool discovery scanned down to block %d; pool not found yet, continuing next tick.", nextDiscoveryUpper))
		} else if ethErr != nil && len(freeRPCs) == 0 {
			return s.queueFreeLocalPoolDiscovery(chain, address, job, task, nextDiscoveryUpper, "Free Etherscan pool discovery is retrying after an API error: "+ethErr.Error())
		} else if ethErr != nil {
			job.ProgressMessage = "Free Etherscan pool discovery is temporarily unavailable; trying free RPC getPair discovery."
			task.ProgressMessage = job.ProgressMessage
			_ = s.store.Onchain().UpsertJob(job)
			_ = s.store.Onchain().UpsertLocalIndexTask(task)
		}
	}
	if len(pools) == 0 && len(freeRPCs) > 0 {
		callPools, callErr := s.discoverPoolsFromPancakeFactoryCalls(ctx, chain, address)
		if callErr == nil && len(callPools) > 0 {
			if upsertErr := s.store.Onchain().UpsertPools(callPools); upsertErr != nil {
				return upsertErr
			}
			pools, err = s.store.Onchain().ListPools(chain, address)
			if err != nil {
				return err
			}
			job.Phase = PhaseScanningSeeds
			job.LastBlock = 0
			task.LastBlock = 0
		}
	}
	if len(pools) == 0 && len(freeRPCs) > 0 && shouldUseRPCSeeds() {
		discoveryUpper := int64(0)
		if job.Phase == PhaseDiscoveringPools && job.LastBlock >= pancakeV2FactoryStartBlock {
			discoveryUpper = job.LastBlock
		}
		maxRequests := config.Get().OnchainFreeBackfillMaxRequestsPerTick
		rpcPools, nextDiscoveryUpper, discoveryComplete, _, rpcErr := s.discoverPoolsFromPancakeFactoryLogs(ctx, chain, address, pancakeV2FactoryStartBlock, discoveryUpper, maxRequests)
		if rpcErr == nil && len(rpcPools) > 0 {
			if upsertErr := s.store.Onchain().UpsertPools(rpcPools); upsertErr != nil {
				return upsertErr
			}
			pools, err = s.store.Onchain().ListPools(chain, address)
			job.Phase = PhaseScanningSeeds
			job.LastBlock = 0
			task.LastBlock = 0
		} else if rpcErr == nil && !discoveryComplete {
			return s.queueFreeLocalPoolDiscovery(chain, address, job, task, nextDiscoveryUpper, fmt.Sprintf("Free RPC pool discovery scanned down to block %d; pool not found yet, continuing next tick.", nextDiscoveryUpper))
		} else if rpcErr != nil {
			return s.queueFreeLocalPoolDiscovery(chain, address, job, task, nextDiscoveryUpper, "Free RPC pool discovery is retrying after an RPC error: "+rpcErr.Error())
		}
	}
	if len(pools) == 0 {
		pools, err = s.ensurePoolsForIndex(ctx, chain, address)
	}
	if err != nil || len(pools) == 0 {
		message := "Free local index is enabled, but no PancakeSwap pool could be discovered yet; retrying in the background."
		if err != nil {
			message = message + " " + err.Error()
		}
		return s.completeFreeLocalJobPartial(chain, address, job, task, message, "pool discovery incomplete")
	}
	if shouldUseEtherscanSeeds() {
		createdPools := filterPoolsWithCreatedBlock(pools)
		if len(createdPools) == 0 {
			if len(freeRPCs) > 0 {
				if err := s.populatePoolCreatedBlocksFromHistoricalCode(ctx, pools, freeRPCs); err == nil {
					_ = s.store.Onchain().UpsertPools(pools)
					createdPools = filterPoolsWithCreatedBlock(pools)
				}
			}
		}
		if len(createdPools) == 0 {
			return s.queueFreeLocalCreatedBlockDiscovery(chain, address, job, task)
		}
		pools = createdPools
		discoveredStart := earliestPoolCreatedBlock(pools)
		if discoveredStart > 0 && job.StartBlock != discoveredStart {
			job.StartBlock = 0
			job.EndBlock = 0
			job.LastBlock = 0
			task.StartBlock = 0
			task.LastBlock = 0
			job.Phase = PhaseScanningSeeds
		}
	}
	startBlock := job.StartBlock
	if startBlock <= 0 {
		startBlock = task.StartBlock
	}
	latest := int64(0)
	if len(freeRPCs) > 0 {
		latest, _ = s.rpcBlockNumber(ctx)
	}
	if startBlock <= 0 {
		startBlock = s.estimateFreeLocalStartBlock(ctx, pools, latest)
	}
	if startBlock <= 0 {
		startBlock = 1
	}
	if startBlock < 1 {
		startBlock = 1
	}
	endBlock := job.EndBlock
	if endBlock <= 0 {
		window := config.Get().OnchainEarlyWindowBlocks
		if window <= 0 {
			window = defaultEarlyWalletIndexWindowBlocks
		}
		endBlock = startBlock + window - 1
	}
	if latest > 0 && endBlock > latest {
		endBlock = latest
	}
	if endBlock < startBlock {
		endBlock = startBlock
	}
	if job.Phase == PhaseDiscoveringPools || job.LastBlock < startBlock-1 {
		job.LastBlock = startBlock - 1
	}
	maxRequests := config.Get().OnchainFreeBackfillMaxRequestsPerTick
	if maxRequests <= 0 {
		maxRequests = 200
	}
	targetSeeds := config.Get().OnchainFreeBackfillTargetSeeds
	if targetSeeds <= 0 || targetSeeds > maxEarlySeedCount {
		targetSeeds = defaultEarlySeedCount
	}
	batch := job.BatchSize
	if batch <= 0 {
		batch = config.Get().OnchainIndexerBatchBlocks
	}
	if batch <= 0 {
		batch = 2000
	}
	from := job.LastBlock + 1
	if from < startBlock {
		from = startBlock
	}
	if shouldUseGeckoTerminalSeeds() {
		count, err := s.seedFreeLocalFromGeckoTerminal(ctx, chain, address, job, task, pools, startBlock, endBlock, targetSeeds)
		if err == nil && count > 0 {
			if count >= targetSeeds {
				_ = s.enrichFreeLocalSeedTransfers(ctx, chain, address, job, task, pools, startBlock, endBlock, targetSeeds)
				return s.completeFreeLocalJob(chain, address, job, task, fmt.Sprintf("Free GeckoTerminal seed index completed with %d early buyers. Public free data may be recent-window limited.", count), "partial")
			}
			message := fmt.Sprintf("Free GeckoTerminal seed index found %d/%d early buyers from public trades. Public free data may be recent-window limited.", count, targetSeeds)
			_ = s.enrichFreeLocalSeedTransfers(ctx, chain, address, job, task, pools, startBlock, endBlock, targetSeeds)
			if !shouldUseEtherscanSeeds() && !shouldUseRPCSeeds() {
				return s.completeFreeLocalJob(chain, address, job, task, message, "partial")
			}
			job.ProgressMessage = message
			task.ProgressMessage = message
			_ = s.store.Onchain().UpsertJob(job)
			_ = s.store.Onchain().UpsertLocalIndexTask(task)
		} else if err != nil && !shouldUseEtherscanSeeds() && !shouldUseRPCSeeds() {
			return s.completeFreeLocalJobPartial(chain, address, job, task, "Free GeckoTerminal seed source is temporarily unavailable; retry later or add another free source.", "geckoterminal unavailable: "+err.Error())
		}
	}
	if !shouldUseEtherscanSeeds() && !shouldUseRPCSeeds() {
		count, err := s.store.Onchain().CountEarlyBuyWallets(chain, address, targetSeeds)
		if err != nil {
			return err
		}
		return s.completeFreeLocalJob(chain, address, job, task, fmt.Sprintf("Free public seed index finished with %d/%d early buyers; no paid Archive RPC was used.", count, targetSeeds), "partial")
	}
	if len(freeRPCs) == 0 {
		if s.freeLogs == nil || !s.freeLogs.configured() {
			return s.completeFreeLocalJobPartial(chain, address, job, task, "Free local index is registered, but no free RPC/log source is configured. Use GeckoTerminal or set ONCHAIN_FREE_RPC_URLS for optional fallback.", "free data source missing")
		}
		return s.completeFreeLocalJobPartial(chain, address, job, task, "Free log source is configured, but ONCHAIN_FREE_RPC_URLS is required for optional block/RPC fallback calls.", "free rpc missing")
	}
	requestsUsed := 0
	lastMessage := ""
	for from <= endBlock && requestsUsed < maxRequests {
		to := from + int64(batch) - 1
		if to > endBlock {
			to = endBlock
		}
		used, err := s.indexFreeLocalEarlySeedBlockRange(ctx, chain, address, pools, from, to)
		requestsUsed += used
		if err != nil {
			if errors.Is(err, errRPCTransportRetryable) {
				return s.queueFreeLocalSeedRetry(chain, address, job, task, startBlock, endBlock, "Free log transport is temporarily unavailable while scanning the early-wallet window; retrying on the next tick. rpc_transport_retrying", "rpc transport retrying: "+err.Error())
			}
			if errors.Is(err, errFreeLogsBudgetExceeded) {
				return s.queueFreeLocalSeedRetry(chain, address, job, task, startBlock, endBlock, "Free Etherscan log request budget is exhausted; retrying after the daily budget resets. free_logs_budget_exhausted", "free logs daily budget exceeded; retrying")
			}
			if errors.Is(err, errRPCArchiveUnsupported) {
				message := "Free RPC does not expose enough historical logs for this token; local indexing is enabled and available data will remain partial."
				return s.completeFreeLocalJob(chain, address, job, task, message, "partial")
			}
			if errors.Is(err, errRPCRateLimited) {
				return s.queueFreeLocalSeedRetry(chain, address, job, task, startBlock, endBlock, "Free log provider is rate limited while scanning the early-wallet window; retrying on the next tick. free_logs_rate_limited", "rpc rate limited; retrying")
			}
			if errors.Is(err, errRPCPayloadTooLarge) && batch > int(minIndexLogRangeBlocks) {
				batch = int(minIndexLogRangeBlocks)
				continue
			}
			return err
		}
		job.LastBlock = to
		job.StartBlock = startBlock
		job.EndBlock = endBlock
		job.BatchSize = batch
		job.Phase = PhaseScanningSeeds
		job.Provider = freeSeedProviderName(s.freeLogs)
		job.RequestBudget = freeLogsBudgetRemaining(s.freeLogs)
		job.UpdatedAtMS = time.Now().UTC().UnixMilli()
		count, err := s.store.Onchain().CountEarlyBuyWallets(chain, address, targetSeeds)
		if err != nil {
			return err
		}
		lastMessage = fmt.Sprintf("Free local index scanned blocks %d-%d; found %d/%d early buyers.", startBlock, job.LastBlock, count, targetSeeds)
		job.ProgressMessage = lastMessage
		task.StartBlock = startBlock
		task.LastBlock = job.LastBlock
		task.Status = store.OnchainJobStatusIndexing
		task.LastError = ""
		task.ProgressMessage = lastMessage
		if err := s.store.Onchain().UpsertJob(job); err != nil {
			return err
		}
		if err := s.store.Onchain().UpsertLocalIndexTask(task); err != nil {
			return err
		}
		if int(count) >= targetSeeds {
			if err := s.enrichFreeLocalSeedTransfers(ctx, chain, address, job, task, pools, startBlock, endBlock, targetSeeds); err != nil {
				if errors.Is(err, errRPCRateLimited) || errors.Is(err, errFreeLogsBudgetExceeded) {
					return err
				}
				return s.completeFreeLocalJobPartial(chain, address, job, task, fmt.Sprintf("Free local early-wallet index found %d early buyers, but transfer enrichment is partial: %v", count, err), "transfer enrichment partial")
			}
			return s.completeFreeLocalJob(chain, address, job, task, fmt.Sprintf("Free local early-wallet index completed after finding %d early buyers.", count), "full")
		}
		from = to + 1
	}
	count, err := s.store.Onchain().CountEarlyBuyWallets(chain, address, targetSeeds)
	if err != nil {
		return err
	}
	if int(count) >= targetSeeds {
		if err := s.enrichFreeLocalSeedTransfers(ctx, chain, address, job, task, pools, startBlock, endBlock, targetSeeds); err != nil {
			if errors.Is(err, errRPCRateLimited) || errors.Is(err, errFreeLogsBudgetExceeded) {
				return err
			}
			return s.completeFreeLocalJobPartial(chain, address, job, task, fmt.Sprintf("Free local early-wallet index found %d early buyers, but transfer enrichment is partial: %v", count, err), "transfer enrichment partial")
		}
		return s.completeFreeLocalJob(chain, address, job, task, fmt.Sprintf("Free local early-wallet index completed after finding %d early buyers.", count), "full")
	}
	if from > endBlock {
		message := fmt.Sprintf("Free historical backfill finished with %d/%d early buyers; old-token history may be incomplete on free RPC.", count, targetSeeds)
		if count > 0 {
			_ = s.enrichFreeLocalSeedTransfers(ctx, chain, address, job, task, pools, startBlock, endBlock, targetSeeds)
		}
		return s.completeFreeLocalJob(chain, address, job, task, message, "partial")
	}
	if lastMessage == "" {
		lastMessage = fmt.Sprintf("Free local index queued; scanned up to block %d of %d.", job.LastBlock, endBlock)
	}
	job.Status = store.OnchainJobStatusQueued
	job.ProgressMessage = lastMessage
	job.UpdatedAtMS = time.Now().UTC().UnixMilli()
	task.Status = store.OnchainJobStatusQueued
	task.ProgressMessage = lastMessage
	task.StartBlock = startBlock
	task.LastBlock = job.LastBlock
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return err
	}
	if err := s.store.Onchain().UpsertLocalIndexTask(task); err != nil {
		return err
	}
	return s.markTokenIndexStatus(chain, address, store.OnchainJobStatusIndexing, job.UpdatedAtMS)
}

func (s *Service) enrichFreeLocalSeedTransfers(ctx context.Context, chain, tokenAddress string, job *store.OnchainIndexJob, task *store.OnchainLocalIndexTask, pools []store.OnchainPool, startBlock, endBlock int64, targetSeeds int) error {
	canUseEtherscan := shouldUseEtherscanSeeds() && s.freeLogs != nil && s.freeLogs.configured()
	canUseRPC := shouldUseRPCSeeds() && len(freeRPCURLs()) > 0
	if !canUseEtherscan && !canUseRPC {
		return nil
	}
	seeds, err := s.store.Onchain().ListEarlyBuyWallets(chain, tokenAddress, targetSeeds)
	if err != nil {
		return err
	}
	if len(seeds) == 0 {
		return nil
	}
	job.Phase = PhaseEnrichingWallets
	job.Provider = freeLogsProviderName(s.freeLogs)
	job.RequestBudget = freeLogsBudgetRemaining(s.freeLogs)
	job.ProgressMessage = fmt.Sprintf("Enriching transfer graph for %d early wallets.", len(seeds))
	job.UpdatedAtMS = time.Now().UTC().UnixMilli()
	_ = s.store.Onchain().UpsertJob(job)

	maxDepth := defaultEarlyMaxDepth
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
	tokenDecimals := 18
	if token, _ := s.store.Onchain().GetToken(chain, tokenAddress); token != nil && token.Decimals > 0 {
		tokenDecimals = token.Decimals
	}
	poolSet := map[string]store.OnchainPool{}
	for _, pool := range pools {
		poolSet[normalizeAddress(pool.PoolAddress)] = pool
	}
	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		next := []string{}
		for _, wallet := range frontier {
			logs, err := s.fetchFreeTransferLogsForAddress(ctx, tokenAddress, wallet, startBlock, endBlock)
			if err != nil {
				return err
			}
			transfers := make([]store.OnchainTokenTransfer, 0, len(logs))
			for _, log := range logs {
				tr, ok := parseTransferLog(chain, tokenAddress, log, 0, tokenDecimals)
				if !ok {
					continue
				}
				transfers = append(transfers, tr)
				from := normalizeAddress(tr.FromAddress)
				to := normalizeAddress(tr.ToAddress)
				_, toIsPool := poolSet[to]
				if known[from] && to != "" && !known[to] && !toIsPool {
					known[to] = true
					next = append(next, to)
				}
				_, fromIsPool := poolSet[from]
				if known[to] && from != "" && !known[from] && !fromIsPool {
					known[from] = true
					next = append(next, from)
				}
			}
			if err := s.store.Onchain().InsertTransfers(transfers); err != nil {
				return err
			}
		}
		frontier = next
		job.RequestBudget = freeLogsBudgetRemaining(s.freeLogs)
		job.ProgressMessage = fmt.Sprintf("Enriched transfer graph depth %d/%d; tracked %d wallets.", depth+1, maxDepth, len(known))
		job.UpdatedAtMS = time.Now().UTC().UnixMilli()
		task.ProgressMessage = job.ProgressMessage
		task.LastBlock = job.LastBlock
		_ = s.store.Onchain().UpsertJob(job)
		_ = s.store.Onchain().UpsertLocalIndexTask(task)
	}
	return nil
}

func (s *Service) fetchFreeTransferLogsForAddress(ctx context.Context, tokenAddress, walletAddress string, from, to int64) ([]rpcLog, error) {
	if shouldUseEtherscanSeeds() && s.freeLogs != nil && s.freeLogs.configured() {
		return s.freeLogs.getTransferLogsForAddress(ctx, tokenAddress, walletAddress, from, to)
	}
	out := make([]rpcLog, 0)
	for _, topics := range [][]any{
		{transferTopic, addressTopic(walletAddress), nil},
		{transferTopic, nil, addressTopic(walletAddress)},
	} {
		logs, err := s.fetchTransferLogsWithTopics(ctx, tokenAddress, topics, from, to)
		if err == nil {
			out = append(out, logs...)
			continue
		}
		if !errors.Is(err, errRPCPayloadTooLarge) || to-from+1 <= minIndexLogRangeBlocks {
			return dedupeRPCLogs(out), err
		}
		for cursor := from; cursor <= to; {
			next := cursor + minIndexLogRangeBlocks - 1
			if next > to {
				next = to
			}
			logs, err := s.fetchTransferLogsWithTopics(ctx, tokenAddress, topics, cursor, next)
			if err != nil {
				return dedupeRPCLogs(out), err
			}
			out = append(out, logs...)
			cursor = next + 1
		}
	}
	return dedupeRPCLogs(out), nil
}

func poolsNeedCreatedMetadata(pools []store.OnchainPool) bool {
	return len(pools) > 0 && !poolsHaveCreatedBlock(pools)
}

func needsFreeLocalCreatedMetadata(job *store.OnchainIndexJob, task *store.OnchainLocalIndexTask, pools []store.OnchainPool) bool {
	return len(pools) > 0 && poolsNeedCreatedMetadata(pools)
}

func poolsHaveCreatedBlock(pools []store.OnchainPool) bool {
	return earliestPoolCreatedBlock(pools) > 0
}

func filterPoolsWithCreatedBlock(pools []store.OnchainPool) []store.OnchainPool {
	out := make([]store.OnchainPool, 0, len(pools))
	for _, pool := range pools {
		if pool.CreatedBlock > 0 {
			out = append(out, pool)
		}
	}
	return out
}

func earliestPoolCreatedBlock(pools []store.OnchainPool) int64 {
	startBlock := int64(0)
	for _, pool := range pools {
		if pool.CreatedBlock <= 0 {
			continue
		}
		if startBlock == 0 || pool.CreatedBlock < startBlock {
			startBlock = pool.CreatedBlock
		}
	}
	return startBlock
}

func (s *Service) seedFreeLocalFromGeckoTerminal(ctx context.Context, chain, tokenAddress string, job *store.OnchainIndexJob, task *store.OnchainLocalIndexTask, pools []store.OnchainPool, startBlock, endBlock int64, targetSeeds int) (int, error) {
	job.Phase = PhaseScanningSeeds
	job.Provider = "geckoterminal_recent"
	job.RequestBudget = freeLogsBudgetRemaining(s.freeLogs)
	job.ProgressMessage = "Scanning public GeckoTerminal trades for free early-wallet seeds."
	job.StartBlock = startBlock
	job.EndBlock = endBlock
	job.UpdatedAtMS = time.Now().UTC().UnixMilli()
	task.StartBlock = startBlock
	task.LastBlock = job.LastBlock
	task.Status = store.OnchainJobStatusIndexing
	task.ProgressMessage = job.ProgressMessage
	_ = s.store.Onchain().UpsertJob(job)
	_ = s.store.Onchain().UpsertLocalIndexTask(task)

	swaps, maxBlock, err := s.fetchGeckoPoolTradeSwaps(ctx, chain, tokenAddress, pools)
	if err != nil {
		return 0, err
	}
	if err := s.store.Onchain().InsertSwaps(swaps); err != nil {
		return 0, err
	}
	if maxBlock > job.LastBlock {
		job.LastBlock = maxBlock
	}
	count, err := s.store.Onchain().CountEarlyBuyWallets(chain, tokenAddress, targetSeeds)
	if err != nil {
		return 0, err
	}
	message := fmt.Sprintf("Free GeckoTerminal seed scan imported %d swaps; found %d/%d early buyers.", len(swaps), count, targetSeeds)
	job.ProgressMessage = message
	job.Provider = "geckoterminal_recent"
	job.UpdatedAtMS = time.Now().UTC().UnixMilli()
	task.LastBlock = job.LastBlock
	task.ProgressMessage = message
	task.LastError = ""
	_ = s.store.Onchain().UpsertJob(job)
	_ = s.store.Onchain().UpsertLocalIndexTask(task)
	return int(count), nil
}

func (s *Service) fetchGeckoPoolTradeSwaps(ctx context.Context, chain, tokenAddress string, pools []store.OnchainPool) ([]store.OnchainSwap, int64, error) {
	limit := config.Get().OnchainGeckoTradesLimit
	if limit <= 0 {
		limit = 300
	}
	if limit > 1000 {
		limit = 1000
	}
	swaps := make([]store.OnchainSwap, 0)
	maxBlock := int64(0)
	var firstErr error
	for _, pool := range pools {
		poolAddress := normalizeAddress(pool.PoolAddress)
		if poolAddress == "" {
			continue
		}
		trades, err := s.fetchGeckoPoolTrades(ctx, chain, poolAddress, limit)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for idx, trade := range trades {
			swap, ok := geckoTradeToSwap(chain, tokenAddress, pool, trade, idx)
			if !ok {
				continue
			}
			if swap.BlockNumber > maxBlock {
				maxBlock = swap.BlockNumber
			}
			swaps = append(swaps, swap)
		}
	}
	if len(swaps) == 0 && firstErr != nil {
		return nil, maxBlock, firstErr
	}
	return swaps, maxBlock, nil
}

func (s *Service) fetchGeckoPoolTrades(ctx context.Context, chain, poolAddress string, limit int) ([]geckoTrade, error) {
	reqCtx, cancel := context.WithTimeout(ctx, onchainPoolDiscoveryAttemptTimeout)
	defer cancel()
	url := fmt.Sprintf("https://api.geckoterminal.com/api/v2/networks/%s/pools/%s/trades?limit=%d", chain, poolAddress, limit)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NOFX-Onchain-FreeIndexer/1.0")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	var decoded geckoTradesResponse
	if err := decodeJSONLimited(resp, &decoded, 4<<20); err != nil {
		return nil, err
	}
	return decoded.Data, nil
}

func geckoTradeToSwap(chain, tokenAddress string, pool store.OnchainPool, trade geckoTrade, fallbackIndex int) (store.OnchainSwap, bool) {
	a := trade.Attributes
	token := normalizeAddress(tokenAddress)
	fromToken := normalizeAddress(a.FromTokenAddress)
	toToken := normalizeAddress(a.ToTokenAddress)
	trader := normalizeAddress(a.TxFromAddress)
	txHash := normalizeAddress(a.TxHash)
	if token == "" || trader == "" || txHash == "" {
		return store.OnchainSwap{}, false
	}
	side := ""
	tokenAmount := float64(0)
	quoteAmount := parseFloatString(a.VolumeUSD)
	quoteToken := ""
	switch {
	case toToken == token:
		side = "buy"
		tokenAmount = parseFloatString(a.ToTokenAmount)
		quoteToken = fromToken
	case fromToken == token:
		side = "sell"
		tokenAmount = parseFloatString(a.FromTokenAmount)
		quoteToken = toToken
	default:
		return store.OnchainSwap{}, false
	}
	if side == "" || tokenAmount <= 0 {
		return store.OnchainSwap{}, false
	}
	if quoteAmount <= 0 {
		if side == "buy" {
			quoteAmount = parseFloatString(a.FromTokenAmount)
		} else {
			quoteAmount = parseFloatString(a.ToTokenAmount)
		}
	}
	return store.OnchainSwap{
		Chain:         chain,
		TokenAddress:  token,
		PoolAddress:   normalizeAddress(pool.PoolAddress),
		TxHash:        txHash,
		LogIndex:      geckoTradeLogIndex(trade, fallbackIndex),
		BlockNumber:   a.BlockNumber,
		BlockTime:     msFromISO(a.BlockTimestamp),
		EventType:     "swap",
		TraderAddress: trader,
		Side:          side,
		TokenAmount:   tokenAmount,
		QuoteAmount:   quoteAmount,
		QuoteToken:    quoteToken,
		EstimatedUSD:  parseFloatString(a.VolumeUSD),
	}, true
}

func geckoTradeLogIndex(trade geckoTrade, fallbackIndex int) int64 {
	parts := strings.Split(trade.ID, "_")
	for i := len(parts) - 1; i >= 0; i-- {
		n, err := strconv.ParseInt(parts[i], 10, 64)
		if err == nil {
			return n
		}
	}
	return int64(fallbackIndex + 1)
}

func configuredFreeLogSources() map[string]bool {
	raw := strings.TrimSpace(config.Get().OnchainFreeLogSource)
	if raw == "" {
		raw = freeLogSourceEtherscan + "," + freeLogSourceGeckoTerminal
	}
	out := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if item == "gecko" || item == "gecko_terminal" {
			item = freeLogSourceGeckoTerminal
		}
		out[item] = true
	}
	if out["all"] {
		out[freeLogSourceGeckoTerminal] = true
		out[freeLogSourceEtherscan] = true
		out[freeLogSourceRPC] = true
	}
	return out
}

func shouldUseGeckoTerminalSeeds() bool {
	return configuredFreeLogSources()[freeLogSourceGeckoTerminal]
}

func shouldUseEtherscanSeeds() bool {
	sources := configuredFreeLogSources()
	return sources[freeLogSourceEtherscan] || sources[freeLogsProviderEtherscan]
}

func shouldUseRPCSeeds() bool {
	return configuredFreeLogSources()[freeLogSourceRPC]
}

func freeSeedProviderName(client *freeLogsClient) string {
	if shouldUseGeckoTerminalSeeds() {
		return "geckoterminal_recent"
	}
	if shouldUseEtherscanSeeds() && client != nil && client.configured() {
		if shouldUseRPCSeeds() && len(freeRPCURLs()) > 0 {
			return freeLogsProviderEtherscan + "+free_rpc"
		}
		return freeLogsProviderEtherscan
	}
	if shouldUseRPCSeeds() {
		return "free_rpc"
	}
	return ""
}

func freeLogsProviderName(client *freeLogsClient) string {
	if shouldUseEtherscanSeeds() && client != nil && client.configured() {
		return freeLogsProviderEtherscan
	}
	if len(freeRPCURLs()) > 0 {
		return "free_rpc"
	}
	return ""
}

func freeLogsBudgetRemaining(client *freeLogsClient) int {
	if client == nil || !client.configured() {
		return 0
	}
	return client.remainingBudget()
}

func (s *Service) indexFreeLocalEarlySeedBlockRange(ctx context.Context, chain, tokenAddress string, pools []store.OnchainPool, from, to int64) (int, error) {
	if from > to {
		return 0, nil
	}
	_, err := s.indexFreeLocalEarlySeedBlockRangeWithBatch(ctx, chain, tokenAddress, pools, from, to)
	if err == nil {
		return len(pools), nil
	}
	if !errors.Is(err, errRPCPayloadTooLarge) || to-from+1 <= minIndexLogRangeBlocks {
		return len(pools), err
	}
	requests := 0
	for cursor := from; cursor <= to; {
		next := cursor + minIndexLogRangeBlocks - 1
		if next > to {
			next = to
		}
		used, err := s.indexFreeLocalEarlySeedBlockRangeWithBatch(ctx, chain, tokenAddress, pools, cursor, next)
		requests += used
		if err != nil {
			return requests, err
		}
		cursor = next + 1
	}
	return requests, nil
}

func (s *Service) indexFreeLocalEarlySeedBlockRangeWithBatch(ctx context.Context, chain, tokenAddress string, pools []store.OnchainPool, from, to int64) (int, error) {
	tokenDecimals := 18
	if token, _ := s.store.Onchain().GetToken(chain, tokenAddress); token != nil && token.Decimals > 0 {
		tokenDecimals = token.Decimals
	}
	swaps := make([]store.OnchainSwap, 0)
	requests := 0
	for _, pool := range pools {
		if pool.Token0 == "" || pool.Token1 == "" {
			continue
		}
		requests++
		logs, err := s.fetchFreePoolSeedLogs(ctx, pool.PoolAddress, from, to)
		if err != nil {
			return requests, err
		}
		for _, log := range logs {
			normalizedPool := pool
			if normalizedPool.Token0Decimals <= 0 {
				normalizedPool.Token0Decimals = tokenDecimals
			}
			if normalizedPool.Token1Decimals <= 0 {
				normalizedPool.Token1Decimals = 18
			}
			swap, ok := parsePoolEvent(chain, tokenAddress, normalizedPool, log, 0)
			if ok {
				swaps = append(swaps, swap)
			}
		}
	}
	return requests, s.store.Onchain().InsertSwaps(swaps)
}

func (s *Service) fetchFreePoolSeedLogs(ctx context.Context, poolAddress string, from, to int64) ([]rpcLog, error) {
	if shouldUseEtherscanSeeds() && s.freeLogs != nil && s.freeLogs.configured() {
		logs, err := s.freeLogs.getLogs(ctx, poolAddress, []string{swapTopic, swapV3Topic}, from, to)
		if err == nil {
			return logs, nil
		}
		if !shouldUseRPCSeeds() || len(freeRPCURLs()) == 0 {
			return logs, err
		}
	}
	return s.fetchPoolSwapLogs(ctx, poolAddress, from, to)
}

func (s *Service) queueFreeLocalPoolDiscovery(chain, address string, job *store.OnchainIndexJob, task *store.OnchainLocalIndexTask, lastScanned int64, message string) error {
	if lastScanned > 0 {
		job.LastBlock = lastScanned
		task.LastBlock = lastScanned
	}
	job.Status = store.OnchainJobStatusQueued
	job.Phase = PhaseDiscoveringPools
	job.Provider = freeSeedProviderName(s.freeLogs)
	job.RequestBudget = freeLogsBudgetRemaining(s.freeLogs)
	job.ProgressMessage = message
	job.ErrorMessage = ""
	job.UpdatedAtMS = time.Now().UTC().UnixMilli()
	task.Enabled = true
	task.Status = store.OnchainJobStatusQueued
	task.IndexSource = store.OnchainIndexSourceFreeLocal
	task.ProgressMessage = message
	task.LastError = ""
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return err
	}
	if err := s.store.Onchain().UpsertLocalIndexTask(task); err != nil {
		return err
	}
	return s.markTokenIndexStatus(chain, address, store.OnchainJobStatusIndexing, job.UpdatedAtMS)
}

func (s *Service) queueFreeLocalCreatedBlockDiscovery(chain, address string, job *store.OnchainIndexJob, task *store.OnchainLocalIndexTask) error {
	return s.queueFreeLocalPoolDiscovery(chain, address, job, task, job.LastBlock, "Pool discovery found candidate pools, but their created block is still pending; retrying free Etherscan PairCreated discovery before seed scanning. pool_created_block_pending")
}

func (s *Service) queueFreeLocalSeedRetry(chain, address string, job *store.OnchainIndexJob, task *store.OnchainLocalIndexTask, startBlock, endBlock int64, message, errMessage string) error {
	now := time.Now().UTC().UnixMilli()
	job.Status = store.OnchainJobStatusQueued
	job.Phase = PhaseScanningSeeds
	job.Provider = freeSeedProviderName(s.freeLogs)
	job.RequestBudget = freeLogsBudgetRemaining(s.freeLogs)
	job.StartBlock = startBlock
	job.EndBlock = endBlock
	job.ProgressMessage = message
	job.ErrorMessage = errMessage
	job.IndexSource = store.OnchainIndexSourceFreeLocal
	job.UpdatedAtMS = now
	task.Enabled = true
	task.Status = store.OnchainJobStatusQueued
	task.IndexSource = store.OnchainIndexSourceFreeLocal
	task.StartBlock = startBlock
	task.LastBlock = job.LastBlock
	task.ProgressMessage = message
	task.LastError = errMessage
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return err
	}
	if err := s.store.Onchain().UpsertLocalIndexTask(task); err != nil {
		return err
	}
	return s.markTokenIndexStatus(chain, address, store.OnchainJobStatusIndexing, now)
}

func (s *Service) discoverPoolsFromPancakeFactoryLogs(ctx context.Context, chain, tokenAddress string, fromBlock, upperBlock int64, maxRequests int) ([]store.OnchainPool, int64, bool, int, error) {
	chain = normalizeChain(chain)
	tokenAddress = normalizeAddress(tokenAddress)
	if chain != "bsc" {
		return nil, fromBlock, true, 0, nil
	}
	if upperBlock <= 0 {
		var err error
		upperBlock, err = s.rpcBlockNumber(ctx)
		if err != nil {
			return nil, fromBlock, false, 0, err
		}
	}
	if upperBlock < pancakeV2FactoryStartBlock {
		return nil, upperBlock, true, 0, nil
	}
	if fromBlock <= 0 || fromBlock < pancakeV2FactoryStartBlock {
		fromBlock = pancakeV2FactoryStartBlock
	}
	if fromBlock > upperBlock {
		return nil, upperBlock, true, 0, nil
	}
	if maxRequests <= 0 {
		maxRequests = 200
	}

	topicsBySlot := [][]any{
		{pairCreatedTopic, addressTopic(tokenAddress), nil},
		{pairCreatedTopic, nil, addressTopic(tokenAddress)},
	}
	poolsByAddress := map[string]store.OnchainPool{}
	nextUpper := upperBlock
	requests := 0
	cursorTo := upperBlock
	for cursorTo >= fromBlock && requests < maxRequests {
		cursorFrom := cursorTo - maxFactoryLogRangeBlocks + 1
		if cursorFrom < fromBlock {
			cursorFrom = fromBlock
		}
		for _, topics := range topicsBySlot {
			if requests >= maxRequests {
				return sortedOnchainPools(poolsByAddress), nextUpper, false, requests, nil
			}
			requests++
			logs, err := s.fetchFactoryPairCreatedLogs(ctx, cursorFrom, cursorTo, topics)
			if err != nil {
				if errors.Is(err, errRPCPayloadTooLarge) && cursorTo > cursorFrom {
					// Retry this same range in smaller slices. Public RPCs vary:
					// PublicNode allows 50k blocks, 1RPC can require tiny ranges.
					smaller := maxInt64(minIndexLogRangeBlocks, (cursorTo-cursorFrom+1)/10)
					if smaller >= cursorTo-cursorFrom+1 {
						smaller = minIndexLogRangeBlocks
					}
					pools, used, complete, splitErr := s.discoverPoolsFromPancakeFactoryLogsSplit(ctx, chain, tokenAddress, cursorFrom, cursorTo, topics, smaller, maxRequests-requests+1)
					requests += used - 1
					if splitErr != nil {
						return sortedOnchainPools(poolsByAddress), nextUpper, false, requests, splitErr
					}
					for _, pool := range pools {
						poolsByAddress[pool.PoolAddress] = pool
					}
					if !complete {
						return sortedOnchainPools(poolsByAddress), nextUpper, false, requests, nil
					}
					continue
				}
				return sortedOnchainPools(poolsByAddress), nextUpper, false, requests, err
			}
			for _, log := range logs {
				pool, ok := parsePancakeV2PairCreatedLog(chain, tokenAddress, log)
				if ok {
					pool.Token0Decimals = tokenDecimalsForToken(ctx, s, pool.Token0)
					pool.Token1Decimals = tokenDecimalsForToken(ctx, s, pool.Token1)
					poolsByAddress[pool.PoolAddress] = pool
				}
			}
		}
		nextUpper = cursorFrom - 1
		cursorTo = cursorFrom - 1
	}
	complete := cursorTo < fromBlock
	return sortedOnchainPools(poolsByAddress), nextUpper, complete, requests, nil
}

func (s *Service) discoverPoolsFromPancakeFactoryFreeLogs(ctx context.Context, chain, tokenAddress string, fromBlock, upperBlock int64, maxRequests int) ([]store.OnchainPool, int64, bool, int, error) {
	chain = normalizeChain(chain)
	tokenAddress = normalizeAddress(tokenAddress)
	if chain != "bsc" {
		return nil, fromBlock, true, 0, nil
	}
	if s.freeLogs == nil || !s.freeLogs.configured() {
		return nil, fromBlock, false, 0, errFreeLogsNotConfigured
	}
	if upperBlock <= 0 {
		var err error
		upperBlock, err = s.rpcBlockNumber(ctx)
		if err != nil {
			return nil, fromBlock, false, 0, err
		}
	}
	if upperBlock < pancakeV2FactoryStartBlock {
		return nil, upperBlock, true, 0, nil
	}
	if fromBlock <= 0 || fromBlock < pancakeV2FactoryStartBlock {
		fromBlock = pancakeV2FactoryStartBlock
	}
	if fromBlock > upperBlock {
		return nil, upperBlock, true, 0, nil
	}
	if maxRequests <= 0 {
		maxRequests = 200
	}

	topicsBySlot := []map[string]string{
		{
			"topic0":       pairCreatedTopic,
			"topic1":       addressTopic(tokenAddress),
			"topic0_1_opr": "and",
		},
		{
			"topic0":       pairCreatedTopic,
			"topic2":       addressTopic(tokenAddress),
			"topic0_2_opr": "and",
		},
	}
	poolsByAddress := map[string]store.OnchainPool{}
	requests := 0
	for _, topics := range topicsBySlot {
		page := 1
		for {
			if requests >= maxRequests {
				return sortedOnchainPools(poolsByAddress), upperBlock, false, requests, nil
			}
			requests++
			logs, err := s.freeLogs.getLogsPageWithTopics(ctx, pancakeV2FactoryAddress, topics, fromBlock, upperBlock, page)
			if err != nil {
				return sortedOnchainPools(poolsByAddress), upperBlock, false, requests, err
			}
			for _, log := range logs {
				pool, ok := parsePancakeV2PairCreatedLog(chain, tokenAddress, log)
				if ok {
					pool.Token0Decimals = tokenDecimalsForToken(ctx, s, pool.Token0)
					pool.Token1Decimals = tokenDecimalsForToken(ctx, s, pool.Token1)
					poolsByAddress[pool.PoolAddress] = pool
				}
			}
			if len(logs) < s.freeLogs.pageSize {
				break
			}
			page++
		}
	}
	return sortedOnchainPools(poolsByAddress), fromBlock - 1, true, requests, nil
}

func (s *Service) discoverPoolsFromPancakeFactoryCalls(ctx context.Context, chain, tokenAddress string) ([]store.OnchainPool, error) {
	chain = normalizeChain(chain)
	tokenAddress = normalizeAddress(tokenAddress)
	if chain != "bsc" || tokenAddress == "" {
		return nil, nil
	}
	if len(rpcEndpoints(ctx)) == 0 {
		if freeRPCs := freeRPCURLs(); len(freeRPCs) > 0 {
			ctx = context.WithValue(ctx, rpcEndpointContextKey{}, freeRPCs)
		}
	}
	poolsByAddress := map[string]store.OnchainPool{}
	for _, quote := range pancakeV2CommonQuoteTokens {
		quote = normalizeAddress(quote)
		if quote == "" || quote == tokenAddress {
			continue
		}
		pair, err := s.pancakeV2GetPair(ctx, tokenAddress, quote)
		if err != nil {
			return sortedOnchainPools(poolsByAddress), err
		}
		if pair == "" || pair == zeroAddress {
			continue
		}
		token0, _ := s.pairToken(ctx, pair, pairToken0Selector)
		token1, _ := s.pairToken(ctx, pair, pairToken1Selector)
		if token0 == "" || token1 == "" {
			token0 = tokenAddress
			token1 = quote
		}
		if !strings.EqualFold(token0, tokenAddress) && !strings.EqualFold(token1, tokenAddress) {
			continue
		}
		resolvedQuote := token1
		if strings.EqualFold(token1, tokenAddress) {
			resolvedQuote = token0
		}
		poolsByAddress[pair] = store.OnchainPool{
			Chain:          chain,
			TokenAddress:   tokenAddress,
			PoolAddress:    pair,
			DexID:          pancakeV2DexID,
			Name:           "PancakeSwap V2",
			BaseToken:      tokenAddress,
			QuoteToken:     resolvedQuote,
			Token0:         normalizeAddress(token0),
			Token1:         normalizeAddress(token1),
			Token0Decimals: tokenDecimalsForToken(ctx, s, token0),
			Token1Decimals: tokenDecimalsForToken(ctx, s, token1),
			PairURL:        "https://pancakeswap.finance/info/v2/pair/" + pair,
		}
	}
	pools := sortedOnchainPools(poolsByAddress)
	s.enrichPoolsWithPublicMetadata(ctx, chain, tokenAddress, pools)
	return sortedOnchainPoolsByCreatedBlockAndLiquidity(pools), nil
}

func (s *Service) enrichPoolsWithPublicMetadata(ctx context.Context, chain, tokenAddress string, pools []store.OnchainPool) {
	if len(pools) == 0 {
		return
	}
	snapshots, err := s.discoverPools(ctx, chain, tokenAddress)
	if err != nil || len(snapshots) == 0 {
		return
	}
	byAddress := make(map[string]PoolSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		address := normalizeAddress(snapshot.Address)
		if address == "" {
			continue
		}
		byAddress[address] = snapshot
	}
	for i := range pools {
		snapshot, ok := byAddress[normalizeAddress(pools[i].PoolAddress)]
		if !ok {
			continue
		}
		if pools[i].CreatedAtMS <= 0 {
			pools[i].CreatedAtMS = snapshot.CreatedAtMS
		}
		if pools[i].CreatedBlock <= 0 {
			pools[i].CreatedBlock = snapshot.CreatedBlock
		}
		if pools[i].LiquidityUSD <= 0 {
			pools[i].LiquidityUSD = snapshot.LiquidityUSD
		}
		if pools[i].PairURL == "" {
			pools[i].PairURL = snapshot.PairURL
		}
		if pools[i].Name == "" {
			pools[i].Name = snapshot.Name
		}
	}
	_ = s.populateStorePoolCreatedBlocks(ctx, pools)
	_ = s.store.Onchain().UpsertPools(pools)
}

func (s *Service) populateStorePoolCreatedBlocks(ctx context.Context, pools []store.OnchainPool) error {
	var firstErr error
	for i := range pools {
		if pools[i].CreatedBlock > 0 || pools[i].CreatedAtMS <= 0 {
			continue
		}
		block, err := s.blockAtOrAfterTime(ctx, pools[i].CreatedAtMS)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		const startBlockBuffer = int64(5000)
		if block > startBlockBuffer {
			block -= startBlockBuffer
		} else {
			block = 1
		}
		pools[i].CreatedBlock = block
	}
	return firstErr
}

func (s *Service) populatePoolCreatedBlocksFromHistoricalCode(ctx context.Context, pools []store.OnchainPool, endpoints []string) error {
	endpoints = normalizeRPCEndpoints(endpoints)
	if len(endpoints) == 0 {
		return fmt.Errorf("free RPC URL is not configured")
	}
	var firstErr error
	for i := range pools {
		if pools[i].CreatedBlock > 0 || normalizeAddress(pools[i].PoolAddress) == "" {
			continue
		}
		block, err := s.poolCreationBlockFromCode(ctx, pools[i].PoolAddress, endpoints)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		pools[i].CreatedBlock = block
	}
	return firstErr
}

func (s *Service) poolCreationBlockFromCode(ctx context.Context, poolAddress string, endpoints []string) (int64, error) {
	poolAddress = normalizeAddress(poolAddress)
	if poolAddress == "" {
		return 0, fmt.Errorf("pool address is required")
	}
	var firstErr error
	for _, endpoint := range normalizeRPCEndpoints(endpoints) {
		block, err := s.poolCreationBlockFromCodeEndpoint(ctx, poolAddress, endpoint)
		if err == nil && block > 0 {
			return block, nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return 0, firstErr
	}
	return 0, fmt.Errorf("pool creation block not found")
}

func (s *Service) poolCreationBlockFromCodeEndpoint(ctx context.Context, poolAddress, endpoint string) (int64, error) {
	callCtx := context.WithValue(ctx, rpcEndpointContextKey{}, []string{endpoint})
	var latestHex string
	if err := s.rpcCall(callCtx, "eth_blockNumber", []any{}, &latestHex); err != nil {
		return 0, err
	}
	latest := hexToInt64(latestHex)
	if latest <= 0 {
		return 0, fmt.Errorf("latest block is unavailable")
	}
	hasCode, err := s.poolHasCodeAtBlock(callCtx, poolAddress, latest)
	if err != nil {
		return 0, err
	}
	if !hasCode {
		return 0, fmt.Errorf("pool code is not present at latest block")
	}
	lo, hi := int64(1), latest
	for lo < hi {
		mid := lo + (hi-lo)/2
		hasCode, err := s.poolHasCodeAtBlock(callCtx, poolAddress, mid)
		if err != nil {
			return 0, err
		}
		if hasCode {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, nil
}

func (s *Service) poolHasCodeAtBlock(ctx context.Context, poolAddress string, block int64) (bool, error) {
	var code string
	if err := s.rpcCall(ctx, "eth_getCode", []any{poolAddress, hexBlock(block)}, &code); err != nil {
		return false, err
	}
	code = strings.TrimSpace(code)
	return code != "" && code != "0x", nil
}

func sortedOnchainPoolsByCreatedBlockAndLiquidity(pools []store.OnchainPool) []store.OnchainPool {
	sort.SliceStable(pools, func(i, j int) bool {
		leftCreated := pools[i].CreatedBlock > 0
		rightCreated := pools[j].CreatedBlock > 0
		if leftCreated != rightCreated {
			return leftCreated
		}
		if pools[i].CreatedBlock != pools[j].CreatedBlock {
			return pools[i].CreatedBlock < pools[j].CreatedBlock
		}
		if pools[i].LiquidityUSD != pools[j].LiquidityUSD {
			return pools[i].LiquidityUSD > pools[j].LiquidityUSD
		}
		return pools[i].PoolAddress < pools[j].PoolAddress
	})
	return pools
}

func (s *Service) discoverPoolsFromPancakeFactoryLogsSplit(ctx context.Context, chain, tokenAddress string, from, to int64, topics []any, step int64, maxRequests int) ([]store.OnchainPool, int, bool, error) {
	if step < minIndexLogRangeBlocks {
		step = minIndexLogRangeBlocks
	}
	poolsByAddress := map[string]store.OnchainPool{}
	requests := 0
	for cursor := from; cursor <= to; {
		if requests >= maxRequests {
			return sortedOnchainPools(poolsByAddress), requests, false, nil
		}
		next := cursor + step - 1
		if next > to {
			next = to
		}
		requests++
		logs, err := s.fetchFactoryPairCreatedLogs(ctx, cursor, next, topics)
		if err != nil {
			if errors.Is(err, errRPCPayloadTooLarge) && step > minIndexLogRangeBlocks {
				step = maxInt64(minIndexLogRangeBlocks, step/10)
				continue
			}
			return sortedOnchainPools(poolsByAddress), requests, false, err
		}
		for _, log := range logs {
			pool, ok := parsePancakeV2PairCreatedLog(chain, tokenAddress, log)
			if ok {
				pool.Token0Decimals = tokenDecimalsForToken(ctx, s, pool.Token0)
				pool.Token1Decimals = tokenDecimalsForToken(ctx, s, pool.Token1)
				poolsByAddress[pool.PoolAddress] = pool
			}
		}
		cursor = next + 1
	}
	return sortedOnchainPools(poolsByAddress), requests, true, nil
}

func (s *Service) fetchFactoryPairCreatedLogs(ctx context.Context, from, to int64, topics []any) ([]rpcLog, error) {
	var logs []rpcLog
	filter := map[string]any{
		"fromBlock": hexBlock(from),
		"toBlock":   hexBlock(to),
		"address":   pancakeV2FactoryAddress,
		"topics":    topics,
	}
	if err := s.rpcCall(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func parsePancakeV2PairCreatedLog(chain, tokenAddress string, log rpcLog) (store.OnchainPool, bool) {
	if len(log.Topics) < 3 || !strings.EqualFold(log.Topics[0], pairCreatedTopic) {
		return store.OnchainPool{}, false
	}
	token0 := topicToAddress(log.Topics[1])
	token1 := topicToAddress(log.Topics[2])
	tokenAddress = normalizeAddress(tokenAddress)
	if token0 != tokenAddress && token1 != tokenAddress {
		return store.OnchainPool{}, false
	}
	values := splitDataWords(log.Data)
	if len(values) == 0 {
		return store.OnchainPool{}, false
	}
	pair := wordToAddress(values[0])
	if pair == "" {
		return store.OnchainPool{}, false
	}
	quote := token1
	if token1 == tokenAddress {
		quote = token0
	}
	return store.OnchainPool{
		Chain:        chain,
		TokenAddress: tokenAddress,
		PoolAddress:  pair,
		DexID:        pancakeV2DexID,
		Name:         "PancakeSwap V2",
		BaseToken:    tokenAddress,
		QuoteToken:   quote,
		Token0:       token0,
		Token1:       token1,
		CreatedBlock: hexToInt64(log.BlockNumber),
		PairURL:      "https://pancakeswap.finance/info/v2/pair/" + pair,
	}, true
}

func sortedOnchainPools(items map[string]store.OnchainPool) []store.OnchainPool {
	out := make([]store.OnchainPool, 0, len(items))
	for _, pool := range items {
		out = append(out, pool)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedBlock != out[j].CreatedBlock {
			return out[i].CreatedBlock < out[j].CreatedBlock
		}
		return out[i].PoolAddress < out[j].PoolAddress
	})
	return out
}

func (s *Service) estimateFreeLocalStartBlock(ctx context.Context, pools []store.OnchainPool, latest int64) int64 {
	startBlock := int64(0)
	for i := range pools {
		if pools[i].CreatedBlock > 0 {
			if startBlock == 0 || pools[i].CreatedBlock < startBlock {
				startBlock = pools[i].CreatedBlock
			}
			continue
		}
		if pools[i].CreatedAtMS <= 0 {
			continue
		}
		block, err := s.blockAtOrAfterTime(ctx, pools[i].CreatedAtMS)
		if err != nil {
			continue
		}
		const startBlockBuffer = int64(5000)
		if block > startBlockBuffer {
			block -= startBlockBuffer
		} else {
			block = 1
		}
		pools[i].CreatedBlock = block
		if startBlock == 0 || block < startBlock {
			startBlock = block
		}
	}
	if startBlock > 0 {
		_ = s.store.Onchain().UpsertPools(pools)
		return startBlock
	}
	if latest > defaultEarlyWalletIndexWindowBlocks {
		return latest - defaultEarlyWalletIndexWindowBlocks + 1
	}
	return 1
}

func (s *Service) completeFreeLocalJob(chain, address string, job *store.OnchainIndexJob, task *store.OnchainLocalIndexTask, message, completeness string) error {
	now := time.Now().UTC().UnixMilli()
	job.Status = store.OnchainJobStatusCompleted
	job.Phase = PhaseCompleted
	job.Provider = freeSeedProviderName(s.freeLogs)
	job.RequestBudget = freeLogsBudgetRemaining(s.freeLogs)
	job.ProgressMessage = message
	if completeness == "full" {
		job.ErrorMessage = ""
	}
	job.CompletedAtMS = now
	job.UpdatedAtMS = now
	job.IndexSource = store.OnchainIndexSourceFreeLocal
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return err
	}
	task.Enabled = true
	task.Status = store.OnchainJobStatusCompleted
	task.IndexSource = store.OnchainIndexSourceFreeLocal
	task.LastBlock = job.LastBlock
	if completeness == "full" {
		task.LastError = ""
	}
	task.ProgressMessage = message
	if err := s.store.Onchain().UpsertLocalIndexTask(task); err != nil {
		return err
	}
	token, _ := s.store.Onchain().GetToken(chain, address)
	if token == nil {
		token = &store.OnchainToken{Chain: chain, Address: address}
	}
	token.IndexStatus = store.OnchainJobStatusCompleted
	token.LastIndexedBlock = job.LastBlock
	token.LastIndexedAt = now
	token.AnalysisCompleteness = completeness
	return s.store.Onchain().UpsertToken(token)
}

func (s *Service) completeFreeLocalJobPartial(chain, address string, job *store.OnchainIndexJob, task *store.OnchainLocalIndexTask, message, errMessage string) error {
	job.ErrorMessage = errMessage
	task.LastError = errMessage
	return s.completeFreeLocalJob(chain, address, job, task, message, "partial")
}

func (s *Service) ensureTokenMetadata(ctx context.Context, chain, address string) error {
	token, _ := s.store.Onchain().GetToken(chain, address)
	if token != nil && token.Symbol != "" {
		return nil
	}
	name, _ := s.erc20String(ctx, address, erc20NameSelector)
	symbol, _ := s.erc20String(ctx, address, erc20SymbolSelector)
	decimals, _ := s.erc20Uint(ctx, address, erc20DecimalsSelector)
	totalSupply, _ := s.erc20Uint(ctx, address, erc20TotalSupplySelector)
	return s.store.Onchain().UpsertToken(&store.OnchainToken{
		Chain:                chain,
		Address:              address,
		Name:                 name,
		Symbol:               symbol,
		Decimals:             int(decimals.Int64()),
		TotalSupply:          totalSupply.String(),
		IndexStatus:          store.OnchainJobStatusIndexing,
		AnalysisCompleteness: "partial",
	})
}

func (s *Service) ensurePoolsForIndex(ctx context.Context, chain, address string) ([]store.OnchainPool, error) {
	existing, err := s.store.Onchain().ListPools(chain, address)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		pools, err := s.discoverPools(ctx, chain, address)
		if err != nil {
			return nil, err
		}
		if err := s.store.Onchain().UpsertPools(poolsToStore(pools, chain, address)); err != nil {
			return nil, err
		}
		existing, err = s.store.Onchain().ListPools(chain, address)
		if err != nil {
			return nil, err
		}
	}
	for i := range existing {
		if existing[i].Token0 == "" || existing[i].Token1 == "" {
			token0, _ := s.pairToken(ctx, existing[i].PoolAddress, pairToken0Selector)
			token1, _ := s.pairToken(ctx, existing[i].PoolAddress, pairToken1Selector)
			if token0 == "" || token1 == "" {
				continue
			}
			existing[i].Token0 = token0
			existing[i].Token1 = token1
			if strings.EqualFold(token0, address) {
				existing[i].Token0Decimals = tokenDecimalsForToken(ctx, s, token0)
				existing[i].Token1Decimals = tokenDecimalsForToken(ctx, s, token1)
			} else {
				existing[i].Token0Decimals = tokenDecimalsForToken(ctx, s, token0)
				existing[i].Token1Decimals = tokenDecimalsForToken(ctx, s, token1)
			}
		}
	}
	if err := s.store.Onchain().UpsertPools(existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func tokenDecimalsForToken(ctx context.Context, s *Service, token string) int {
	if token == "" {
		return 18
	}
	n, err := s.erc20Uint(ctx, token, erc20DecimalsSelector)
	if err != nil {
		return 18
	}
	return int(n.Int64())
}

func (s *Service) indexBlockRange(ctx context.Context, chain, tokenAddress string, pools []store.OnchainPool, from, to int64) error {
	transfers, err := s.fetchTransferLogs(ctx, tokenAddress, from, to)
	if err != nil {
		if errors.Is(err, errRPCPayloadTooLarge) && from < to {
			return s.indexSplitBlockRange(ctx, chain, tokenAddress, pools, from, to)
		}
		return err
	}
	tokenDecimals := 18
	if token, _ := s.store.Onchain().GetToken(chain, tokenAddress); token != nil && token.Decimals > 0 {
		tokenDecimals = token.Decimals
	}
	parsedTransfers := make([]store.OnchainTokenTransfer, 0, len(transfers))
	blockTimes := map[int64]int64{}
	for _, log := range transfers {
		blockNumber := hexToInt64(log.BlockNumber)
		blockTime := blockTimes[blockNumber]
		if blockTime == 0 {
			blockTime, _ = s.blockTime(ctx, blockNumber)
			blockTimes[blockNumber] = blockTime
		}
		tr, ok := parseTransferLog(chain, tokenAddress, log, blockTime, tokenDecimals)
		if ok {
			parsedTransfers = append(parsedTransfers, tr)
		}
	}
	if err := s.store.Onchain().InsertTransfers(parsedTransfers); err != nil {
		return err
	}

	swaps := make([]store.OnchainSwap, 0)
	for _, pool := range pools {
		logs, err := s.fetchPoolLogs(ctx, pool.PoolAddress, from, to)
		if err != nil {
			if errors.Is(err, errRPCPayloadTooLarge) && from < to {
				return s.indexSplitBlockRange(ctx, chain, tokenAddress, pools, from, to)
			}
			return err
		}
		for _, log := range logs {
			blockNumber := hexToInt64(log.BlockNumber)
			blockTime := blockTimes[blockNumber]
			if blockTime == 0 {
				blockTime, _ = s.blockTime(ctx, blockNumber)
				blockTimes[blockNumber] = blockTime
			}
			swap, ok := parsePoolEvent(chain, tokenAddress, pool, log, blockTime)
			if ok {
				swaps = append(swaps, swap)
			}
		}
	}
	return s.store.Onchain().InsertSwaps(swaps)
}

func (s *Service) indexEarlySeedBlockRange(ctx context.Context, chain, tokenAddress string, pools []store.OnchainPool, from, to int64) error {
	tokenDecimals := 18
	if token, _ := s.store.Onchain().GetToken(chain, tokenAddress); token != nil && token.Decimals > 0 {
		tokenDecimals = token.Decimals
	}
	swaps := make([]store.OnchainSwap, 0)
	for _, pool := range pools {
		if pool.Token0 == "" || pool.Token1 == "" {
			continue
		}
		logs, err := s.fetchPoolLogs(ctx, pool.PoolAddress, from, to)
		if err != nil {
			if errors.Is(err, errRPCPayloadTooLarge) && to-from+1 > minIndexLogRangeBlocks {
				return s.indexSplitEarlySeedBlockRange(ctx, chain, tokenAddress, pools, from, to)
			}
			return err
		}
		for _, log := range logs {
			normalizedPool := pool
			if normalizedPool.Token0Decimals <= 0 {
				normalizedPool.Token0Decimals = tokenDecimals
			}
			if normalizedPool.Token1Decimals <= 0 {
				normalizedPool.Token1Decimals = 18
			}
			swap, ok := parsePoolEvent(chain, tokenAddress, normalizedPool, log, 0)
			if ok {
				swaps = append(swaps, swap)
			}
		}
	}
	return s.store.Onchain().InsertSwaps(swaps)
}

func (s *Service) indexSplitEarlySeedBlockRange(ctx context.Context, chain, tokenAddress string, pools []store.OnchainPool, from, to int64) error {
	mid := from + (to-from)/2
	if err := s.indexEarlySeedBlockRange(ctx, chain, tokenAddress, pools, from, mid); err != nil {
		return err
	}
	return s.indexEarlySeedBlockRange(ctx, chain, tokenAddress, pools, mid+1, to)
}

func (s *Service) indexSplitBlockRange(ctx context.Context, chain, tokenAddress string, pools []store.OnchainPool, from, to int64) error {
	mid := from + (to-from)/2
	if err := s.indexBlockRange(ctx, chain, tokenAddress, pools, from, mid); err != nil {
		return err
	}
	return s.indexBlockRange(ctx, chain, tokenAddress, pools, mid+1, to)
}

func (s *Service) fetchTransferLogs(ctx context.Context, tokenAddress string, from, to int64) ([]rpcLog, error) {
	return s.fetchTransferLogsWithTopics(ctx, tokenAddress, []any{transferTopic}, from, to)
}

func (s *Service) fetchTransferLogsWithTopics(ctx context.Context, tokenAddress string, topics []any, from, to int64) ([]rpcLog, error) {
	var logs []rpcLog
	filter := map[string]any{
		"fromBlock": hexBlock(from),
		"toBlock":   hexBlock(to),
		"address":   tokenAddress,
		"topics":    topics,
	}
	if err := s.rpcCall(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *Service) fetchPoolLogs(ctx context.Context, poolAddress string, from, to int64) ([]rpcLog, error) {
	var logs []rpcLog
	filter := map[string]any{
		"fromBlock": hexBlock(from),
		"toBlock":   hexBlock(to),
		"address":   poolAddress,
		"topics":    []any{[]string{swapTopic, swapV3Topic, mintTopic, burnTopic}},
	}
	if err := s.rpcCall(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *Service) fetchPoolSwapLogs(ctx context.Context, poolAddress string, from, to int64) ([]rpcLog, error) {
	var logs []rpcLog
	filter := map[string]any{
		"fromBlock": hexBlock(from),
		"toBlock":   hexBlock(to),
		"address":   poolAddress,
		"topics":    []any{[]string{swapTopic, swapV3Topic}},
	}
	if err := s.rpcCall(ctx, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func parseTransferLog(chain, tokenAddress string, log rpcLog, blockTime int64, decimals int) (store.OnchainTokenTransfer, bool) {
	if len(log.Topics) < 3 {
		return store.OnchainTokenTransfer{}, false
	}
	amountRaw := hexToBigInt(log.Data)
	amount := rawToFloat(amountRaw, decimals)
	return store.OnchainTokenTransfer{
		Chain:        chain,
		TokenAddress: tokenAddress,
		TxHash:       normalizeAddress(log.TransactionHash),
		LogIndex:     hexToInt64(log.LogIndex),
		BlockNumber:  hexToInt64(log.BlockNumber),
		BlockTime:    blockTime,
		FromAddress:  topicToAddress(log.Topics[1]),
		ToAddress:    topicToAddress(log.Topics[2]),
		AmountRaw:    amountRaw.String(),
		Amount:       amount,
	}, true
}

func parsePoolEvent(chain, tokenAddress string, pool store.OnchainPool, log rpcLog, blockTime int64) (store.OnchainSwap, bool) {
	if len(log.Topics) == 0 {
		return store.OnchainSwap{}, false
	}
	topic := strings.ToLower(log.Topics[0])
	if topic != swapTopic && topic != swapV3Topic && topic != mintTopic && topic != burnTopic {
		return store.OnchainSwap{}, false
	}
	values := splitDataWords(log.Data)
	if topic == swapTopic && len(values) < 4 {
		return store.OnchainSwap{}, false
	}
	if topic == swapV3Topic && len(values) < 2 {
		return store.OnchainSwap{}, false
	}
	if topic != swapTopic && len(values) < 2 {
		return store.OnchainSwap{}, false
	}
	token0IsTarget := strings.EqualFold(pool.Token0, tokenAddress)
	tokenDecimals := pool.Token1Decimals
	quoteDecimals := pool.Token0Decimals
	quoteToken := pool.Token0
	if token0IsTarget {
		tokenDecimals = pool.Token0Decimals
		quoteDecimals = pool.Token1Decimals
		quoteToken = pool.Token1
	}

	side := ""
	trader := ""
	tokenAmountRaw := big.NewInt(0)
	quoteAmountRaw := big.NewInt(0)
	if topic == swapTopic {
		if len(log.Topics) >= 3 {
			trader = topicToAddress(log.Topics[2])
		}
		amount0In := hexWordToBig(values[0])
		amount1In := hexWordToBig(values[1])
		amount0Out := hexWordToBig(values[2])
		amount1Out := hexWordToBig(values[3])
		if token0IsTarget {
			if amount0Out.Sign() > 0 {
				side = "buy"
				tokenAmountRaw = amount0Out
				quoteAmountRaw = amount1In
			} else if amount0In.Sign() > 0 {
				side = "sell"
				tokenAmountRaw = amount0In
				quoteAmountRaw = amount1Out
			}
		} else {
			if amount1Out.Sign() > 0 {
				side = "buy"
				tokenAmountRaw = amount1Out
				quoteAmountRaw = amount0In
			} else if amount1In.Sign() > 0 {
				side = "sell"
				tokenAmountRaw = amount1In
				quoteAmountRaw = amount0Out
			}
		}
	} else if topic == swapV3Topic {
		if len(log.Topics) >= 3 {
			trader = topicToAddress(log.Topics[2])
		}
		amount0 := hexWordToSignedBig(values[0])
		amount1 := hexWordToSignedBig(values[1])
		if token0IsTarget {
			if amount0.Sign() < 0 {
				side = "buy"
				tokenAmountRaw = absBig(amount0)
				quoteAmountRaw = absBig(amount1)
			} else if amount0.Sign() > 0 {
				side = "sell"
				tokenAmountRaw = absBig(amount0)
				quoteAmountRaw = absBig(amount1)
			}
		} else {
			if amount1.Sign() < 0 {
				side = "buy"
				tokenAmountRaw = absBig(amount1)
				quoteAmountRaw = absBig(amount0)
			} else if amount1.Sign() > 0 {
				side = "sell"
				tokenAmountRaw = absBig(amount1)
				quoteAmountRaw = absBig(amount0)
			}
		}
	} else {
		side = "lp_event"
		if len(log.Topics) >= 2 {
			trader = topicToAddress(log.Topics[1])
		}
		if topic == burnTopic && len(log.Topics) >= 3 {
			trader = topicToAddress(log.Topics[2])
		}
		if token0IsTarget {
			tokenAmountRaw = hexWordToBig(values[0])
			quoteAmountRaw = hexWordToBig(values[1])
		} else {
			tokenAmountRaw = hexWordToBig(values[1])
			quoteAmountRaw = hexWordToBig(values[0])
		}
	}
	if tokenAmountRaw.Sign() == 0 {
		return store.OnchainSwap{}, false
	}
	return store.OnchainSwap{
		Chain:         chain,
		TokenAddress:  tokenAddress,
		PoolAddress:   pool.PoolAddress,
		TxHash:        normalizeAddress(log.TransactionHash),
		LogIndex:      hexToInt64(log.LogIndex),
		BlockNumber:   hexToInt64(log.BlockNumber),
		BlockTime:     blockTime,
		EventType:     eventTypeForTopic(topic),
		TraderAddress: trader,
		Side:          side,
		TokenAmount:   rawToFloat(tokenAmountRaw, tokenDecimals),
		QuoteAmount:   rawToFloat(quoteAmountRaw, quoteDecimals),
		QuoteToken:    quoteToken,
		EstimatedUSD:  0,
	}, true
}

func eventTypeForTopic(topic string) string {
	switch topic {
	case mintTopic:
		return "mint"
	case burnTopic:
		return "burn"
	default:
		return "swap"
	}
}

func (s *Service) rpcBlockNumber(ctx context.Context) (int64, error) {
	var result string
	if err := s.rpcCall(ctx, "eth_blockNumber", []any{}, &result); err != nil {
		return 0, err
	}
	return hexToInt64(result), nil
}

func (s *Service) blockTime(ctx context.Context, block int64) (int64, error) {
	var result struct {
		Timestamp string `json:"timestamp"`
	}
	if err := s.rpcCall(ctx, "eth_getBlockByNumber", []any{hexBlock(block), false}, &result); err != nil {
		return 0, err
	}
	return hexToInt64(result.Timestamp) * 1000, nil
}

func (s *Service) populatePoolCreatedBlocks(ctx context.Context, pools []PoolSnapshot) error {
	var firstErr error
	for i := range pools {
		if pools[i].CreatedBlock > 0 || pools[i].CreatedAtMS <= 0 {
			continue
		}
		block, err := s.blockAtOrAfterTime(ctx, pools[i].CreatedAtMS)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		const startBlockBuffer = int64(5000)
		if block > startBlockBuffer {
			block -= startBlockBuffer
		} else {
			block = 1
		}
		pools[i].CreatedBlock = block
	}
	return firstErr
}

func (s *Service) blockAtOrAfterTime(ctx context.Context, timestampMS int64) (int64, error) {
	targetSeconds := timestampMS / 1000
	latest, err := s.rpcBlockNumber(ctx)
	if err != nil {
		return 0, err
	}
	lo, hi := int64(1), latest
	for lo < hi {
		mid := lo + (hi-lo)/2
		blockTimeMS, err := s.blockTime(ctx, mid)
		if err != nil {
			return 0, err
		}
		if blockTimeMS/1000 < targetSeconds {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo, nil
}

func (s *Service) erc20String(ctx context.Context, address, selector string) (string, error) {
	var result string
	if err := s.rpcCall(ctx, "eth_call", []any{map[string]any{"to": address, "data": selector}, "latest"}, &result); err != nil {
		return "", err
	}
	return decodeABIString(result), nil
}

func (s *Service) erc20Uint(ctx context.Context, address, selector string) (*big.Int, error) {
	var result string
	if err := s.rpcCall(ctx, "eth_call", []any{map[string]any{"to": address, "data": selector}, "latest"}, &result); err != nil {
		return big.NewInt(0), err
	}
	return hexToBigInt(result), nil
}

func (s *Service) pairToken(ctx context.Context, pair, selector string) (string, error) {
	var result string
	if err := s.rpcCall(ctx, "eth_call", []any{map[string]any{"to": pair, "data": selector}, "latest"}, &result); err != nil {
		return "", err
	}
	cleaned := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(result)), "0x")
	if len(cleaned) < 40 {
		return "", fmt.Errorf("invalid pair token result")
	}
	return "0x" + cleaned[len(cleaned)-40:], nil
}

func (s *Service) pancakeV2GetPair(ctx context.Context, tokenA, tokenB string) (string, error) {
	data := pancakeV2GetPairSelector + abiAddressWord(tokenA) + abiAddressWord(tokenB)
	var result string
	if err := s.rpcCall(ctx, "eth_call", []any{map[string]any{"to": pancakeV2FactoryAddress, "data": data}, "latest"}, &result); err != nil {
		return "", err
	}
	pair := wordToAddress(result)
	if pair == zeroAddress {
		return "", nil
	}
	return pair, nil
}

func (s *Service) rpcCall(ctx context.Context, method string, params []any, out any) error {
	var lastErr error
	endpoints := rpcEndpoints(ctx)
	if len(endpoints) == 0 {
		return fmt.Errorf("RPC URL is not configured")
	}
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*attempt) * 2 * time.Second
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		var payloadErr error
		var archiveErr error
		var rateErr error
		var transportErr error
		for _, endpoint := range endpoints {
			err := s.rpcCallOnce(ctx, endpoint, method, params, out)
			if err == nil {
				return nil
			}
			lastErr = err
			if !isRetryableRPCError(err) {
				return err
			}
			switch {
			case errors.Is(err, errRPCPayloadTooLarge):
				if payloadErr == nil {
					payloadErr = err
				}
			case errors.Is(err, errRPCArchiveUnsupported):
				if archiveErr == nil {
					archiveErr = err
				}
			case errors.Is(err, errRPCRateLimited):
				if rateErr == nil {
					rateErr = err
				}
			default:
				if transportErr == nil {
					transportErr = err
				}
			}
		}

		switch {
		case payloadErr != nil:
			return payloadErr
		case archiveErr != nil:
			return archiveErr
		case rateErr != nil:
			lastErr = rateErr
		case transportErr != nil:
			lastErr = fmt.Errorf("%w: %v", errRPCTransportRetryable, transportErr)
		}
	}
	return lastErr
}

func (s *Service) rpcCallOnce(ctx context.Context, endpoint, method string, params []any, out any) error {
	if endpoint == "" {
		return fmt.Errorf("RPC URL is not configured")
	}
	payload, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := s.rpcHTTPClient
	if client == nil {
		client = s.httpClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if classified := classifyRPCError(resp.StatusCode, string(body)); classified != nil {
			return classified
		}
		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			return errRPCPayloadTooLarge
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			return errRPCRateLimited
		}
		return fmt.Errorf("rpc status %d", resp.StatusCode)
	}
	var decoded rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return err
	}
	if decoded.Error != nil {
		if classified := classifyRPCError(decoded.Error.Code, decoded.Error.Message); classified != nil {
			return classified
		}
		return fmt.Errorf("rpc error %d: %s", decoded.Error.Code, decoded.Error.Message)
	}
	return json.Unmarshal(decoded.Result, out)
}

func rpcEndpoints(ctx context.Context) []string {
	if endpoints, ok := ctx.Value(rpcEndpointContextKey{}).([]string); ok && len(endpoints) > 0 {
		return normalizeRPCEndpoints(endpoints)
	}
	if endpoint := strings.TrimSpace(config.Get().OnchainBSCArchiveRPCURL); endpoint != "" {
		return []string{endpoint}
	}
	return nil
}

func normalizeRPCEndpoints(endpoints []string) []string {
	out := make([]string, 0, len(endpoints))
	seen := map[string]bool{}
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" || seen[endpoint] {
			continue
		}
		seen[endpoint] = true
		out = append(out, endpoint)
	}
	return out
}

func isRetryableRPCError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, errRPCPayloadTooLarge) ||
		errors.Is(err, errRPCArchiveUnsupported) ||
		errors.Is(err, errRPCRateLimited) ||
		errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection refused") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "deadline") ||
		strings.Contains(message, "eof") ||
		strings.Contains(message, "tls") ||
		strings.Contains(message, "timeout") ||
		strings.Contains(message, "temporary")
}

func freeRPCURLs() []string {
	cfg := config.Get()
	urls := make([]string, 0, len(cfg.OnchainFreeRPCURLs)+1)
	seen := map[string]bool{}
	for _, endpoint := range cfg.OnchainFreeRPCURLs {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" || seen[endpoint] {
			continue
		}
		seen[endpoint] = true
		urls = append(urls, endpoint)
	}
	if endpoint := strings.TrimSpace(cfg.OnchainBSCArchiveRPCURL); endpoint != "" && !seen[endpoint] {
		urls = append(urls, endpoint)
	}
	return urls
}

func classifyRPCError(code int, message string) error {
	message = strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(message, "archive") &&
		(strings.Contains(message, "not available") ||
			strings.Contains(message, "upgrade") ||
			strings.Contains(message, "current plan") ||
			strings.Contains(message, "not allow") ||
			strings.Contains(message, "not support")) {
		return errRPCArchiveUnsupported
	}
	if strings.Contains(message, "missing trie node") ||
		strings.Contains(message, "historical state") ||
		strings.Contains(message, "header not found") {
		return errRPCArchiveUnsupported
	}
	if strings.Contains(message, "method") &&
		strings.Contains(message, "eth_getlogs") &&
		(strings.Contains(message, "not support") ||
			strings.Contains(message, "unsupported") ||
			strings.Contains(message, "not available") ||
			strings.Contains(message, "not found")) {
		return errRPCArchiveUnsupported
	}
	if code == -32615 ||
		code == -32701 ||
		strings.Contains(message, "limit exceeded") ||
		strings.Contains(message, "range") ||
		strings.Contains(message, "too many blocks") ||
		strings.Contains(message, "maximum block") {
		return errRPCPayloadTooLarge
	}
	if code == -32005 ||
		code == http.StatusTooManyRequests ||
		strings.Contains(message, "rate") ||
		strings.Contains(message, "too many") ||
		strings.Contains(message, "daily request limit") ||
		strings.Contains(message, "usage limit") ||
		strings.Contains(message, "higher limits") ||
		strings.Contains(message, "uninterrupted access") ||
		strings.Contains(message, "pricing") {
		return errRPCRateLimited
	}
	return nil
}

func hexBlock(block int64) string {
	return "0x" + strconv.FormatInt(block, 16)
}

func hexToInt64(value string) int64 {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if value == "" {
		return 0
	}
	n, _ := strconv.ParseInt(value, 16, 64)
	return n
}

func hexToBigInt(value string) *big.Int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if value == "" {
		return big.NewInt(0)
	}
	n := new(big.Int)
	n.SetString(value, 16)
	return n
}

func hexWordToBig(value string) *big.Int {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if value == "" {
		return big.NewInt(0)
	}
	n := new(big.Int)
	n.SetString(value, 16)
	return n
}

func hexWordToSignedBig(value string) *big.Int {
	n := hexWordToBig(value)
	if n.BitLen() < 255 {
		return n
	}
	two256 := new(big.Int).Lsh(big.NewInt(1), 256)
	return new(big.Int).Sub(n, two256)
}

func absBig(n *big.Int) *big.Int {
	if n == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Abs(n)
}

func topicToAddress(topic string) string {
	topic = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(topic)), "0x")
	if len(topic) < 40 {
		return ""
	}
	return "0x" + topic[len(topic)-40:]
}

func wordToAddress(word string) string {
	word = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(word)), "0x")
	if len(word) < 40 {
		return ""
	}
	return "0x" + word[len(word)-40:]
}

func abiAddressWord(address string) string {
	address = strings.TrimPrefix(normalizeAddress(address), "0x")
	if len(address) > 40 {
		address = address[len(address)-40:]
	}
	return strings.Repeat("0", 64-len(address)) + address
}

func splitDataWords(data string) []string {
	data = strings.TrimPrefix(strings.TrimSpace(data), "0x")
	if data == "" {
		return nil
	}
	var out []string
	for len(data) >= 64 {
		out = append(out, data[:64])
		data = data[64:]
	}
	return out
}

func rawToFloat(n *big.Int, decimals int) float64 {
	if n == nil {
		return 0
	}
	if decimals < 0 || decimals > 77 {
		decimals = 18
	}
	f, _ := new(big.Float).SetInt(n).Float64()
	return f / math.Pow10(decimals)
}

func decodeABIString(hexValue string) string {
	hexValue = strings.TrimPrefix(strings.TrimSpace(hexValue), "0x")
	if hexValue == "" {
		return ""
	}
	raw, err := hex.DecodeString(hexValue)
	if err != nil {
		return ""
	}
	if len(raw) >= 64 {
		offset := new(big.Int).SetBytes(raw[:32]).Int64()
		if offset >= 0 && int(offset)+32 <= len(raw) {
			length := new(big.Int).SetBytes(raw[offset : offset+32]).Int64()
			start := int(offset) + 32
			end := start + int(length)
			if length >= 0 && end <= len(raw) {
				return strings.TrimRight(string(raw[start:end]), "\x00")
			}
		}
	}
	return strings.TrimRight(string(raw), "\x00")
}

func eventTopic(signature string) string {
	h := sha3.NewLegacyKeccak256()
	_, _ = h.Write([]byte(signature))
	return "0x" + hex.EncodeToString(h.Sum(nil))
}
