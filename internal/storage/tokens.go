package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Tokens struct {
	pool *pgxpool.Pool
}

func NewTokens(pool *pgxpool.Pool) *Tokens {
	return &Tokens{pool: pool}
}

func (t *Tokens) PlayerID(ctx context.Context, tokenHash []byte) (string, bool, error) {
	var playerID string
	err := t.pool.QueryRow(ctx,
		`SELECT player_id FROM api_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&playerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find token: %w", err)
	}
	return playerID, true, nil
}
