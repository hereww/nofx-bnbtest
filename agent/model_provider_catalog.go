package agent

import (
	"fmt"
	"strings"
)

type modelProviderSpec struct {
	ID                     string
	DisplayName            string
	DefaultModel           string
	CredentialLabelZH      string
	CredentialLabelEN      string
	SupportsCustomAPIURL   bool
	SupportsCustomModel    bool
	UsesWalletCredential   bool
	Recommended            bool
	RecommendedModelHints  []string
}

func supportedModelProviders() []modelProviderSpec {
	return []modelProviderSpec{
		{ID: "deepseek", DisplayName: "DeepSeek", DefaultModel: "deepseek-chat", CredentialLabelZH: "API Key", CredentialLabelEN: "API key", SupportsCustomAPIURL: true, SupportsCustomModel: true},
		{ID: "qwen", DisplayName: "Qwen", DefaultModel: "qwen3-max", CredentialLabelZH: "API Key", CredentialLabelEN: "API key", SupportsCustomAPIURL: true, SupportsCustomModel: true},
		{ID: "openai", DisplayName: "OpenAI", DefaultModel: "gpt-5.1", CredentialLabelZH: "API Key", CredentialLabelEN: "API key", SupportsCustomAPIURL: true, SupportsCustomModel: true},
		{ID: "claude", DisplayName: "Claude", DefaultModel: "claude-opus-4-6", CredentialLabelZH: "API Key", CredentialLabelEN: "API key", SupportsCustomAPIURL: true, SupportsCustomModel: true},
		{ID: "gemini", DisplayName: "Google Gemini", DefaultModel: "gemini-3-pro-preview", CredentialLabelZH: "API Key", CredentialLabelEN: "API key", SupportsCustomAPIURL: true, SupportsCustomModel: true},
		{ID: "grok", DisplayName: "Grok (xAI)", DefaultModel: "grok-3-latest", CredentialLabelZH: "API Key", CredentialLabelEN: "API key", SupportsCustomAPIURL: true, SupportsCustomModel: true},
		{ID: "kimi", DisplayName: "Kimi (Moonshot)", DefaultModel: "moonshot-v1-auto", CredentialLabelZH: "API Key", CredentialLabelEN: "API key", SupportsCustomAPIURL: true, SupportsCustomModel: true},
		{ID: "minimax", DisplayName: "MiniMax", DefaultModel: "MiniMax-M2.5", CredentialLabelZH: "API Key", CredentialLabelEN: "API key", SupportsCustomAPIURL: true, SupportsCustomModel: true},
		{
			ID:                    "blockrun-base",
			DisplayName:           "BlockRun (Base Wallet)",
			DefaultModel:          "auto",
			CredentialLabelZH:     "钱包私钥",
			CredentialLabelEN:     "wallet private key",
			SupportsCustomAPIURL:  false,
			SupportsCustomModel:   false,
			UsesWalletCredential:  true,
		},
		{
			ID:                    "blockrun-sol",
			DisplayName:           "BlockRun (Solana Wallet)",
			DefaultModel:          "auto",
			CredentialLabelZH:     "钱包私钥",
			CredentialLabelEN:     "wallet private key",
			SupportsCustomAPIURL:  false,
			SupportsCustomModel:   false,
			UsesWalletCredential:  true,
		},
	}
}

func modelProviderSpecByID(provider string) (modelProviderSpec, bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	for _, spec := range supportedModelProviders() {
		if spec.ID == provider {
			return spec, true
		}
	}
	return modelProviderSpec{}, false
}

func supportedModelProviderIDs() []string {
	specs := supportedModelProviders()
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.ID)
	}
	return out
}

func defaultModelNameForProvider(provider string) string {
	spec, ok := modelProviderSpecByID(provider)
	if !ok {
		return ""
	}
	return strings.TrimSpace(spec.DefaultModel)
}

func defaultModelConfigName(provider string) string {
	spec, ok := modelProviderSpecByID(provider)
	if !ok {
		provider = strings.TrimSpace(provider)
		if provider == "" {
			return ""
		}
		return provider + " AI"
	}
	return spec.DisplayName
}

func modelProviderSupportsCustomAPIURL(provider string) bool {
	spec, ok := modelProviderSpecByID(provider)
	return ok && spec.SupportsCustomAPIURL
}

func modelProviderSupportsCustomModel(provider string) bool {
	spec, ok := modelProviderSpecByID(provider)
	return ok && spec.SupportsCustomModel
}

func modelProviderCredentialLabel(lang, provider string) string {
	spec, ok := modelProviderSpecByID(provider)
	if !ok {
		if lang == "zh" {
			return "API Key"
		}
		return "API key"
	}
	if lang == "zh" {
		return spec.CredentialLabelZH
	}
	return spec.CredentialLabelEN
}

func modelProviderSummaryList(lang string) string {
	parts := make([]string, 0, len(supportedModelProviders()))
	for _, spec := range supportedModelProviders() {
		if lang == "zh" {
			item := fmt.Sprintf("%s（默认 %s）", spec.ID, spec.DefaultModel)
			if spec.Recommended {
				item += " [推荐]"
			}
			parts = append(parts, item)
			continue
		}
		item := fmt.Sprintf("%s (default %s)", spec.ID, spec.DefaultModel)
		if spec.Recommended {
			item += " [recommended]"
		}
		parts = append(parts, item)
	}
	if lang == "zh" {
		return strings.Join(parts, "、")
	}
	return strings.Join(parts, ", ")
}

func modelProviderChoicePrompt(lang string) string {
	if lang == "zh" {
		return "可选模型 provider：" + modelProviderSummaryList(lang) + "。这些 provider 是并列可选的：你可以选择 DeepSeek / OpenAI / Claude / Gemini / Qwen / Kimi / Grok / MiniMax 这类 API Key provider，或者选 `blockrun-base` / `blockrun-sol` 这类钱包 provider。请先告诉我你想用哪个 provider。"
	}
	return "Available model providers: " + modelProviderSummaryList(lang) + ". These providers are peer options: choose an API-key provider such as DeepSeek / OpenAI / Claude / Gemini / Qwen / Kimi / Grok / MiniMax, or a wallet-based provider such as `blockrun-base` / `blockrun-sol`. Tell me which provider you want first."
}

func modelProviderDetailedGuidance(lang, provider string) string {
	spec, ok := modelProviderSpecByID(provider)
	if !ok {
		return ""
	}
	if lang == "zh" {
		lines := []string{
			fmt.Sprintf("你现在选的是 %s。", spec.DisplayName),
			fmt.Sprintf("- 默认模型名：%s", spec.DefaultModel),
			fmt.Sprintf("- 凭证类型：%s", spec.CredentialLabelZH),
		}
		if spec.SupportsCustomModel {
			lines = append(lines, "- `custom_model_name` 可选；留空时默认用上面的默认模型。")
		} else {
			lines = append(lines, "- 这个 provider 不需要单独填写 `custom_model_name`。")
		}
		if spec.SupportsCustomAPIURL {
			lines = append(lines, "- `custom_api_url` 可选；留空时使用官方默认地址。")
		} else {
			lines = append(lines, "- 这个 provider 不需要 `custom_api_url`。")
		}
		if len(spec.RecommendedModelHints) > 0 {
			lines = append(lines, "- 常见可选模型："+strings.Join(spec.RecommendedModelHints, "、"))
		}
		return strings.Join(lines, "\n")
	}
	lines := []string{
		fmt.Sprintf("You selected %s.", spec.DisplayName),
		fmt.Sprintf("- Default model: %s", spec.DefaultModel),
		fmt.Sprintf("- Credential type: %s", spec.CredentialLabelEN),
	}
	if spec.SupportsCustomModel {
		lines = append(lines, "- `custom_model_name` is optional; if omitted, the default model will be used.")
	} else {
		lines = append(lines, "- This provider does not need a separate `custom_model_name`.")
	}
	if spec.SupportsCustomAPIURL {
		lines = append(lines, "- `custom_api_url` is optional; if omitted, the official default endpoint will be used.")
	} else {
		lines = append(lines, "- This provider does not need `custom_api_url`.")
	}
	if len(spec.RecommendedModelHints) > 0 {
		lines = append(lines, "- Common model choices: "+strings.Join(spec.RecommendedModelHints, ", "))
	}
	return strings.Join(lines, "\n")
}

func modelProviderCredentialGuidance(lang, provider string) string {
	spec, ok := modelProviderSpecByID(provider)
	if !ok {
		return ""
	}
	provider = strings.TrimSpace(spec.ID)
	if lang == "zh" {
		switch provider {
		case "blockrun-base":
			return "blockrun-base 这里要填的是 Base 链 EVM 钱包私钥。你可以从现有 EVM 钱包导出私钥后发我。"
		case "blockrun-sol":
			return "blockrun-sol 这里要填的是 Solana 钱包私钥。你可以从现有 Solana 钱包导出私钥后发我。"
		default:
			return fmt.Sprintf("%s 这里要填的是 %s。你把完整值发我就行，我会继续当前模型草稿。", spec.DisplayName, spec.CredentialLabelZH)
		}
	}
	switch provider {
	case "blockrun-base":
		return "For blockrun-base, this field expects a Base-chain EVM wallet private key. You can export it from an existing EVM wallet and send it to me."
	case "blockrun-sol":
		return "For blockrun-sol, this field expects a Solana wallet private key. You can export it from an existing Solana wallet and send it to me."
	default:
		return fmt.Sprintf("For %s, this field expects your %s. Send me the full value and I'll continue the current model draft.", spec.DisplayName, spec.CredentialLabelEN)
	}
}
