package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	wsSubprotocol     = "dice.v1"
	maxWSMessageBytes = 1 << 10
	pingInterval      = 30 * time.Second
)

type wsMessage struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId"`
	Data      json.RawMessage `json:"data"`
}

type wsReply struct {
	Type      string   `json:"type"`
	RequestID string   `json:"requestId,omitempty"`
	Data      any      `json:"data,omitempty"`
	Error     *wsError `json:"error,omitempty"`
}

type wsError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	playerID, err := s.authenticate(r.Context(), bearerToken(r))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols:   []string{wsSubprotocol},
		OriginPatterns: s.wsOrigins,
	})
	if err != nil {
		return
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(maxWSMessageBytes)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go keepAlive(ctx, conn)

	if err := wsjson.Write(ctx, conn, s.walletReply(ctx, playerID, "")); err != nil {
		return
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if err := wsjson.Write(ctx, conn, s.handleWSMessage(ctx, playerID, data)); err != nil {
			return
		}
	}
}

func (s *server) handleWSMessage(ctx context.Context, playerID string, data []byte) wsReply {
	var message wsMessage
	if err := decodeJSON(data, &message); err != nil {
		return s.errorReply(ctx, "", err)
	}
	switch message.Type {
	case "wallet":
		return s.walletReply(ctx, playerID, message.RequestID)
	case "play":
		return s.playReply(ctx, playerID, message)
	case "end_play":
		return s.endPlayReply(ctx, playerID, message.RequestID)
	}
	return s.errorReply(ctx, message.RequestID, fmt.Errorf("%w: unknown message type %q", errInvalidRequest, message.Type))
}

func (s *server) walletReply(ctx context.Context, playerID, requestID string) wsReply {
	state, err := s.game.Wallet(ctx, playerID)
	if err != nil {
		return s.errorReply(ctx, requestID, err)
	}
	return wsReply{Type: "wallet_result", RequestID: requestID, Data: newWalletResponse(playerID, state)}
}

func (s *server) playReply(ctx context.Context, playerID string, message wsMessage) wsReply {
	if message.RequestID == "" || len(message.RequestID) > maxIdempotencyKeyLength {
		return s.errorReply(ctx, message.RequestID, fmt.Errorf("%w: requestId is required", errInvalidRequest))
	}
	var request playRequest
	if err := decodeJSON(message.Data, &request); err != nil {
		return s.errorReply(ctx, message.RequestID, err)
	}
	play, err := s.game.Play(ctx, playerID, message.RequestID, request.Amount, request.Type)
	if err != nil {
		return s.errorReply(ctx, message.RequestID, err)
	}
	return wsReply{Type: "play_result", RequestID: message.RequestID, Data: newPlayResponse(play)}
}

func (s *server) endPlayReply(ctx context.Context, playerID, requestID string) wsReply {
	play, balance, err := s.game.EndPlay(ctx, playerID)
	if err != nil {
		return s.errorReply(ctx, requestID, err)
	}
	return wsReply{Type: "end_play_result", RequestID: requestID, Data: endPlayResponse{PlayID: play.ID, Credited: play.Payout, Balance: balance}}
}

func (s *server) errorReply(ctx context.Context, requestID string, err error) wsReply {
	matched := lookupError(err)
	if matched.status >= http.StatusInternalServerError {
		s.logger.Error("websocket message failed", "requestId", requestIDFrom(ctx), "messageRequestId", requestID, "error", err)
	}
	return wsReply{Type: "error", RequestID: requestID, Error: &wsError{Code: matched.code, Message: matched.message}}
}

func keepAlive(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, pingInterval/2)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				_ = conn.CloseNow()
				return
			}
		}
	}
}
