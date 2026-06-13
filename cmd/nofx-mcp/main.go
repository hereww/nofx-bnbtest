package main

import (
	"context"
	"log"
	"net/http"
	"nofx/mcpserver"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	cfg := mcpserver.ConfigFromEnv()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid nofx-mcp config: %v", err)
	}

	apiClient := mcpserver.NewAPIClient(cfg.BaseURL, cfg.NOFXAPIToken, &http.Client{Timeout: cfg.LongActionTimeout})
	mcp := mcpserver.NewServer(cfg, apiClient)
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mcp.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("nofx-mcp listening on http://%s/mcp (NOFX_BASE_URL=%s)", cfg.Addr, cfg.BaseURL)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("nofx-mcp failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("nofx-mcp shutdown error: %v", err)
	}
}
