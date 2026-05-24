package onchain

import (
	"context"
	"encoding/json"
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
	StatusOK                = "ok"
	StatusPartialData       = "partial_data"
	StatusIndexing          = "indexing"
	StatusArchiveRPCMissing = "archive_rpc_missing"
	StatusIndexRequired     = "index_required"
	StatusInvalidRequest    = "invalid_request"

	DepthRecent = "recent"
	DepthFull   = "full"

	bscChainID = "56"
)

var evmAddressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

type Service struct {
	store      *store.Store
	httpClient *http.Client
	mu         sync.Mutex
	running    map[string]bool
}

func NewService(st *store.Store) *Service {
	httpClient, err := netclient.NewProxyAwareHTTPClient(20*time.Second, config.Get().MarketHTTPProxy)
	if err != nil {
		logger.Warnf("invalid onchain HTTP proxy ignored: %v", err)
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Service{
		store:      st,
		httpClient: httpClient,
		running:    make(map[string]bool),
	}
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
	if err := validateRequest(chain, address); err != nil {
		return &IndexStatusResponse{Success: false, Chain: chain, Address: address, Status: StatusInvalidRequest, ErrorMessage: err.Error()}, nil
	}
	if strings.TrimSpace(config.Get().OnchainBSCArchiveRPCURL) == "" {
		return &IndexStatusResponse{
			Success:      false,
			Chain:        chain,
			Address:      address,
			Status:       StatusArchiveRPCMissing,
			Message:      "ONCHAIN_BSC_ARCHIVE_RPC_URL is required for full-history indexing.",
			ErrorMessage: "archive RPC missing",
		}, nil
	}

	startBlock := req.StartBlock
	if startBlock == 0 {
		pools, _ := s.discoverPools(ctx, chain, address)
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
		latest, err := s.rpcBlockNumber(ctx)
		if err != nil {
			return nil, err
		}
		endBlock = latest
	}
	now := time.Now().UTC().UnixMilli()
	job := &store.OnchainIndexJob{
		Chain:         chain,
		TokenAddress:  address,
		Status:        store.OnchainJobStatusQueued,
		StartBlock:    startBlock,
		EndBlock:      endBlock,
		LastBlock:     startBlock - 1,
		BatchSize:     config.Get().OnchainIndexerBatchBlocks,
		StartedAtMS:   now,
		UpdatedAtMS:   now,
		ErrorMessage:  "",
		CompletedAtMS: 0,
	}
	if existing, _ := s.store.Onchain().GetJob(chain, address); existing != nil && existing.LastBlock >= startBlock {
		job.LastBlock = existing.LastBlock
	}
	if err := s.store.Onchain().UpsertJob(job); err != nil {
		return nil, err
	}
	s.StartJob(ctx, chain, address)
	return s.IndexStatus(chain, address)
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
		return &IndexStatusResponse{Success: true, Chain: chain, Address: address, Status: StatusIndexRequired, Message: "Token has not been indexed yet."}, nil
	}
	return &IndexStatusResponse{
		Success:      true,
		Chain:        chain,
		Address:      address,
		Status:       job.Status,
		StartBlock:   job.StartBlock,
		EndBlock:     job.EndBlock,
		LastBlock:    job.LastBlock,
		UpdatedAtMS:  job.UpdatedAtMS,
		ErrorMessage: job.ErrorMessage,
	}, nil
}

func (s *Service) StartConfiguredJobs(ctx context.Context) {
	if !config.Get().OnchainIndexerEnabled || strings.TrimSpace(config.Get().OnchainBSCArchiveRPCURL) == "" {
		return
	}
	safe.GoNamed("onchain-indexer", func() {
		interval := time.Duration(config.Get().OnchainIndexerPollIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = 15 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				jobs, err := s.store.Onchain().ListActiveJobs()
				if err != nil {
					logger.Warnf("onchain indexer list jobs failed: %v", err)
					continue
				}
				for _, job := range jobs {
					s.StartJob(ctx, job.Chain, job.TokenAddress)
				}
			}
		}
	})
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
			job, _ := s.store.Onchain().GetJob(chain, address)
			if job == nil {
				job = &store.OnchainIndexJob{
					Chain:        chain,
					TokenAddress: address,
				}
			}
			job.Status = store.OnchainJobStatusFailed
			job.ErrorMessage = err.Error()
			job.UpdatedAtMS = time.Now().UTC().UnixMilli()
			_ = s.store.Onchain().UpsertJob(job)
			token, _ := s.store.Onchain().GetToken(chain, address)
			if token == nil {
				token = &store.OnchainToken{Chain: chain, Address: address}
			}
			token.IndexStatus = store.OnchainJobStatusFailed
			token.LastIndexedAt = job.UpdatedAtMS
			if token.AnalysisCompleteness == "" {
				token.AnalysisCompleteness = "partial"
			}
			_ = s.store.Onchain().UpsertToken(token)
		}
	})
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
