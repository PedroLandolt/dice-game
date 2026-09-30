package game

import (
	"context"
	"errors"
	"time"
)

type PlayStatus string

const (
	StatusPending  PlayStatus = "pending"
	StatusOpen     PlayStatus = "open"
	StatusClosed   PlayStatus = "closed"
	StatusRejected PlayStatus = "rejected"
)

var (
	ErrPlayAlreadyOpen = errors.New("play already open")
	ErrPlayInProgress  = errors.New("play in progress")
)

type Play struct {
	ID           string
	PlayerID     string
	RequestID    string
	Amount       int64
	BetType      BetType
	Rolled       int
	Won          bool
	Payout       int64
	BalanceAfter int64
	Status       PlayStatus
	ErrorCode    string
}

type PlayStore interface {
	Create(ctx context.Context, play Play) (Play, error)
	FindByRequestID(ctx context.Context, playerID, requestID string) (Play, bool, error)
	FindOpen(ctx context.Context, playerID string) (Play, bool, error)
	FindStale(ctx context.Context, status PlayStatus, olderThan time.Duration) ([]Play, error)
	MarkOpen(ctx context.Context, play Play) (bool, error)
	MarkRejected(ctx context.Context, id, errorCode string) error
	MarkClosed(ctx context.Context, id string) error
}
