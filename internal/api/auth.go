package api

import (
	"context"
	"crypto/sha256"
	"net/http"
	"strings"
)

const wsTokenPrefix = "bearer."

func (s *server) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		playerID, err := s.authenticate(r.Context(), bearerToken(r))
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		if r.PathValue("clientId") != playerID {
			s.writeError(w, r, errForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), playerIDKey, playerID)))
	})
}

func (s *server) authenticate(ctx context.Context, token string) (string, error) {
	if token == "" {
		return "", errUnauthorized
	}
	hash := sha256.Sum256([]byte(token))
	playerID, found, err := s.tokens.PlayerID(ctx, hash[:])
	if err != nil {
		return "", err
	}
	if !found {
		return "", errUnauthorized
	}
	return playerID, nil
}

func bearerToken(r *http.Request) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return token
	}
	for protocol := range strings.SplitSeq(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		if token, ok := strings.CutPrefix(strings.TrimSpace(protocol), wsTokenPrefix); ok {
			return token
		}
	}
	return ""
}

func playerIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(playerIDKey).(string)
	return id
}
