package main

import (
	"crypto/sha256"
	"encoding/base64"
	"log"
	"nofx/api"
	"nofx/auth"
	"nofx/config"
	"nofx/crypto"
	"nofx/logger"
	"nofx/manager"
	"nofx/store"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	logger.Init(nil)

	ensurePreviewEnv()
	config.Init()
	cfg := config.Get()
	cfg.DBType = "sqlite"
	if os.Getenv("DB_PATH") == "" {
		cfg.DBPath = filepath.Join(os.TempDir(), "nofx-api-preview.db")
	}
	if os.Getenv("ONCHAIN_INDEXER_ENABLED") == "" {
		cfg.OnchainIndexerEnabled = false
	}

	cryptoService, err := crypto.NewCryptoService()
	if err != nil {
		log.Fatalf("init crypto service: %v", err)
	}
	crypto.SetGlobalCryptoService(cryptoService)
	auth.SetJWTSecret(cfg.JWTSecret)

	st, err := store.NewWithConfig(store.DBConfig{
		Type: store.DBTypeSQLite,
		Path: cfg.DBPath,
	})
	if err != nil {
		log.Fatalf("init preview database: %v", err)
	}
	defer st.Close()
	if err := st.User().EnsureAdmin(); err != nil {
		log.Fatalf("ensure preview admin marker: %v", err)
	}

	server := api.NewServer(manager.NewTraderManager(), st, cryptoService, cfg.APIServerPort)
	log.Printf("NOFX API preview server listening on http://localhost:%d using isolated DB %s", cfg.APIServerPort, cfg.DBPath)
	log.Printf("Preview mode does not load stored traders, start traders, run NOFXi agent, or start Telegram.")
	if err := server.Start(); err != nil {
		log.Fatalf("api preview server stopped: %v", err)
	}
}

func ensurePreviewEnv() {
	if os.Getenv(crypto.EnvDataEncryptionKey) == "" {
		sum := sha256.Sum256([]byte("nofx-api-preview-local-data-key-v1"))
		_ = os.Setenv(crypto.EnvDataEncryptionKey, base64.StdEncoding.EncodeToString(sum[:]))
	}
	if os.Getenv(crypto.EnvRSAPrivateKey) == "" {
		privateKey, _, err := crypto.GenerateKeyPair()
		if err != nil {
			log.Fatalf("generate preview RSA key: %v", err)
		}
		_ = os.Setenv(crypto.EnvRSAPrivateKey, privateKey)
	}
}
