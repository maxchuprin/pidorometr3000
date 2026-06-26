package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"strings"

	_ "github.com/lib/pq"
)

type Store struct{ db *sql.DB }

var (
	// ErrAutomaticDrawExists means the official daily draw has already been held.
	ErrAutomaticDrawExists = errors.New("automatic draw already exists for today")
	// ErrManualDrawLimitReached means a chat has already used all manual draws for the day.
	ErrManualDrawLimitReached = errors.New("manual draw limit reached for today")
	ErrNoActiveUsers          = errors.New("no active users")
)

const manualDrawLimitPerDay = 5

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
	db, err := sql.Open("postgres", path)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS chats (
  chat_id BIGINT PRIMARY KEY,
  title TEXT NOT NULL DEFAULT 'Пидор дня',
  draw_time TEXT NOT NULL DEFAULT '09:00',
  timezone TEXT NOT NULL DEFAULT 'Asia/Aqtobe',
  exclude_admins BOOLEAN NOT NULL DEFAULT TRUE,
  auto_register BOOLEAN NOT NULL DEFAULT TRUE,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS users (
  telegram_id BIGINT NOT NULL,
  chat_id BIGINT NOT NULL,
  username TEXT,
  first_name TEXT,
  last_name TEXT,
  is_admin BOOLEAN NOT NULL DEFAULT FALSE,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
  PRIMARY KEY (telegram_id, chat_id)
);
CREATE TABLE IF NOT EXISTS draws (
  id SERIAL PRIMARY KEY,
  chat_id BIGINT NOT NULL,
  dt TEXT NOT NULL,
  telegram_id BIGINT NOT NULL,
  text TEXT NOT NULL,
  manual BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
-- Earlier versions allowed just one draw per chat and date. Manual draws now have
-- their own daily limit, while the automatic draw remains limited to one per day.
ALTER TABLE draws DROP CONSTRAINT IF EXISTS draws_chat_id_dt_key;
CREATE INDEX IF NOT EXISTS idx_users_chat_active ON users(chat_id, active);
CREATE INDEX IF NOT EXISTS idx_draws_chat_dt ON draws(chat_id, dt);
CREATE INDEX IF NOT EXISTS idx_draws_chat_dt_manual ON draws(chat_id, dt, manual);
CREATE UNIQUE INDEX IF NOT EXISTS idx_draws_one_automatic_per_day
  ON draws(chat_id, dt)
  WHERE manual = FALSE;
`)
	return err
}

func (s *Store) EnsureChat(ctx context.Context, st ChatSettings) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO chats(chat_id,title,draw_time,timezone,exclude_admins,auto_register)
VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(chat_id) DO NOTHING`, st.ChatID, st.Title, st.DrawTime, st.Timezone, st.ExcludeAdmins, st.AutoRegister)
	return err
}

func (s *Store) UpsertUser(ctx context.Context, u User) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(telegram_id,chat_id,username,first_name,last_name,is_admin,active,updated_at)
VALUES($1,$2,$3,$4,$5,$6,true,NOW())
ON CONFLICT(telegram_id, chat_id) DO UPDATE SET
 username=excluded.username, first_name=excluded.first_name, last_name=excluded.last_name,
 is_admin=excluded.is_admin, active=true, updated_at=NOW()`, u.TelegramID, u.ChatID, nullable(u.Username), nullable(u.FirstName), nullable(u.LastName), u.IsAdmin)
	return err
}

func (s *Store) SetActive(ctx context.Context, chatID, telegramID int64, active bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET active=$1, updated_at=NOW() WHERE chat_id=$2 AND telegram_id=$3`, active, chatID, telegramID)
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
	var ex, ar bool
	err := s.db.QueryRowContext(ctx, `SELECT chat_id,title,draw_time,timezone,exclude_admins,auto_register FROM chats WHERE chat_id=$1`, chatID).
		Scan(&st.ChatID, &st.Title, &st.DrawTime, &st.Timezone, &ex, &ar)
	st.ExcludeAdmins = ex
	st.AutoRegister = ar
	return st, err
}

func (s *Store) SetDrawTime(ctx context.Context, chatID int64, drawTime string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET draw_time=$1 WHERE chat_id=$2`, drawTime, chatID)
	return err
}
func (s *Store) SetTitle(ctx context.Context, chatID int64, title string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET title=$1 WHERE chat_id=$2`, title, chatID)
	return err
}

func (s *Store) SetAutoRegister(ctx context.Context, chatID int64, v bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE chats SET auto_register=$1 WHERE chat_id=$2`, v, chatID)
	return err
}

func (s *Store) ListUsers(ctx context.Context, chatID int64, activeOnly bool) ([]User, error) {
	q := `SELECT u.telegram_id,u.chat_id,u.username,u.first_name,u.last_name,u.is_admin,u.active,
          (SELECT COUNT(*) FROM draws d WHERE d.chat_id=u.chat_id AND d.telegram_id=u.telegram_id) AS wins,
          (SELECT MAX(dt) FROM draws d WHERE d.chat_id=u.chat_id AND d.telegram_id=u.telegram_id) AS last_win
          FROM users u WHERE u.chat_id=$1`

	q += ` ORDER BY lower(COALESCE(u.first_name,u.username,''))`
	rows, err := s.db.QueryContext(ctx, q, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var adm, act bool
		if err := rows.Scan(&u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act, &u.WinCount, &u.LastWinDate); err != nil {
			return nil, err
		}
		u.IsAdmin = adm
		u.Active = act
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

	// Serializes draws within one chat. It makes the five-per-day manual limit
	// reliable even when several /force commands arrive at the same time.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, chatID); err != nil {
		return Winner{}, err
	}

	if manual {
		var count int
		if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM draws
WHERE chat_id=$1 AND dt=$2 AND manual=TRUE`, chatID, dt).Scan(&count); err != nil {
			return Winner{}, err
		}
		if count >= manualDrawLimitPerDay {
			return Winner{}, ErrManualDrawLimitReached
		}
	} else {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
SELECT EXISTS(
  SELECT 1 FROM draws WHERE chat_id=$1 AND dt=$2 AND manual=FALSE
)`, chatID, dt).Scan(&exists); err != nil {
			return Winner{}, err
		}
		if exists {
			return Winner{}, ErrAutomaticDrawExists
		}
	}

	q := `SELECT telegram_id,chat_id,username,first_name,last_name,is_admin,active FROM users WHERE chat_id=$1 AND active=true`
	rows, err := tx.QueryContext(ctx, q, chatID)
	if err != nil {
		return Winner{}, err
	}
	defer rows.Close()

	var candidates []User
	for rows.Next() {
		var u User
		var adm, act bool
		if err := rows.Scan(&u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act); err != nil {
			return Winner{}, err
		}
		u.IsAdmin = adm
		u.Active = act
		candidates = append(candidates, u)
	}
	if err := rows.Err(); err != nil {
		return Winner{}, err
	}
	if len(candidates) == 0 {
		return Winner{}, ErrNoActiveUsers
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
	var id int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO draws(chat_id,dt,telegram_id,text,manual)
VALUES($1,$2,$3,$4,$5)
RETURNING id`, chatID, dt, winner.TelegramID, text, manual).Scan(&id)
	if err != nil {
		// The partial unique index is an additional cross-process safeguard for
		// automatic draws. The advisory lock above handles the normal path.
		return Winner{}, err
	}
	if err := tx.Commit(); err != nil {
		return Winner{}, err
	}
	return Winner{ID: id, Date: dt, User: winner, Text: text}, nil
}

func (s *Store) lastWinnerStreak(ctx context.Context, tx *sql.Tx, chatID int64) (int64, int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT telegram_id FROM draws WHERE chat_id=$1 ORDER BY dt DESC, id DESC LIMIT 3`, chatID)
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
	_, err := s.db.ExecContext(ctx, `UPDATE draws SET text=$1 WHERE id=$2`, text, id)
	return err
}

func (s *Store) TodayWinner(ctx context.Context, chatID int64, dt string) (Winner, error) {
	var w Winner
	var u User
	var adm, act bool
	err := s.db.QueryRowContext(ctx, `SELECT d.id,d.dt,d.text,u.telegram_id,u.chat_id,u.username,u.first_name,u.last_name,u.is_admin,u.active
FROM draws d JOIN users u ON u.chat_id=d.chat_id AND u.telegram_id=d.telegram_id
WHERE d.chat_id=$1 AND d.dt=$2 AND d.manual=FALSE`, chatID, dt).Scan(&w.ID, &w.Date, &w.Text, &u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act)
	u.IsAdmin = adm
	u.Active = act
	w.User = u
	return w, err
}

func (s *Store) Rating(ctx context.Context, chatID int64, limit int) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.telegram_id,u.chat_id,u.username,u.first_name,u.last_name,u.is_admin,u.active,COUNT(d.id) wins,MAX(d.dt) last_win
FROM users u LEFT JOIN draws d ON d.chat_id=u.chat_id AND d.telegram_id=u.telegram_id
WHERE u.chat_id=$1 GROUP BY u.telegram_id,u.chat_id,u.username,u.first_name,u.last_name,u.is_admin,u.active ORDER BY wins DESC, last_win DESC LIMIT $2`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		var adm, act bool
		if err := rows.Scan(&u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act, &u.WinCount, &u.LastWinDate); err != nil {
			return nil, err
		}
		u.IsAdmin = adm
		u.Active = act
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) History(ctx context.Context, chatID int64, limit int) ([]Winner, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.dt,d.text,u.telegram_id,u.chat_id,u.username,u.first_name,u.last_name,u.is_admin,u.active
FROM draws d JOIN users u ON u.chat_id=d.chat_id AND u.telegram_id=d.telegram_id
WHERE d.chat_id=$1 ORDER BY d.dt DESC, d.id DESC LIMIT $2`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Winner
	for rows.Next() {
		var w Winner
		var u User
		var adm, act bool
		if err := rows.Scan(&w.ID, &w.Date, &w.Text, &u.TelegramID, &u.ChatID, &u.Username, &u.FirstName, &u.LastName, &adm, &act); err != nil {
			return nil, err
		}
		u.IsAdmin = adm
		u.Active = act
		w.User = u
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) ActiveChats(ctx context.Context) ([]ChatSettings, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT chat_id,title,draw_time,timezone,exclude_admins,auto_register FROM chats WHERE enabled=true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChatSettings
	for rows.Next() {
		var st ChatSettings
		var ex, ar bool
		if err := rows.Scan(&st.ChatID, &st.Title, &st.DrawTime, &st.Timezone, &ex, &ar); err != nil {
			return nil, err
		}
		st.ExcludeAdmins = ex
		st.AutoRegister = ar
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
