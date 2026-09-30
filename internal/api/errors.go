package api

import (
	"errors"
	"net/http"

	"github.com/PedroLandolt/dice-game/internal/game"
)

var (
	errInvalidRequest        = errors.New("invalid request")
	errInvalidIdempotencyKey = errors.New("invalid idempotency key")
	errUnauthorized          = errors.New("unauthorized")
	errForbidden             = errors.New("forbidden")
	errNotFound              = errors.New("not found")
)

type apiError struct {
	err     error
	status  int
	code    string
	message string
}

var apiErrors = []apiError{
	{game.ErrInvalidBetAmount, http.StatusBadRequest, "INVALID_BET_AMOUNT", "bet amount is outside the allowed limits"},
	{game.ErrInvalidBetType, http.StatusBadRequest, "INVALID_BET_TYPE", "bet type must be even or odd"},
	{errInvalidRequest, http.StatusBadRequest, "INVALID_REQUEST", "request is not valid"},
	{errInvalidIdempotencyKey, http.StatusBadRequest, "INVALID_REQUEST", "Idempotency-Key header is required and must be at most 128 characters"},
	{errUnauthorized, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid token"},
	{errForbidden, http.StatusForbidden, "FORBIDDEN", "client does not match token"},
	{errNotFound, http.StatusNotFound, "NOT_FOUND", "resource not found"},
	{game.ErrPlayAlreadyOpen, http.StatusConflict, "PLAY_ALREADY_OPEN", "another play is still active"},
	{game.ErrPlayInProgress, http.StatusConflict, "PLAY_IN_PROGRESS", "this play is still being processed"},
	{game.ErrNoOpenPlay, http.StatusConflict, "NO_OPEN_PLAY", "there is no open play to end"},
	{game.ErrInsufficientFunds, http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS", "bet exceeds available balance"},
	{game.ErrIdempotencyKeyReused, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", "idempotency key was used with a different request"},
	{game.ErrWalletUnavailable, http.StatusServiceUnavailable, "WALLET_UNAVAILABLE", "wallet is unavailable, try again later"},
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

func (s *server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	matched := lookupError(err)
	requestID := requestIDFrom(r.Context())
	if matched.status >= http.StatusInternalServerError {
		s.logger.Error("request failed", "requestId", requestID, "error", err)
	}
	writeJSON(w, matched.status, errorResponse{Error: errorBody{
		Code:      matched.code,
		Message:   matched.message,
		RequestID: requestID,
	}})
}

func lookupError(err error) apiError {
	for _, candidate := range apiErrors {
		if errors.Is(err, candidate.err) {
			return candidate
		}
	}
	return apiError{status: http.StatusInternalServerError, code: "INTERNAL_ERROR", message: "internal error"}
}
