package app

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/robfig/cron/v3"
	"html"
	"log/slog"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"pidorometr3000/internal/config"
	"pidorometr3000/internal/store"
	"pidorometr3000/internal/texts"
)

type App struct {
	cfg  config.Config
	st   *store.Store
	log  *slog.Logger
	bot  *tgbotapi.BotAPI
	cron *cron.Cron
	loc  *time.Location
}

func New(cfg config.Config, st *store.Store, logger *slog.Logger) (*App, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return nil, err
	}
	bot, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		return nil, err
	}
	bot.Debug = cfg.Debug
	rand.Seed(time.Now().UnixNano())
	return &App{cfg: cfg, st: st, log: logger, bot: bot, loc: loc}, nil
}

func (a *App) Run() error {
	a.log.Info("authorized", "bot", a.bot.Self.UserName)
	ctx := context.Background()
	if err := a.startCron(ctx); err != nil {
		return err
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := a.bot.GetUpdatesChan(u)
	for upd := range updates {
		if upd.Message == nil {
			continue
		}
		a.handleMessage(ctx, upd.Message)
	}
	return nil
}

func (a *App) startCron(ctx context.Context) error {
	c := cron.New(cron.WithLocation(a.loc))
	_, err := c.AddFunc("* * * * *", func() { a.checkScheduledDraws(ctx) })
	if err != nil {
		return err
	}
	c.Start()
	a.cron = c
	return nil
}

func (a *App) checkScheduledDraws(ctx context.Context) {
	chats, err := a.st.ActiveChats(ctx)
	if err != nil {
		a.log.Error("active chats", "err", err)
		return
	}
	now := time.Now()
	for _, ch := range chats {
		loc, err := time.LoadLocation(ch.Timezone)
		if err != nil {
			loc = a.loc
		}
		n := now.In(loc)
		if n.Format("15:04") != ch.DrawTime {
			continue
		}
		dt := n.Format("2006-01-02")
		if _, err := a.st.TodayWinner(ctx, ch.ChatID, dt); err == nil {
			continue
		}
		_, _ = a.draw(ctx, ch.ChatID, false)
	}
}

func (a *App) handleMessage(ctx context.Context, m *tgbotapi.Message) {
	if m.Chat == nil || m.From == nil {
		return
	}
	if m.Chat.IsPrivate() {
		a.reply(m.Chat.ID, "Добавь меня в группу и напиши /register. В личке розыгрыш не имеет смысла 🙂")
		return
	}

	_ = a.ensureChat(ctx, m.Chat.ID)
	st, _ := a.st.GetSettings(ctx, m.Chat.ID)
	if st.AutoRegister && !m.From.IsBot {
		_ = a.registerFromMessage(ctx, m)
	}

	if !m.IsCommand() {
		return
	}
	cmd := strings.ToLower(m.Command())
	args := strings.TrimSpace(m.CommandArguments())
	switch cmd {
	case "start", "help":
		a.help(m.Chat.ID)
	case "register":
		a.cmdRegister(ctx, m)
	case "leave":
		a.cmdLeave(ctx, m)
	case "list":
		a.cmdList(ctx, m.Chat.ID)
	case "today":
		a.cmdToday(ctx, m.Chat.ID)
	case "force":
		a.cmdForce(ctx, m.Chat.ID)
	case "rating", "top":
		a.cmdRating(ctx, m.Chat.ID)
	case "history":
		a.cmdHistory(ctx, m.Chat.ID)
	case "settings":
		a.cmdSettings(ctx, m.Chat.ID)
	case "settime":
		a.cmdSetTime(ctx, m.Chat.ID, args)
	case "settitle":
		a.cmdSetTitle(ctx, m.Chat.ID, args)
	case "autoregister":
		a.cmdAutoRegister(ctx, m.Chat.ID, args)
	default:
		a.reply(m.Chat.ID, "Не знаю такую команду. Напиши /help")
	}
}

func (a *App) ensureChat(ctx context.Context, chatID int64) error {
	return a.st.EnsureChat(ctx, store.ChatSettings{ChatID: chatID, Title: a.cfg.DefaultTitle, DrawTime: a.cfg.DefaultDrawTime, Timezone: a.cfg.Timezone, ExcludeAdmins: false, AutoRegister: true})
}

func (a *App) registerFromMessage(ctx context.Context, m *tgbotapi.Message) error {
	return a.st.UpsertUser(ctx, store.User{TelegramID: m.From.ID, ChatID: m.Chat.ID, Username: store.NewNullString(m.From.UserName), FirstName: store.NewNullString(m.From.FirstName), LastName: store.NewNullString(m.From.LastName), IsAdmin: false, Active: true})
}

func (a *App) cmdRegister(ctx context.Context, m *tgbotapi.Message) {
	if err := a.registerFromMessage(ctx, m); err != nil {
		a.reply(m.Chat.ID, "Не смог зарегистрировать: "+err.Error())
		return
	}
	a.replyHTML(m.Chat.ID, "✅ Зарегистрировал: "+a.mention(store.User{TelegramID: m.From.ID, Username: store.NewNullString(m.From.UserName), FirstName: store.NewNullString(m.From.FirstName), LastName: store.NewNullString(m.From.LastName)}))
}
func (a *App) cmdLeave(ctx context.Context, m *tgbotapi.Message) {
	if err := a.st.SetActive(ctx, m.Chat.ID, m.From.ID, false); err != nil {
		a.reply(m.Chat.ID, "Тебя и так нет в активном списке.")
		return
	}
	a.reply(m.Chat.ID, "Ок, убрал из розыгрыша.")
}
func (a *App) cmdList(ctx context.Context, chatID int64) {
	users, err := a.st.ListUsers(ctx, chatID, true)
	if err != nil {
		a.reply(chatID, err.Error())
		return
	}
	if len(users) == 0 {
		a.reply(chatID, "Пока никого нет. Напишите /register")
		return
	}
	var b strings.Builder
	b.WriteString("👥 Участники:\n")
	for i, u := range users {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, store.DisplayName(u)))
	}
	a.reply(chatID, b.String())
}
func (a *App) cmdToday(ctx context.Context, chatID int64) {
	st, _ := a.st.GetSettings(ctx, chatID)
	loc, _ := time.LoadLocation(st.Timezone)
	dt := time.Now().In(loc).Format("2006-01-02")
	w, err := a.st.TodayWinner(ctx, chatID, dt)
	if err == nil {
		a.sendWinnerSequence(chatID, st, w)
		return
	}
	_, _ = a.draw(ctx, chatID, true)
}
func (a *App) cmdForce(ctx context.Context, chatID int64) { _, _ = a.draw(ctx, chatID, true) }
func (a *App) draw(ctx context.Context, chatID int64, manual bool) (store.Winner, error) {
	st, err := a.st.GetSettings(ctx, chatID)
	if err != nil {
		a.reply(chatID, "Чат не настроен. Напиши /help")
		return store.Winner{}, err
	}
	loc, err := time.LoadLocation(st.Timezone)
	if err != nil {
		loc = a.loc
	}
	dt := time.Now().In(loc).Format("2006-01-02")
	w, err := a.st.PickWinner(ctx, chatID, dt, "", manual, false)
	if err != nil {
		if strings.Contains(err.Error(), "already") {
			a.reply(chatID, "Сегодня розыгрыш уже был. Посмотреть: /today")
			return store.Winner{}, err
		}
		if strings.Contains(err.Error(), "no active") {
			a.reply(chatID, "Некого выбирать. Пусть участники напишут /register")
			return store.Winner{}, err
		}
		a.reply(chatID, "Ошибка розыгрыша: "+err.Error())
		return store.Winner{}, err
	}
	w.Text = texts.ReasonForUser(w.User.Username.String, store.DisplayName(w.User))
	if err := a.st.UpdateWinnerText(ctx, w.ID, w.Text); err != nil {
		a.log.Error("update winner text", "err", err)
	}
	a.sendWinnerSequence(chatID, st, w)
	return w, nil
}

func (a *App) cmdRating(ctx context.Context, chatID int64) {
	users, err := a.st.Rating(ctx, chatID, 20)
	if err != nil {
		a.reply(chatID, err.Error())
		return
	}
	if len(users) == 0 {
		a.reply(chatID, "Рейтинг пуст.")
		return
	}
	var b strings.Builder
	b.WriteString("🏆 Рейтинг:\n")
	for i, u := range users {
		b.WriteString(fmt.Sprintf("%d. %s — %d\n", i+1, store.DisplayName(u), u.WinCount))
	}
	a.reply(chatID, b.String())
}
func (a *App) cmdHistory(ctx context.Context, chatID int64) {
	h, err := a.st.History(ctx, chatID, 15)
	if err != nil {
		a.reply(chatID, err.Error())
		return
	}
	if len(h) == 0 {
		a.reply(chatID, "История пустая.")
		return
	}
	var b strings.Builder
	b.WriteString("📜 История:\n")
	for _, w := range h {
		b.WriteString(fmt.Sprintf("%s — %s\n", w.Date, store.DisplayName(w.User)))
	}
	a.reply(chatID, b.String())
}
func (a *App) cmdSettings(ctx context.Context, chatID int64) {
	st, err := a.st.GetSettings(ctx, chatID)
	if err != nil {
		a.reply(chatID, err.Error())
		return
	}
	a.reply(chatID, fmt.Sprintf("⚙️ Настройки:\nНазвание: %s\nВремя: %s\nЧасовой пояс: %s\nАвторегистрация: %v", st.Title, st.DrawTime, st.Timezone, st.AutoRegister))
}
func (a *App) cmdSetTime(ctx context.Context, chatID int64, args string) {
	if !regexp.MustCompile(`^\d{2}:\d{2}$`).MatchString(args) {
		a.reply(chatID, "Формат: /settime 09:00")
		return
	}
	if _, err := time.Parse("15:04", args); err != nil {
		a.reply(chatID, "Некорректное время. Пример: /settime 09:00")
		return
	}
	if err := a.st.SetDrawTime(ctx, chatID, args); err != nil {
		a.reply(chatID, err.Error())
		return
	}
	a.reply(chatID, "Ок, ежедневный розыгрыш теперь в "+args)
}
func (a *App) cmdSetTitle(ctx context.Context, chatID int64, args string) {
	args = strings.TrimSpace(args)
	if args == "" {
		a.reply(chatID, "Формат: /settitle Герой дня")
		return
	}
	if len([]rune(args)) > 64 {
		a.reply(chatID, "Слишком длинное название, максимум 64 символа.")
		return
	}
	if err := a.st.SetTitle(ctx, chatID, args); err != nil {
		a.reply(chatID, err.Error())
		return
	}
	a.reply(chatID, "Ок, новое название: "+args)
}
func (a *App) cmdAutoRegister(ctx context.Context, chatID int64, args string) {
	a.setBool(ctx, chatID, args, a.st.SetAutoRegister, "Авторегистрация")
}
func (a *App) setBool(ctx context.Context, chatID int64, args string, fn func(context.Context, int64, bool) error, label string) {
	v, ok := parseOnOff(args)
	if !ok {
		a.reply(chatID, "Формат: on/off. Например: /excludeadmins on")
		return
	}
	if err := fn(ctx, chatID, v); err != nil {
		a.reply(chatID, err.Error())
		return
	}
	a.reply(chatID, fmt.Sprintf("%s: %v", label, v))
}
func parseOnOff(s string) (bool, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "on", "true", "1", "yes", "да":
		return true, true
	case "off", "false", "0", "no", "нет":
		return false, true
	default:
		return false, false
	}
}

func (a *App) mention(u store.User) string {
	name := store.DisplayName(u)
	if u.Username.Valid && u.Username.String != "" {
		return "@" + html.EscapeString(u.Username.String)
	}
	return fmt.Sprintf(`<a href="tg://user?id=%d">%s</a>`, u.TelegramID, html.EscapeString(name))
}

func (a *App) help(chatID int64) {
	a.reply(chatID, strings.TrimSpace(`🤖 Команды:
/register — записаться
/leave — выйти из розыгрыша
/list — участники
/today — показать или провести сегодняшний розыгрыш
/force — попробовать провести розыгрыш сегодня
/rating — рейтинг
/history — история
/settings — настройки
/settime 09:00 — время ежедневного розыгрыша
/settitle Герой дня — изменить название конкурса
/autoregister on|off — регистрировать тех, кто пишет в чат

Важно: чтобы бот видел обычные сообщения, отключи Privacy Mode у бота в BotFather.`))
}

func (a *App) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	_, _ = a.bot.Send(msg)
}
func (a *App) replyHTML(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true
	_, _ = a.bot.Send(msg)
}

// keep imports useful for future deterministic sorting in command output
var _ = sort.Slice
var _ = sql.ErrNoRows

func (a *App) sendWinnerSequence(chatID int64, st store.ChatSettings, w store.Winner) {
	a.reply(chatID, texts.Intro())

	time.Sleep(1 * time.Second)

	a.replyHTML(chatID, fmt.Sprintf(
		"<b>%s</b> — %s 🏆",
		html.EscapeString(st.Title),
		a.mention(w.User),
	))

	time.Sleep(1 * time.Second)

	a.replyHTML(chatID, fmt.Sprintf(
		"<b>Причина:</b> %s",
		html.EscapeString(w.Text),
	))
}
