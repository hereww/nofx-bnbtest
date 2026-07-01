package api

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nofx/config"
	"nofx/logger"

	"github.com/gin-gonic/gin"
)

var dataGatewayHTTPClient = &http.Client{Timeout: 30 * time.Second}

// handleDataGatewayProxy proxies read-only requests to the self-hosted market data gateway.
// The browser should call this same-origin endpoint instead of connecting to DATA_GATEWAY_URL directly.
func (s *Server) handleDataGatewayProxy(c *gin.Context) {
	if c.Request.Method != http.MethodGet {
		c.JSON(http.StatusMethodNotAllowed, gin.H{"success": false, "error": "method not allowed"})
		return
	}

	if s.dataGatewayHandler != nil {
		s.serveEmbeddedDataGateway(c)
		return
	}

	target, err := buildDataGatewayURL(c.Param("path"), c.Request.URL.RawQuery)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "failed to build data gateway request"})
		return
	}
	req.Header.Set("Accept", "application/json")
	if token := strings.TrimSpace(config.Get().DataGatewayToken); token != "" {
		req.Header.Set("X-Gateway-Token", token)
	}

	resp, err := dataGatewayHTTPClient.Do(req)
	if err != nil {
		logger.Warnf("Data gateway proxy request failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "data gateway unavailable"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"success": false, "error": "failed to read data gateway response"})
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	c.Data(resp.StatusCode, contentType, body)
}

func (s *Server) serveEmbeddedDataGateway(c *gin.Context) {
	path, err := dataGatewayProxyPath(c.Param("path"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": err.Error()})
		return
	}

	target := path
	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, target, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "failed to build embedded data gateway request"})
		return
	}
	req.Header = c.Request.Header.Clone()
	if token := strings.TrimSpace(config.Get().DataGatewayToken); token != "" {
		req.Header.Set("X-Gateway-Token", token)
	}

	rec := &embeddedDataGatewayResponseWriter{header: http.Header{}}
	s.dataGatewayHandler.ServeHTTP(rec, req)

	contentType := rec.header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}
	for key, values := range rec.header {
		if strings.EqualFold(key, "Content-Type") {
			continue
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	c.Data(status, contentType, rec.body)
}

type embeddedDataGatewayResponseWriter struct {
	header http.Header
	body   []byte
	status int
}

func (w *embeddedDataGatewayResponseWriter) Header() http.Header {
	return w.header
}

func (w *embeddedDataGatewayResponseWriter) WriteHeader(statusCode int) {
	if w.status == 0 {
		w.status = statusCode
	}
}

func (w *embeddedDataGatewayResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body = append(w.body, body...)
	return len(body), nil
}

func buildDataGatewayURL(pathParam, rawQuery string) (string, error) {
	base := strings.TrimRight(config.Get().DataGatewayURL, "/")
	if base == "" {
		base = "http://127.0.0.1:8090"
	}

	path, pathErr := dataGatewayProxyPath(pathParam)
	if pathErr != nil {
		return "", pathErr
	}

	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", &dataGatewayPathError{"invalid DATA_GATEWAY_URL"}
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawQuery = rawQuery
	return u.String(), nil
}

func dataGatewayProxyPath(pathParam string) (string, error) {
	path := "/" + strings.TrimLeft(pathParam, "/")
	if path == "/" {
		path = "/health"
	}
	if path != "/health" && !strings.HasPrefix(path, "/api/") {
		return "", &dataGatewayPathError{"unsupported data gateway path"}
	}
	return path, nil
}

type dataGatewayPathError struct {
	message string
}

func (e *dataGatewayPathError) Error() string {
	return e.message
}
