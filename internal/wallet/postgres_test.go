package wallet

import (
	"crypto/rand"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PedroLandolt/dice-game/internal/game"
)

func TestBalance(t *testing.T) {
	w := newTestWallet(t)
	playerID := createTestPlayer(t, w.pool, 10000)

	balance, currency, err := w.Balance(t.Context(), playerID)
	if err != nil {
		t.Fatal(err)
	}
	if balance != 10000 || currency != "EUR" {
		t.Errorf("Balance() = %d %s; want 10000 EUR", balance, currency)
	}
}

func TestDebitAndCredit(t *testing.T) {
	w := newTestWallet(t)
	playerID := createTestPlayer(t, w.pool, 10000)

	balance, err := w.Debit(t.Context(), playerID, 300, playerID+":debit")
	if err != nil {
		t.Fatal(err)
	}
	if balance != 9700 {
		t.Errorf("Debit() = %d; want 9700", balance)
	}
	balance, err = w.Credit(t.Context(), playerID, 600, playerID+":credit")
	if err != nil {
		t.Fatal(err)
	}
	if balance != 10300 {
		t.Errorf("Credit() = %d; want 10300", balance)
	}
	assertBalance(t, w, playerID, 10300)
}

func TestDebitInsufficientFunds(t *testing.T) {
	w := newTestWallet(t)
	playerID := createTestPlayer(t, w.pool, 500)

	_, err := w.Debit(t.Context(), playerID, 501, playerID+":debit")
	if !errors.Is(err, game.ErrInsufficientFunds) {
		t.Fatalf("Debit() error = %v; want %v", err, game.ErrInsufficientFunds)
	}
	assertBalance(t, w, playerID, 500)
}

func TestSameTxIDAppliesOnce(t *testing.T) {
	w := newTestWallet(t)
	playerID := createTestPlayer(t, w.pool, 10000)

	for range 2 {
		balance, err := w.Debit(t.Context(), playerID, 300, playerID+":debit")
		if err != nil {
			t.Fatal(err)
		}
		if balance != 9700 {
			t.Errorf("Debit() = %d; want 9700", balance)
		}
	}
	for range 2 {
		balance, err := w.Credit(t.Context(), playerID, 600, playerID+":credit")
		if err != nil {
			t.Fatal(err)
		}
		if balance != 10300 {
			t.Errorf("Credit() = %d; want 10300", balance)
		}
	}
	assertBalance(t, w, playerID, 10300)
}

func TestRollbackAfterDebit(t *testing.T) {
	w := newTestWallet(t)
	playerID := createTestPlayer(t, w.pool, 10000)
	txID := playerID + ":debit"

	if _, err := w.Debit(t.Context(), playerID, 300, txID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := w.Rollback(t.Context(), playerID, txID); err != nil {
			t.Fatal(err)
		}
	}
	assertBalance(t, w, playerID, 10000)

	_, err := w.Debit(t.Context(), playerID, 300, txID)
	if !errors.Is(err, game.ErrTxRolledBack) {
		t.Fatalf("Debit() after rollback error = %v; want %v", err, game.ErrTxRolledBack)
	}
}

func TestRollbackBeforeDebit(t *testing.T) {
	w := newTestWallet(t)
	playerID := createTestPlayer(t, w.pool, 10000)
	txID := playerID + ":debit"

	if err := w.Rollback(t.Context(), playerID, txID); err != nil {
		t.Fatal(err)
	}
	_, err := w.Debit(t.Context(), playerID, 300, txID)
	if !errors.Is(err, game.ErrTxRolledBack) {
		t.Fatalf("late Debit() error = %v; want %v", err, game.ErrTxRolledBack)
	}
	assertBalance(t, w, playerID, 10000)
}

func TestRollbackOtherPlayersTxHasNoEffect(t *testing.T) {
	w := newTestWallet(t)
	victimID := createTestPlayer(t, w.pool, 10000)
	attackerID := createTestPlayer(t, w.pool, 10000)
	txID := victimID + ":debit"

	if _, err := w.Debit(t.Context(), victimID, 300, txID); err != nil {
		t.Fatal(err)
	}
	if err := w.Rollback(t.Context(), attackerID, txID); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, w, attackerID, 10000)
	assertBalance(t, w, victimID, 9700)

	if err := w.Rollback(t.Context(), victimID, txID); err != nil {
		t.Fatal(err)
	}
	assertBalance(t, w, victimID, 10000)
}

func newTestWallet(t *testing.T) *Postgres {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return NewPostgres(pool)
}

func createTestPlayer(t *testing.T, pool *pgxpool.Pool, balance int64) string {
	t.Helper()
	playerID := "test-" + rand.Text()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO wallets (player_id, balance, currency) VALUES ($1, $2, 'EUR')`,
		playerID, balance,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO ledger_entries (player_id, tx_id, kind, amount, balance_after)
		VALUES ($1, 'seed:' || $1, 'adjustment', $2, $2)`,
		playerID, balance,
	); err != nil {
		t.Fatal(err)
	}
	return playerID
}

func assertBalance(t *testing.T, w *Postgres, playerID string, want int64) {
	t.Helper()
	balance, _, err := w.Balance(t.Context(), playerID)
	if err != nil {
		t.Fatal(err)
	}
	var ledgerSum int64
	if err := w.pool.QueryRow(t.Context(),
		`SELECT COALESCE(SUM(amount), 0)::bigint FROM ledger_entries WHERE player_id = $1`,
		playerID,
	).Scan(&ledgerSum); err != nil {
		t.Fatal(err)
	}
	if balance != want || ledgerSum != want {
		t.Errorf("balance = %d, ledger sum = %d; want %d", balance, ledgerSum, want)
	}
}
