package main

import (
	"log/slog"
	"os"

	"pidorometr3000/internal/app"
	"pidorometr3000/internal/config"
	"pidorometr3000/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("config error", "err", err)
		os.Exit(1)
	}

	db, err := store.Open(cfg.DatabasePath)
	if err != nil {
		logger.Error("db open error", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		logger.Error("db migrate error", "err", err)
		os.Exit(1)
	}

	bot, err := app.New(cfg, db, logger)
	if err != nil {
		logger.Error("bot init error", "err", err)
		os.Exit(1)
	}
	if err := bot.Run(); err != nil {
		logger.Error("bot stopped", "err", err)
		os.Exit(1)
	}
}
