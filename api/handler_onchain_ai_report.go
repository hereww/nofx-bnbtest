package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"nofx/auth"
	"nofx/mcp"
	_ "nofx/mcp/provider"
	"nofx/onchain"
	"nofx/store"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type onchainAIReportRequest struct {
	Chain           string                           `json:"chain"`
	Address         string                           `json:"address"`
	Depth           string                           `json:"depth"`
	Language        string                           `json:"language"`
	Analysis        *onchain.TokenAnalysisResponse   `json:"analysis"`
	WalletGraph     *onchain.WalletGraphResponse     `json:"wallet_graph"`
	EarlyWalletFlow *onchain.EarlyWalletFlowResponse `json:"early_wallet_flow"`
	ModelID         string                           `json:"model_id"`
	Agent           onchainAIReportAgentConfig       `json:"agent"`
}

type onchainAIReportAgentConfig struct {
	ReportStyle       string `json:"report_style"`
	RiskProfile       string `json:"risk_profile"`
	Focus             string `json:"focus"`
	CustomPrompt      string `json:"custom_prompt"`
	IncludeRawSignals bool   `json:"include_raw_signals"`
}

type onchainAIReportJobStatus string

const (
	onchainAIReportJobQueued    onchainAIReportJobStatus = "queued"
	onchainAIReportJobRunning   onchainAIReportJobStatus = "running"
	onchainAIReportJobCompleted onchainAIReportJobStatus = "completed"
	onchainAIReportJobFailed    onchainAIReportJobStatus = "failed"
)

type onchainAIReportJob struct {
	ID          string
	UserID      string
	Status      onchainAIReportJobStatus
	ModelID     string
	ModelName   string
	Chain       string
	Address     string
	Depth       string
	Report      string
	Error       string
	ErrorParams map[string]string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt time.Time
}

type onchainAIReportJobStore struct {
	mu   sync.Mutex
	jobs map[string]*onchainAIReportJob
}

func newOnchainAIReportJobStore() *onchainAIReportJobStore {
	return &onchainAIReportJobStore{jobs: map[string]*onchainAIReportJob{}}
}

func (s *onchainAIReportJobStore) create(job *onchainAIReportJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	s.jobs[job.ID] = job
}

func (s *onchainAIReportJobStore) get(id string) (*onchainAIReportJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	job, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	copy := *job
	if job.ErrorParams != nil {
		copy.ErrorParams = map[string]string{}
		for key, value := range job.ErrorParams {
			copy.ErrorParams[key] = value
		}
	}
	return &copy, true
}

func (s *onchainAIReportJobStore) update(id string, update func(*onchainAIReportJob)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job, ok := s.jobs[id]; ok {
		update(job)
		job.UpdatedAt = time.Now()
	}
}

func (s *onchainAIReportJobStore) pruneLocked(now time.Time) {
	for id, job := range s.jobs {
		ttl := 30 * time.Minute
		if job.Status == onchainAIReportJobCompleted || job.Status == onchainAIReportJobFailed {
			ttl = 10 * time.Minute
		}
		if now.Sub(job.UpdatedAt) > ttl {
			delete(s.jobs, id)
		}
	}
}

func (s *Server) handleOnchainAIReportPreview(c *gin.Context) {
	var req onchainAIReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body"})
		return
	}
	if req.Chain == "" {
		req.Chain = "bsc"
	}
	if req.Depth == "" {
		req.Depth = "recent"
	}

	analysis := req.Analysis
	if analysis == nil {
		resp, err := s.onchainService.AnalyzeToken(c.Request.Context(), onchain.TokenAnalysisRequest{
			Chain:   req.Chain,
			Address: req.Address,
			Depth:   req.Depth,
		})
		if err != nil {
			SafeInternalError(c, "Onchain AI report prompt analysis", err)
			return
		}
		analysis = resp
	}
	if analysis == nil || !analysis.Success {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "token analysis is required before previewing an AI report prompt"})
		return
	}
	if req.EarlyWalletFlow != nil {
		analysis.EarlyWalletFlow = req.EarlyWalletFlow
	} else if analysis.EarlyWalletFlow == nil {
		analysis.EarlyWalletFlow, _ = s.onchainService.EarlyWalletFlow(c.Request.Context(), onchain.EarlyWalletFlowRequest{
			Chain:   analysis.Chain,
			Address: analysis.Address,
		})
	}

	walletGraph := req.WalletGraph
	if walletGraph == nil {
		walletGraph, _ = s.onchainService.WalletGraph(c.Request.Context(), onchain.WalletGraphRequest{
			Chain:   analysis.Chain,
			Address: analysis.Address,
			Depth:   analysis.Depth,
			Limit:   80,
		})
	}

	systemPrompt, userPrompt, payload, err := buildOnchainAIReportPrompts(req.Language, req.Agent, analysis, walletGraph)
	if err != nil {
		SafeInternalError(c, "Onchain AI report prompt", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"system_prompt": systemPrompt,
		"user_prompt":   userPrompt,
		"payload":       payload,
		"config_summary": gin.H{
			"chain":           analysis.Chain,
			"address":         analysis.Address,
			"depth":           analysis.Depth,
			"report_style":    normalizeAIReportStyle(req.Agent.ReportStyle),
			"risk_profile":    normalizeAIReportRiskProfile(req.Agent.RiskProfile),
			"focus":           strings.TrimSpace(req.Agent.Focus),
			"custom_prompt":   strings.TrimSpace(req.Agent.CustomPrompt) != "",
			"include_raw":     req.Agent.IncludeRawSignals,
			"token_symbol":    analysis.Token.Symbol,
			"holder_count":    holderCountForSummary(analysis),
			"risk_flag_count": len(analysis.RiskFlags),
		},
	})
}

func (s *Server) handleOnchainAIReport(c *gin.Context) {
	var req onchainAIReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid request body"})
		return
	}
	if req.Chain == "" {
		req.Chain = "bsc"
	}
	if req.Depth == "" {
		req.Depth = "recent"
	}

	analysis := req.Analysis
	if analysis == nil {
		resp, err := s.onchainService.AnalyzeToken(c.Request.Context(), onchain.TokenAnalysisRequest{
			Chain:   req.Chain,
			Address: req.Address,
			Depth:   req.Depth,
		})
		if err != nil {
			SafeInternalError(c, "Onchain AI report analysis", err)
			return
		}
		analysis = resp
	}
	if analysis == nil || !analysis.Success {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "token analysis is required before generating an AI report"})
		return
	}
	if req.EarlyWalletFlow != nil {
		analysis.EarlyWalletFlow = req.EarlyWalletFlow
	} else if analysis.EarlyWalletFlow == nil {
		analysis.EarlyWalletFlow, _ = s.onchainService.EarlyWalletFlow(c.Request.Context(), onchain.EarlyWalletFlowRequest{
			Chain:   analysis.Chain,
			Address: analysis.Address,
		})
	}

	userID := authenticatedOrDefaultUserID(c)
	model, err := s.selectAIReportModel(userID, req.ModelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	walletGraph := req.WalletGraph
	if walletGraph == nil {
		walletGraph, _ = s.onchainService.WalletGraph(c.Request.Context(), onchain.WalletGraphRequest{
			Chain:   analysis.Chain,
			Address: analysis.Address,
			Depth:   analysis.Depth,
			Limit:   80,
		})
	}

	if strings.EqualFold(c.Query("sync"), "1") || strings.EqualFold(c.Query("mode"), "sync") {
		report, err := s.generateOnchainAIReport(c.Request.Context(), model, req.Language, req.Agent, analysis, walletGraph)
		if err != nil {
			publicMsg, params := describeOnchainAIReportError(model, err)
			SafeErrorWithDetails(c, http.StatusBadGateway, publicMsg, "onchain.ai_report.failed", params, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":      true,
			"chain":        analysis.Chain,
			"address":      analysis.Address,
			"depth":        analysis.Depth,
			"model_id":     model.ID,
			"model_name":   modelNameForReport(model),
			"generated_at": time.Now().UTC().Format(time.RFC3339),
			"report":       report,
		})
		return
	}

	jobID := newOnchainAIReportJobID()
	now := time.Now()
	s.onchainAIReports.create(&onchainAIReportJob{
		ID:        jobID,
		UserID:    userID,
		Status:    onchainAIReportJobQueued,
		ModelID:   model.ID,
		ModelName: modelNameForReport(model),
		Chain:     analysis.Chain,
		Address:   analysis.Address,
		Depth:     analysis.Depth,
		CreatedAt: now,
		UpdatedAt: now,
	})

	go s.runOnchainAIReportJob(jobID, model, req.Language, req.Agent, analysis, walletGraph)

	c.JSON(http.StatusAccepted, gin.H{
		"success":       true,
		"status":        onchainAIReportJobQueued,
		"job_id":        jobID,
		"chain":         analysis.Chain,
		"address":       analysis.Address,
		"depth":         analysis.Depth,
		"model_id":      model.ID,
		"model_name":    modelNameForReport(model),
		"poll_after_ms": 3000,
		"message":       "AI report generation queued",
	})
}

func (s *Server) handleOnchainAIReportJob(c *gin.Context) {
	jobID := strings.TrimSpace(c.Param("job_id"))
	job, ok := s.onchainAIReports.get(jobID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "status": "not_found", "error": "AI report job not found or expired"})
		return
	}
	body := gin.H{
		"success":    job.Status != onchainAIReportJobFailed,
		"job_id":     job.ID,
		"status":     job.Status,
		"chain":      job.Chain,
		"address":    job.Address,
		"depth":      job.Depth,
		"model_id":   job.ModelID,
		"model_name": job.ModelName,
		"created_at": job.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": job.UpdatedAt.UTC().Format(time.RFC3339),
	}
	switch job.Status {
	case onchainAIReportJobCompleted:
		body["generated_at"] = job.CompletedAt.UTC().Format(time.RFC3339)
		body["report"] = job.Report
	case onchainAIReportJobFailed:
		body["error"] = job.Error
		body["error_params"] = job.ErrorParams
	default:
		body["poll_after_ms"] = 3000
	}
	c.JSON(http.StatusOK, body)
}

func (s *Server) runOnchainAIReportJob(jobID string, model *store.AIModel, language string, agentConfig onchainAIReportAgentConfig, analysis *onchain.TokenAnalysisResponse, walletGraph *onchain.WalletGraphResponse) {
	s.onchainAIReports.update(jobID, func(job *onchainAIReportJob) {
		job.Status = onchainAIReportJobRunning
	})
	report, err := s.generateOnchainAIReport(context.Background(), model, language, agentConfig, analysis, walletGraph)
	if err != nil {
		publicMsg, params := describeOnchainAIReportError(model, err)
		s.onchainAIReports.update(jobID, func(job *onchainAIReportJob) {
			job.Status = onchainAIReportJobFailed
			job.Error = publicMsg
			job.ErrorParams = params
			job.CompletedAt = time.Now()
		})
		return
	}
	s.onchainAIReports.update(jobID, func(job *onchainAIReportJob) {
		job.Status = onchainAIReportJobCompleted
		job.Report = report
		job.CompletedAt = time.Now()
	})
}

func newOnchainAIReportJobID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return "air_" + hex.EncodeToString(buf[:])
	}
	return fmt.Sprintf("air_%d", time.Now().UnixNano())
}

func authenticatedOrDefaultUserID(c *gin.Context) string {
	if value, ok := c.Get("user_id"); ok {
		if userID, ok := value.(string); ok && strings.TrimSpace(userID) != "" {
			return userID
		}
	}
	authHeader := strings.TrimSpace(c.GetHeader("Authorization"))
	if authHeader != "" {
		parts := strings.Fields(authHeader)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && !auth.IsTokenBlacklisted(parts[1]) {
			if claims, err := auth.ValidateJWT(parts[1]); err == nil && strings.TrimSpace(claims.UserID) != "" {
				return claims.UserID
			}
		}
	}
	return "default"
}

func (s *Server) selectAIReportModel(userID, modelID string) (*store.AIModel, error) {
	if strings.TrimSpace(modelID) != "" {
		model, err := s.store.AIModel().Get(userID, strings.TrimSpace(modelID))
		if err != nil {
			return nil, fmt.Errorf("AI model not found: %s", strings.TrimSpace(modelID))
		}
		return validateAIReportModel(model)
	}
	model, err := s.store.AIModel().GetDefault(userID)
	if err == nil {
		return validateAIReportModel(model)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) && userID != "default" {
		return nil, err
	}
	model, err = s.store.AIModel().GetAnyEnabled()
	if err != nil {
		return nil, fmt.Errorf("请先在「配置 > 模型」启用一个可用 AI 模型，再生成链上 AI 报告")
	}
	return validateAIReportModel(model)
}

func validateAIReportModel(model *store.AIModel) (*store.AIModel, error) {
	if model == nil {
		return nil, fmt.Errorf("AI model not found")
	}
	if !model.Enabled {
		return nil, fmt.Errorf("AI model %s is not enabled", model.Name)
	}
	if strings.TrimSpace(string(model.APIKey)) == "" {
		return nil, fmt.Errorf("AI model %s is missing API Key", model.Name)
	}
	return model, nil
}

func (s *Server) generateOnchainAIReport(ctx context.Context, model *store.AIModel, language string, agentConfig onchainAIReportAgentConfig, analysis *onchain.TokenAnalysisResponse, walletGraph *onchain.WalletGraphResponse) (string, error) {
	reportCtx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	client := mcp.NewAIClientByProvider(strings.ToLower(strings.TrimSpace(model.Provider)), mcp.WithTimeout(180*time.Second), mcp.WithMaxTokens(1800))
	if client == nil {
		client = mcp.NewClient(mcp.WithTimeout(180*time.Second), mcp.WithMaxTokens(1800))
	}
	client.SetAPIKey(
		strings.TrimSpace(string(model.APIKey)),
		normalizeOpenAICompatibleBaseURL(strings.TrimSpace(model.Provider), strings.TrimSpace(model.CustomAPIURL)),
		strings.TrimSpace(model.CustomModelName),
	)

	systemPrompt, userPrompt, _, err := buildOnchainAIReportPrompts(language, agentConfig, analysis, walletGraph)
	if err != nil {
		return "", err
	}
	temp := 0.2
	maxTokens := 1800
	return client.CallWithRequest(&mcp.Request{
		Ctx: reportCtx,
		Messages: []mcp.Message{
			mcp.NewSystemMessage(systemPrompt),
			mcp.NewUserMessage(userPrompt),
		},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	})
}

func normalizeOpenAICompatibleBaseURL(provider, rawURL string) string {
	clean := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if clean == "" || strings.HasSuffix(clean, "#") {
		return rawURL
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != mcp.ProviderOpenAI {
		return clean
	}
	parsed, err := url.Parse(clean)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return clean
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/v1") || strings.Contains(path, "/v1/") || strings.HasSuffix(path, "/chat/completions") {
		return clean
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1"
	return strings.TrimRight(parsed.String(), "/")
}

func describeOnchainAIReportError(model *store.AIModel, err error) (string, map[string]string) {
	raw := ""
	if err != nil {
		raw = err.Error()
	}
	params := map[string]string{
		"model_id":   strings.TrimSpace(model.ID),
		"model_name": modelNameForReport(model),
		"provider":   strings.TrimSpace(model.Provider),
	}
	if strings.Contains(raw, "invalid character '<'") {
		params["reason"] = "upstream_returned_html"
		return "AI 报告生成失败：模型接口返回了 HTML 而不是 JSON。请检查该模型的 Base URL 是否为 OpenAI 兼容接口地址，通常需要填写到 /v1，例如 https://你的域名/v1。", params
	}
	if strings.Contains(raw, "API API key not set") || strings.Contains(raw, "API key not set") || strings.Contains(strings.ToLower(raw), "api key") {
		params["reason"] = "api_key_invalid_or_missing"
		return "AI 报告生成失败：当前模型 API Key 不可用。请在「配置 > 模型」重新保存该模型的 API Key。", params
	}
	if strings.Contains(strings.ToLower(raw), "status 401") || strings.Contains(strings.ToLower(raw), "unauthorized") {
		params["reason"] = "unauthorized"
		return "AI 报告生成失败：模型接口认证失败。请检查 API Key 是否正确、是否有该模型权限。", params
	}
	if strings.Contains(strings.ToLower(raw), "status 404") {
		params["reason"] = "endpoint_or_model_not_found"
		return "AI 报告生成失败：模型接口或模型名不存在。请检查 Base URL 和 Model Name。", params
	}
	if strings.Contains(strings.ToLower(raw), "status 429") {
		params["reason"] = "rate_limited"
		return "AI 报告生成失败：模型接口限流或额度不足。请稍后重试或更换模型。", params
	}
	params["reason"] = "upstream_failed"
	return "AI 报告生成失败：模型接口调用失败。请检查模型配置、Base URL、Model Name 和 API Key 后重试。", params
}

func buildOnchainAIReportSystemPrompt(language string) string {
	if strings.EqualFold(language, "en") {
		return "You are NOFXi's on-chain token risk analyst. The main goal is to judge whether suspected major wallets/dealer clusters are accumulating, distributing, mixed, or data-insufficient, including early-buyer cost basis and transfer-descendant selling when available. Produce a concise trading-support report, not financial advice. Never promise guaranteed profit. Separate objective facts, inferred risks, and actionable monitoring steps. Use Markdown sections: Dealer Flow Verdict, Early Wallet Cost Flow, Key On-chain Facts, Risk Signals, Wallet Relationship Interpretation, Monitoring Plan, Decision Notes. Keep it practical and conditional."
	}
	return "你是 NOFXi 的链上代币风险分析师。核心目标是判断疑似主力/庄家关联钱包是在进货、出货、多空混合，还是数据不足；如果有 early_wallet_flow，要重点解读前100早期买家成本、转出衍生地址卖出和已实现盈利。生成交易辅助报告，不提供绝对买卖建议，绝不能承诺保证盈利。请区分客观事实、推断风险和可执行监控步骤。使用 Markdown 小节：庄家资金流结论、早期地址成本流、关键链上事实、风险信号、钱包关系解读、后续监控计划、决策备注。语言要专业、谨慎、可执行。"
}

func buildOnchainAIReportPrompts(language string, agentConfig onchainAIReportAgentConfig, analysis *onchain.TokenAnalysisResponse, walletGraph *onchain.WalletGraphResponse) (string, string, string, error) {
	payload, err := compactOnchainAnalysisForAI(analysis, walletGraph)
	if err != nil {
		return "", "", "", err
	}
	systemPrompt := buildOnchainAIReportSystemPrompt(language)
	if addition := buildOnchainAIReportAgentPrompt(language, agentConfig); addition != "" {
		systemPrompt += "\n\n" + addition
	}

	if strings.EqualFold(language, "en") {
		return systemPrompt, fmt.Sprintf("Generate the report from this on-chain analysis JSON. Use only the provided data; do not invent missing data.\n\n%s", payload), payload, nil
	}
	return systemPrompt, fmt.Sprintf("请基于以下链上分析 JSON 生成报告。只基于数据本身，不能编造没有出现的数据。\n\n%s", payload), payload, nil
}

func buildOnchainAIReportAgentPrompt(language string, config onchainAIReportAgentConfig) string {
	style := normalizeAIReportStyle(config.ReportStyle)
	risk := normalizeAIReportRiskProfile(config.RiskProfile)
	focus := strings.TrimSpace(config.Focus)
	custom := strings.TrimSpace(config.CustomPrompt)
	includeRaw := config.IncludeRawSignals

	if strings.EqualFold(language, "en") {
		parts := []string{
			"Agent settings:",
			fmt.Sprintf("- Report style: %s.", style),
			fmt.Sprintf("- Risk profile: %s. Adjust the strictness of risk interpretation to this profile, while staying conditional.", risk),
		}
		if focus != "" {
			parts = append(parts, fmt.Sprintf("- Extra focus: %s.", focus))
		}
		if custom != "" {
			parts = append(parts, fmt.Sprintf("- Custom instruction: %s.", custom))
		}
		if includeRaw {
			parts = append(parts, "- Include a compact raw signal appendix when useful.")
		}
		return strings.Join(parts, "\n")
	}

	parts := []string{
		"分析 Agent 设置：",
		fmt.Sprintf("- 报告风格：%s。", style),
		fmt.Sprintf("- 风险偏好：%s。按照这个风险偏好调整风险解读的严格程度，但必须保持条件化表达。", risk),
	}
	if focus != "" {
		parts = append(parts, fmt.Sprintf("- 额外关注：%s。", focus))
	}
	if custom != "" {
		parts = append(parts, fmt.Sprintf("- 自定义要求：%s。", custom))
	}
	if includeRaw {
		parts = append(parts, "- 必要时在报告末尾加入精简原始信号附录。")
	}
	return strings.Join(parts, "\n")
}

func normalizeAIReportStyle(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "brief", "concise":
		return "brief"
	case "deep", "detailed":
		return "deep"
	case "watchlist", "monitor":
		return "watchlist"
	default:
		return "balanced"
	}
}

func normalizeAIReportRiskProfile(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "aggressive":
		return "aggressive"
	case "defensive", "conservative":
		return "defensive"
	default:
		return "balanced"
	}
}

func holderCountForSummary(analysis *onchain.TokenAnalysisResponse) int {
	if analysis == nil || analysis.Security == nil {
		return 0
	}
	return analysis.Security.HolderCount
}

func compactOnchainAnalysisForAI(analysis *onchain.TokenAnalysisResponse, walletGraph *onchain.WalletGraphResponse) (string, error) {
	type compactReportInput struct {
		Chain           string                       `json:"chain"`
		Address         string                       `json:"address"`
		Depth           string                       `json:"depth"`
		Status          string                       `json:"status"`
		Completeness    string                       `json:"completeness"`
		Token           onchain.TokenProfile         `json:"token"`
		Security        *onchain.TokenSecurity       `json:"security,omitempty"`
		Pools           []onchain.PoolSnapshot       `json:"pools,omitempty"`
		Recent          *onchain.RecentTradeAnalysis `json:"recent,omitempty"`
		Full            *onchain.FullHistoryAnalysis `json:"full,omitempty"`
		DealerFlow      *onchain.DealerFlowAnalysis  `json:"dealer_flow,omitempty"`
		EarlyWalletFlow *compactEarlyWalletFlow      `json:"early_wallet_flow,omitempty"`
		WalletGraph     *compactWalletGraphSummary   `json:"wallet_graph_summary,omitempty"`
		RiskFlags       []string                     `json:"risk_flags,omitempty"`
		Source          []string                     `json:"source,omitempty"`
	}
	input := compactReportInput{
		Chain:           analysis.Chain,
		Address:         analysis.Address,
		Depth:           analysis.Depth,
		Status:          analysis.Status,
		Completeness:    analysis.Completeness,
		Token:           analysis.Token,
		Security:        compactSecurity(analysis.Security),
		Pools:           firstPools(analysis.Pools, 5),
		Recent:          compactRecent(analysis.Recent),
		Full:            compactFull(analysis.Full),
		DealerFlow:      analysis.DealerFlow,
		EarlyWalletFlow: compactEarlyWalletFlowForAI(analysis.EarlyWalletFlow),
		WalletGraph:     compactWalletGraphForAI(walletGraph),
		RiskFlags:       analysis.RiskFlags,
		Source:          analysis.Source,
	}
	raw, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

type compactEarlyWalletFlow struct {
	Status       string                          `json:"status"`
	Completeness string                          `json:"completeness"`
	SeedCount    int                             `json:"seed_count"`
	MaxDepth     int                             `json:"max_depth"`
	QuoteSymbol  string                          `json:"quote_symbol,omitempty"`
	Summary      onchain.EarlyWalletFlowSummary  `json:"summary"`
	TopSeeds     []onchain.EarlyWalletSeed       `json:"top_seeds,omitempty"`
	KeyWallets   []onchain.EarlyWalletFlowWallet `json:"key_wallets,omitempty"`
}

func compactEarlyWalletFlowForAI(flow *onchain.EarlyWalletFlowResponse) *compactEarlyWalletFlow {
	if flow == nil {
		return nil
	}
	return &compactEarlyWalletFlow{
		Status:       flow.Status,
		Completeness: flow.Completeness,
		SeedCount:    flow.SeedCount,
		MaxDepth:     flow.MaxDepth,
		QuoteSymbol:  flow.QuoteSymbol,
		Summary:      flow.Summary,
		TopSeeds:     firstEarlySeeds(flow.Seeds, 8),
		KeyWallets:   firstEarlyWallets(flow.Wallets, 12),
	}
}

type compactWalletGraphSummary struct {
	Depth          string                   `json:"depth"`
	Status         string                   `json:"status"`
	NodeCount      int                      `json:"node_count"`
	EdgeCount      int                      `json:"edge_count"`
	NodeTypes      map[string]int           `json:"node_types,omitempty"`
	Relations      map[string]int           `json:"relations,omitempty"`
	KeyWalletNodes []compactWalletGraphNode `json:"key_wallet_nodes,omitempty"`
	KeyEdges       []compactWalletGraphEdge `json:"key_edges,omitempty"`
}

type compactWalletGraphNode struct {
	Label           string  `json:"label,omitempty"`
	NodeType        string  `json:"node_type"`
	Address         string  `json:"address,omitempty"`
	WalletType      string  `json:"wallet_type,omitempty"`
	Percent         float64 `json:"percent,omitempty"`
	BuyCount        int     `json:"buy_count,omitempty"`
	SellCount       int     `json:"sell_count,omitempty"`
	NetBoughtAmount float64 `json:"net_bought_amount,omitempty"`
	FirstBuyAt      string  `json:"first_buy_at,omitempty"`
}

type compactWalletGraphEdge struct {
	Source   string  `json:"source"`
	Target   string  `json:"target"`
	Relation string  `json:"relation"`
	Amount   float64 `json:"amount,omitempty"`
	Weight   float64 `json:"weight,omitempty"`
}

func compactWalletGraphForAI(graph *onchain.WalletGraphResponse) *compactWalletGraphSummary {
	if graph == nil || !graph.Success || (len(graph.Nodes) == 0 && len(graph.Edges) == 0) {
		return nil
	}
	summary := &compactWalletGraphSummary{
		Depth:     graph.Depth,
		Status:    graph.Status,
		NodeCount: len(graph.Nodes),
		EdgeCount: len(graph.Edges),
		NodeTypes: map[string]int{},
		Relations: map[string]int{},
	}
	for _, node := range graph.Nodes {
		summary.NodeTypes[node.NodeType]++
		switch node.NodeType {
		case "accumulator", "seller", "related_wallet", "top_holder", "owner", "creator":
			if len(summary.KeyWalletNodes) >= 14 {
				continue
			}
			summary.KeyWalletNodes = append(summary.KeyWalletNodes, compactWalletGraphNode{
				Label:           node.Label,
				NodeType:        node.NodeType,
				Address:         node.Address,
				WalletType:      node.WalletType,
				Percent:         node.Percent,
				BuyCount:        node.BuyCount,
				SellCount:       node.SellCount,
				NetBoughtAmount: node.NetBoughtAmount,
				FirstBuyAt:      node.FirstBuyAt,
			})
		}
	}
	for _, edge := range graph.Edges {
		summary.Relations[edge.Relation]++
		if len(summary.KeyEdges) >= 20 {
			continue
		}
		summary.KeyEdges = append(summary.KeyEdges, compactWalletGraphEdge{
			Source:   edge.Source,
			Target:   edge.Target,
			Relation: edge.Relation,
			Amount:   edge.Amount,
			Weight:   edge.Weight,
		})
	}
	return summary
}

func compactSecurity(security *onchain.TokenSecurity) *onchain.TokenSecurity {
	if security == nil {
		return nil
	}
	copy := *security
	copy.TopHolders = firstHolders(copy.TopHolders, 10)
	copy.LPHolders = firstHolders(copy.LPHolders, 5)
	return &copy
}

func compactRecent(recent *onchain.RecentTradeAnalysis) *onchain.RecentTradeAnalysis {
	if recent == nil {
		return nil
	}
	copy := *recent
	copy.FirstBuyBuckets = lastBuckets(copy.FirstBuyBuckets, 8)
	copy.TopAccumulators = firstWallets(copy.TopAccumulators, 8)
	copy.TopSellers = firstWallets(copy.TopSellers, 8)
	copy.RelatedWalletClusters = firstWallets(copy.RelatedWalletClusters, 8)
	return &copy
}

func compactFull(full *onchain.FullHistoryAnalysis) *onchain.FullHistoryAnalysis {
	if full == nil {
		return nil
	}
	copy := *full
	copy.FirstBuyBuckets = lastBuckets(copy.FirstBuyBuckets, 8)
	copy.TopAccumulators = firstWallets(copy.TopAccumulators, 8)
	copy.TopSellers = firstWallets(copy.TopSellers, 8)
	copy.RelatedWalletClusters = firstWallets(copy.RelatedWalletClusters, 8)
	return &copy
}

func firstPools(items []onchain.PoolSnapshot, limit int) []onchain.PoolSnapshot {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func firstHolders(items []onchain.HolderSnapshot, limit int) []onchain.HolderSnapshot {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func firstWallets(items []onchain.WalletAnalysis, limit int) []onchain.WalletAnalysis {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func firstEarlySeeds(items []onchain.EarlyWalletSeed, limit int) []onchain.EarlyWalletSeed {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func firstEarlyWallets(items []onchain.EarlyWalletFlowWallet, limit int) []onchain.EarlyWalletFlowWallet {
	if len(items) <= limit {
		return items
	}
	return items[:limit]
}

func lastBuckets(items []onchain.TimeBucket, limit int) []onchain.TimeBucket {
	if len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

func modelNameForReport(model *store.AIModel) string {
	if strings.TrimSpace(model.CustomModelName) != "" {
		return strings.TrimSpace(model.CustomModelName)
	}
	if strings.TrimSpace(model.Name) != "" {
		return strings.TrimSpace(model.Name)
	}
	return strings.TrimSpace(model.ID)
}
