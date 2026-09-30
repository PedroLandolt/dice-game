package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PedroLandolt/dice-game/internal/api"
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

	walletStore := wallet.NewPostgres(pool)
	limits := game.BetLimits{Min: cfg.MinBet, Max: cfg.MaxBet}
	service := game.NewService(walletStore, storage.NewPlays(pool), limits, cfg.WalletTimeout)
	go service.RunCleanup(ctx, logger)

	var devReset func(context.Context) error
	if cfg.DevMode {
		devReset = walletStore.Reset
	}
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.NewHandler(service, storage.NewTokens(pool), logger, devReset, cfg.WSAllowedOrigins),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server starting", "addr", cfg.Addr, "devMode", cfg.DevMode)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("server shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}
