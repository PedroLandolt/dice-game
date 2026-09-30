package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PedroLandolt/dice-game/internal/game"
)

const playColumns = `id, player_id, request_id, amount, bet_type,
	COALESCE(rolled, 0), COALESCE(won, false), COALESCE(payout, 0), COALESCE(balance_after, 0),
	status, COALESCE(error_code, '')`

type Plays struct {
	pool *pgxpool.Pool
}

func NewPlays(pool *pgxpool.Pool) *Plays {
	return &Plays{pool: pool}
}

func (p *Plays) Create(ctx context.Context, play game.Play) (game.Play, error) {
	created, err := scanPlay(p.pool.QueryRow(ctx,
		`INSERT INTO plays (player_id, request_id, amount, bet_type, status)
		VALUES ($1, $2, $3, $4, 'pending')
		RETURNING `+playColumns,
		play.PlayerID, play.RequestID, play.Amount, play.BetType,
	))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "plays_one_active_per_player":
			return game.Play{}, game.ErrPlayAlreadyOpen
		case "plays_player_request_unique":
			return game.Play{}, game.ErrPlayInProgress
		}
	}
	if err != nil {
		return game.Play{}, fmt.Errorf("create play: %w", err)
	}
	return created, nil
}

func (p *Plays) FindByRequestID(ctx context.Context, playerID, requestID string) (game.Play, bool, error) {
	return p.findPlay(ctx,
		`SELECT `+playColumns+` FROM plays WHERE player_id = $1 AND request_id = $2`,
		playerID, requestID,
	)
}

func (p *Plays) FindOpen(ctx context.Context, playerID string) (game.Play, bool, error) {
	return p.findPlay(ctx,
		`SELECT `+playColumns+` FROM plays WHERE player_id = $1 AND status = 'open'`,
		playerID,
	)
}

func (p *Plays) FindStale(ctx context.Context, status game.PlayStatus, olderThan time.Duration) ([]game.Play, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+playColumns+` FROM plays
		WHERE status IN ('pending', 'open') AND status = $1 AND created_at < now() - $2::interval
		ORDER BY created_at
		LIMIT 100`,
		status, olderThan,
	)
	if err != nil {
		return nil, fmt.Errorf("find stale plays: %w", err)
	}
	plays, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (game.Play, error) {
		return scanPlay(row)
	})
	if err != nil {
		return nil, fmt.Errorf("find stale plays: %w", err)
	}
	return plays, nil
}

func (p *Plays) MarkOpen(ctx context.Context, play game.Play) (bool, error) {
	tag, err := p.pool.Exec(ctx,
		`UPDATE plays
		SET status = 'open', rolled = $2, won = $3, payout = $4, balance_after = $5
		WHERE id = $1 AND status = 'pending'`,
		play.ID, play.Rolled, play.Won, play.Payout, play.BalanceAfter,
	)
	if err != nil {
		return false, fmt.Errorf("mark play open: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (p *Plays) MarkRejected(ctx context.Context, id, errorCode string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE plays SET status = 'rejected', error_code = $2, closed_at = now()
		WHERE id = $1 AND status = 'pending'`,
		id, errorCode,
	)
	if err != nil {
		return fmt.Errorf("mark play rejected: %w", err)
	}
	return nil
}

func (p *Plays) MarkClosed(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE plays SET status = 'closed', closed_at = now()
		WHERE id = $1 AND status = 'open'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("mark play closed: %w", err)
	}
	return nil
}

func (p *Plays) findPlay(ctx context.Context, query string, args ...any) (game.Play, bool, error) {
	play, err := scanPlay(p.pool.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return game.Play{}, false, nil
	}
	if err != nil {
		return game.Play{}, false, fmt.Errorf("find play: %w", err)
	}
	return play, true, nil
}

func scanPlay(row pgx.Row) (game.Play, error) {
	var play game.Play
	err := row.Scan(
		&play.ID, &play.PlayerID, &play.RequestID, &play.Amount, &play.BetType,
		&play.Rolled, &play.Won, &play.Payout, &play.BalanceAfter,
		&play.Status, &play.ErrorCode,
	)
	return play, err
}
