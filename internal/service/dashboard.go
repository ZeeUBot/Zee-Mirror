package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"zee-mirror/pkg/i18n"
	"zee-mirror/pkg/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var lastStatusText = make(map[int64]string)
var sendMsgMu sync.Mutex

func (s *BotService) HandleStatus(message *tgbotapi.Message) {
	s.UpdateSharedDashboard(message.Chat.ID, true)
}

func (s *BotService) UpdateSharedDashboard(chatID int64, forceNew bool) {
	s.UpdateSharedDashboardNonBlocking(chatID, forceNew, false)
}

func (s *BotService) UpdateSharedDashboardNonBlocking(chatID int64, forceNew bool, nonBlocking bool) {
	if nonBlocking {
		if !s.TaskManager.StatusMu.TryLock() {
			return
		}
	} else {
		s.TaskManager.StatusMu.Lock()
	}

	tm := s.TaskManager
	tm.Mu.RLock()
	page := tm.StatusPages[chatID]
	tm.Mu.RUnlock()

	tasks := tm.GetActiveTasks()

	bm := s.BatchManager
	bm.Mu.RLock()
	var batches []*BatchTask
	for _, b := range bm.Batches {
		if b.Status != StatusCompleted && b.Status != StatusFailed && b.Status != StatusCancelled {
			batches = append(batches, b)
		}
	}
	bm.Mu.RUnlock()

	totalTasks := len(tasks) + len(batches)
	if totalTasks == 0 {
		tm.Mu.Lock()
		lastMsgID, exists := tm.LastStatusMsg[chatID]

		if !exists {
			key := fmt.Sprintf("dashboard_msg_%d", chatID)
			if val, err := s.SettingsRepo.Get(context.Background(), key); err == nil && val != "" {
				var storedID int
				if n, _ := fmt.Sscanf(val, "%d", &storedID); n > 0 && storedID != 0 {
					lastMsgID = storedID
					exists = true
				}
			}
		}

		if exists {
			_, _ = s.Bot.Request(tgbotapi.NewDeleteMessage(chatID, lastMsgID))
			delete(tm.LastStatusMsg, chatID)
			delete(lastStatusText, chatID)
			delete(tm.LastDashUpdateAt, chatID)
			delete(tm.LastDashProgressSum, chatID)
			_ = s.SettingsRepo.Set(context.Background(), fmt.Sprintf("dashboard_msg_%d", chatID), "")
		}
		tm.Mu.Unlock()
		s.TaskManager.StatusMu.Unlock()

		if forceNew {
			lang := s.GetUserLanguage(chatID)
			msg := tgbotapi.NewMessage(chatID, i18n.T(lang, "no_active_tasks"))
			msg.ParseMode = MarkdownV2
			sentMsg, err := s.Bot.Send(msg)
			if err != nil {
				slog.Error("Failed to send no_active_tasks message", "error", err, "chatID", chatID)
			} else {
				s.AutoDeleteMessage(chatID, sentMsg.MessageID, 30*time.Second)
			}
		}
		return
	}

	var currentProgressSum float64
	for _, t := range tasks {
		snapshot := t.GetSnapshot()
		currentProgressSum += snapshot.Progress
	}
	for _, b := range batches {
		b.Mu.RLock()
		currentProgressSum += b.Progress
		b.Mu.RUnlock()
	}

	tm.Mu.Lock()
	lastUpdateAt := tm.LastDashUpdateAt[chatID]
	lastProgressSum := tm.LastDashProgressSum[chatID]
	lastCount := tm.LastTasksCount[chatID]

	shouldUpdate := forceNew ||
		totalTasks != lastCount ||
		time.Since(lastUpdateAt) >= 3*time.Second ||
		(currentProgressSum-lastProgressSum) >= 5.0 ||
		(lastProgressSum-currentProgressSum) >= 5.0

	if !shouldUpdate {
		tm.Mu.Unlock()
		s.TaskManager.StatusMu.Unlock()
		return
	}

	tm.LastDashUpdateAt[chatID] = time.Now()
	tm.LastDashProgressSum[chatID] = currentProgressSum
	tm.LastTasksCount[chatID] = totalTasks
	tm.Mu.Unlock()

	lang := s.GetUserLanguage(chatID)
	text := s.buildStatusDashboardText(lang, tasks, batches, page)
	totalPages := (totalTasks + 4) / 5
	keyboard := buildNavigationKeyboard(page, totalPages)

	s.TaskManager.StatusMu.Unlock()

	s.sendStatusMessage(chatID, text, keyboard, forceNew)
}

func (s *BotService) HandleRefreshStatusCallback(callback *tgbotapi.CallbackQuery) {
	s.UpdateSharedDashboard(callback.Message.Chat.ID, false)
	lang := s.GetUserLanguage(callback.From.ID)
	callbackConfig := tgbotapi.NewCallback(callback.ID, i18n.T(lang, "dashboard_refreshed"))
	_, _ = s.Bot.Request(callbackConfig)
}

func calculateTotalPages(totalTasks int) int {
	return (totalTasks + 4) / 5
}

func (s *BotService) buildStatusDashboardText(lang string, tasks []*Task, batches []*BatchTask, page int) string {
	text := GetStatusHeader()

	allTasks := make([]interface{}, 0, len(tasks)+len(batches))
	for _, b := range batches {
		allTasks = append(allTasks, b)
	}
	for _, t := range tasks {
		allTasks = append(allTasks, t)
	}

	sort.Slice(allTasks, func(i, j int) bool {
		var timeI, timeJ time.Time

		if t, ok := allTasks[i].(*Task); ok {
			timeI = t.CreatedAt
		} else if b, ok := allTasks[i].(*BatchTask); ok {
			timeI = b.CreatedAt
		}

		if t, ok := allTasks[j].(*Task); ok {
			timeJ = t.CreatedAt
		} else if b, ok := allTasks[j].(*BatchTask); ok {
			timeJ = b.CreatedAt
		}

		return timeI.Before(timeJ)
	})

	totalTasks := len(allTasks)
	start := page * 5
	end := start + 5
	if end > totalTasks {
		end = totalTasks
	}

	if start >= totalTasks {
		return GetErrorMessage("PAGING ERROR", i18n.T(lang, "paging_error"))
	}

	visibleItems := allTasks[start:end]
	var visibleTasksCount, visibleBatchesCount int

	for _, item := range visibleItems {
		if batch, ok := item.(*BatchTask); ok {
			visibleBatchesCount++
			batch.Mu.RLock()
			emoji := utils.StatusEmoji(string(batch.Status))
			batchID := html.EscapeString(batch.ID)
			text += fmt.Sprintf("📦 <b>Batch:</b> <code>%s</code> • %s <b>%s</b>\n"+
				"📈 <b>Progres:</b> <code>%.1f%%</code> (%d/%d)\n"+
				"🚫 <b>Cancel:</b> /cancel_%s\n\n",
				batchID,
				emoji,
				html.EscapeString(utils.FormatStatus(string(batch.Status))),
				batch.Progress,
				batch.Completed,
				len(batch.SubTasks),
				batchID,
			)
			batch.Mu.RUnlock()
		} else if task, ok := item.(*Task); ok {
			visibleTasksCount++
			snapshot := task.GetSnapshot()
			text += FormatTaskProfessional(lang, snapshot) + "\n"
		}
	}

	totalPages := calculateTotalPages(totalTasks)
	text += CompactSeparator + "\n"
	if totalPages > 1 {
		text += fmt.Sprintf("📄 <b>Halaman:</b> %d/%d • ", page+1, totalPages)
	}

	if len(batches) > 0 {
		text += fmt.Sprintf("<i>Active: %d batches, %d regular tasks</i>", len(batches), visibleTasksCount)
	} else {
		text += fmt.Sprintf("<i>Total: %d task aktif</i>", visibleTasksCount)
	}

	return text
}

func buildNavigationKeyboard(_, _ int) tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.InlineKeyboardMarkup{}
}

func (s *BotService) sendStatusMessage(chatID int64, text string, _ tgbotapi.InlineKeyboardMarkup, forceNew bool) {
	sendMsgMu.Lock()
	defer sendMsgMu.Unlock()

	s.TaskManager.Mu.RLock()
	lastMsgID, exists := s.TaskManager.LastStatusMsg[chatID]
	s.TaskManager.Mu.RUnlock()

	if !exists {
		key := fmt.Sprintf("dashboard_msg_%d", chatID)
		if val, err := s.SettingsRepo.Get(context.Background(), key); err == nil && val != "" {
			var storedID int
			if n, _ := fmt.Sscanf(val, "%d", &storedID); n > 0 && storedID != 0 {
				lastMsgID = storedID
				exists = true
				s.TaskManager.Mu.Lock()
				s.TaskManager.LastStatusMsg[chatID] = storedID
				s.TaskManager.Mu.Unlock()
			}
		}
	}

	lastText, textExists := lastStatusText[chatID]

	if exists && forceNew {
		_, _ = s.Bot.Request(tgbotapi.NewDeleteMessage(chatID, lastMsgID))
		exists = false
		delete(lastStatusText, chatID)
	}

	if exists && !forceNew {
		if textExists && lastText == text {
			return
		}

		slog.Debug("Editing dashboard message", "chatID", chatID, "msgID", lastMsgID, "text", text)
		editMsg := tgbotapi.NewEditMessageText(chatID, lastMsgID, text)
		editMsg.ParseMode = "HTML"
		_, sendErr := s.Bot.Send(editMsg)
		if sendErr == nil {
			lastStatusText[chatID] = text
			return
		}
		if strings.Contains(sendErr.Error(), "message is not modified") {
			lastStatusText[chatID] = text
			return
		}
		slog.Warn("Failed to edit status message, falling back to new message", "error", sendErr, "chatID", chatID)
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	sentMsg, err := s.Bot.Send(msg)
	if err != nil {
		logText := text
		if len(logText) > 500 {
			logText = logText[:500]
		}
		slog.Error("Failed to send status dashboard message", "error", err, "chatID", chatID, "text", logText)
		return
	}
	s.TaskManager.Mu.Lock()
	s.TaskManager.LastStatusMsg[chatID] = sentMsg.MessageID
	s.TaskManager.Mu.Unlock()
	lastStatusText[chatID] = text

	key := fmt.Sprintf("dashboard_msg_%d", chatID)
	_ = s.SettingsRepo.Set(context.Background(), key, fmt.Sprintf("%d", sentMsg.MessageID))
}

func (s *BotService) HandleCancel(message *tgbotapi.Message, args string) {
	if args == "" {
		s.reply(message, GetErrorMessage("CANCEL ERROR", "Gunakan: /cancel <TaskID>\n\nLihat daftar task dengan /status"))
		return
	}

	taskID := args

	if s.TaskManager.CancelTask(taskID) {
		lang := s.GetUserLanguage(message.From.ID)
		s.reply(message, GetSuccessMessage("TASK CANCELLED", fmt.Sprintf(i18n.T(lang, "task_cancelled"), taskID)))
		s.UpdateSharedDashboard(message.Chat.ID, false)
		return
	}

	task := s.TaskManager.GetTaskByGID(taskID)
	if task != nil {
		if s.TaskManager.CancelTask(task.ID) {
			s.reply(message, fmt.Sprintf("✅ *Task `%s` dibatalkan*", task.ID))
			return
		}
	}

	bm := s.BatchManager
	bm.Mu.RLock()
	foundBatch := false
	var targetBatch *BatchTask
	for _, batch := range bm.Batches {
		if batch.ID == taskID {
			targetBatch = batch
			foundBatch = true
			break
		}
	}
	bm.Mu.RUnlock()

	if foundBatch {
		lang := s.GetUserLanguage(message.From.ID)
		targetBatch.Cancel()
		s.reply(message, GetSuccessMessage("BATCH CANCELLED", fmt.Sprintf(i18n.T(lang, "batch_cancelled"), taskID)))
		s.UpdateSharedDashboard(message.Chat.ID, false)
		return
	}

	if s.BatchManager.CancelSubTask(taskID) {
		lang := s.GetUserLanguage(message.From.ID)
		s.reply(message, GetSuccessMessage("SUB-TASK CANCELLED", fmt.Sprintf(i18n.T(lang, "sub_task_cancelled"), taskID)))
		s.UpdateSharedDashboard(message.Chat.ID, false)
		return
	}

	lang := s.GetUserLanguage(message.From.ID)
	s.reply(message, GetErrorMessage("NOT FOUND", fmt.Sprintf(i18n.T(lang, "task_not_found"), utils.EscapeMarkdownV2(taskID))))
}

func (s *BotService) HandleCancelAll(message *tgbotapi.Message) {
	if !s.IsAdmin(message.From.ID) {
		lang := s.GetUserLanguage(message.From.ID)
		msg := tgbotapi.NewMessage(message.Chat.ID, GetErrorMessage("PERMISSION DENIED", i18n.T(lang, "admin_only")))
		msg.ParseMode = MarkdownV2
		_, _ = s.Bot.Send(msg)
		return
	}

	count := s.TaskManager.CancelAllTasks()

	s.BatchManager.Mu.RLock()
	var batchIDs []string
	for id := range s.BatchManager.Batches {
		batchIDs = append(batchIDs, id)
	}
	s.BatchManager.Mu.RUnlock()

	for _, id := range batchIDs {
		s.BatchManager.Mu.RLock()
		b, exists := s.BatchManager.Batches[id]
		s.BatchManager.Mu.RUnlock()
		if exists && b.Cancel() {
			count++
		}
	}

	lang := s.GetUserLanguage(message.From.ID)
	msg := tgbotapi.NewMessage(message.Chat.ID, GetSuccessMessage("CANCEL ALL", fmt.Sprintf(i18n.T(lang, "all_tasks_cancelled"), count)))
	msg.ParseMode = MarkdownV2
	_, _ = s.Bot.Send(msg)
	s.UpdateSharedDashboard(message.Chat.ID, false)
}

func (s *BotService) HandleCancelCallback(callback *tgbotapi.CallbackQuery, taskID string) {
	success := false

	if s.TaskManager.CancelTask(taskID) {
		success = true
	} else {
		task := s.TaskManager.GetTaskByGID(taskID)
		if task != nil {
			if s.TaskManager.CancelTask(task.ID) {
				success = true
			}
		}
	}

	if success {
		callbackConfig := tgbotapi.NewCallback(callback.ID, fmt.Sprintf("✅ Task %s dibatalkan", taskID))
		_, _ = s.Bot.Request(callbackConfig)
		s.UpdateSharedDashboard(callback.Message.Chat.ID, false)
	} else {
		callbackConfig := tgbotapi.NewCallback(callback.ID, "❌ Gagal membatalkan task")
		_, _ = s.Bot.Request(callbackConfig)
	}
}
