package onchain

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

const defaultWalletGraphLimit = 80

func (s *Service) WalletGraph(ctx context.Context, req WalletGraphRequest) (*WalletGraphResponse, error) {
	chain := normalizeChain(req.Chain)
	address := normalizeAddress(req.Address)
	depth := strings.ToLower(strings.TrimSpace(req.Depth))
	if depth == "" {
		depth = DepthRecent
	}
	if depth != DepthRecent && depth != DepthFull {
		return &WalletGraphResponse{Success: false, Chain: chain, Address: address, Depth: depth, Status: StatusInvalidRequest, Error: "depth must be recent or full"}, nil
	}
	if err := validateRequest(chain, address); err != nil {
		return &WalletGraphResponse{Success: false, Chain: chain, Address: address, Depth: depth, Status: StatusInvalidRequest, Error: err.Error()}, nil
	}
	limit := req.Limit
	if limit <= 0 || limit > 120 {
		limit = defaultWalletGraphLimit
	}

	analysis, err := s.AnalyzeToken(ctx, TokenAnalysisRequest{Chain: chain, Address: address, Depth: depth})
	if err != nil {
		return nil, err
	}
	if analysis == nil || !analysis.Success {
		resp := &WalletGraphResponse{Success: false, Chain: chain, Address: address, Depth: depth, Status: StatusPartialData}
		if analysis != nil {
			resp.Status = analysis.Status
			resp.Error = analysis.Error
			resp.Message = analysis.Message
		}
		return resp, nil
	}

	builder := newWalletGraphBuilder(limit)
	tokenLabel := analysis.Token.Symbol
	if strings.TrimSpace(tokenLabel) == "" {
		tokenLabel = shortenForGraph(address)
	}
	builder.addNode(WalletGraphNode{
		ID:       "token:" + address,
		Label:    tokenLabel,
		NodeType: "token",
		Address:  address,
		Value:    28,
	})

	for _, pool := range analysis.Pools {
		if !builder.canAddMore() {
			break
		}
		poolID := "pool:" + pool.Address
		builder.addNode(WalletGraphNode{
			ID:       poolID,
			Label:    labelOrAddress(pool.Name, pool.Address),
			NodeType: "pool",
			Address:  pool.Address,
			Value:    math.Max(pool.LiquidityUSD, pool.Volume24hUSD),
		})
		builder.addEdge(WalletGraphEdge{Source: "token:" + address, Target: poolID, Relation: "pool", Weight: 4, Amount: pool.LiquidityUSD})
	}

	if analysis.Security != nil {
		if analysis.Security.OwnerAddress != "" && analysis.Security.OwnerAddress != "0x0000000000000000000000000000000000000000" {
			builder.addAddressNode(analysis.Security.OwnerAddress, "owner", "owner", WalletAnalysis{})
			builder.addEdge(WalletGraphEdge{Source: "token:" + address, Target: "owner:" + analysis.Security.OwnerAddress, Relation: "owner", Weight: 5})
		}
		if analysis.Security.CreatorAddress != "" {
			builder.addAddressNode(analysis.Security.CreatorAddress, "creator", "creator", WalletAnalysis{})
			builder.addEdge(WalletGraphEdge{Source: "token:" + address, Target: "creator:" + analysis.Security.CreatorAddress, Relation: "creator", Weight: 4})
		}
		for _, holder := range firstHoldersForGraph(analysis.Security.TopHolders, 10) {
			if !builder.canAddMore() {
				break
			}
			nodeID := "holder:" + holder.Address
			builder.addNode(WalletGraphNode{
				ID:         nodeID,
				Label:      labelOrAddress(holder.Tag, holder.Address),
				NodeType:   "top_holder",
				Address:    holder.Address,
				Value:      holder.Percent,
				Percent:    holder.Percent,
				WalletType: "top_holder",
			})
			builder.addEdge(WalletGraphEdge{Source: "token:" + address, Target: nodeID, Relation: "holds", Weight: math.Max(holder.Percent*100, 1), Amount: holder.Percent})
		}
	}

	accumulators, sellers, related := graphWalletSets(analysis)
	builder.addWallets("accumulator", accumulators, "buy")
	builder.addWallets("seller", sellers, "sell")
	builder.addWallets("related", related, "related")

	if depth == DepthFull {
		if err := s.addFullTransferEdges(builder, chain, address); err != nil {
			return nil, err
		}
	}

	return &WalletGraphResponse{
		Success:    true,
		Chain:      chain,
		Address:    address,
		Depth:      depth,
		Status:     analysis.Status,
		Message:    analysis.Message,
		Token:      analysis.Token,
		DealerFlow: analysis.DealerFlow,
		Nodes:      builder.nodes,
		Edges:      builder.edges,
	}, nil
}

func (s *Service) addFullTransferEdges(builder *walletGraphBuilder, chain, address string) error {
	addresses := builder.addresses()
	transfers, err := s.store.Onchain().ListTransfersForGraph(chain, address, addresses, 160)
	if err != nil {
		return err
	}
	for _, transfer := range transfers {
		sourceID := builder.nodeIDForAddress(transfer.FromAddress)
		targetID := builder.nodeIDForAddress(transfer.ToAddress)
		if sourceID == "" || targetID == "" || sourceID == targetID {
			continue
		}
		builder.addEdge(WalletGraphEdge{
			Source:   sourceID,
			Target:   targetID,
			Relation: "transfer",
			Amount:   transfer.Amount,
			Weight:   math.Max(math.Log10(math.Abs(transfer.Amount)+10), 1),
			TxHash:   transfer.TxHash,
		})
	}
	return nil
}

type walletGraphBuilder struct {
	limit         int
	nodes         []WalletGraphNode
	edges         []WalletGraphEdge
	nodeSeen      map[string]bool
	edgeSeen      map[string]bool
	addressToNode map[string]string
}

func newWalletGraphBuilder(limit int) *walletGraphBuilder {
	return &walletGraphBuilder{
		limit:         limit,
		nodeSeen:      map[string]bool{},
		edgeSeen:      map[string]bool{},
		addressToNode: map[string]string{},
	}
}

func (b *walletGraphBuilder) canAddMore() bool {
	return len(b.nodes) < b.limit
}

func (b *walletGraphBuilder) addNode(node WalletGraphNode) {
	if node.ID == "" || b.nodeSeen[node.ID] || !b.canAddMore() {
		return
	}
	if node.Label == "" {
		node.Label = shortenForGraph(node.Address)
	}
	if node.Value == 0 {
		node.Value = 1
	}
	b.nodeSeen[node.ID] = true
	b.nodes = append(b.nodes, node)
	if node.Address != "" && b.addressToNode[normalizeAddress(node.Address)] == "" {
		b.addressToNode[normalizeAddress(node.Address)] = node.ID
	}
}

func (b *walletGraphBuilder) addEdge(edge WalletGraphEdge) {
	if edge.Source == "" || edge.Target == "" || edge.Source == edge.Target {
		return
	}
	key := fmt.Sprintf("%s|%s|%s|%s", edge.Source, edge.Target, edge.Relation, edge.TxHash)
	if b.edgeSeen[key] {
		return
	}
	if edge.Weight == 0 {
		edge.Weight = 1
	}
	b.edgeSeen[key] = true
	b.edges = append(b.edges, edge)
}

func (b *walletGraphBuilder) addAddressNode(address, prefix, nodeType string, wallet WalletAnalysis) {
	address = normalizeAddress(address)
	if address == "" {
		return
	}
	nodeID := prefix + ":" + address
	b.addNode(WalletGraphNode{
		ID:              nodeID,
		Label:           shortenForGraph(address),
		NodeType:        nodeType,
		Address:         address,
		WalletType:      wallet.WalletType,
		Value:           math.Abs(wallet.NetBoughtAmount) + math.Abs(wallet.BuyAmount) + math.Abs(wallet.SellAmount),
		BuyCount:        wallet.BuyCount,
		SellCount:       wallet.SellCount,
		BuyAmount:       wallet.BuyAmount,
		SellAmount:      wallet.SellAmount,
		NetBoughtAmount: wallet.NetBoughtAmount,
		FirstBuyAt:      wallet.FirstBuyAt,
	})
}

func (b *walletGraphBuilder) addWallets(prefix string, wallets []WalletAnalysis, relation string) {
	for _, wallet := range wallets {
		if !b.canAddMore() {
			return
		}
		nodeType := prefix
		if prefix == "related" {
			nodeType = "related_wallet"
		}
		b.addAddressNode(wallet.Address, prefix, nodeType, wallet)
	}
	// Add pool-token directional edges after nodes are known. Use the first token node
	// as the relation anchor so recent mode still produces a readable network.
	tokenID := ""
	for _, node := range b.nodes {
		if node.NodeType == "token" {
			tokenID = node.ID
			break
		}
	}
	if tokenID == "" {
		return
	}
	for _, wallet := range wallets {
		nodeID := prefix + ":" + normalizeAddress(wallet.Address)
		if !b.nodeSeen[nodeID] {
			continue
		}
		amount := math.Abs(wallet.NetBoughtAmount)
		weight := math.Max(math.Log10(amount+10), 1)
		if relation == "sell" {
			b.addEdge(WalletGraphEdge{Source: nodeID, Target: tokenID, Relation: relation, Amount: amount, Weight: weight})
		} else {
			b.addEdge(WalletGraphEdge{Source: tokenID, Target: nodeID, Relation: relation, Amount: amount, Weight: weight})
		}
	}
}

func (b *walletGraphBuilder) addresses() []string {
	out := make([]string, 0, len(b.addressToNode))
	for address := range b.addressToNode {
		out = append(out, address)
	}
	sort.Strings(out)
	return out
}

func (b *walletGraphBuilder) nodeIDForAddress(address string) string {
	return b.addressToNode[normalizeAddress(address)]
}

func graphWalletSets(analysis *TokenAnalysisResponse) ([]WalletAnalysis, []WalletAnalysis, []WalletAnalysis) {
	if analysis.Full != nil && len(analysis.Full.TopAccumulators)+len(analysis.Full.TopSellers)+len(analysis.Full.RelatedWalletClusters) > 0 {
		return analysis.Full.TopAccumulators, analysis.Full.TopSellers, analysis.Full.RelatedWalletClusters
	}
	if analysis.Recent != nil {
		return analysis.Recent.TopAccumulators, analysis.Recent.TopSellers, analysis.Recent.RelatedWalletClusters
	}
	return nil, nil, nil
}

func firstHoldersForGraph(items []HolderSnapshot, limit int) []HolderSnapshot {
	out := append([]HolderSnapshot(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Percent > out[j].Percent })
	if len(out) <= limit {
		return out
	}
	return out[:limit]
}

func labelOrAddress(label, address string) string {
	label = strings.TrimSpace(label)
	if label != "" {
		return label
	}
	return shortenForGraph(address)
}

func shortenForGraph(address string) string {
	address = strings.TrimSpace(address)
	if len(address) <= 12 {
		return address
	}
	return address[:6] + "..." + address[len(address)-4:]
}
