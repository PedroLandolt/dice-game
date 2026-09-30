package game

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	cleanupInterval = 5 * time.Second
	stalePendingAge = 10 * time.Second
	staleOpenAge    = 5 * time.Minute
)

var (
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with a different request")
	ErrNoOpenPlay           = errors.New("no open play")
	ErrWalletUnavailable    = errors.New("wallet unavailable")
)

var rejectionErrors = map[string]error{
	"INSUFFICIENT_FUNDS": ErrInsufficientFunds,
	"WALLET_UNAVAILABLE": ErrWalletUnavailable,
}

type Service struct {
	wallet        Wallet
	plays         PlayStore
	limits        BetLimits
	walletTimeout time.Duration
}

func NewService(wallet Wallet, plays PlayStore, limits BetLimits, walletTimeout time.Duration) *Service {
	return &Service{wallet: wallet, plays: plays, limits: limits, walletTimeout: walletTimeout}
}

func (s *Service) Balance(ctx context.Context, playerID string) (int64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.walletTimeout)
	defer cancel()
	balance, currency, err := s.wallet.Balance(ctx, playerID)
	if err != nil {
		return 0, "", fmt.Errorf("%w: %w", ErrWalletUnavailable, err)
	}
	return balance, currency, nil
}

func (s *Service) Play(ctx context.Context, playerID, requestID string, amount int64, bet BetType) (Play, error) {
	if err := s.limits.Validate(amount, bet); err != nil {
		return Play{}, err
	}
	existing, found, err := s.plays.FindByRequestID(ctx, playerID, requestID)
	if err != nil {
		return Play{}, err
	}
	if found {
		return replay(existing, amount, bet)
	}
	play, err := s.plays.Create(ctx, Play{PlayerID: playerID, RequestID: requestID, Amount: amount, BetType: bet})
	if err != nil {
		return Play{}, err
	}
	balanceAfter, err := s.debit(ctx, play)
	if err != nil {
		return Play{}, err
	}
	rolled, err := RollD6()
	if err != nil {
		return Play{}, err
	}
	play.Rolled = rolled
	play.Won, play.Payout = Settle(rolled, bet, amount)
	play.BalanceAfter = balanceAfter
	opened, err := s.plays.MarkOpen(context.WithoutCancel(ctx), play)
	if err != nil {
		return Play{}, err
	}
	if !opened {
		return Play{}, ErrWalletUnavailable
	}
	play.Status = StatusOpen
	return play, nil
}

func (s *Service) EndPlay(ctx context.Context, playerID string) (Play, int64, error) {
	play, found, err := s.plays.FindOpen(ctx, playerID)
	if err != nil {
		return Play{}, 0, err
	}
	if !found {
		return Play{}, 0, ErrNoOpenPlay
	}
	balance, err := s.settle(ctx, play)
	if err != nil {
		return Play{}, 0, err
	}
	play.Status = StatusClosed
	return play, balance, nil
}

func (s *Service) debit(ctx context.Context, play Play) (int64, error) {
	debitCtx, cancel := context.WithTimeout(ctx, s.walletTimeout)
	defer cancel()
	balanceAfter, err := s.wallet.Debit(debitCtx, play.PlayerID, play.Amount, play.ID+":debit")
	if errors.Is(err, ErrInsufficientFunds) {
		return 0, s.reject(ctx, play, "INSUFFICIENT_FUNDS", err)
	}
	if err == nil {
		return balanceAfter, nil
	}
	if rollbackErr := s.rollback(ctx, play); rollbackErr != nil {
		return 0, fmt.Errorf("%w: %w, rollback: %w", ErrWalletUnavailable, err, rollbackErr)
	}
	return 0, s.reject(ctx, play, "WALLET_UNAVAILABLE", fmt.Errorf("%w: %w", ErrWalletUnavailable, err))
}

func (s *Service) rollback(ctx context.Context, play Play) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.walletTimeout)
	defer cancel()
	return s.wallet.Rollback(ctx, play.PlayerID, play.ID+":debit")
}

func (s *Service) reject(ctx context.Context, play Play, code string, cause error) error {
	if err := s.plays.MarkRejected(context.WithoutCancel(ctx), play.ID, code); err != nil {
		return err
	}
	return cause
}

func (s *Service) settle(ctx context.Context, play Play) (int64, error) {
	walletCtx, cancel := context.WithTimeout(ctx, s.walletTimeout)
	defer cancel()
	var balance int64
	var err error
	if play.Payout > 0 {
		balance, err = s.wallet.Credit(walletCtx, play.PlayerID, play.Payout, play.ID+":credit")
	} else {
		balance, _, err = s.wallet.Balance(walletCtx, play.PlayerID)
	}
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrWalletUnavailable, err)
	}
	if err := s.plays.MarkClosed(context.WithoutCancel(ctx), play.ID); err != nil {
		return 0, err
	}
	return balance, nil
}

func (s *Service) RunCleanup(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.cleanup(ctx); err != nil {
				logger.Error("cleanup failed", "error", err)
			}
		}
	}
}

func (s *Service) cleanup(ctx context.Context) error {
	pending, err := s.plays.FindStale(ctx, StatusPending, stalePendingAge)
	if err != nil {
		return err
	}
	open, err := s.plays.FindStale(ctx, StatusOpen, staleOpenAge)
	if err != nil {
		return err
	}
	var errs []error
	for _, play := range pending {
		if err := s.rollback(ctx, play); err != nil {
			errs = append(errs, fmt.Errorf("rollback play %s: %w", play.ID, err))
			continue
		}
		if err := s.reject(ctx, play, "WALLET_UNAVAILABLE", nil); err != nil {
			errs = append(errs, err)
		}
	}
	for _, play := range open {
		if _, err := s.settle(ctx, play); err != nil {
			errs = append(errs, fmt.Errorf("settle play %s: %w", play.ID, err))
		}
	}
	return errors.Join(errs...)
}

func replay(play Play, amount int64, bet BetType) (Play, error) {
	if play.Amount != amount || play.BetType != bet {
		return Play{}, ErrIdempotencyKeyReused
	}
	switch play.Status {
	case StatusPending:
		return Play{}, ErrPlayInProgress
	case StatusRejected:
		return Play{}, rejectionErrors[play.ErrorCode]
	}
	return play, nil
}
