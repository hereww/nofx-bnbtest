package agent

import (
	"log/slog"
	"path/filepath"
	"testing"

	"nofx/store"
)

func TestLoadAIClientFromStoreUserPrefersLatestEnabledModel(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agent-model-selection.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := st.AIModel().UpdateWithName("default", "default_openai", "OpenAI", true, "sk-test", "", "gpt-5.2"); err != nil {
		t.Fatalf("create openai model: %v", err)
	}
	if err := st.AIModel().UpdateWithName("default", "default_deepseek", "DeepSeek", true, "sk-deepseek", "", "deepseek-chat"); err != nil {
		t.Fatalf("create deepseek model: %v", err)
	}

	a := New(nil, st, DefaultConfig(), slog.Default())
	_, modelName, ok := a.loadAIClientFromStoreUser("default")
	if !ok {
		t.Fatalf("expected model selection to succeed")
	}
	if modelName != "deepseek-chat" {
		t.Fatalf("expected latest enabled model to be selected, got %q", modelName)
	}
}
