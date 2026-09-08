package router

import (
	"testing"

	"zee-mirror/internal/service"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestRouter_HandleMessage(t *testing.T) {
	r := NewRouter(nil)
	called := false
	r.RegisterCommandWithInfo(CommandInfo{Name: "testcmd"}, func(_ *service.BotService, _ *tgbotapi.Message) {
		called = true
	})

	msg := &tgbotapi.Message{
		Text: "/testcmd",
		Entities: []tgbotapi.MessageEntity{
			{Type: "bot_command", Offset: 0, Length: 8},
		},
	}

	r.HandleMessage(msg)
	if !called {
		t.Error("Command handler for 'testcmd' was not called")
	}
}

func TestRouter_HandleCallback(t *testing.T) {
	r := NewRouter(nil)
	called := false
	r.RegisterCallback("testcb", func(_ *service.BotService, _ *tgbotapi.CallbackQuery) {
		called = true
	})

	cb := &tgbotapi.CallbackQuery{
		Data: "testcb:action:id",
	}

	r.HandleCallback(cb)
	if !called {
		t.Error("Callback handler for 'testcb' was not called")
	}
}
