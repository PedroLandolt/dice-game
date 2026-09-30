package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr             string
	DatabaseURL      string
	MinBet           int64
	MaxBet           int64
	WalletTimeout    time.Duration
	DevMode          bool
	WSAllowedOrigins []string
}

func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	minBet, err := envInt64("MIN_BET", 5)
	if err != nil {
		return Config{}, err
	}
	maxBet, err := envInt64("MAX_BET", 2000)
	if err != nil {
		return Config{}, err
	}
	walletTimeout, err := envDuration("WALLET_TIMEOUT", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Addr:             envOr("HTTP_ADDR", ":8080"),
		DatabaseURL:      databaseURL,
		MinBet:           minBet,
		MaxBet:           maxBet,
		WalletTimeout:    walletTimeout,
		DevMode:          os.Getenv("DEV_MODE") == "true",
		WSAllowedOrigins: envList("WS_ALLOWED_ORIGINS"),
	}, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt64(key string, fallback int64) (int64, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return n, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return d, nil
}

func envList(key string) []string {
	var values []string
	for value := range strings.SplitSeq(os.Getenv(key), ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}
