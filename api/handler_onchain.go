package api

import (
	"net/http"
	"strconv"
	"strings"

	"nofx/onchain"

	"github.com/gin-gonic/gin"
)

func (s *Server) handleOnchainTokenAnalysis(c *gin.Context) {
	req := onchain.TokenAnalysisRequest{
		Chain:   c.DefaultQuery("chain", "bsc"),
		Address: c.Query("address"),
		Depth:   c.DefaultQuery("depth", "recent"),
	}
	resp, err := s.onchainService.AnalyzeToken(c.Request.Context(), req)
	if err != nil {
		SafeInternalError(c, "Onchain token analysis", err)
		return
	}
	status := http.StatusOK
	if resp != nil && resp.Status == onchain.StatusInvalidRequest {
		status = http.StatusBadRequest
	}
	c.JSON(status, resp)
}

func (s *Server) handleOnchainWalletGraph(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "80"))
	resp, err := s.onchainService.WalletGraph(c.Request.Context(), onchain.WalletGraphRequest{
		Chain:   c.DefaultQuery("chain", "bsc"),
		Address: c.Query("address"),
		Depth:   c.DefaultQuery("depth", "recent"),
		Limit:   limit,
	})
	if err != nil {
		SafeInternalError(c, "Onchain wallet graph", err)
		return
	}
	status := http.StatusOK
	if resp != nil && resp.Status == onchain.StatusInvalidRequest {
		status = http.StatusBadRequest
	}
	c.JSON(status, resp)
}

func (s *Server) handleOnchainIndexStatus(c *gin.Context) {
	resp, err := s.onchainService.IndexStatus(c.DefaultQuery("chain", "bsc"), c.Query("address"))
	if err != nil {
		SafeInternalError(c, "Onchain index status", err)
		return
	}
	status := http.StatusOK
	if resp != nil && resp.Status == onchain.StatusInvalidRequest {
		status = http.StatusBadRequest
	}
	c.JSON(status, resp)
}

func (s *Server) handleOnchainIndexToken(c *gin.Context) {
	var req onchain.IndexTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "status": onchain.StatusInvalidRequest, "error": "invalid request body"})
		return
	}
	if req.Chain == "" {
		req.Chain = "bsc"
	}
	resp, err := s.onchainService.QueueIndex(c.Request.Context(), req)
	if err != nil {
		SafeInternalError(c, "Onchain index token", err)
		return
	}
	status := http.StatusOK
	if resp != nil && resp.Status == onchain.StatusInvalidRequest {
		status = http.StatusBadRequest
	}
	c.JSON(status, resp)
}

func (s *Server) handleOnchainEarlyWalletFlow(c *gin.Context) {
	seedCount, _ := strconv.Atoi(c.DefaultQuery("seed_count", "100"))
	maxDepth, _ := strconv.Atoi(c.DefaultQuery("max_depth", "4"))
	resp, err := s.onchainService.EarlyWalletFlow(c.Request.Context(), onchain.EarlyWalletFlowRequest{
		Chain:     c.DefaultQuery("chain", "bsc"),
		Address:   c.Query("address"),
		SeedCount: seedCount,
		MaxDepth:  maxDepth,
	})
	if err != nil {
		SafeInternalError(c, "Onchain early wallet flow", err)
		return
	}
	status := http.StatusOK
	if resp != nil && resp.Status == onchain.StatusInvalidRequest {
		status = http.StatusBadRequest
	}
	c.JSON(status, resp)
}

func (s *Server) handleOnchainEarlyWalletFlowIndex(c *gin.Context) {
	var req onchain.EarlyWalletFlowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "status": onchain.StatusInvalidRequest, "error": "invalid request body"})
		return
	}
	if req.Chain == "" {
		req.Chain = "bsc"
	}
	resp, err := s.onchainService.QueueIndex(c.Request.Context(), onchain.IndexTokenRequest{
		Chain:   req.Chain,
		Address: req.Address,
	})
	if err != nil {
		SafeInternalError(c, "Onchain early wallet flow index", err)
		return
	}
	status := http.StatusOK
	if resp != nil && resp.Status == onchain.StatusInvalidRequest {
		status = http.StatusBadRequest
	}
	c.JSON(status, resp)
}

func (s *Server) handleOnchainEarlyWalletFlowExport(c *gin.Context) {
	seedCount, _ := strconv.Atoi(c.DefaultQuery("seed_count", "100"))
	maxDepth, _ := strconv.Atoi(c.DefaultQuery("max_depth", "4"))
	address := strings.ToLower(strings.TrimSpace(c.Query("address")))
	req := onchain.EarlyWalletFlowRequest{
		Chain:     c.DefaultQuery("chain", "bsc"),
		Address:   address,
		SeedCount: seedCount,
		MaxDepth:  maxDepth,
	}
	resp, err := s.onchainService.EarlyWalletFlow(c.Request.Context(), req)
	if err != nil {
		SafeInternalError(c, "Onchain early wallet flow export", err)
		return
	}
	if resp != nil && resp.Status == onchain.StatusInvalidRequest {
		c.JSON(http.StatusBadRequest, resp)
		return
	}
	filenameAddress := address
	if len(filenameAddress) > 12 {
		filenameAddress = filenameAddress[:6] + "-" + filenameAddress[len(filenameAddress)-4:]
	}
	if filenameAddress == "" {
		filenameAddress = "token"
	}
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="early-wallet-flow-`+filenameAddress+`.csv"`)
	if err := onchain.WriteEarlyWalletFlowResponseCSV(c.Writer, resp); err != nil {
		SafeInternalError(c, "Onchain early wallet flow export", err)
		return
	}
}
