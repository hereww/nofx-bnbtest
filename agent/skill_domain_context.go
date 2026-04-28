package agent

import "strings"

func buildSkillDomainPrimer(lang, skillName string) string {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return ""
	}
	switch skillName {
	case "model_management":
		fields := []string{
			fieldKnowledgeDisplayName("provider", lang),
			displayCatalogFieldName("name", lang),
			displayCatalogFieldName("api_key", lang),
			displayCatalogFieldName("custom_api_url", lang),
			displayCatalogFieldName("custom_model_name", lang),
			displayCatalogFieldName("enabled", lang),
		}
		if lang == "zh" {
			return strings.Join([]string{
				"### 模型配置领域约束",
				"- 当前领域是 AI 模型配置，不是交易所配置。",
				"- provider 指模型厂商，不是交易所类型。",
				"- 关键字段：" + strings.Join(fields, "、"),
				"- 候选 provider：" + modelProviderSummaryList(lang),
				"- 推荐 provider：claw402。claw402 是 NOFXi 官方推荐方案，按次付费，使用 Base 链 EVM 钱包 + USDC 支付。",
				"- 如果用户不确定选哪个 provider，可以优先推荐 claw402 并说明其优势，但绝不能替用户自动选中 claw402；必须先展示完整 provider 选项并让用户自己选择。",
				"- 如果 provider 还没选定，下一步必须先让用户从完整 provider 列表里选一个，不能先收集 API Key、钱包私钥或其他凭证。",
				"- 普通 provider（openai/deepseek/claude 等）通常要填 API Key；custom_model_name 和 custom_api_url 可以留空走默认值。",
				"- claw402 需要钱包私钥，custom_model_name 留空时默认 deepseek。",
				"- blockrun-base / blockrun-sol 走钱包私钥模式，不需要 custom_api_url，custom_model_name 默认 auto。",
			}, "\n")
		}
		return strings.Join([]string{
			"### Model Config Domain Guard",
			"- The current domain is AI model configuration, not exchange configuration.",
			"- provider means the model vendor, not an exchange venue.",
			"- Key fields: " + strings.Join(fields, ", "),
			"- Supported providers: " + modelProviderSummaryList(lang),
			"- Recommended provider: claw402. claw402 is the NOFXi recommended pay-per-use option that uses a Base chain wallet + USDC.",
			"- If the user is unsure which provider to pick, you may recommend claw402 and explain its advantages, but you must not auto-select claw402 for them. Show the full provider options first and let the user choose.",
			"- If provider is still missing, the next step must be to ask the user to choose one from the full provider list. Do not ask for an API key, wallet private key, or other credentials before the provider is chosen.",
			"- Standard providers (openai/deepseek/claude etc.) usually require an API key; `custom_model_name` and `custom_api_url` can be omitted to use defaults.",
			"- claw402 uses a wallet private key and defaults to `deepseek` if `custom_model_name` is omitted.",
			"- blockrun-base / blockrun-sol use wallet private keys, do not need `custom_api_url`, and default to `auto`.",
		}, "\n")
	case "exchange_management":
		fields := []string{
			slotDisplayName("exchange_type", lang),
			displayCatalogFieldName("account_name", lang),
			displayCatalogFieldName("api_key", lang),
			displayCatalogFieldName("secret_key", lang),
			displayCatalogFieldName("passphrase", lang),
			displayCatalogFieldName("enabled", lang),
		}
		if lang == "zh" {
			return strings.Join([]string{
				"### 交易所配置领域约束",
				"- 当前领域是交易所账户配置，不是 AI 模型配置。",
				"- exchange_type 指交易所类型，provider 这个词不应用来代指交易所。",
				"- 关键字段：" + strings.Join(fields, "、"),
				"- 支持的交易所类型：" + strings.Join(enumOptionValues("exchange_management", "exchange_type"), "、"),
			}, "\n")
		}
		return strings.Join([]string{
			"### Exchange Config Domain Guard",
			"- The current domain is exchange account configuration, not AI model configuration.",
			"- exchange_type means the trading venue. Do not use provider to mean an exchange.",
			"- Key fields: " + strings.Join(fields, ", "),
			"- Supported exchange types: " + strings.Join(enumOptionValues("exchange_management", "exchange_type"), ", "),
		}, "\n")
	case "trader_management":
		fields := []string{
			slotDisplayName("name", lang),
			slotDisplayName("exchange", lang),
			slotDisplayName("model", lang),
			slotDisplayName("strategy", lang),
			displayCatalogFieldName("scan_interval_minutes", lang),
		}
		if lang == "zh" {
			return strings.Join([]string{
				"### 交易员配置领域约束",
				"- 交易员是装配层，负责创建、换绑策略/交易所/模型，以及启动、停止、删除、查询。",
				"- 编辑交易员时，默认只处理绑定关系；不要顺手改策略、模型、交易所内部配置。",
				"- 交易员初始余额由系统在创建时自动读取绑定交易所账户净值，不接受手动设置、充值或人为改余额。",
				"- 若用户要改策略参数、模型配置或交易所凭证，应切到对应 management skill。",
				"- 创建交易员时最关键的是：名称、交易所、模型、策略。",
				"- 关键字段：" + strings.Join(fields, "、"),
			}, "\n")
		}
		return strings.Join([]string{
			"### Trader Config Domain Guard",
			"- Traders are the assembly layer: create, rebind strategy/exchange/model, and control lifecycle.",
			"- When editing a trader, default to changing bindings only; do not silently edit the internals of the strategy, model, or exchange.",
			"- Trader initial balance is auto-read from the bound exchange account equity at creation time; do not ask the user to set, top up, or manually edit trader balance.",
			"- If the user wants to change strategy parameters, model config, or exchange credentials, switch to the corresponding management skill.",
			"- The key create fields are name, exchange, model, and strategy.",
			"- Key fields: " + strings.Join(fields, ", "),
		}, "\n")
	case "strategy_management":
		fields := []string{
			slotDisplayName("name", lang),
			displayCatalogFieldName("strategy_type", lang),
			displayCatalogFieldName("source_type", lang),
			displayCatalogFieldName("primary_timeframe", lang),
			displayCatalogFieldName("selected_timeframes", lang),
			displayCatalogFieldName("custom_prompt", lang),
		}
		if lang == "zh" {
			return strings.Join([]string{
				"### 策略配置领域约束",
				"- 策略围绕策略类型、选币来源、时间周期、风险参数和提示词展开。",
				"- source_type 是选币来源，不是交易所，也不是模型。",
				"- strategy_type 选项：ai_trading、grid_trading。",
				"- source_type 选项：static、ai500、oi_top、oi_low、mixed。",
				"- grid_trading 页面交易对选项：BTCUSDT、ETHUSDT、SOLUSDT、BNBUSDT、XRPUSDT、DOGEUSDT。",
				"- grid_trading 页面范围：grid_count 5～50、total_investment 最小 100、leverage 1～5、atr_multiplier 1～5、max_drawdown_pct 5～50、stop_loss_pct 1～20、daily_loss_limit_pct 1～30、direction_bias_ratio 0.55～0.90。",
				"- ai_trading 页面范围：static_coins 最多 10 个、selected_timeframes 最多 4 个、primary_count 10～30、min_confidence 50～100、min_risk_reward_ratio 1～10。",
				"- 排行榜页面选项：duration 为 1h/4h/24h（价格榜还支持 1h,4h,24h），limit 为 5/10/15/20。",
				"- max_positions、仓位价值占比、max_margin_usage、min_position_size 在策略页属于 System enforced / 非普通手动编辑项。",
				"- 关键字段：" + strings.Join(fields, "、"),
			}, "\n")
		}
		return strings.Join([]string{
			"### Strategy Config Domain Guard",
			"- Strategy configuration revolves around strategy type, coin source, timeframes, risk parameters, and prompts.",
			"- source_type means the coin source, not an exchange or model.",
			"- strategy_type options: ai_trading, grid_trading.",
			"- source_type options: static, ai500, oi_top, oi_low, mixed.",
			"- grid_trading symbol dropdown: BTCUSDT, ETHUSDT, SOLUSDT, BNBUSDT, XRPUSDT, DOGEUSDT.",
			"- grid_trading page ranges: grid_count 5-50, total_investment >=100, leverage 1-5, atr_multiplier 1-5, max_drawdown_pct 5-50, stop_loss_pct 1-20, daily_loss_limit_pct 1-30, direction_bias_ratio 0.55-0.90.",
			"- ai_trading page ranges: static_coins at most 10, selected_timeframes at most 4, primary_count 10-30, min_confidence 50-100, min_risk_reward_ratio 1-10.",
			"- Ranking page options: duration 1h/4h/24h (price ranking also supports 1h,4h,24h), limit 5/10/15/20.",
			"- max_positions, position value ratios, max_margin_usage, and min_position_size are System enforced / not ordinary manual fields on the strategy page.",
			"- Key fields: " + strings.Join(fields, ", "),
		}, "\n")
	default:
		return ""
	}
}

func buildManagementDomainPrimer(lang string) string {
	parts := []string{
		buildSkillDomainPrimer(lang, "model_management"),
		buildSkillDomainPrimer(lang, "exchange_management"),
		buildSkillDomainPrimer(lang, "trader_management"),
		buildSkillDomainPrimer(lang, "strategy_management"),
	}
	return strings.Join(filterNonEmptyStrings(parts), "\n\n")
}
