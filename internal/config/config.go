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
	DatabaseURL          string
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
		DatabaseURL:          getenv("DATABASE_URL", ""),
		Timezone:             getenv("TIMEZONE", "Asia/Aqtobe"),
		DefaultDrawTime:      getenv("DEFAULT_DRAW_TIME", "07:00"),
		DefaultTitle:         getenv("DEFAULT_TITLE", "Қотақбас дня"),
		DefaultExcludeAdmins: getenvBool("DEFAULT_EXCLUDE_ADMINS", false),
		Debug:                getenvBool("DEBUG", false),
	}
	if cfg.BotToken == "" {
		return cfg, errors.New("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.DatabaseURL == "" {
		return cfg, errors.New("DATABASE_URL is required")
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
