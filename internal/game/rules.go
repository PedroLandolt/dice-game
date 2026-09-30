package game

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
)

type BetType string

const (
	BetEven BetType = "even"
	BetOdd  BetType = "odd"
)

var (
	ErrInvalidBetAmount = errors.New("invalid bet amount")
	ErrInvalidBetType   = errors.New("invalid bet type")
)

type BetLimits struct {
	Min int64
	Max int64
}

func (l BetLimits) Validate(amount int64, bet BetType) error {
	if amount <= 0 || amount < l.Min || amount > l.Max {
		return ErrInvalidBetAmount
	}
	if bet != BetEven && bet != BetOdd {
		return ErrInvalidBetType
	}
	return nil
}

func RollD6() (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(6))
	if err != nil {
		return 0, fmt.Errorf("roll d6: %w", err)
	}
	return int(n.Int64()) + 1, nil
}

func Settle(rolled int, bet BetType, amount int64) (won bool, payout int64) {
	rolledEven := rolled%2 == 0
	if rolledEven != (bet == BetEven) {
		return false, 0
	}
	return true, 2 * amount
}
