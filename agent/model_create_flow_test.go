package agent

import (
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"nofx/store"
)

func TestHandleModelCreateSkillAsksProviderFirst(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agent-model-create.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	a := New(nil, st, DefaultConfig(), slog.Default())
	reply := a.handleModelCreateSkill("default", 42, "zh", "请帮我创建一个模型", skillSession{})

	for _, want := range []string{
		"还缺这些字段：模型提供商",
		"可选模型 provider",
		"并列可选",
		"DeepSeek",
		"OpenAI",
		"blockrun-base",
	} {
		if !strings.Contains(reply, want) {
			t.Fatalf("expected reply to contain %q, got: %s", want, reply)
		}
	}
	for _, unexpected := range []string{
		"还缺这些字段：模型提供商、API Key",
		"还缺这些字段：模型提供商、钱包私钥",
		"还缺这些字段：模型提供商、wallet private key",
	} {
		if strings.Contains(reply, unexpected) {
			t.Fatalf("provider-first reply should not ask for credentials yet: %s", reply)
		}
	}
	if strings.Contains(strings.ToLower(reply), "claw402") {
		t.Fatalf("provider-first reply should not mention claw402: %s", reply)
	}
}

func TestHandleModelCreateSkillUsesCollectedDeepSeekAPIKey(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "agent-model-create-deepseek.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	a := New(nil, st, DefaultConfig(), slog.Default())
	session := skillSession{
		Name:   "model_management",
		Action: "create",
		Phase:  "collecting",
		Fields: map[string]string{
			"provider":          "deepseek",
			"name":              "DeepSeek",
			"api_key":           "sk-deepseek-test",
			"custom_model_name": "deepseek-chat",
		},
	}

	reply := a.handleModelCreateSkill("default", 42, "zh", "继续", session)

	if strings.Contains(reply, "还缺这些字段") {
		t.Fatalf("expected API key to be accepted, got: %s", reply)
	}
	if !strings.Contains(reply, "我先整理了一份模型配置草稿") {
		t.Fatalf("expected draft summary after accepting private key, got: %s", reply)
	}
}
