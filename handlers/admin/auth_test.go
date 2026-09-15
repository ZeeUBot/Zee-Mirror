package admin

import (
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestParseUserArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		wantU   string
		message *tgbotapi.Message
		wantID  int64
	}{
		{
			name:    "reply with username",
			message: &tgbotapi.Message{ReplyToMessage: &tgbotapi.Message{From: &tgbotapi.User{ID: 42, UserName: "alice"}}},
			wantID:  42, wantU: "alice",
		},
		{
			name:    "reply without username falls back to first name",
			message: &tgbotapi.Message{ReplyToMessage: &tgbotapi.Message{From: &tgbotapi.User{ID: 7, FirstName: "Bob"}}},
			wantID:  7, wantU: "Bob",
		},
		{
			name:    "id and username args",
			message: &tgbotapi.Message{},
			args:    "12345 carol",
			wantID:  12345, wantU: "carol",
		},
		{
			name:    "id only uses unknown placeholder",
			message: &tgbotapi.Message{},
			args:    "999",
			wantID:  999, wantU: UnknownSize,
		},
		{
			name:    "non numeric id",
			message: &tgbotapi.Message{},
			args:    "abc",
			wantID:  0, wantU: "",
		},
		{
			name:    "empty args",
			message: &tgbotapi.Message{},
			args:    "",
			wantID:  0, wantU: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, u := parseUserArgs(tt.message, tt.args)
			if id != tt.wantID || u != tt.wantU {
				t.Fatalf("parseUserArgs() = (%d, %q), want (%d, %q)", id, u, tt.wantID, tt.wantU)
			}
		})
	}
}
