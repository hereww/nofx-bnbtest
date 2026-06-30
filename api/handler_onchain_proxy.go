package api

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nofx/config"
	"nofx/logger"
	"nofx/onchain"

	"github.com/gin-gonic/gin"
)

var onchainIndexerHTTPClient = &http.Client{Timeout: 90 * time.Second}

func shouldProxyOnchainIndexer() bool {
	return strings.TrimSpace(config.Get().OnchainIndexerURL) != ""
}

func (s *Server) proxyOnchainIndexer(c *gin.Context, path string) bool {
	base := strings.TrimRight(config.Get().OnchainIndexerURL, "/")
	if base == "" {
		return false
	}
	target, err := url.Parse(base)
	if err != nil || target.Scheme == "" || target.Host == "" {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "status": onchain.StatusIndexerUnavailable, "error": "invalid ONCHAIN_INDEXER_URL"})
		return true
	}
	target.Path = strings.TrimRight(target.Path, "/") + path
	target.RawQuery = c.Request.URL.RawQuery

	var body io.Reader
	if c.Request.Body != nil {
		raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 4<<20))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "status": onchain.StatusInvalidRequest, "error": "failed to read request body"})
			return true
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target.String(), body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "status": onchain.StatusIndexerUnavailable, "error": "failed to build indexer request"})
		return true
	}
	req.Header.Set("Accept", c.GetHeader("Accept"))
	if ct := c.GetHeader("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	resp, err := onchainIndexerHTTPClient.Do(req)
	if err != nil {
		logger.Warnf("onchain indexer proxy request failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "status": onchain.StatusIndexerUnavailable, "error": "onchain indexer unavailable"})
		return true
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "status": onchain.StatusIndexerUnavailable, "error": "failed to read indexer response"})
		return true
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	c.Data(resp.StatusCode, contentType, raw)
	return true
}
