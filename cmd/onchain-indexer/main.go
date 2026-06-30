package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"nofx/config"
	"nofx/logger"
	"nofx/onchain"
	"nofx/store"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	_ "modernc.org/sqlite"
)

func main() {
	_ = godotenv.Load()
	logger.Init(nil)
	config.Init()

	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrate(os.Args[2:]); err != nil {
			log.Fatalf("onchain migrate failed: %v", err)
		}
		return
	}

	cfg := config.Get()
	if dir := filepath.Dir(cfg.OnchainIndexerDBPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("create onchain indexer data dir: %v", err)
		}
	}
	st, err := store.NewOnchainOnly(store.DBConfig{
		Type: store.DBTypeSQLite,
		Path: cfg.OnchainIndexerDBPath,
	})
	if err != nil {
		log.Fatalf("open onchain indexer db: %v", err)
	}
	defer st.Close()

	svc := onchain.NewService(st)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.StartConfiguredJobs(ctx)

	server := &http.Server{
		Addr:              cfg.OnchainIndexerAddr,
		Handler:           newRouter(svc),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("nofx-onchain-indexer listening on %s db=%s", cfg.OnchainIndexerAddr, cfg.OnchainIndexerDBPath)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("onchain indexer server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("onchain indexer shutdown error: %v", err)
	}
}

func newRouter(svc *onchain.Service) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "nofx-onchain-indexer"})
	})
	api := r.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "nofx-onchain-indexer"})
	})
	api.GET("/onchain/index-status", func(c *gin.Context) {
		resp, err := svc.IndexStatus(c.DefaultQuery("chain", "bsc"), c.Query("address"))
		respond(c, resp, err)
	})
	api.POST("/onchain/early-wallet-flow/index", func(c *gin.Context) {
		var req onchain.EarlyWalletFlowRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "status": onchain.StatusInvalidRequest, "error": "invalid request body"})
			return
		}
		if req.Chain == "" {
			req.Chain = "bsc"
		}
		resp, err := svc.QueueIndex(c.Request.Context(), onchain.IndexTokenRequest{
			Chain:       req.Chain,
			Address:     req.Address,
			Scope:       store.OnchainIndexScopeEarlyWalletWindow,
			IndexSource: req.IndexSource,
		})
		respond(c, resp, err)
	})
	api.GET("/onchain/early-wallet-flow", func(c *gin.Context) {
		seedCount := intQuery(c, "seed_count", 100)
		maxDepth := intQuery(c, "max_depth", 4)
		resp, err := svc.EarlyWalletFlow(c.Request.Context(), onchain.EarlyWalletFlowRequest{
			Chain:       c.DefaultQuery("chain", "bsc"),
			Address:     c.Query("address"),
			IndexSource: c.Query("index_source"),
			SeedCount:   seedCount,
			MaxDepth:    maxDepth,
		})
		respond(c, resp, err)
	})
	api.GET("/onchain/early-wallet-flow/export", func(c *gin.Context) {
		seedCount := intQuery(c, "seed_count", 100)
		maxDepth := intQuery(c, "max_depth", 4)
		resp, err := svc.EarlyWalletFlow(c.Request.Context(), onchain.EarlyWalletFlowRequest{
			Chain:       c.DefaultQuery("chain", "bsc"),
			Address:     c.Query("address"),
			IndexSource: c.Query("index_source"),
			SeedCount:   seedCount,
			MaxDepth:    maxDepth,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
			return
		}
		if resp != nil && resp.Status == onchain.StatusInvalidRequest {
			c.JSON(http.StatusBadRequest, resp)
			return
		}
		c.Header("Content-Type", "text/csv; charset=utf-8")
		c.Header("Content-Disposition", `attachment; filename="early-wallet-flow.csv"`)
		if err := onchain.WriteEarlyWalletFlowResponseCSV(c.Writer, resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		}
	})
	return r
}

func respond(c *gin.Context, body any, err error) {
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}
	status := http.StatusOK
	switch v := body.(type) {
	case *onchain.IndexStatusResponse:
		if v != nil && v.Status == onchain.StatusInvalidRequest {
			status = http.StatusBadRequest
		}
	case *onchain.EarlyWalletFlowResponse:
		if v != nil && v.Status == onchain.StatusInvalidRequest {
			status = http.StatusBadRequest
		}
	}
	c.JSON(status, body)
}

func intQuery(c *gin.Context, key string, fallback int) int {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return fallback
	}
	var out int
	if _, err := fmt.Sscanf(value, "%d", &out); err != nil || out <= 0 {
		return fallback
	}
	return out
}

func runMigrate(args []string) error {
	source := ""
	target := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--source":
			if i+1 < len(args) {
				source = args[i+1]
				i++
			}
		case "--target":
			if i+1 < len(args) {
				target = args[i+1]
				i++
			}
		}
	}
	if source == "" || target == "" {
		return fmt.Errorf("usage: nofx-onchain-indexer migrate --source data/data.db --target data/onchain-indexer.db")
	}
	if dir := filepath.Dir(target); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	src, err := sql.Open("sqlite", source)
	if err != nil {
		return err
	}
	defer src.Close()
	dstStore, err := store.NewOnchainOnly(store.DBConfig{Type: store.DBTypeSQLite, Path: target})
	if err != nil {
		return err
	}
	defer dstStore.Close()
	dst := dstStore.DB()
	for _, table := range []string{
		"onchain_tokens",
		"onchain_pools",
		"onchain_token_transfers",
		"onchain_swaps",
		"onchain_wallet_snapshots",
		"onchain_index_jobs",
		"onchain_local_index_tasks",
	} {
		if err := copyTable(src, dst, table); err != nil {
			return fmt.Errorf("copy %s: %w", table, err)
		}
	}
	return nil
}

func copyTable(src, dst *sql.DB, table string) error {
	rows, err := src.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table)
	if err != nil {
		return err
	}
	exists := rows.Next()
	rows.Close()
	if !exists {
		return nil
	}
	return copyRows(src, dst, table)
}

func copyRows(src, dst *sql.DB, table string) error {
	colsRows, err := src.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	cols := []string{}
	for colsRows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := colsRows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			colsRows.Close()
			return err
		}
		cols = append(cols, name)
	}
	colsRows.Close()
	if len(cols) == 0 {
		return nil
	}
	rows, err := src.Query("SELECT " + strings.Join(cols, ",") + " FROM " + table)
	if err != nil {
		return err
	}
	defer rows.Close()
	placeholders := strings.TrimRight(strings.Repeat("?,", len(cols)), ",")
	stmt, err := dst.Prepare("INSERT OR IGNORE INTO " + table + " (" + strings.Join(cols, ",") + ") VALUES (" + placeholders + ")")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		for i, value := range values {
			if b, ok := value.([]byte); ok {
				values[i] = string(b)
			}
		}
		if _, err := stmt.Exec(values...); err != nil {
			return err
		}
	}
	return rows.Err()
}
