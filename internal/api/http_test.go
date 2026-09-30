package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PedroLandolt/dice-game/internal/game"
)

func TestAuth(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		path       string
		wantStatus int
	}{
		{"no token", "", "/v1/clients/gandalf/wallet", http.StatusUnauthorized},
		{"unknown token", "nope", "/v1/clients/gandalf/wallet", http.StatusUnauthorized},
		{"other client", "token-mr-robot", "/v1/clients/gandalf/wallet", http.StatusForbidden},
		{"own client", "token-gandalf", "/v1/clients/gandalf/wallet", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := send(newTestHandler(&fakeGame{}, nil), http.MethodGet, tt.path, tt.token, "", nil)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d; want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestPlayBadRequests(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		body    string
		wantErr string
	}{
		{"missing idempotency key", "", `{"amount":500,"type":"even"}`, "INVALID_REQUEST"},
		{"idempotency key too long", strings.Repeat("k", 129), `{"amount":500,"type":"even"}`, "INVALID_REQUEST"},
		{"invalid json", "k1", `{"amount":`, "INVALID_REQUEST"},
		{"unknown field", "k1", `{"amount":500,"type":"even","bonus":true}`, "INVALID_REQUEST"},
		{"decimal amount", "k1", `{"amount":5.5,"type":"even"}`, "INVALID_REQUEST"},
		{"trailing data", "k1", `{"amount":500,"type":"even"} {}`, "INVALID_REQUEST"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{"Idempotency-Key": tt.key}
			rec := send(newTestHandler(&fakeGame{}, nil), http.MethodPost, "/v1/clients/gandalf/play", "token-gandalf", tt.body, headers)
			if rec.Code != http.StatusBadRequest || decodeError(t, rec).Code != tt.wantErr {
				t.Errorf("response = %d %s; want 400 %s", rec.Code, rec.Body, tt.wantErr)
			}
		})
	}
}

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{game.ErrInvalidBetAmount, http.StatusBadRequest, "INVALID_BET_AMOUNT"},
		{game.ErrPlayAlreadyOpen, http.StatusConflict, "PLAY_ALREADY_OPEN"},
		{game.ErrInsufficientFunds, http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS"},
		{game.ErrIdempotencyKeyReused, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED"},
		{game.ErrWalletUnavailable, http.StatusServiceUnavailable, "WALLET_UNAVAILABLE"},
		{errors.New("pgx: relation plays does not exist"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.wantCode, func(t *testing.T) {
			headers := map[string]string{"Idempotency-Key": "k1"}
			rec := send(newTestHandler(&fakeGame{err: tt.err}, nil), http.MethodPost, "/v1/clients/gandalf/play", "token-gandalf", `{"amount":500,"type":"even"}`, headers)
			body := decodeError(t, rec)
			if rec.Code != tt.wantStatus || body.Code != tt.wantCode {
				t.Errorf("response = %d %s; want %d %s", rec.Code, body.Code, tt.wantStatus, tt.wantCode)
			}
			if body.RequestID == "" || body.RequestID != rec.Header().Get("X-Request-Id") {
				t.Errorf("requestId = %q; want the X-Request-Id header %q", body.RequestID, rec.Header().Get("X-Request-Id"))
			}
			if strings.Contains(rec.Body.String(), "plays") {
				t.Errorf("body leaks internal details: %s", rec.Body)
			}
		})
	}
}

func TestPanicIsRecovered(t *testing.T) {
	rec := send(newTestHandler(&fakeGame{panics: true}, nil), http.MethodGet, "/v1/clients/gandalf/wallet", "token-gandalf", "", nil)
	if rec.Code != http.StatusInternalServerError || decodeError(t, rec).Code != "INTERNAL_ERROR" {
		t.Errorf("response = %d %s; want 500 INTERNAL_ERROR", rec.Code, rec.Body)
	}
}

func TestRequestID(t *testing.T) {
	handler := newTestHandler(&fakeGame{}, nil)

	rec := send(handler, http.MethodGet, "/healthz", "", "", map[string]string{"X-Request-Id": "abc-123"})
	if got := rec.Header().Get("X-Request-Id"); got != "abc-123" {
		t.Errorf("X-Request-Id = %q; want the client's abc-123", got)
	}
	rec = send(handler, http.MethodGet, "/healthz", "", "", map[string]string{"X-Request-Id": "bad id\ninjected"})
	if got := rec.Header().Get("X-Request-Id"); got == "" || strings.Contains(got, "injected") {
		t.Errorf("X-Request-Id = %q; want a newly generated id", got)
	}
}

func TestPlayAndWalletResponses(t *testing.T) {
	play := game.Play{ID: "p1", Rolled: 4, Won: true, Payout: 1000, BalanceAfter: 9500}
	handler := newTestHandler(&fakeGame{play: play, state: game.WalletState{Balance: 9500, Currency: "EUR", OpenPlay: &play}}, nil)

	rec := send(handler, http.MethodPost, "/v1/clients/gandalf/play", "token-gandalf", `{"amount":500,"type":"even"}`, map[string]string{"Idempotency-Key": "k1"})
	want := `{"playId":"p1","rolled":4,"result":"win","payout":1000,"balance":9500}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("play response = %d %s; want 200 %s", rec.Code, rec.Body, want)
	}

	rec = send(handler, http.MethodGet, "/v1/clients/gandalf/wallet", "token-gandalf", "", nil)
	want = `{"clientId":"gandalf","balance":9500,"currency":"EUR","openPlay":{"playId":"p1","rolled":4,"result":"win","payout":1000,"balance":9500}}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("wallet response = %d %s; want 200 %s", rec.Code, rec.Body, want)
	}
}

func TestNotFoundAndDevReset(t *testing.T) {
	rec := send(newTestHandler(&fakeGame{}, nil), http.MethodPost, "/dev/reset", "", "", nil)
	if rec.Code != http.StatusNotFound || decodeError(t, rec).Code != "NOT_FOUND" {
		t.Errorf("reset without dev mode = %d %s; want 404 NOT_FOUND", rec.Code, rec.Body)
	}

	resets := 0
	reset := func(context.Context) error {
		resets++
		return nil
	}
	rec = send(newTestHandler(&fakeGame{}, reset), http.MethodPost, "/dev/reset", "", "", nil)
	if rec.Code != http.StatusNoContent || resets != 1 {
		t.Errorf("reset in dev mode = %d with %d resets; want 204 with 1", rec.Code, resets)
	}
}

func newTestHandler(g *fakeGame, devReset func(context.Context) error) http.Handler {
	tokens := fakeTokens{"token-gandalf": "gandalf", "token-mr-robot": "mr-robot"}
	return NewHandler(g, tokens, slog.New(slog.DiscardHandler), devReset, nil)
}

func send(handler http.Handler, method, path, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var response errorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body, err)
	}
	return response.Error
}

type fakeGame struct {
	state        game.WalletState
	play         game.Play
	err          error
	panics       bool
	playRequests chan string
}

func (g *fakeGame) Wallet(ctx context.Context, playerID string) (game.WalletState, error) {
	if g.panics {
		panic("boom")
	}
	return g.state, g.err
}

func (g *fakeGame) Play(ctx context.Context, playerID, requestID string, amount int64, bet game.BetType) (game.Play, error) {
	if g.playRequests != nil {
		g.playRequests <- requestID
	}
	return g.play, g.err
}

func (g *fakeGame) EndPlay(ctx context.Context, playerID string) (game.Play, int64, error) {
	return g.play, g.state.Balance, g.err
}

type fakeTokens map[string]string

func (f fakeTokens) PlayerID(ctx context.Context, tokenHash []byte) (string, bool, error) {
	for token, playerID := range f {
		if hash := sha256.Sum256([]byte(token)); bytes.Equal(hash[:], tokenHash) {
			return playerID, true, nil
		}
	}
	return "", false, nil
}
