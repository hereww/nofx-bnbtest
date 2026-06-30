package telegram

import (
	"nofx/api"
	"nofx/config"
	"nofx/logger"
	"nofx/mcp"
	_ "nofx/mcp/provider"
	"nofx/store"
	"nofx/telegram/agent"
	"os"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Start initializes and runs the Telegram bot in a blocking supervisor loop.
// Supports hot-reload: when a signal is sent on reloadCh, the bot restarts
// with the latest token (re-read from DB or env). Must be called as a goroutine from main.go.
func Start(cfg *config.Config, st *store.Store, reloadCh <-chan struct{}) {
	for {
		token := resolveToken(cfg, st)
		if token == "" {
			logger.Info("Telegram bot disabled (no token configured), waiting for reload signal...")
			<-reloadCh
			continue
		}

		stopped := runBot(token, cfg, st)
		if !stopped {
			return
		}

		select {
		case <-reloadCh:
			logger.Info("Reloading Telegram bot with new token...")
		}
	}
}

// resolveToken returns the bot token from DB (configured via Web UI).
func resolveToken(cfg *config.Config, st *store.Store) string {
	dbCfg, err := st.TelegramConfig().Get()
	if err == nil && dbCfg.BotToken != "" {
		return dbCfg.BotToken
	}
	return ""
}

// runBot runs the bot until the updates channel closes (clean stop → true) or a fatal error (false).
func runBot(token string, cfg *config.Config, st *store.Store) bool {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		logger.Errorf("Telegram bot failed to start: %v", err)
		return false
	}
	logger.Infof("Telegram bot @%s started (groups=%t, privacy_mode=%t)",
		bot.Self.UserName, bot.Self.CanJoinGroups, !bot.Self.CanReadAllGroupMessages)

	// Preserve the existing private binding and optionally allow one group.
	// In a group, only the Telegram user who owns the private binding may operate the bot.
	var privateChatID, ownerUserID, groupChatID int64
	if tgCfg, err := st.TelegramConfig().Get(); err == nil {
		privateChatID = tgCfg.ChatID
		ownerUserID = tgCfg.UserID
		groupChatID = tgCfg.GroupChatID
		// Legacy private bindings predate user_id. A private chat ID equals its user's ID.
		if ownerUserID == 0 && privateChatID > 0 {
			ownerUserID = privateChatID
		}
	}

	// botUserID / botToken / agents are resolved lazily and refresh when user registers.
	var (
		botUserID    string
		botUserEmail string
		botToken     string
		agents       *agent.Manager
		guestAgents  *agent.Manager
	)

	resolveBotUser := func() bool {
		users, err := st.User().GetAll()
		if err != nil || len(users) == 0 {
			return false
		}
		u := users[0]
		if u.ID == botUserID {
			return true
		}
		newToken, err := agent.GenerateBotToken(u.ID)
		if err != nil {
			logger.Errorf("Failed to generate bot JWT for user %s: %v", u.ID, err)
			return false
		}
		prev := botUserID
		botUserID = u.ID
		botUserEmail = u.Email
		botToken = newToken
		agents = agent.NewManager(cfg.APIServerPort, botToken, botUserEmail, botUserID,
			func() mcp.AIClient { return newLLMClient(st, botUserID) },
			api.GetAPIDocs(),
		)
		guestAgents = agent.NewReadOnlyManager(
			func() mcp.AIClient { return newLLMClient(st, botUserID) },
		)
		if prev == "" {
			logger.Infof("Bot: resolved user %s (%s)", botUserID, botUserEmail)
		} else {
			logger.Infof("Bot: user changed → %s (%s)", botUserID, botUserEmail)
		}
		return true
	}
	resolveBotUser()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// awaitingLang is scoped per authorized chat.
	awaitingLang := make(map[int64]bool)

	for update := range updates {
		if update.Message == nil || update.Message.Chat == nil {
			continue
		}
		message := update.Message
		chatID := message.Chat.ID
		senderID := int64(0)
		if message.From != nil {
			senderID = message.From.ID
		}
		isPrivate := message.Chat.IsPrivate()
		isGroup := message.Chat.IsGroup() || message.Chat.IsSuperGroup()
		command := commandForBot(message, bot.Self.UserName)
		text := strings.TrimSpace(message.Text)
		isOwner := ownerUserID != 0 && senderID == ownerUserID
		authorizedPrivate := isPrivate && chatID == privateChatID && isOwner
		boundGroup := isGroup && chatID == groupChatID
		authorizedGroup := boundGroup && isOwner

		// ── Language selection (triggered only by /lang) ──────────────────────
		if awaitingLang[chatID] && (authorizedPrivate || authorizedGroup) {
			if lang := parseLangChoice(text); lang != "" {
				delete(awaitingLang, chatID)
				st.TelegramConfig().SetLanguage(lang) //nolint:errcheck
				sendMarkdownMsg(bot, chatID, statusMsg(st, botUserID, cfg.APIServerPort, lang))
			} else {
				sendMarkdownMsg(bot, chatID, langMenuMsg())
			}
			continue
		}

		// ── /start ────────────────────────────────────────────────────────────
		if command == "start" {
			resolveBotUser()
			if botUserID == "" {
				sendMsg(bot, chatID,
					"No account found.\nOpen the web dashboard to register, then send /start.")
				continue
			}
			if isGroup {
				if groupChatID == chatID && isOwner {
					agents.Reset(chatID)
					lang := st.TelegramConfig().GetLanguage()
					sendMarkdownMsg(bot, chatID, statusMsg(st, botUserID, cfg.APIServerPort, lang))
				} else if groupChatID == chatID {
					sendMsg(bot, chatID, "本群已接入 NOFX。发送 /ask@"+bot.Self.UserName+" 你的问题，或直接 @Bot 对话。")
				} else if privateChatID == 0 {
					sendMsg(bot, chatID, "请先私聊 Bot 发送 /start 绑定管理员，再回群发送 /bindgroup@"+bot.Self.UserName+"。")
				} else if !isOwner {
					sendMsg(bot, chatID, "只有已绑定的 Telegram 管理员可以把本群接入交易系统。")
				} else {
					sendMsg(bot, chatID, "请发送 /bindgroup@"+bot.Self.UserName+" 绑定本群。")
				}
				continue
			}
			if !isPrivate || senderID == 0 {
				continue
			}
			if privateChatID == 0 {
				username := telegramUsername(message.From)
				if err := st.TelegramConfig().BindUser(chatID, senderID, username); err != nil {
					logger.Errorf("Failed to bind Telegram user: %v", err)
					sendMsg(bot, chatID, "Binding failed. Please try again.")
					continue
				}
				privateChatID = chatID
				ownerUserID = senderID
				isOwner = true
				authorizedPrivate = true
				logger.Infof("Telegram owner bound to %s (userID: %d, chatID: %d)", username, senderID, chatID)
			} else if chatID != privateChatID || !isOwner {
				sendMsg(bot, chatID, "This bot is already bound to another account.")
				continue
			} else {
				// Backfill the owner identity for deployments created before group support.
				if tgCfg, err := st.TelegramConfig().Get(); err == nil && tgCfg.UserID == 0 {
					st.TelegramConfig().BindUser(chatID, senderID, telegramUsername(message.From)) //nolint:errcheck
				}
				agents.Reset(chatID)
			}
			lang := st.TelegramConfig().GetLanguage()
			sendMarkdownMsg(bot, chatID, statusMsg(st, botUserID, cfg.APIServerPort, lang))
			continue
		}

		// ── Group binding ─────────────────────────────────────────────────────
		if command == "bindgroup" {
			if !isGroup {
				sendMsg(bot, chatID, "请在需要接入的 Telegram 群里发送此命令。")
				continue
			}
			if privateChatID == 0 {
				sendMsg(bot, chatID, "请先私聊 Bot 发送 /start 绑定管理员。")
				continue
			}
			if !isOwner {
				sendMsg(bot, chatID, "只有已绑定的 Telegram 管理员可以执行此操作。")
				continue
			}
			if err := st.TelegramConfig().BindGroup(chatID, senderID, message.Chat.Title); err != nil {
				logger.Errorf("Failed to bind Telegram group: %v", err)
				sendMsg(bot, chatID, "群聊绑定失败，请稍后重试。")
				continue
			}
			groupChatID = chatID
			authorizedGroup = true
			agents.Reset(chatID)
			logger.Infof("Telegram group bound (chatID: %d, ownerUserID: %d, title: %q)", chatID, senderID, message.Chat.Title)
			sendMsg(bot, chatID,
				"✅ 本群已接入 NOFX。\n\n"+
					"所有群成员都可以发送 /ask@"+bot.Self.UserName+" 你的问题，或直接 @Bot 对话；之后回复 Bot 的消息即可继续。\n"+
					"群成员使用独立只读会话；只有已绑定管理员可以访问账户和控制交易系统。")
			continue
		}

		if command == "unbindgroup" {
			if !authorizedGroup {
				if isGroup {
					sendMsg(bot, chatID, "只有本群已绑定的 Telegram 管理员可以执行此操作。")
				}
				continue
			}
			if err := st.TelegramConfig().UnbindGroup(); err != nil {
				logger.Errorf("Failed to unbind Telegram group: %v", err)
				sendMsg(bot, chatID, "群聊解绑失败，请稍后重试。")
				continue
			}
			groupChatID = 0
			delete(awaitingLang, chatID)
			logger.Infof("Telegram group unbound (chatID: %d, ownerUserID: %d)", chatID, senderID)
			sendMsg(bot, chatID, "本群已与 NOFX 解绑，私聊绑定仍然保留。")
			continue
		}

		// ── /lang ─────────────────────────────────────────────────────────────
		if command == "lang" {
			if !authorizedPrivate && !authorizedGroup {
				continue
			}
			awaitingLang[chatID] = true
			sendMarkdownMsg(bot, chatID, langMenuMsg())
			continue
		}

		// ── /help ─────────────────────────────────────────────────────────────
		if command == "help" {
			if !authorizedPrivate && !boundGroup {
				continue
			}
			lang := st.TelegramConfig().GetLanguage()
			if boundGroup && !isOwner {
				sendMarkdownMsg(bot, chatID, guestHelpMsg(lang))
			} else {
				sendMarkdownMsg(bot, chatID, helpMsg(lang))
			}
			continue
		}

		// ── Access control ────────────────────────────────────────────────────
		if isPrivate {
			if privateChatID == 0 {
				sendMsg(bot, chatID, "Send /start first.")
				continue
			}
			if !authorizedPrivate {
				sendMsg(bot, chatID, "Unauthorized.")
				continue
			}
			if command == "ask" {
				text = strings.TrimSpace(message.CommandArguments())
			}
		} else if isGroup {
			if !boundGroup {
				// Ignore ordinary group traffic. This avoids leaking account state or
				// turning the bot into a noisy participant in an unbound group.
				continue
			}
			var addressed bool
			text, addressed = groupPrompt(message, &bot.Self)
			if !addressed {
				continue
			}
		} else {
			continue
		}
		if text == "" {
			if command == "ask" {
				sendMsg(bot, chatID, "用法：/ask@"+bot.Self.UserName+" 你的问题")
			}
			continue
		}

		// ── Refresh user before every AI call ────────────────────────────────
		resolveBotUser()
		if botUserID == "" {
			sendMsg(bot, chatID, "No account found. Open the web dashboard to register.")
			continue
		}

		// ── Guard: verify an AI model is available ────────────────────────────
		if newLLMClient(st, botUserID) == nil {
			if isOwner {
				lang := st.TelegramConfig().GetLanguage()
				sendMarkdownMsg(bot, chatID, statusMsg(st, botUserID, cfg.APIServerPort, lang))
			} else {
				sendMsg(bot, chatID, "AI 助手暂不可用，请联系群管理员检查模型配置。")
			}
			continue
		}

		// ── AI agent ─────────────────────────────────────────────────────────
		conversationID := senderID
		if conversationID == 0 && message.SenderChat != nil {
			conversationID = message.SenderChat.ID
		}
		if conversationID == 0 {
			conversationID = int64(message.MessageID)
		}
		go func(chatID, conversationID int64, messageID int, groupMessage, fullAccess bool, text string) {
			placeholder := tgbotapi.NewMessage(chatID, "⏳")
			if groupMessage {
				placeholder.ReplyToMessageID = messageID
			}
			sent, err := bot.Send(placeholder)
			placeholderID := 0
			if err == nil {
				placeholderID = sent.MessageID
			}

			var (
				mu       sync.Mutex
				lastEdit time.Time
			)
			onChunk := func(accumulated string) {
				if placeholderID == 0 {
					return
				}
				mu.Lock()
				defer mu.Unlock()
				if accumulated != "⏳" && time.Since(lastEdit) < time.Second {
					return
				}
				lastEdit = time.Now()
				edit := tgbotapi.NewEditMessageText(chatID, placeholderID, accumulated)
				bot.Send(edit) //nolint:errcheck
			}

			var reply string
			if fullAccess {
				reply = agents.Run(chatID, text, onChunk)
			} else {
				reply = guestAgents.Run(conversationID, text, onChunk)
			}

			if placeholderID != 0 {
				edit := tgbotapi.NewEditMessageText(chatID, placeholderID, reply)
				edit.ParseMode = "Markdown"
				if _, err := bot.Send(edit); err != nil {
					edit2 := tgbotapi.NewEditMessageText(chatID, placeholderID, reply)
					bot.Send(edit2) //nolint:errcheck
				}
			} else {
				msg := tgbotapi.NewMessage(chatID, reply)
				if groupMessage {
					msg.ReplyToMessageID = messageID
				}
				msg.ParseMode = "Markdown"
				if _, err := bot.Send(msg); err != nil {
					msg.ParseMode = ""
					bot.Send(msg) //nolint:errcheck
				}
			}
		}(chatID, conversationID, message.MessageID, isGroup, isOwner, text)
	}

	return true
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func sendMsg(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	bot.Send(msg) //nolint:errcheck
}

func sendMarkdownMsg(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	if _, err := bot.Send(msg); err != nil {
		plain := tgbotapi.NewMessage(chatID, text)
		bot.Send(plain) //nolint:errcheck
	}
}

func commandForBot(message *tgbotapi.Message, botUsername string) string {
	if message == nil || !message.IsCommand() {
		return ""
	}
	commandWithAt := message.CommandWithAt()
	if at := strings.LastIndex(commandWithAt, "@"); at >= 0 {
		if !strings.EqualFold(commandWithAt[at+1:], botUsername) {
			return ""
		}
	}
	return strings.ToLower(message.Command())
}

func groupPrompt(message *tgbotapi.Message, bot *tgbotapi.User) (string, bool) {
	if message == nil || bot == nil {
		return "", false
	}

	if commandForBot(message, bot.UserName) == "ask" {
		return strings.TrimSpace(message.CommandArguments()), true
	}

	text := strings.TrimSpace(message.Text)
	if text == "" {
		return "", false
	}

	repliedToBot := message.ReplyToMessage != nil &&
		message.ReplyToMessage.From != nil &&
		message.ReplyToMessage.From.ID == bot.ID

	mention := "@" + bot.UserName
	mentioned := bot.UserName != "" && strings.Contains(strings.ToLower(text), strings.ToLower(mention))
	if !repliedToBot && !mentioned {
		return "", false
	}
	if mentioned {
		text = removeFold(text, mention)
	}
	return strings.TrimSpace(text), true
}

func removeFold(text, target string) string {
	if target == "" {
		return text
	}
	for {
		index := strings.Index(strings.ToLower(text), strings.ToLower(target))
		if index < 0 {
			return text
		}
		text = text[:index] + text[index+len(target):]
	}
}

func telegramUsername(user *tgbotapi.User) string {
	if user == nil {
		return ""
	}
	if user.UserName != "" {
		return "@" + user.UserName
	}
	return user.String()
}

// ── LLM client ───────────────────────────────────────────────────────────────

func newLLMClient(st *store.Store, userID string) mcp.AIClient {
	// 1. Prefer the model explicitly configured for Telegram (Settings → Telegram → AI Model)
	if tgCfg, err := st.TelegramConfig().Get(); err == nil && tgCfg.ModelID != "" {
		if model, err := st.AIModel().Get(userID, tgCfg.ModelID); err == nil && model.Enabled {
			apiKey := string(model.APIKey)
			if apiKey != "" {
				client := clientForProvider(model.Provider)
				client.SetAPIKey(apiKey, model.CustomAPIURL, model.CustomModelName)
				if isUSDCProvider(model.Provider) {
					logger.Infof("Telegram agent: provider=%s (USDC payment) user=%s", model.Provider, userID)
				} else {
					logger.Infof("Telegram agent: provider=%s user=%s", model.Provider, userID)
				}
				return client
			}
		}
	}

	// 2. Fall back to first enabled model
	if model, err := st.AIModel().GetDefault(userID); err == nil {
		apiKey := string(model.APIKey)
		if apiKey != "" {
			client := clientForProvider(model.Provider)
			client.SetAPIKey(apiKey, model.CustomAPIURL, model.CustomModelName)
			if isUSDCProvider(model.Provider) {
				logger.Infof("Telegram agent: provider=%s (USDC payment) user=%s", model.Provider, userID)
			} else {
				logger.Infof("Telegram agent: provider=%s user=%s", model.Provider, userID)
			}
			return client
		}
	}

	// 3. Environment variable fallback
	for _, pair := range []struct{ provider, key, url string }{
		{"deepseek", os.Getenv("DEEPSEEK_API_KEY"), mcp.DefaultDeepSeekBaseURL},
		{"openai", os.Getenv("OPENAI_API_KEY"), ""},
		{"claude", os.Getenv("ANTHROPIC_API_KEY"), ""},
	} {
		if pair.key != "" {
			client := clientForProvider(pair.provider)
			client.SetAPIKey(pair.key, pair.url, "")
			return client
		}
	}
	return nil
}

// isUSDCProvider returns true for providers that pay per call with USDC.
func isUSDCProvider(provider string) bool {
	return provider == "blockrun-base"
}

func clientForProvider(provider string) mcp.AIClient {
	client := mcp.NewAIClientByProvider(provider)
	if client == nil {
		client = mcp.NewAIClientByProvider("deepseek")
	}
	return client
}

// ── Status message ────────────────────────────────────────────────────────────

// statusMsg is the single entry-point message shown after /start.
// It checks what's configured and shows either a setup prompt or the ready state.
func statusMsg(st *store.Store, userID string, apiPort int, lang string) string {
	webURL := "http://localhost:3000"

	// Determine what's missing.
	hasModel := false
	if _, err := st.AIModel().GetDefault(userID); err == nil {
		hasModel = true
	}

	hasExchange := false
	if exchanges, err := st.Exchange().List(userID); err == nil {
		for _, e := range exchanges {
			if e.Enabled {
				hasExchange = true
				break
			}
		}
	}

	if !hasModel || !hasExchange {
		missing := ""
		if lang == "zh" {
			if !hasModel {
				missing += "\n❌ AI 模型 → 设置 → AI 模型 → 添加"
			}
			if !hasExchange {
				missing += "\n❌ 交易所 → 设置 → 交易所 → 添加"
			}
			return "⚙️ *需要完成初始配置*\n\n打开 Web 管理界面完成配置：\n→ " + webURL + "\n" + missing + "\n\n配置完成后发送 /start"
		}
		if !hasModel {
			missing += "\n❌ AI Model → Settings → AI Models → Add"
		}
		if !hasExchange {
			missing += "\n❌ Exchange → Settings → Exchanges → Add"
		}
		return "⚙️ *Setup required*\n\nOpen the web dashboard to complete setup:\n→ " + webURL + "\n" + missing + "\n\nSend /start when done."
	}

	// All configured — show ready state.
	if lang == "zh" {
		return `✅ *NOFX 就绪，开始交易吧！*

直接告诉我你想做什么：

📊 "查看我的持仓"
💰 "账户余额多少"
🤖 "帮我创建 BTC 趋势策略并启动"
⏹ "停止所有交易员"

/help 查看更多 · /lang 切换语言`
	}
	return `✅ *NOFX is ready!*

Just tell me what you want:

📊 "Show my positions"
💰 "What's my balance?"
🤖 "Create a BTC trend strategy and start it"
⏹ "Stop all traders"

/help for more · /lang to change language`
}

// ── Language ──────────────────────────────────────────────────────────────────

func langMenuMsg() string {
	return "🌐 *Choose your language*\n\n1 — English\n2 — 中文\n\nReply with 1 or 2"
}

func parseLangChoice(text string) string {
	switch strings.TrimSpace(text) {
	case "1", "en", "EN", "English", "english":
		return "en"
	case "2", "zh", "ZH", "中文", "chinese", "Chinese":
		return "zh"
	}
	return ""
}

// ── Help ──────────────────────────────────────────────────────────────────────

func helpMsg(lang string) string {
	if lang == "zh" {
		return `*NOFX 使用指南*

*查询*
• "查看我的持仓"
• "账户余额多少"
• "列出我的交易员"

*创建 & 启动*
• "帮我创建 BTC 趋势策略并跑起来"
• "保守型策略，只交易 BTC 和 ETH"

*控制*
• "启动交易员"
• "暂停交易员"
• "停止所有交易"

*命令*
/start — 刷新状态
/lang  — 切换语言
/help  — 帮助
/bindgroup — 在群内绑定当前群
/ask 问题 — 在群内开始对话
/unbindgroup — 解绑当前群

群内所有成员都可以对话；只有已绑定管理员可以访问账户和操作交易系统。`
	}
	return `*NOFX Help*

*Query*
• "Show my positions"
• "What's my balance?"
• "List my traders"

*Create & start*
• "Create a BTC trend strategy and start it"
• "Conservative strategy, BTC and ETH only"

*Control*
• "Start trader"
• "Stop trader"
• "Stop all trading"

*Commands*
/start — refresh status
/lang  — change language
/help  — show this
/bindgroup — bind the current group
/ask question — start a group conversation
/unbindgroup — unbind the current group

Everyone in the bound group can chat. Only the bound administrator can access the account or operate the trading system.`
}

func guestHelpMsg(lang string) string {
	if lang == "zh" {
		return `*NOFX 群聊助手*

你可以询问：
• 市场结构、指标和交易概念
• 风险管理与策略设计
• NOFX 的一般使用方法

使用 /ask 问题、@Bot，或回复 Bot 的消息继续对话。

出于安全考虑，账户、持仓和交易操作仅限绑定管理员。`
	}
	return `*NOFX Group Assistant*

You can ask about:
• Market structure, indicators, and trading concepts
• Risk management and strategy design
• General NOFX usage

Use /ask question, mention the Bot, or reply to a Bot message.

For security, account data and trading operations are restricted to the bound administrator.`
}
