package mcpserver

import (
	"strings"
)

const redactedValue = "[REDACTED]"

func redactSensitive(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if isSensitiveKey(key) {
				out[key] = redactedValue
				continue
			}
			out[key] = redactSensitive(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = redactSensitive(child)
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	compact := strings.ReplaceAll(normalized, "_", "")
	if strings.Contains(normalized, "api_key") ||
		strings.Contains(compact, "apikey") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "private_key") ||
		strings.Contains(compact, "privatekey") ||
		strings.Contains(normalized, "passphrase") ||
		strings.Contains(normalized, "password") {
		return true
	}
	switch normalized {
	case "token", "access_token", "refresh_token", "id_token", "auth_token", "session_token", "bearer_token", "jwt":
		return true
	default:
		return false
	}
}
