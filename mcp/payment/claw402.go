package payment

import (
	"crypto/ecdsa"
	"fmt"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"

	"nofx/mcp"
	"nofx/mcp/provider"
	"nofx/store"
	"nofx/wallet"
)

const (
	preflightSafetyMultiplier         = 1.5
	preflightReasonerSafetyMultiplier = 4.0

	DefaultClaw402URL   = "https://claw402.ai"
	DefaultClaw402Model = "deepseek-v4-flash"
)

type ErrInsufficientFunds struct {
	Address string
	Balance float64
	Needed  float64
	Model   string
}

func (e *ErrInsufficientFunds) Error() string {
	return fmt.Sprintf(
		"claw402 insufficient USDC: wallet=%s balance=$%.4f needed=$%.4f model=%s",
		shortAddr(e.Address), e.Balance, e.Needed, e.Model,
	)
}

func shortAddr(addr string) string {
	if len(addr) < 10 {
		return addr
	}
	return addr[:6] + "..." + addr[len(addr)-4:]
}

var claw402ModelEndpoints = map[string]string{
	"gpt-5.4":           "/api/v1/ai/openai/chat/5.4",
	"gpt-5.4-pro":       "/api/v1/ai/openai/chat/5.4-pro",
	"gpt-5.3":           "/api/v1/ai/openai/chat/5.3",
	"gpt-5-mini":        "/api/v1/ai/openai/chat/5-mini",
	"claude-opus":       "/api/v1/ai/anthropic/messages/opus",
	"deepseek":          "/api/v1/ai/deepseek/chat",
	"deepseek-reasoner": "/api/v1/ai/deepseek/chat/reasoner",
	"deepseek-v4-flash": "/api/v1/ai/deepseek/v4-flash",
	"deepseek-v4-pro":   "/api/v1/ai/deepseek/v4-pro",
	"qwen-max":          "/api/v1/ai/qwen/chat/max",
	"qwen-plus":         "/api/v1/ai/qwen/chat/plus",
	"qwen-turbo":        "/api/v1/ai/qwen/chat/turbo",
	"qwen-flash":        "/api/v1/ai/qwen/chat/flash",
	"grok-4.1":          "/api/v1/ai/grok/chat/4.1",
	"gemini-3.1-pro":    "/api/v1/ai/gemini/chat/3.1-pro",
	"kimi-k2.5":         "/api/v1/ai/kimi/chat/k2.5",
	"glm-5":             "/api/v1/ai/zhipu/chat",
	"glm-5-turbo":       "/api/v1/ai/zhipu/chat/turbo",
}

func init() {
	mcp.RegisterProvider(mcp.ProviderClaw402, func(opts ...mcp.ClientOption) mcp.AIClient {
		return NewClaw402ClientWithOptions(opts...)
	})
}

type Claw402Client struct {
	*mcp.Client
	privateKey  *ecdsa.PrivateKey
	claudeProxy *provider.ClaudeClient
}

func (c *Claw402Client) BaseClient() *mcp.Client { return c.Client }

func NewClaw402ClientWithOptions(opts ...mcp.ClientOption) mcp.AIClient {
	baseOpts := []mcp.ClientOption{
		mcp.WithProvider(mcp.ProviderClaw402),
		mcp.WithModel(DefaultClaw402Model),
		mcp.WithBaseURL(DefaultClaw402URL),
		mcp.WithTimeout(X402Timeout),
		mcp.WithMaxRetries(1),
	}
	baseClient := mcp.NewClient(append(baseOpts, opts...)...).(*mcp.Client)
	baseClient.UseFullURL = true
	baseClient.BaseURL = DefaultClaw402URL + claw402ModelEndpoints[DefaultClaw402Model]

	c := &Claw402Client{Client: baseClient}
	baseClient.Hooks = c
	return c
}

func (c *Claw402Client) SetAPIKey(apiKey string, _ string, customModel string) {
	hexKey := strings.TrimPrefix(apiKey, "0x")
	privKey, err := crypto.HexToECDSA(hexKey)
	if err != nil {
		c.Log.Warnf("[MCP] Claw402 invalid private key: %v", err)
	} else {
		c.privateKey = privKey
		c.APIKey = apiKey
		addr := crypto.PubkeyToAddress(privKey.PublicKey).Hex()
		c.Log.Infof("[MCP] Claw402 wallet: %s", addr)
	}
	if customModel != "" {
		c.Model = customModel
	}
	endpoint := c.resolveEndpoint()
	c.BaseURL = DefaultClaw402URL + endpoint
	if strings.Contains(endpoint, "/anthropic/") {
		c.claudeProxy = &provider.ClaudeClient{Client: c.Client}
	} else {
		c.claudeProxy = nil
	}
}

func (c *Claw402Client) resolveEndpoint() string {
	if ep, ok := claw402ModelEndpoints[c.Model]; ok {
		return ep
	}
	if strings.HasPrefix(c.Model, "/api/") {
		return c.Model
	}
	return claw402ModelEndpoints[DefaultClaw402Model]
}

func (c *Claw402Client) SetAuthHeader(h http.Header) { X402SetAuthHeader(h) }

func (c *Claw402Client) Call(systemPrompt, userPrompt string) (string, error) {
	if err := c.preflightBalance(); err != nil {
		return "", err
	}
	return X402CallStream(c.Client, c.signPayment, "Claw402", systemPrompt, userPrompt, nil)
}

func (c *Claw402Client) CallWithRequestFull(req *mcp.Request) (*mcp.LLMResponse, error) {
	if err := c.preflightBalance(); err != nil {
		return nil, err
	}
	return X402CallFull(c.Client, c.signPayment, "Claw402", req)
}

func (c *Claw402Client) walletAddress() string {
	if c.privateKey == nil {
		return ""
	}
	return crypto.PubkeyToAddress(c.privateKey.PublicKey).Hex()
}

func (c *Claw402Client) preflightBalance() error {
	addr := c.walletAddress()
	if addr == "" {
		return nil
	}
	balance, err := wallet.QueryUSDCBalanceCached(addr)
	if err != nil {
		c.Log.Warnf("[MCP] Claw402 balance preflight skipped: %v", err)
		return nil
	}
	multiplier := preflightSafetyMultiplier
	if strings.Contains(strings.ToLower(c.Model), "reasoner") {
		multiplier = preflightReasonerSafetyMultiplier
	}
	needed := store.GetModelPrice(c.Model) * multiplier
	if balance < needed {
		return &ErrInsufficientFunds{Address: addr, Balance: balance, Needed: needed, Model: c.Model}
	}
	return nil
}

func (c *Claw402Client) signPayment(paymentHeaderB64 string) (string, error) {
	return SignBasePaymentHeader(c.privateKey, paymentHeaderB64, "Claw402")
}

func stripMaxTokens(body map[string]any) map[string]any {
	if body == nil {
		return body
	}
	delete(body, "max_tokens")
	delete(body, "max_completion_tokens")
	return body
}

func (c *Claw402Client) BuildMCPRequestBody(systemPrompt, userPrompt string) map[string]any {
	if c.claudeProxy != nil {
		return c.claudeProxy.BuildMCPRequestBody(systemPrompt, userPrompt)
	}
	return stripMaxTokens(c.Client.BuildMCPRequestBody(systemPrompt, userPrompt))
}

func (c *Claw402Client) BuildRequestBodyFromRequest(req *mcp.Request) map[string]any {
	if c.claudeProxy != nil {
		return c.claudeProxy.BuildRequestBodyFromRequest(req)
	}
	return stripMaxTokens(c.Client.BuildRequestBodyFromRequest(req))
}

func (c *Claw402Client) ParseMCPResponse(body []byte) (string, error) {
	if c.claudeProxy != nil {
		return c.claudeProxy.ParseMCPResponse(body)
	}
	return c.Client.ParseMCPResponse(body)
}

func (c *Claw402Client) ParseMCPResponseFull(body []byte) (*mcp.LLMResponse, error) {
	if c.claudeProxy != nil {
		return c.claudeProxy.ParseMCPResponseFull(body)
	}
	return c.Client.ParseMCPResponseFull(body)
}

func (c *Claw402Client) BuildUrl() string {
	return c.BaseURL
}

func (c *Claw402Client) BuildRequest(url string, jsonData []byte) (*http.Request, error) {
	return X402BuildRequest(url, jsonData)
}
