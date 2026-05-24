package agent

import (
	"strings"
	"testing"
)

func TestModelProviderChoicePromptListsSelfConfiguredProviders(t *testing.T) {
	msg := modelProviderChoicePrompt("zh")
	for _, want := range []string{
		"可选模型 provider",
		"DeepSeek",
		"OpenAI",
		"并列可选",
		"blockrun-base",
		"请先告诉我你想用哪个 provider",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected prompt to contain %q, got: %s", want, msg)
		}
	}
	if strings.Contains(msg, "把私钥发给我") {
		t.Fatalf("provider choice prompt should not jump ahead to credential collection: %s", msg)
	}
	if strings.Contains(strings.ToLower(msg), "claw402") {
		t.Fatalf("provider choice prompt should not mention claw402: %s", msg)
	}
}
