package main

import (
	"context"
	"log"
	"net/http"
	"nofx/datagateway"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	cfg := datagateway.Config{
		Addr:            env("DATA_GATEWAY_ADDR", ":8090"),
		Token:           strings.TrimSpace(os.Getenv("DATA_GATEWAY_TOKEN")),
		DBPath:          env("DATA_GATEWAY_DB_PATH", "data/data-gateway.db"),
		RefreshInterval: durationEnv("DATA_GATEWAY_REFRESH_INTERVAL", time.Minute),
	}

	store, err := datagateway.OpenStore(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to open data gateway store: %v", err)
	}
	defer store.Close()

	svc := datagateway.NewService(cfg, store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.Start(ctx)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           datagateway.NewRouter(svc, cfg.Token),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("nofx-data-gateway listening on %s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("data gateway server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("data gateway shutdown error: %v", err)
	}
}

func env(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
