package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PedroLandolt/dice-game/internal/game"
)

type Postgres struct {
	pool *pgxpool.Pool
}

type txRecord struct {
	amount       int64
	balanceAfter int64
}

func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

func (p *Postgres) Balance(ctx context.Context, playerID string) (int64, string, error) {
	var balance int64
	var currency string
	err := p.pool.QueryRow(ctx,
		`SELECT balance, currency FROM wallets WHERE player_id = $1`,
		playerID,
	).Scan(&balance, &currency)
	if err != nil {
		return 0, "", fmt.Errorf("get balance: %w", err)
	}
	return balance, currency, nil
}

func (p *Postgres) Debit(ctx context.Context, playerID string, amount int64, txID string) (int64, error) {
	var balanceAfter int64
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		balance, err := lockBalance(ctx, tx, playerID)
		if err != nil {
			return err
		}
		_, rolledBack, err := findTx(ctx, tx, playerID, txID, "rollback")
		if err != nil {
			return err
		}
		if rolledBack {
			return game.ErrTxRolledBack
		}
		debit, found, err := findTx(ctx, tx, playerID, txID, "debit")
		if err != nil {
			return err
		}
		if found {
			balanceAfter = debit.balanceAfter
			return nil
		}
		if balance < amount {
			return game.ErrInsufficientFunds
		}
		balanceAfter = balance - amount
		return recordTx(ctx, tx, playerID, txID, "debit", amount, balanceAfter)
	})
	if err != nil {
		return 0, fmt.Errorf("debit %s: %w", txID, err)
	}
	return balanceAfter, nil
}

func (p *Postgres) Credit(ctx context.Context, playerID string, amount int64, txID string) (int64, error) {
	var balanceAfter int64
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		balance, err := lockBalance(ctx, tx, playerID)
		if err != nil {
			return err
		}
		credit, found, err := findTx(ctx, tx, playerID, txID, "credit")
		if err != nil {
			return err
		}
		if found {
			balanceAfter = credit.balanceAfter
			return nil
		}
		balanceAfter = balance + amount
		return recordTx(ctx, tx, playerID, txID, "credit", amount, balanceAfter)
	})
	if err != nil {
		return 0, fmt.Errorf("credit %s: %w", txID, err)
	}
	return balanceAfter, nil
}

func (p *Postgres) Rollback(ctx context.Context, playerID string, txID string) error {
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		balance, err := lockBalance(ctx, tx, playerID)
		if err != nil {
			return err
		}
		_, done, err := findTx(ctx, tx, playerID, txID, "rollback")
		if err != nil || done {
			return err
		}
		debit, debited, err := findTx(ctx, tx, playerID, txID, "debit")
		if err != nil {
			return err
		}
		if debited {
			return recordTx(ctx, tx, playerID, txID, "rollback", debit.amount, balance+debit.amount)
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO wallet_transactions (tx_id, kind, player_id, amount, balance_after)
			VALUES ($1, 'rollback', $2, 0, $3)`,
			txID, playerID, balance,
		)
		if err != nil {
			return fmt.Errorf("insert rollback marker: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("rollback %s: %w", txID, err)
	}
	return nil
}

func lockBalance(ctx context.Context, tx pgx.Tx, playerID string) (int64, error) {
	var balance int64
	err := tx.QueryRow(ctx,
		`SELECT balance FROM wallets WHERE player_id = $1 FOR UPDATE`,
		playerID,
	).Scan(&balance)
	if err != nil {
		return 0, fmt.Errorf("lock wallet: %w", err)
	}
	return balance, nil
}

func findTx(ctx context.Context, tx pgx.Tx, playerID, txID, kind string) (txRecord, bool, error) {
	var record txRecord
	err := tx.QueryRow(ctx,
		`SELECT amount, balance_after FROM wallet_transactions
		WHERE tx_id = $1 AND kind = $2 AND player_id = $3`,
		txID, kind, playerID,
	).Scan(&record.amount, &record.balanceAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return txRecord{}, false, nil
	}
	if err != nil {
		return txRecord{}, false, fmt.Errorf("find tx: %w", err)
	}
	return record, true, nil
}

func recordTx(ctx context.Context, tx pgx.Tx, playerID, txID, kind string, amount, balanceAfter int64) error {
	ledgerAmount := amount
	if kind == "debit" {
		ledgerAmount = -amount
	}
	if _, err := tx.Exec(ctx,
		`UPDATE wallets SET balance = $2 WHERE player_id = $1`,
		playerID, balanceAfter,
	); err != nil {
		return fmt.Errorf("update balance: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO wallet_transactions (tx_id, kind, player_id, amount, balance_after)
		VALUES ($1, $2, $3, $4, $5)`,
		txID, kind, playerID, amount, balanceAfter,
	); err != nil {
		return fmt.Errorf("insert wallet transaction: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO ledger_entries (player_id, tx_id, kind, amount, balance_after)
		VALUES ($1, $2, $3, $4, $5)`,
		playerID, txID, kind, ledgerAmount, balanceAfter,
	); err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}
	return nil
}
