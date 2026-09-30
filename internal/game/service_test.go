package game

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestPlayAndEndPlay(t *testing.T) {
	s, w, _ := newTestService(10000)

	play, err := s.Play(t.Context(), "gandalf", "k1", 500, BetEven)
	if err != nil {
		t.Fatal(err)
	}
	if play.Status != StatusOpen || play.BalanceAfter != 9500 {
		t.Fatalf("Play() = %+v; want status open and balance 9500", play)
	}
	if won, payout := Settle(play.Rolled, BetEven, 500); play.Won != won || play.Payout != payout {
		t.Errorf("Play() won, payout = %v, %d; want %v, %d", play.Won, play.Payout, won, payout)
	}

	closed, balance, err := s.EndPlay(t.Context(), "gandalf")
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != StatusClosed || balance != 9500+play.Payout || w.balance != balance {
		t.Errorf("EndPlay() = %s, %d; want closed, %d", closed.Status, balance, 9500+play.Payout)
	}
	if _, _, err := s.EndPlay(t.Context(), "gandalf"); !errors.Is(err, ErrNoOpenPlay) {
		t.Errorf("second EndPlay() error = %v; want %v", err, ErrNoOpenPlay)
	}
}

func TestPlayWalletRefuses(t *testing.T) {
	s, w, p := newTestService(10000)
	w.debitErr = ErrInsufficientFunds

	_, err := s.Play(t.Context(), "luffy", "k1", 500, BetOdd)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("Play() error = %v; want %v", err, ErrInsufficientFunds)
	}
	if got := p.plays["1"].Status; got != StatusRejected {
		t.Errorf("play status = %s; want rejected", got)
	}
	if _, err := s.Play(t.Context(), "luffy", "k1", 500, BetOdd); !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("retry Play() error = %v; want the same %v", err, ErrInsufficientFunds)
	}

	w.debitErr = nil
	if _, err := s.Play(t.Context(), "luffy", "k2", 500, BetOdd); err != nil {
		t.Errorf("new Play() after rejection error = %v; want nil", err)
	}
}

func TestPlayWalletTimeout(t *testing.T) {
	s, w, p := newTestService(10000)
	w.hangDebit = true

	_, err := s.Play(t.Context(), "dante", "k1", 500, BetEven)
	if !errors.Is(err, ErrWalletUnavailable) {
		t.Fatalf("Play() error = %v; want %v", err, ErrWalletUnavailable)
	}
	if w.rollbacks != 1 {
		t.Errorf("rollbacks = %d; want 1", w.rollbacks)
	}
	if play := p.plays["1"]; play.Status != StatusRejected || play.ErrorCode != "WALLET_UNAVAILABLE" {
		t.Errorf("play = %s %q; want rejected WALLET_UNAVAILABLE", play.Status, play.ErrorCode)
	}

	w.hangDebit = false
	if _, err := s.Play(t.Context(), "dante", "k2", 500, BetEven); err != nil {
		t.Errorf("new Play() after timeout error = %v; want nil", err)
	}
	if w.balance != 9500 {
		t.Errorf("balance = %d; want 9500", w.balance)
	}
}

func TestPlaySameKeyReturnsSameResult(t *testing.T) {
	s, w, _ := newTestService(10000)

	first, err := s.Play(t.Context(), "gandalf", "k1", 500, BetOdd)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Play(t.Context(), "gandalf", "k1", 500, BetOdd)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("retry Play() = %+v; want %+v", second, first)
	}
	if w.balance != 9500 {
		t.Errorf("balance = %d; want 9500 (one debit)", w.balance)
	}
}

func TestPlaySameKeyDifferentBody(t *testing.T) {
	s, _, _ := newTestService(10000)

	if _, err := s.Play(t.Context(), "gandalf", "k1", 500, BetOdd); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Play(t.Context(), "gandalf", "k1", 700, BetOdd); !errors.Is(err, ErrIdempotencyKeyReused) {
		t.Errorf("Play() error = %v; want %v", err, ErrIdempotencyKeyReused)
	}
}

func TestCleanup(t *testing.T) {
	s, w, p := newTestService(0)
	p.plays["1"] = Play{ID: "1", PlayerID: "dante", Status: StatusPending}
	p.plays["2"] = Play{ID: "2", PlayerID: "gandalf", Status: StatusOpen, Payout: 1000}

	if err := s.cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if play := p.plays["1"]; play.Status != StatusRejected || w.rollbacks != 1 {
		t.Errorf("stale pending play = %s with %d rollbacks; want rejected with 1", play.Status, w.rollbacks)
	}
	if play := p.plays["2"]; play.Status != StatusClosed || w.balance != 1000 {
		t.Errorf("stale open play = %s with balance %d; want closed with 1000", play.Status, w.balance)
	}
}

func newTestService(balance int64) (*Service, *fakeWallet, *fakePlays) {
	w := &fakeWallet{balance: balance}
	p := &fakePlays{plays: map[string]Play{}}
	return NewService(w, p, BetLimits{Min: 5, Max: 2000}, 20*time.Millisecond), w, p
}

type fakeWallet struct {
	balance   int64
	debitErr  error
	hangDebit bool
	rollbacks int
}

func (w *fakeWallet) Balance(ctx context.Context, playerID string) (int64, string, error) {
	return w.balance, "EUR", nil
}

func (w *fakeWallet) Debit(ctx context.Context, playerID string, amount int64, txID string) (int64, error) {
	if w.hangDebit {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	if w.debitErr != nil {
		return 0, w.debitErr
	}
	w.balance -= amount
	return w.balance, nil
}

func (w *fakeWallet) Credit(ctx context.Context, playerID string, amount int64, txID string) (int64, error) {
	w.balance += amount
	return w.balance, nil
}

func (w *fakeWallet) Rollback(ctx context.Context, playerID string, txID string) error {
	w.rollbacks++
	return nil
}

type fakePlays struct {
	plays map[string]Play
}

func (f *fakePlays) Create(ctx context.Context, play Play) (Play, error) {
	for _, existing := range f.plays {
		if existing.PlayerID != play.PlayerID {
			continue
		}
		if existing.RequestID == play.RequestID {
			return Play{}, ErrPlayInProgress
		}
		if existing.Status == StatusPending || existing.Status == StatusOpen {
			return Play{}, ErrPlayAlreadyOpen
		}
	}
	play.ID = strconv.Itoa(len(f.plays) + 1)
	play.Status = StatusPending
	f.plays[play.ID] = play
	return play, nil
}

func (f *fakePlays) FindByRequestID(ctx context.Context, playerID, requestID string) (Play, bool, error) {
	for _, play := range f.plays {
		if play.PlayerID == playerID && play.RequestID == requestID {
			return play, true, nil
		}
	}
	return Play{}, false, nil
}

func (f *fakePlays) FindOpen(ctx context.Context, playerID string) (Play, bool, error) {
	for _, play := range f.plays {
		if play.PlayerID == playerID && play.Status == StatusOpen {
			return play, true, nil
		}
	}
	return Play{}, false, nil
}

func (f *fakePlays) FindStale(ctx context.Context, status PlayStatus, olderThan time.Duration) ([]Play, error) {
	var stale []Play
	for _, play := range f.plays {
		if play.Status == status {
			stale = append(stale, play)
		}
	}
	return stale, nil
}

func (f *fakePlays) MarkOpen(ctx context.Context, play Play) (bool, error) {
	if f.plays[play.ID].Status != StatusPending {
		return false, nil
	}
	play.Status = StatusOpen
	f.plays[play.ID] = play
	return true, nil
}

func (f *fakePlays) MarkRejected(ctx context.Context, id, errorCode string) error {
	play := f.plays[id]
	if play.Status == StatusPending {
		play.Status, play.ErrorCode = StatusRejected, errorCode
		f.plays[id] = play
	}
	return nil
}

func (f *fakePlays) MarkClosed(ctx context.Context, id string) error {
	play := f.plays[id]
	if play.Status == StatusOpen {
		play.Status = StatusClosed
		f.plays[id] = play
	}
	return nil
}
