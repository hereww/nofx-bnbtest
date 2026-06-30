package onchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nofx/config"
	"nofx/logger"
	"nofx/netclient"
	"nofx/safe"
	"nofx/store"
)

const (
	StatusOK                    = "ok"
	StatusPartialData           = "partial_data"
	StatusIndexing              = "indexing"
	StatusIndexerUnavailable    = "indexer_unavailable"
	StatusArchiveRPCMissing     = "archive_rpc_missing"
	StatusArchiveRPCUnsupported = "archive_rpc_unsupported"
	StatusIndexRequired         = "index_required"
	StatusInvalidRequest        = "invalid_request"

	DepthRecent = "recent"
	DepthFull   = "full"

	bscChainID = "56"

	defaultEarlyWalletIndexWindowBlocks = int64(100000)

	PhaseDiscoveringPools = "discovering_pools"
	PhaseScanningSeeds    = "scanning_seed_window"
	PhaseEnrichingWallets = "enriching_wallets"
	PhaseCompleted        = "completed"
)

var evmAddressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

type Service struct {
	store         *store.Store
	httpClient    *http.Client
	rpcHTTPClient *http.Client
	freeLogs      *freeLogsClient
	mu            sync.Mutex
	running       map[string]bool
}

func NewService(st *store.Store) *Service {
	marketProxy := strings.TrimSpace(config.Get().MarketHTTPProxy)
	var httpClient *http.Client
	var err error
	if marketProxy == "" {
		httpClient = netclient.NewDirectHTTPClient(20 * time.Second)
	} else {
		httpClient, err = netclient.NewProxyAwareHTTPClient(20*time.Second, marketProxy)
		if err != nil {
			logger.Warnf("invalid onchain HTTP proxy ignored: %v", err)
			httpClient = netclient.NewDirectHTTPClient(20 * time.Second)
		}
	}
	var rpcHTTPClient *http.Client
	if strings.TrimSpace(config.Get().OnchainArchiveHTTPProxy) == "" {
		rpcHTTPClient = netclient.NewDirectHTTPClient(60 * time.Second)
	} else {
		rpcHTTPClient, err = netclient.NewProxyAwareHTTPClient(60*time.Second, config.Get().OnchainArchiveHTTPProxy)
		if err != nil {
			logger.Warnf("invalid onchain archive HTTP proxy ignored: %v", err)
			rpcHTTPClient = netclient.NewDirectHTTPClient(60 * time.Second)
		}
	}
	svc := &Service{
		store:         st,
		httpClient:    httpClient,
		rpcHTTPClient: rpcHTTPClient,
		running:       make(map[string]bool),
	}
	svc.freeLogs = svc.newFreeLogsClient()
	return svc
}

func (s *Service) AnalyzeToken(ctx context.Context, req TokenAnalysisRequest) (*TokenAnalysisResponse, error) {
	chain := normalizeChain(req.Chain)
	address := normalizeAddress(req.Address)
	depth := strings.ToLower(strings.TrimSpace(req.Depth))
	if depth == "" {
		depth = DepthRecent
	}
	if depth != DepthRecent && depth != DepthFull {
		return &TokenAnalysisResponse{Success: false, Chain: chain, Address: address, Depth: depth, Status: StatusInvalidRequest, Error: "depth must be recent or full"}, nil
	}
	if err := validateRequest(chain, address); err != nil {
		return &TokenAnalysisResponse{Success: false, Chain: chain, Address: address, Depth: depth, Status: StatusInvalidRequest, Error: err.Error()}, nil
	}

	if depth == DepthFull && strings.TrimSpace(config.Get().OnchainBSCArchiveRPCURL) == "" {
		return &TokenAnalysisResponse{
			Success:      true,
			Chain:        chain,
			Address:      address,
			Depth:        depth,
			Status:       StatusArchiveRPCMissing,
			Completeness: "unavailable",
			Message:      "Full-history analysis requires ONCHAIN_BSC_ARCHIVE_RPC_URL. Use depth=recent for public-API analysis.",
		}, nil
	}

	resp := &TokenAnalysisResponse{
		Success:      true,
		Chain:        chain,
		Address:      address,
		Depth:        depth,
		Status:       StatusOK,
		Completeness: "recent",
		Source:       []string{},
	}

	recent, err := s.buildRecentAnalysis(ctx, chain, address)
	if err != nil {
		resp.Status = StatusPartialData
		resp.Message = err.Error()
	} else {
		mergeRecent(resp, recent)
	}

	if depth != DepthFull {
		resp.DealerFlow = buildDealerFlowAnalysis(resp)
		return resp, nil
	}

	resp.Completeness = "full_requested"
	full, status, message, err := s.buildFullAnalysis(chain, address)
	if err != nil {
		return nil, err
	}
	resp.Full = full
	resp.Status = status
	resp.Message = message
	if status == StatusOK {
		resp.Completeness = "full"
	}
	resp.DealerFlow = buildDealerFlowAnalysis(resp)
	return resp, nil
}

func (s *Service) QueueIndex(ctx context.Context, req IndexTokenRequest) (*IndexStatusResponse, error) {
	chain := normalizeChain(req.Chain)
	address := normalizeAddress(req.Address)
	scope := normalizeIndexScope(req.Scope)
	indexSource := normalizeIndexSource(req.IndexSource, scope)
	if err := validateRequest(chain, address); err != nil {
		return &IndexStatusResponse{Success: false, Chain: chain, Address: address, Status: StatusInvalidRequest, Scope: scope, IndexSource: indexSource, ErrorMessage: err.Error()}, nil
	}
	if indexSource == store.OnchainIndexSourceFreeLocal {
		return s.queueFreeLocalEarlyIndex(ctx, chain, address, req)
	}
	if strings.TrimSpace(config.Get().OnchainBSCArchiveRPCURL) == "" {
		return &IndexStatusResponse{
			Success:      false,
			Chain:        chain,
			Address:      address,
			Status:       StatusArchiveRPCMissing,
			Scope:        scope,
			IndexSource:  indexSource,
			Message:      "ONCHAIN_BSC_ARCHIVE_RPC_URL is required for full-history indexing.",
			ErrorMessage: "archive RPC missing",
		}, nil
	}

	startBlock := req.StartBlock
	if startBlock == 0 {
		pools, _ := s.discoverPools(ctx, chain, address)
		if len(pools) > 0 {
			if err := s.populatePoolCreatedBlocks(ctx, pools); err != nil {
				logger.Warnf("onchain pool created block lookup failed for %s: %v", address, err)
			}
		}
		for _, pool := range pools {
			if pool.CreatedBlock > 0 && (startBlock == 0 || pool.CreatedBlock < startBlock) {
				startBlock = pool.CreatedBlock
			}
		}
		if len(pools) > 0 {
			_ = s.store.Onchain().UpsertPools(poolsToStore(pools, chain, address))
		}
	}
	if startBlock == 0 {
		return &IndexStatusResponse{
			Success:      false,
			Chain:        chain,
			Address:      address,
			Status:       StatusInvalidRequest,
			Message:      "start_block is required because pool creation block could not be discovered.",
			ErrorMessage: "missing start_block",
		}, nil
	}

	endBlock := req.EndBlock
	if endBlock == 0 {
		if scope == store.OnchainIndexScopeEarlyWalletWindow {
			window := config.Get().OnchainEarlyWindowBlocks
			if window <= 0 {
				window = defaultEarlyWalletIndexWindowBlocks
			}
			endBlock = startBlock + window - 1
		} else {
			latest, err := s.rpcBlockNumber(ctx)
			if err != nil {
				return nil, err
			}
			endBlock = latest
		}
	}
	if endBlock < startBlock {
		endBlock = startBlock
	}
	now := time.Now().UTC().UnixMilli()
	job := &store.OnchainIndexJob{
		Chain:           chain,
		TokenAddress:    address,
		Status:          store.OnchainJobStatusQueued,
		Scope:           scope,
		IndexSource:     indexSource,
		Phase:           PhaseScanningSeeds,
		Provider:        freeLogsProviderName(s.freeLogs),
		RequestBudget:   freeLogsBudgetRemaining(s.freeLogs),
		ProgressMessage: "Archive RPC index queued.",
		StartBlock:      startBlock,
		EndBlock:        endBlock,
		LastBlock:       startBlock - 1,
		BatchSize:       config.Get().OnchainIndexerBatchBlocks,
		StartedAtMS:     now,
		UpdatedAtMS:     now,
		ErrorMessage:    "",
		CompletedAtMS:   0,
	}
	if existing, _ := s.store.Onchain().GetJob(chain, address); existing != nil && existing.LastBlock >= startBlock {
		if existing.LastBlock > endBlock {
			job.LastBlock = endBlock
		} else {
			job.LastBlock = existing.LastBlock
		}
	}
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return nil, err
	}
	_ = s.markTokenIndexStatus(chain, address, store.OnchainJobStatusIndexing, now)
	s.StartJob(context.Background(), chain, address)
	return s.IndexStatus(chain, address)
}

func (s *Service) queueFreeLocalEarlyIndex(ctx context.Context, chain, address string, req IndexTokenRequest) (*IndexStatusResponse, error) {
	if !config.Get().OnchainFreeIndexerEnabled {
		return &IndexStatusResponse{
			Success:      false,
			Chain:        chain,
			Address:      address,
			Status:       StatusArchiveRPCMissing,
			Scope:        store.OnchainIndexScopeEarlyWalletWindow,
			IndexSource:  store.OnchainIndexSourceFreeLocal,
			Message:      "ONCHAIN_FREE_INDEXER_ENABLED is false; enable it to use free local early-wallet indexing.",
			ErrorMessage: "free local indexer disabled",
		}, nil
	}
	startBlock := req.StartBlock
	endBlock := req.EndBlock
	progress := "Free local early-wallet index queued; historical backfill may be slow."
	if endBlock < startBlock {
		endBlock = 0
	}
	now := time.Now().UTC().UnixMilli()
	if err := s.store.Onchain().UpsertLocalIndexTask(&store.OnchainLocalIndexTask{
		Chain:           chain,
		TokenAddress:    address,
		Enabled:         true,
		IndexSource:     store.OnchainIndexSourceFreeLocal,
		StartBlock:      startBlock,
		LastBlock:       0,
		Status:          store.OnchainJobStatusQueued,
		LastError:       "",
		ProgressMessage: progress,
	}); err != nil {
		return nil, err
	}
	job := &store.OnchainIndexJob{
		Chain:           chain,
		TokenAddress:    address,
		Status:          store.OnchainJobStatusQueued,
		Scope:           store.OnchainIndexScopeEarlyWalletWindow,
		IndexSource:     store.OnchainIndexSourceFreeLocal,
		Phase:           PhaseScanningSeeds,
		Provider:        freeSeedProviderName(s.freeLogs),
		RequestBudget:   freeLogsBudgetRemaining(s.freeLogs),
		ProgressMessage: progress,
		StartBlock:      startBlock,
		EndBlock:        endBlock,
		LastBlock:       0,
		BatchSize:       config.Get().OnchainIndexerBatchBlocks,
		StartedAtMS:     now,
		UpdatedAtMS:     now,
		ErrorMessage:    "",
		CompletedAtMS:   0,
	}
	if startBlock > 0 {
		job.LastBlock = startBlock - 1
	}
	if existing, _ := s.store.Onchain().GetJob(chain, address); existing != nil && existing.IndexSource == store.OnchainIndexSourceFreeLocal && req.StartBlock > 0 {
		if existing.StartBlock > 0 && job.StartBlock == 0 {
			job.StartBlock = existing.StartBlock
		}
	}
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return nil, err
	}
	_ = s.markTokenIndexStatus(chain, address, store.OnchainJobStatusIndexing, now)
	s.StartJob(context.Background(), chain, address)
	return s.IndexStatus(chain, address)
}

func normalizeIndexScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case store.OnchainIndexScopeEarlyWalletWindow:
		return store.OnchainIndexScopeEarlyWalletWindow
	default:
		return store.OnchainIndexScopeFullHistory
	}
}

func normalizeIndexSource(source, scope string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case store.OnchainIndexSourceArchiveRPC:
		return store.OnchainIndexSourceArchiveRPC
	case store.OnchainIndexSourceFreeLocal:
		return store.OnchainIndexSourceFreeLocal
	default:
		if scope == store.OnchainIndexScopeEarlyWalletWindow {
			return store.OnchainIndexSourceFreeLocal
		}
		return store.OnchainIndexSourceArchiveRPC
	}
}

func (s *Service) IndexStatus(chain, address string) (*IndexStatusResponse, error) {
	chain = normalizeChain(chain)
	address = normalizeAddress(address)
	if err := validateRequest(chain, address); err != nil {
		return &IndexStatusResponse{Success: false, Chain: chain, Address: address, Status: StatusInvalidRequest, ErrorMessage: err.Error()}, nil
	}
	job, err := s.store.Onchain().GetJob(chain, address)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return &IndexStatusResponse{Success: true, Chain: chain, Address: address, Status: StatusIndexRequired, Scope: store.OnchainIndexScopeEarlyWalletWindow, IndexSource: store.OnchainIndexSourceFreeLocal, Message: "Token has not been indexed yet."}, nil
	}
	seedWallets, _ := s.store.Onchain().CountEarlyBuyWallets(chain, address, defaultEarlySeedCount)
	return &IndexStatusResponse{
		Success:                true,
		Chain:                  chain,
		Address:                address,
		Status:                 job.Status,
		Scope:                  job.Scope,
		IndexSource:            job.IndexSource,
		Phase:                  job.Phase,
		Provider:               job.Provider,
		RequestBudgetRemaining: job.RequestBudget,
		ProgressMessage:        job.ProgressMessage,
		StartBlock:             job.StartBlock,
		EndBlock:               job.EndBlock,
		LastBlock:              job.LastBlock,
		SeedWallets:            seedWallets,
		UpdatedAtMS:            job.UpdatedAtMS,
		ErrorMessage:           job.ErrorMessage,
	}, nil
}

func (s *Service) StartConfiguredJobs(ctx context.Context) {
	if (!config.Get().OnchainIndexerEnabled || strings.TrimSpace(config.Get().OnchainBSCArchiveRPCURL) == "") && !config.Get().OnchainFreeIndexerEnabled {
		logger.Infof("onchain indexer disabled archive_enabled=%v archive_rpc_configured=%v free_enabled=%v", config.Get().OnchainIndexerEnabled, strings.TrimSpace(config.Get().OnchainBSCArchiveRPCURL) != "", config.Get().OnchainFreeIndexerEnabled)
		return
	}
	safe.GoNamed("onchain-indexer", func() {
		interval := time.Duration(config.Get().OnchainIndexerPollIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 15 * time.Second
		}
		logger.Infof("onchain indexer started interval=%s", interval)
		s.startActiveIndexJobs(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.startActiveIndexJobs(ctx)
			}
		}
	})
}

func (s *Service) startActiveIndexJobs(ctx context.Context) {
	jobs, err := s.store.Onchain().ListActiveJobs()
	if err != nil {
		logger.Warnf("onchain indexer list jobs failed: %v", err)
		return
	}
	if config.Get().OnchainFreeIndexerEnabled {
		tasks, err := s.store.Onchain().ListEnabledLocalIndexTasks()
		if err != nil {
			logger.Warnf("onchain indexer list local tasks failed: %v", err)
		} else {
			for _, task := range tasks {
				job, _ := s.store.Onchain().GetJob(task.Chain, task.TokenAddress)
				if job != nil && job.IndexSource == store.OnchainIndexSourceFreeLocal {
					jobs = append(jobs, *job)
					continue
				}
				jobs = append(jobs, store.OnchainIndexJob{
					Chain:        task.Chain,
					TokenAddress: task.TokenAddress,
					Status:       task.Status,
					Scope:        store.OnchainIndexScopeEarlyWalletWindow,
					IndexSource:  store.OnchainIndexSourceFreeLocal,
					StartBlock:   task.StartBlock,
					LastBlock:    task.LastBlock,
					ErrorMessage: task.LastError,
					UpdatedAtMS:  time.Now().UTC().UnixMilli(),
				})
			}
		}
	}
	if len(jobs) > 0 {
		logger.Infof("onchain indexer scheduling %d active job(s)", len(jobs))
	}
	seen := map[string]bool{}
	for _, job := range jobs {
		key := normalizeChain(job.Chain) + ":" + normalizeAddress(job.TokenAddress)
		if seen[key] {
			continue
		}
		seen[key] = true
		s.StartJob(ctx, job.Chain, job.TokenAddress)
	}
}

func (s *Service) StartJob(ctx context.Context, chain, address string) {
	chain = normalizeChain(chain)
	address = normalizeAddress(address)
	key := chain + ":" + address
	s.mu.Lock()
	if s.running[key] {
		s.mu.Unlock()
		return
	}
	s.running[key] = true
	s.mu.Unlock()
	safe.GoNamed("onchain-index-"+address, func() {
		defer func() {
			s.mu.Lock()
			delete(s.running, key)
			s.mu.Unlock()
		}()
		if err := s.runIndexJob(ctx, chain, address); err != nil {
			logger.Warnf("onchain index job failed for %s: %v", address, err)
			_ = s.markIndexJobFailure(ctx, chain, address, err)
		}
	})
}

func (s *Service) markIndexJobFailure(_ context.Context, chain, address string, runErr error) error {
	job, _ := s.store.Onchain().GetJob(chain, address)
	if job == nil {
		job = &store.OnchainIndexJob{
			Chain:        chain,
			TokenAddress: address,
		}
	}
	job.UpdatedAtMS = time.Now().UTC().UnixMilli()
	if errors.Is(runErr, errRPCRateLimited) || errors.Is(runErr, errFreeLogsBudgetExceeded) || errors.Is(runErr, errRPCPayloadTooLarge) || errors.Is(runErr, errRPCTransportRetryable) {
		job.Status = store.OnchainJobStatusQueued
		if errors.Is(runErr, errFreeLogsBudgetExceeded) {
			job.ErrorMessage = "free logs daily budget exceeded; retrying"
			job.ProgressMessage = "Free log request budget is exhausted; free local index will retry later."
		} else if errors.Is(runErr, errRPCPayloadTooLarge) {
			job.ErrorMessage = "rpc log range too large; retrying with smaller batches"
			job.ProgressMessage = "Free RPC log range is too large; the early-window index will retry with smaller batches."
			if job.BatchSize <= 0 || job.BatchSize > int(minIndexLogRangeBlocks) {
				job.BatchSize = int(minIndexLogRangeBlocks)
			}
		} else if errors.Is(runErr, errRPCTransportRetryable) {
			job.ErrorMessage = "rpc transport retrying: " + runErr.Error()
			job.ProgressMessage = "Free RPC transport is temporarily unavailable; free local index will retry on the next tick."
		} else {
			job.ErrorMessage = "rpc rate limited; retrying"
			job.ProgressMessage = "RPC rate limited; free local index will retry on the next tick."
		}
		if err := s.store.Onchain().UpsertJob(job); err != nil {
			return err
		}
		if job.IndexSource == store.OnchainIndexSourceFreeLocal {
			_ = s.store.Onchain().UpsertLocalIndexTask(&store.OnchainLocalIndexTask{
				Chain:           chain,
				TokenAddress:    address,
				Enabled:         true,
				IndexSource:     store.OnchainIndexSourceFreeLocal,
				StartBlock:      job.StartBlock,
				LastBlock:       job.LastBlock,
				Status:          store.OnchainJobStatusQueued,
				LastError:       job.ErrorMessage,
				ProgressMessage: job.ProgressMessage,
			})
		}
		return s.markTokenIndexStatus(chain, address, store.OnchainJobStatusIndexing, job.UpdatedAtMS)
	}
	job.Status = store.OnchainJobStatusFailed
	if errors.Is(runErr, errRPCArchiveUnsupported) {
		job.ErrorMessage = "archive rpc unsupported: current RPC plan does not allow historical eth_getLogs; upgrade the RPC plan or use an archive-capable BSC endpoint"
	} else {
		job.ErrorMessage = runErr.Error()
	}
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return err
	}
	return s.markTokenIndexStatus(chain, address, store.OnchainJobStatusFailed, job.UpdatedAtMS)
}

func (s *Service) markTokenIndexStatus(chain, address, status string, updatedAtMS int64) error {
	token, _ := s.store.Onchain().GetToken(chain, address)
	if token == nil {
		token = &store.OnchainToken{Chain: chain, Address: address}
	}
	token.IndexStatus = status
	token.LastIndexedAt = updatedAtMS
	if token.AnalysisCompleteness == "" {
		token.AnalysisCompleteness = "partial"
	}
	return s.store.Onchain().UpsertToken(token)
}

func validateRequest(chain, address string) error {
	if normalizeChain(chain) != "bsc" {
		return fmt.Errorf("only bsc is supported")
	}
	if !evmAddressPattern.MatchString(address) {
		return fmt.Errorf("invalid EVM address")
	}
	return nil
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

func mergeRecent(dst *TokenAnalysisResponse, src *TokenAnalysisResponse) {
	if src == nil {
		return
	}
	if src.Status != "" && src.Status != StatusOK {
		dst.Status = src.Status
	}
	if src.Completeness != "" && src.Completeness != "recent" {
		dst.Completeness = src.Completeness
	}
	if src.Message != "" {
		dst.Message = src.Message
	}
	if src.Token.Name != "" || src.Token.Symbol != "" {
		dst.Token = src.Token
	}
	dst.Security = src.Security
	dst.Pools = src.Pools
	dst.Recent = src.Recent
	dst.RiskFlags = append(dst.RiskFlags, src.RiskFlags...)
	seenSources := make(map[string]bool, len(dst.Source)+len(src.Source))
	mergedSources := make([]string, 0, len(dst.Source)+len(src.Source))
	for _, source := range append(dst.Source, src.Source...) {
		if source == "" || seenSources[source] {
			continue
		}
		seenSources[source] = true
		mergedSources = append(mergedSources, source)
	}
	dst.Source = mergedSources
}

func parseFloatString(value string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return f
}

func parseIntString(value string) int {
	i, _ := strconv.Atoi(strings.TrimSpace(value))
	return i
}

func isoFromMS(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func msFromISO(value string) int64 {
	if value == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0
	}
	return t.UTC().UnixMilli()
}

func sortedWalletsByNet(wallets []WalletAnalysis, desc bool) []WalletAnalysis {
	out := append([]WalletAnalysis(nil), wallets...)
	sort.SliceStable(out, func(i, j int) bool {
		if desc {
			return out[i].NetBoughtAmount > out[j].NetBoughtAmount
		}
		return out[i].NetBoughtAmount < out[j].NetBoughtAmount
	})
	return out
}

func capWallets(wallets []WalletAnalysis, limit int) []WalletAnalysis {
	if limit <= 0 || len(wallets) <= limit {
		return wallets
	}
	return wallets[:limit]
}

func walletTypeFromStats(buyCount, sellCount, poolCount int, net, total float64) string {
	if buyCount+sellCount >= 10 && poolCount >= 2 && total > 0 && math.Abs(net) <= math.Max(total*0.05, 1) {
		return store.OnchainWalletArbBot
	}
	if net > 0 {
		return store.OnchainWalletAccumulator
	}
	if net < 0 || sellCount > buyCount {
		return store.OnchainWalletSeller
	}
	return store.OnchainWalletUnknown
}

func decodeJSONLimited(resp *http.Response, out any, limit int64) error {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("upstream status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(out)
}
