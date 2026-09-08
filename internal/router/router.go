package router

import (
	"log/slog"
	"sort"
	"strings"

	"zee-mirror/handlers/download"
	"zee-mirror/internal/service"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type CommandHandler func(s *service.BotService, msg *tgbotapi.Message)
type CallbackHandler func(s *service.BotService, cb *tgbotapi.CallbackQuery)

type CommandInfo struct {
	DetailedFn  func() string
	Name        string
	Description string
	Category    string
	Emoji       string
	Aliases     []string
}

type Router struct {
	service          *service.BotService
	commands         map[string]CommandHandler
	callbacks        map[string]CallbackHandler
	defaultCommand   CommandHandler
	magnetHandler    CommandHandler
	callbackPrefixes map[string]CallbackHandler
	commandInfos     map[string]*CommandInfo
}

func NewRouter(service *service.BotService) *Router {
	return &Router{
		service:          service,
		commands:         make(map[string]CommandHandler),
		callbacks:        make(map[string]CallbackHandler),
		callbackPrefixes: make(map[string]CallbackHandler),
		commandInfos:     make(map[string]*CommandInfo),
	}
}

func (r *Router) RegisterCommandWithInfo(info CommandInfo, handler CommandHandler) {
	r.commands[info.Name] = handler
	for _, alias := range info.Aliases {
		r.commands[alias] = handler
	}
	r.commandInfos[info.Name] = &info
}

func (r *Router) GetAllCommandsFlat() []CommandInfo {
	var result []CommandInfo
	for _, info := range r.commandInfos {
		result = append(result, *info)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (r *Router) RegisterCallback(prefix string, handler CallbackHandler) {
	r.callbackPrefixes[prefix] = handler
}

func (r *Router) RegisterMagnetHandler(handler CommandHandler) {
	r.magnetHandler = handler
}

func (r *Router) HandleMessage(msg *tgbotapi.Message) {
	if r.service != nil && msg.From != nil {
		r.service.SyncUser(msg.From)
	}
	if msg.IsCommand() {
		username := "unknown"
		if msg.From != nil {
			username = msg.From.UserName
		}
		slog.Info("Command received", "user", username, "command", msg.Command(), "args", msg.CommandArguments())
		if r.service != nil {
			r.service.AutoDeleteCommandAndReply(msg)
		}
		command := msg.Command()
		if handler, ok := r.commands[command]; ok {
			handler(r.service, msg)
			return
		}

		if strings.HasPrefix(command, "cancel_") {
			taskID := strings.TrimPrefix(command, "cancel_")
			r.service.HandleCancel(msg, taskID)
			return
		}

		if r.defaultCommand != nil {
			r.defaultCommand(r.service, msg)
		}
		return
	}

	text := msg.Text
	if text == "" && msg.Caption != "" {
		text = msg.Caption
	}

	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From.ID == r.service.Bot.Self.ID {
		replyText := msg.ReplyToMessage.Text
		if strings.Contains(replyText, "Silakan kirim URL") || strings.Contains(replyText, "Silakan kirim Nama Baru") {
			if r.service != nil {
				download.HandleWizardInput(r.service, msg)
				return
			}
		}
	}

	if text != "" {
		if strings.HasPrefix(text, "magnet:?") {
			if r.service != nil {
				r.service.AutoDeleteCommandAndReply(msg)
			}
			if r.magnetHandler != nil {
				r.magnetHandler(r.service, msg)
			}
			return
		}
	}
}

func (r *Router) HandleCallback(cb *tgbotapi.CallbackQuery) {
	if r.service != nil && cb.From != nil {
		r.service.SyncUser(cb.From)
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) == 0 {
		return
	}

	prefix := parts[0]

	if strings.HasPrefix(prefix, "cancel_") {
		taskID := strings.TrimPrefix(prefix, "cancel_")
		r.service.HandleCancelCallback(cb, taskID)
		return
	}

	if handler, ok := r.callbackPrefixes[prefix]; ok {
		handler(r.service, cb)
		return
	}

	slog.Warn("Unknown callback received", "data", cb.Data)
	if r.service != nil && cb.ID != "" {
		_, _ = r.service.Bot.Request(tgbotapi.NewCallback(cb.ID, "Aksi tidak dikenali"))
	}
}
