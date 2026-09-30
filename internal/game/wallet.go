package game

import (
	"context"
	"errors"
)

var (
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrTxRolledBack      = errors.New("transaction rolled back")
)

type Wallet interface {
	Balance(ctx context.Context, playerID string) (balance int64, currency string, err error)
	Debit(ctx context.Context, playerID string, amount int64, txID string) (balanceAfter int64, err error)
	Credit(ctx context.Context, playerID string, amount int64, txID string) (balanceAfter int64, err error)
	Rollback(ctx context.Context, playerID string, txID string) error
}
