package onchain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
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
)

var (
	transferTopic = eventTopic("Transfer(address,address,uint256)")
	swapTopic     = eventTopic("Swap(address,uint256,uint256,uint256,uint256,address)")
	mintTopic     = eventTopic("Mint(address,uint256,uint256)")
	burnTopic     = eventTopic("Burn(address,uint256,uint256,address)")
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

	batch := config.Get().OnchainIndexerBatchBlocks
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
		if err := s.indexBlockRange(ctx, chain, address, pools, from, to); err != nil {
			return err
		}
		job.LastBlock = to
		job.EndBlock = end
		job.UpdatedAtMS = time.Now().UTC().UnixMilli()
		if err := s.store.Onchain().UpsertJob(job); err != nil {
			return err
		}
		from = to + 1
	}
	if err := s.store.Onchain().RecomputeWalletSnapshots(chain, address); err != nil {
		return err
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

func (s *Service) fetchTransferLogs(ctx context.Context, tokenAddress string, from, to int64) ([]rpcLog, error) {
	var logs []rpcLog
	filter := map[string]any{
		"fromBlock": hexBlock(from),
		"toBlock":   hexBlock(to),
		"address":   tokenAddress,
		"topics":    []any{transferTopic},
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
		"topics":    []any{[]string{swapTopic, mintTopic, burnTopic}},
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
	if topic != swapTopic && topic != mintTopic && topic != burnTopic {
		return store.OnchainSwap{}, false
	}
	values := splitDataWords(log.Data)
	if topic == swapTopic && len(values) < 4 {
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

func (s *Service) rpcCall(ctx context.Context, method string, params []any, out any) error {
	endpoint := strings.TrimSpace(config.Get().OnchainBSCArchiveRPCURL)
	if endpoint == "" {
		return fmt.Errorf("ONCHAIN_BSC_ARCHIVE_RPC_URL is not configured")
	}
	payload, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("rpc status %d", resp.StatusCode)
	}
	var decoded rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return err
	}
	if decoded.Error != nil {
		return fmt.Errorf("rpc error %d: %s", decoded.Error.Code, decoded.Error.Message)
	}
	return json.Unmarshal(decoded.Result, out)
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

func topicToAddress(topic string) string {
	topic = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(topic)), "0x")
	if len(topic) < 40 {
		return ""
	}
	return "0x" + topic[len(topic)-40:]
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
