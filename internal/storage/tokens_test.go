package storage

import (
	"crypto/sha256"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTokensPlayerID(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tokens := NewTokens(pool)

	known := sha256.Sum256([]byte("dev-gandalf"))
	playerID, found, err := tokens.PlayerID(t.Context(), known[:])
	if err != nil || !found || playerID != "gandalf" {
		t.Errorf("PlayerID(dev-gandalf) = %q, %v, %v; want gandalf, true, nil", playerID, found, err)
	}

	unknown := sha256.Sum256([]byte("not-a-token"))
	if _, found, err := tokens.PlayerID(t.Context(), unknown[:]); err != nil || found {
		t.Errorf("PlayerID(unknown) = %v, %v; want false, nil", found, err)
	}
}
