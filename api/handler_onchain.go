package api

import (
	"net/http"
	"strconv"

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
