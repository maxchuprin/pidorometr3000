package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"strings"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

type User struct {
	TelegramID  int64
	ChatID      int64
	Username    sql.NullString
	FirstName   sql.NullString
	LastName    sql.NullString
	IsAdmin     bool
	Active      bool
	WinCount    int
	LastWinDate sql.NullString
}

type ChatSettings struct {
	ChatID        int64
	Title         string
	DrawTime      string
	Timezone      string
	ExcludeAdmins bool
	AutoRegister  bool
}

type Winner struct {
	ID   int64
	Date string
	User User
	Text string
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS chats (
  chat_id INTEGER PRIMARY KEY,
  title TEXT NOT NULL DEFAULT 'Пидор дня',
  draw_time TEXT NOT NULL DEFAULT '09:00',
  timezone TEXT NOT NULL DEFAULT 'Asia/Aqtobe',
  exclude_admins INTEGER NOT NULL DEFAULT 1,
  auto_register INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS users (
  telegram_id INTEGER NOT NULL,
  chat_id INTEGER NOT NULL,
  username TEXT,
  first_name TEXT,
  last_name TEXT,
  is_admin INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (telegram_id, chat_id)
);
CREATE TABLE IF NOT EXISTS draws (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id INTEGER NOT NULL,
  dt TEXT NOT NULL,
  telegram_id INTEGER NOT NULL,
  text TEXT NOT NULL,
  manual INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(chat_id, dt)
);
CREATE INDEX IF NOT EXISTS idx_users_chat_active ON users(chat_id, active);
CREATE INDEX IF NOT EXISTS idx_draws_chat_dt ON draws(chat_id, dt);
`)
	return err
}

func (s *Store) EnsureChat(ctx context.Context, st ChatSettings) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO chats(chat_id,title,draw_time,timezone,exclude_admins,auto_register)
VALUES(?,?,?,?,?,?)
ON CONFLICT(chat_id) DO NOTHING`, st.ChatID, st.Title, st.DrawTime, st.Timezone, boolInt(st.ExcludeAdmins), boolInt(st.AutoRegister))
	return err
}

func (s *Store) UpsertUser(ctx context.Context, u User) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(telegram_id,chat_id,username,first_name,last_name,is_admin,active,updated_at)
VALUES(?,?,?,?,?,?,1,CURRENT_TIMESTAMP)
ON CONFLICT(telegram_id, chat_id) DO UPDATE SET
 username=excluded.username, first_name=excluded.first_name, last_name=excluded.last_name,
 is_admin=excluded.is_admin, active=1, updated_at=CURRENT_TIMESTAMP`, u.TelegramID, u.ChatID, nullable(u.Username), nullable(u.FirstName), nullable(u.LastName), boolInt(u.IsAdmin))
	return err
}

func (s *Store) SetActive(ctx context.Context, chatID, telegramID int64, active bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET active=?, updated_at=CURRENT_TIMESTAMP WHERE chat_id=? AND telegram_id=?`, boolInt(active), chatID, telegramID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) GetSettings(ctx context.Context, chatID int64) (ChatSettings, error) {
	var st ChatSettings
	var ex, ar int
	err := s.db.QueryRowContext(ctx, `SELECT chat_id,title,draw_time,timezone,exclude_admins,auto_register FROM chats WHERE chat_id=?`, chatID).
		Scan(&st.ChatID, &st.Title, &st.DrawTime, &st.Timezone, &ex, &ar)
	st.ExcludeAdmins = ex == 1
	st.AutoRegister = ar == 1
	return st, err
}

func (s *Store) SetDrawTime(ctx context.Context, chatID int64, drawTime string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET draw_time=? WHERE chat_id=?`, drawTime, chatID)
	return err
}
func (s *Store) SetTitle(ctx context.Context, chatID int64, title string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET title=? WHERE chat_id=?`, title, chatID)
	return err
}
func (s *Store) SetExcludeAdmins(ctx context.Context, chatID int64, v bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET exclude_admins=? WHERE chat_id=?`, boolInt(v), chatID)
	return err
}
func (s *Store) SetAutoRegister(ctx context.Context, chatID int64, v bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET auto_register=? WHERE chat_id=?`, boolInt(v), chatID)
	return err
}

func (s *Store) ListUsers(ctx context.Context, chatID int64, activeOnly bool) ([]User, error) {
	q := `SELECT u.telegram_id,u.chat_id,u.username,u.first_name,u.last_name,u.is_admin,u.active,
          (SELECT COUNT(*) FROM draws d WHERE d.chat_id=u.chat_id AND d.telegram_id=u.telegram_id) AS wins,
          (SELECT MAX(dt) FROM draws d WHERE d.chat_id=u.chat_id AND d.telegram_id=u.telegram_id) AS last_win
          FROM users u WHERE u.chat_id=?`
	if activeOnly {
		q += ` AND u.active=1`
	}
	q += ` ORDER BY lower(COALESCE(u.first_name,u.username,''))`
	rows, err := s.db.QueryContext(ctx, q, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var adm, act int
		if err := rows.Scan(&u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act, &u.WinCount, &u.LastWinDate); err != nil {
			return nil, err
		}
		u.IsAdmin = adm == 1
		u.Active = act == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) PickWinner(ctx context.Context, chatID int64, dt string, text string, manual bool, excludeAdmins bool) (Winner, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Winner{}, err
	}
	defer tx.Rollback()
	var exists int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM draws WHERE chat_id=? AND dt=?`, chatID, dt).Scan(&exists)
	if exists > 0 {
		return Winner{}, errors.New("draw already exists for today")
	}

	q := `SELECT telegram_id,chat_id,username,first_name,last_name,is_admin,active FROM users WHERE chat_id=? AND active=1`
	if excludeAdmins {
		q += ` AND is_admin=0`
	}
	rows, err := tx.QueryContext(ctx, q, chatID)
	if err != nil {
		return Winner{}, err
	}
	var candidates []User
	for rows.Next() {
		var u User
		var adm, act int
		if err := rows.Scan(&u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act); err != nil {
			rows.Close()
			return Winner{}, err
		}
		u.IsAdmin = adm == 1
		u.Active = act == 1
		candidates = append(candidates, u)
	}
	rows.Close()
	if len(candidates) == 0 {
		return Winner{}, errors.New("no active users")
	}

	// Чистый рандом: каждый активный участник имеет одинаковый шанс.
	// Единственное ограничение: один и тот же участник не может победить больше 3 раз подряд.
	pool := candidates
	if len(candidates) > 1 {
		lastWinnerID, streak, err := s.lastWinnerStreak(ctx, tx, chatID)
		if err != nil {
			return Winner{}, err
		}
		if streak >= 3 {
			filtered := make([]User, 0, len(candidates)-1)
			for _, c := range candidates {
				if c.TelegramID != lastWinnerID {
					filtered = append(filtered, c)
				}
			}
			if len(filtered) > 0 {
				pool = filtered
			}
		}
	}

	winner := pool[rand.Intn(len(pool))]
	res, err := tx.ExecContext(ctx, `INSERT INTO draws(chat_id,dt,telegram_id,text,manual) VALUES(?,?,?,?,?)`, chatID, dt, winner.TelegramID, text, boolInt(manual))
	if err != nil {
		return Winner{}, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return Winner{}, err
	}
	return Winner{ID: id, Date: dt, User: winner, Text: text}, nil
}

func (s *Store) lastWinnerStreak(ctx context.Context, tx *sql.Tx, chatID int64) (int64, int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT telegram_id FROM draws WHERE chat_id=? ORDER BY dt DESC, id DESC LIMIT 3`, chatID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	var lastWinnerID int64
	streak := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, 0, err
		}
		if streak == 0 {
			lastWinnerID = id
		}
		if id != lastWinnerID {
			break
		}
		streak++
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	return lastWinnerID, streak, nil
}

func (s *Store) UpdateWinnerText(ctx context.Context, id int64, text string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE draws SET text=? WHERE id=?`, text, id)
	return err
}

func (s *Store) TodayWinner(ctx context.Context, chatID int64, dt string) (Winner, error) {
	var w Winner
	var u User
	var adm, act int
	err := s.db.QueryRowContext(ctx, `SELECT d.id,d.dt,d.text,u.telegram_id,u.chat_id,u.username,u.first_name,u.last_name,u.is_admin,u.active
FROM draws d JOIN users u ON u.chat_id=d.chat_id AND u.telegram_id=d.telegram_id
WHERE d.chat_id=? AND d.dt=?`, chatID, dt).Scan(&w.ID, &w.Date, &w.Text, &u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act)
	u.IsAdmin = adm == 1
	u.Active = act == 1
	w.User = u
	return w, err
}

func (s *Store) Rating(ctx context.Context, chatID int64, limit int) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.telegram_id,u.chat_id,u.username,u.first_name,u.last_name,u.is_admin,u.active,COUNT(d.id) wins,MAX(d.dt) last_win
FROM users u LEFT JOIN draws d ON d.chat_id=u.chat_id AND d.telegram_id=u.telegram_id
WHERE u.chat_id=? GROUP BY u.telegram_id,u.chat_id ORDER BY wins DESC, last_win DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var adm, act int
		if err := rows.Scan(&u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act, &u.WinCount, &u.LastWinDate); err != nil {
			return nil, err
		}
		u.IsAdmin = adm == 1
		u.Active = act == 1
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) History(ctx context.Context, chatID int64, limit int) ([]Winner, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.dt,d.text,u.telegram_id,u.chat_id,u.username,u.first_name,u.last_name,u.is_admin,u.active
FROM draws d JOIN users u ON u.chat_id=d.chat_id AND u.telegram_id=d.telegram_id
WHERE d.chat_id=? ORDER BY d.dt DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Winner
	for rows.Next() {
		var w Winner
		var u User
		var adm, act int
		if err := rows.Scan(&w.ID, &w.Date, &w.Text, &u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act); err != nil {
			return nil, err
		}
		u.IsAdmin = adm == 1
		u.Active = act == 1
		w.User = u
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) ActiveChats(ctx context.Context) ([]ChatSettings, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT chat_id,title,draw_time,timezone,exclude_admins,auto_register FROM chats WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatSettings
	for rows.Next() {
		var st ChatSettings
		var ex, ar int
		if err := rows.Scan(&st.ChatID, &st.Title, &st.DrawTime, &st.Timezone, &ex, &ar); err != nil {
			return nil, err
		}
		st.ExcludeAdmins = ex == 1
		st.AutoRegister = ar == 1
		out = append(out, st)
	}
	return out, rows.Err()
}

func DisplayName(u User) string {
	name := strings.TrimSpace(strings.TrimSpace(u.FirstName.String + " " + u.LastName.String))
	if name != "" {
		return name
	}
	if u.Username.Valid && u.Username.String != "" {
		return "@" + u.Username.String
	}
	return fmt.Sprintf("user_%d", u.TelegramID)
}
func nullable(ns sql.NullString) any {
	if ns.Valid {
		return ns.String
	}
	return nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func NewNullString(v string) sql.NullString {
	v = strings.TrimSpace(v)
	return sql.NullString{String: v, Valid: v != ""}
}
