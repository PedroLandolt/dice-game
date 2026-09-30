package httpapi

import (
	"net/http"

	"github.com/PedroLandolt/dice-game/internal/game"
)

const maxIdempotencyKeyLength = 128

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
	response := walletResponse{ClientID: playerID, Balance: state.Balance, Currency: state.Currency}
	if state.OpenPlay != nil {
		openPlay := newPlayResponse(*state.OpenPlay)
		response.OpenPlay = &openPlay
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) handlePlay(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > maxIdempotencyKeyLength {
		s.writeError(w, r, errInvalidIdempotencyKey)
		return
	}
	var request playRequest
	if err := decodeJSON(w, r, &request); err != nil {
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
