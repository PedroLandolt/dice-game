package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/PedroLandolt/dice-game/internal/game"
)

const (
	maxBodyBytes            = 1 << 10
	maxIdempotencyKeyLength = 128
)

type playRequest struct {
	Amount int64        `json:"amount"`
	Type   game.BetType `json:"type"`
}

type playResponse struct {
	PlayID  string `json:"playId"`
	Rolled  int    `json:"rolled"`
	Result  string `json:"result"`
	Payout  int64  `json:"payout"`
	Balance int64  `json:"balance"`
}

type walletResponse struct {
	ClientID string        `json:"clientId"`
	Balance  int64         `json:"balance"`
	Currency string        `json:"currency"`
	OpenPlay *playResponse `json:"openPlay"`
}

type endPlayResponse struct {
	PlayID   string `json:"playId"`
	Credited int64  `json:"credited"`
	Balance  int64  `json:"balance"`
}

func (s *server) handleWallet(w http.ResponseWriter, r *http.Request) {
	playerID := playerIDFrom(r.Context())
	state, err := s.game.Wallet(r.Context(), playerID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newWalletResponse(playerID, state))
}

func (s *server) handlePlay(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > maxIdempotencyKeyLength {
		s.writeError(w, r, errInvalidIdempotencyKey)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		s.writeError(w, r, fmt.Errorf("%w: %w", errInvalidRequest, err))
		return
	}
	var request playRequest
	if err := decodeJSON(body, &request); err != nil {
		s.writeError(w, r, err)
		return
	}
	play, err := s.game.Play(r.Context(), playerIDFrom(r.Context()), key, request.Amount, request.Type)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newPlayResponse(play))
}

func (s *server) handleEndPlay(w http.ResponseWriter, r *http.Request) {
	play, balance, err := s.game.EndPlay(r.Context(), playerIDFrom(r.Context()))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, endPlayResponse{PlayID: play.ID, Credited: play.Payout, Balance: balance})
}

func (s *server) handleDevReset(w http.ResponseWriter, r *http.Request) {
	if err := s.devReset(r.Context()); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, errNotFound)
}

func newWalletResponse(playerID string, state game.WalletState) walletResponse {
	response := walletResponse{ClientID: playerID, Balance: state.Balance, Currency: state.Currency}
	if state.OpenPlay != nil {
		openPlay := newPlayResponse(*state.OpenPlay)
		response.OpenPlay = &openPlay
	}
	return response
}

func newPlayResponse(play game.Play) playResponse {
	result := "lose"
	if play.Won {
		result = "win"
	}
	return playResponse{
		PlayID:  play.ID,
		Rolled:  play.Rolled,
		Result:  result,
		Payout:  play.Payout,
		Balance: play.BalanceAfter,
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func decodeJSON(data []byte, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("%w: %w", errInvalidRequest, err)
	}
	if decoder.More() {
		return fmt.Errorf("%w: unexpected data after json body", errInvalidRequest)
	}
	return nil
}
