package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/PedroLandolt/dice-game/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	logger.Info("server starting", "addr", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, mux); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
