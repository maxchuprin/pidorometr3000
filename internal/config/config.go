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
	BotToken             string
	DatabasePath         string
	Timezone             string
	DefaultDrawTime      string
	DefaultTitle         string
	DefaultExcludeAdmins bool
	Debug                bool
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		BotToken:             strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		DatabasePath:         getenv("DATABASE_PATH", "./pidorometr3000.db"),
		Timezone:             getenv("TIMEZONE", "Asia/Aqtobe"),
		DefaultDrawTime:      getenv("DEFAULT_DRAW_TIME", "09:00"),
		DefaultTitle:         getenv("DEFAULT_TITLE", "Пидор дня"),
		DefaultExcludeAdmins: getenvBool("DEFAULT_EXCLUDE_ADMINS", true),
		Debug:                getenvBool("DEBUG", false),
	}
	if cfg.BotToken == "" {
		return cfg, errors.New("TELEGRAM_BOT_TOKEN is required")
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
