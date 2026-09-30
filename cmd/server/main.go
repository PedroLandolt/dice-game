package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PedroLandolt/dice-game/internal/config"
	"github.com/PedroLandolt/dice-game/internal/game"
	"github.com/PedroLandolt/dice-game/internal/storage"
	"github.com/PedroLandolt/dice-game/internal/wallet"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	limits := game.BetLimits{Min: cfg.MinBet, Max: cfg.MaxBet}
	service := game.NewService(wallet.NewPostgres(pool), storage.NewPlays(pool), limits, cfg.WalletTimeout)
	go service.RunCleanup(ctx, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	logger.Info("server starting", "addr", cfg.Addr)
	return http.ListenAndServe(cfg.Addr, mux)
}
