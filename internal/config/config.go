package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	BotToken        string
	DatabaseURL     string
	Timezone        string
	DefaultDrawTime string
	DefaultTitle    string
	Debug           bool
	OwnerTelegramID int64
	BonusTokenTTL   time.Duration
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		BotToken:        strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		DatabaseURL:     getenv("DATABASE_URL", ""),
		Timezone:        getenv("TIMEZONE", "Asia/Aqtobe"),
		DefaultDrawTime: getenv("DEFAULT_DRAW_TIME", "07:00"),
		DefaultTitle:    getenv("DEFAULT_TITLE", "Қотақбас дня"),
		Debug:           getenvBool("DEBUG", false),
		OwnerTelegramID: getenvInt64("OWNER_TELEGRAM_ID", 0),
		BonusTokenTTL:   time.Duration(getenvInt64("BONUS_TOKEN_TTL_HOURS", 24)) * time.Hour,
	}
	if cfg.BotToken == "" {
		return cfg, errors.New("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("DATABASE_URL is required")
	}
	if cfg.BonusTokenTTL <= 0 {
		return cfg, errors.New("BONUS_TOKEN_TTL_HOURS must be greater than zero")
	}

	if _, err := time.LoadLocation(cfg.Timezone); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func getenv(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func getenvBool(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getenvInt64(key string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
