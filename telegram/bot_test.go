package telegram

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func telegramCommandMessage(text string) *tgbotapi.Message {
	length := len(text)
	if space := strings.IndexByte(text, ' '); space >= 0 {
		length = space
	}
	return &tgbotapi.Message{
		Text: text,
		Entities: []tgbotapi.MessageEntity{{
			Type:   "bot_command",
			Offset: 0,
			Length: length,
		}},
	}
}

func TestCommandForBotAcceptsGroupCommandSuffix(t *testing.T) {
	message := telegramCommandMessage("/start@NOFX_TestBot")
	if got := commandForBot(message, "nofx_testbot"); got != "start" {
		t.Fatalf("commandForBot() = %q, want start", got)
	}
}

func TestCommandForBotRejectsAnotherBot(t *testing.T) {
	message := telegramCommandMessage("/start@another_bot")
	if got := commandForBot(message, "nofx_testbot"); got != "" {
		t.Fatalf("commandForBot() = %q, want empty", got)
	}
}

func TestGroupPromptAcceptsAskCommand(t *testing.T) {
	message := telegramCommandMessage("/ask@nofx_testbot 查看我的持仓")
	got, ok := groupPrompt(message, &tgbotapi.User{ID: 99, UserName: "nofx_testbot"})
	if !ok || got != "查看我的持仓" {
		t.Fatalf("groupPrompt() = (%q, %t), want (%q, true)", got, ok, "查看我的持仓")
	}
}

func TestGroupPromptAcceptsMentionAndRemovesIt(t *testing.T) {
	message := &tgbotapi.Message{Text: "@NOFX_TestBot 查看账户余额"}
	got, ok := groupPrompt(message, &tgbotapi.User{ID: 99, UserName: "nofx_testbot"})
	if !ok || got != "查看账户余额" {
		t.Fatalf("groupPrompt() = (%q, %t), want (%q, true)", got, ok, "查看账户余额")
	}
}

func TestGroupPromptAcceptsReplyToBot(t *testing.T) {
	message := &tgbotapi.Message{
		Text: "继续分析",
		ReplyToMessage: &tgbotapi.Message{
			From: &tgbotapi.User{ID: 99, IsBot: true},
		},
	}
	got, ok := groupPrompt(message, &tgbotapi.User{ID: 99, UserName: "nofx_testbot"})
	if !ok || got != "继续分析" {
		t.Fatalf("groupPrompt() = (%q, %t), want (%q, true)", got, ok, "继续分析")
	}
}

func TestGroupPromptIgnoresOrdinaryGroupTraffic(t *testing.T) {
	message := &tgbotapi.Message{Text: "大家晚上好"}
	if got, ok := groupPrompt(message, &tgbotapi.User{ID: 99, UserName: "nofx_testbot"}); ok || got != "" {
		t.Fatalf("groupPrompt() = (%q, %t), want empty and false", got, ok)
	}
}
