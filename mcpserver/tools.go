package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(context.Context, map[string]any) (any, error)
}

func buildTools(client *APIClient, cfg Config) map[string]ToolSpec {
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Second
	}
	if cfg.LongActionTimeout <= 0 {
		cfg.LongActionTimeout = 90 * time.Second
	}

	tools := []ToolSpec{
		{
			Name:        "nofx_health",
			Description: "Check the NOFX backend health endpoint.",
			InputSchema: emptySchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return client.Get(ctx, "/api/health", nil, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_list_traders",
			Description: "List the authenticated user's NOFX traders with status and bindings.",
			InputSchema: emptySchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return client.Get(ctx, "/api/my-traders", nil, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_trader_config",
			Description: "Get full configuration for one trader. Requires trader_id from nofx_list_traders.",
			InputSchema: requiredStringSchema("trader_id", "Exact trader_id from nofx_list_traders."),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				traderID, err := requiredString(args, "trader_id")
				if err != nil {
					return nil, err
				}
				return client.Get(ctx, "/api/traders/"+url.PathEscape(traderID)+"/config", nil, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_account",
			Description: "Get account balance and equity for one trader.",
			InputSchema: requiredStringSchema("trader_id", "Exact trader_id from nofx_list_traders."),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return getTraderQuery(ctx, client, "/api/account", args, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_positions",
			Description: "Get current open positions for one trader.",
			InputSchema: requiredStringSchema("trader_id", "Exact trader_id from nofx_list_traders."),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return getTraderQuery(ctx, client, "/api/positions", args, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_position_history",
			Description: "Get closed position history and summary stats for one trader. limit defaults to 20 and is capped at 100.",
			InputSchema: traderLimitSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return getTraderQueryWithLimit(ctx, client, "/api/positions/history", args, cfg.DefaultTimeout, 20, 100)
			},
		},
		{
			Name:        "nofx_get_trades",
			Description: "Get recent trade records for one trader. Optional symbol filter. limit defaults to 20 and is capped at 100.",
			InputSchema: traderSymbolLimitSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				query, err := traderQueryWithLimit(args, 20, 100)
				if err != nil {
					return nil, err
				}
				if symbol := optionalString(args, "symbol"); symbol != "" {
					query["symbol"] = symbol
				}
				return client.Get(ctx, "/api/trades", query, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_orders",
			Description: "Get recent order records for one trader. Optional symbol and status filters. limit defaults to 20 and is capped at 100.",
			InputSchema: traderOrdersSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				query, err := traderQueryWithLimit(args, 20, 100)
				if err != nil {
					return nil, err
				}
				if symbol := optionalString(args, "symbol"); symbol != "" {
					query["symbol"] = symbol
				}
				if status := optionalString(args, "status"); status != "" {
					query["status"] = status
				}
				return client.Get(ctx, "/api/orders", query, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_decisions",
			Description: "Get recent AI decision records for one trader. limit defaults to 20 and is capped at 100.",
			InputSchema: traderLimitSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return getTraderQueryWithLimit(ctx, client, "/api/decisions", args, cfg.DefaultTimeout, 20, 100)
			},
		},
		{
			Name:        "nofx_get_latest_decisions",
			Description: "Get latest AI decisions from the most recent scan cycle for one trader.",
			InputSchema: requiredStringSchema("trader_id", "Exact trader_id from nofx_list_traders."),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return getTraderQuery(ctx, client, "/api/decisions/latest", args, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_list_models",
			Description: "List AI model configs without credential values.",
			InputSchema: emptySchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return client.Get(ctx, "/api/models", nil, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_list_exchanges",
			Description: "List exchange account configs without credential values.",
			InputSchema: emptySchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return client.Get(ctx, "/api/exchanges", nil, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_exchange_account_states",
			Description: "Get exchange connection, balance, and health states.",
			InputSchema: emptySchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return client.Get(ctx, "/api/exchanges/account-state", nil, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_list_strategies",
			Description: "List strategy templates for the authenticated user.",
			InputSchema: emptySchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return client.Get(ctx, "/api/strategies", nil, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_strategy",
			Description: "Get one strategy template by strategy_id.",
			InputSchema: requiredStringSchema("strategy_id", "Exact strategy id from nofx_list_strategies."),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				strategyID, err := requiredString(args, "strategy_id")
				if err != nil {
					return nil, err
				}
				return client.Get(ctx, "/api/strategies/"+url.PathEscape(strategyID), nil, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_default_strategy_config",
			Description: "Get the complete default strategy configuration template.",
			InputSchema: emptySchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return client.Get(ctx, "/api/strategies/default-config", nil, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_strategy_test_run",
			Description: "Run strategy analysis simulation. It only returns generated prompts and optional AI analysis; it never executes trades.",
			InputSchema: strategyTestRunSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				config, ok := args["config"]
				if !ok {
					return nil, fmt.Errorf("config is required")
				}
				body := map[string]any{"config": config}
				if promptVariant := optionalString(args, "prompt_variant"); promptVariant != "" {
					body["prompt_variant"] = promptVariant
				}
				if modelID := optionalString(args, "ai_model_id"); modelID != "" {
					body["ai_model_id"] = modelID
				}
				if runRealAI, ok := optionalBool(args, "run_real_ai"); ok {
					body["run_real_ai"] = runRealAI
				}
				return client.Post(ctx, "/api/strategies/test-run", body, cfg.LongActionTimeout)
			},
		},
		{
			Name:        "nofx_get_klines",
			Description: "Get candlestick data for a symbol. limit defaults to 100 and is capped at 1500.",
			InputSchema: klinesSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				symbol, err := requiredString(args, "symbol")
				if err != nil {
					return nil, err
				}
				limit := optionalInt(args, "limit", 100, 1, 1500)
				query := map[string]string{
					"symbol":   symbol,
					"interval": defaultString(optionalString(args, "interval"), "5m"),
					"exchange": defaultString(optionalString(args, "exchange"), "binance"),
					"limit":    strconv.Itoa(limit),
				}
				return client.Get(ctx, "/api/klines", query, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_list_symbols",
			Description: "List available symbols for an exchange.",
			InputSchema: exchangeOptionalSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				query := map[string]string{}
				if exchange := optionalString(args, "exchange"); exchange != "" {
					query["exchange"] = exchange
				}
				return client.Get(ctx, "/api/symbols", query, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_get_custom_tokens",
			Description: "Get DEX market snapshots for monitored or supplied EVM token contract addresses. These are monitor-only and not trade execution symbols.",
			InputSchema: customTokensSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				query := map[string]string{}
				addresses, err := optionalStringList(args, "addresses")
				if err != nil {
					return nil, err
				}
				if len(addresses) > 0 {
					query["addresses"] = strings.Join(addresses, ",")
				}
				return client.Get(ctx, "/api/custom-tokens", query, cfg.DefaultTimeout)
			},
		},
		{
			Name:        "nofx_analyze_bsc_token",
			Description: "Analyze one BSC token contract address using NOFX on-chain analysis. depth defaults to recent.",
			InputSchema: bscTokenSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				address, err := requiredString(args, "address")
				if err != nil {
					return nil, err
				}
				query := map[string]string{
					"chain":   "bsc",
					"address": address,
					"depth":   defaultString(optionalString(args, "depth"), "recent"),
				}
				return client.Get(ctx, "/api/onchain/token-analysis", query, cfg.LongActionTimeout)
			},
		},
		{
			Name:        "nofx_get_bsc_wallet_graph",
			Description: "Get wallet relationship graph for one BSC token contract address.",
			InputSchema: bscWalletGraphSchema(),
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				address, err := requiredString(args, "address")
				if err != nil {
					return nil, err
				}
				query := map[string]string{
					"chain":   "bsc",
					"address": address,
					"depth":   defaultString(optionalString(args, "depth"), "recent"),
					"limit":   strconv.Itoa(optionalInt(args, "limit", 80, 1, 200)),
				}
				return client.Get(ctx, "/api/onchain/wallet-graph", query, cfg.LongActionTimeout)
			},
		},
	}

	byName := make(map[string]ToolSpec, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	return byName
}

func getTraderQuery(ctx context.Context, client *APIClient, path string, args map[string]any, timeout time.Duration) (any, error) {
	traderID, err := requiredString(args, "trader_id")
	if err != nil {
		return nil, err
	}
	return client.Get(ctx, path, map[string]string{"trader_id": traderID}, timeout)
}

func getTraderQueryWithLimit(ctx context.Context, client *APIClient, path string, args map[string]any, timeout time.Duration, defaultLimit, maxLimit int) (any, error) {
	query, err := traderQueryWithLimit(args, defaultLimit, maxLimit)
	if err != nil {
		return nil, err
	}
	return client.Get(ctx, path, query, timeout)
}

func traderQueryWithLimit(args map[string]any, defaultLimit, maxLimit int) (map[string]string, error) {
	traderID, err := requiredString(args, "trader_id")
	if err != nil {
		return nil, err
	}
	limit := optionalInt(args, "limit", defaultLimit, 1, maxLimit)
	return map[string]string{"trader_id": traderID, "limit": strconv.Itoa(limit)}, nil
}

func requiredString(args map[string]any, key string) (string, error) {
	value := optionalString(args, key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalString(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func optionalBool(args map[string]any, key string) (bool, bool) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return false, false
	}
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		return parsed, err == nil
	default:
		return false, false
	}
}

func optionalInt(args map[string]any, key string, fallback, min, max int) int {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback
	}
	var value int
	switch v := raw.(type) {
	case float64:
		value = int(v)
	case int:
		value = v
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return fallback
		}
		value = int(i)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fallback
		}
		value = i
	default:
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func optionalStringList(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil, nil
		}
		return splitCSV(v), nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out, nil
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if text := strings.TrimSpace(item); text != "" {
				out = append(out, text)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s must be a string or array of strings", key)
	}
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if text := strings.TrimSpace(part); text != "" {
			out = append(out, text)
		}
	}
	return out
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func emptySchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}
}

func requiredStringSchema(name, description string) map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			name: map[string]any{"type": "string", "description": description},
		},
		"required":             []string{name},
		"additionalProperties": false,
	}
}

func traderLimitSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"trader_id": map[string]any{"type": "string"},
			"limit":     map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "default": 20},
		},
		"required":             []string{"trader_id"},
		"additionalProperties": false,
	}
}

func traderSymbolLimitSchema() map[string]any {
	schema := traderLimitSchema()
	schema["properties"].(map[string]any)["symbol"] = map[string]any{"type": "string"}
	return schema
}

func traderOrdersSchema() map[string]any {
	schema := traderSymbolLimitSchema()
	schema["properties"].(map[string]any)["status"] = map[string]any{"type": "string"}
	return schema
}

func strategyTestRunSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"config":         map[string]any{"type": "object", "description": "Complete NOFX StrategyConfig object."},
			"prompt_variant": map[string]any{"type": "string", "default": "balanced"},
			"ai_model_id":    map[string]any{"type": "string"},
			"run_real_ai":    map[string]any{"type": "boolean", "default": false},
		},
		"required":             []string{"config"},
		"additionalProperties": false,
	}
}

func klinesSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"symbol":   map[string]any{"type": "string"},
			"interval": map[string]any{"type": "string", "default": "5m"},
			"exchange": map[string]any{"type": "string", "default": "binance"},
			"limit":    map[string]any{"type": "integer", "minimum": 1, "maximum": 1500, "default": 100},
		},
		"required":             []string{"symbol"},
		"additionalProperties": false,
	}
}

func exchangeOptionalSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"exchange": map[string]any{"type": "string", "default": "hyperliquid"},
		},
		"additionalProperties": false,
	}
}

func customTokensSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"addresses": map[string]any{
				"oneOf": []map[string]any{
					{"type": "string"},
					{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"description": "Optional EVM token contract address list. Omit to use NOFX defaults.",
			},
		},
		"additionalProperties": false,
	}
}

func bscTokenSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"address": map[string]any{"type": "string", "description": "BSC token contract address."},
			"depth":   map[string]any{"type": "string", "enum": []string{"recent", "full"}, "default": "recent"},
		},
		"required":             []string{"address"},
		"additionalProperties": false,
	}
}

func bscWalletGraphSchema() map[string]any {
	schema := bscTokenSchema()
	schema["properties"].(map[string]any)["limit"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 80}
	return schema
}
