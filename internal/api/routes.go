package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/PedroLandolt/dice-game/internal/game"
)

type Game interface {
	Wallet(ctx context.Context, playerID string) (game.WalletState, error)
	Play(ctx context.Context, playerID, requestID string, amount int64, bet game.BetType) (game.Play, error)
	EndPlay(ctx context.Context, playerID string) (game.Play, int64, error)
}

type Tokens interface {
	PlayerID(ctx context.Context, tokenHash []byte) (string, bool, error)
}

type server struct {
	game      Game
	tokens    Tokens
	logger    *slog.Logger
	devReset  func(context.Context) error
	wsOrigins []string
}

func NewHandler(service Game, tokens Tokens, logger *slog.Logger, devReset func(context.Context) error, wsOrigins []string) http.Handler {
	s := &server{game: service, tokens: tokens, logger: logger, devReset: devReset, wsOrigins: wsOrigins}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle("GET /v1/clients/{clientId}/wallet", s.requireAuth(s.handleWallet))
	mux.Handle("POST /v1/clients/{clientId}/play", s.requireAuth(s.handlePlay))
	mux.Handle("POST /v1/clients/{clientId}/end-play", s.requireAuth(s.handleEndPlay))
	mux.HandleFunc("GET /v1/ws", s.handleWS)
	if devReset != nil {
		mux.HandleFunc("POST /dev/reset", s.handleDevReset)
	}
	mux.HandleFunc("/", s.handleNotFound)

	return withRequestID(s.withLogging(s.withRecover(mux)))
}
