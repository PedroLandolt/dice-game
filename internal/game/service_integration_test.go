package game_test

import (
	"crypto/rand"
	"errors"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PedroLandolt/dice-game/internal/game"
	"github.com/PedroLandolt/dice-game/internal/storage"
	"github.com/PedroLandolt/dice-game/internal/wallet"
)

func TestConcurrentPlays(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	playerID := "test-" + rand.Text()
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO wallets (player_id, balance, currency) VALUES ($1, 10000, 'EUR')`,
		playerID,
	); err != nil {
		t.Fatal(err)
	}
	service := game.NewService(wallet.NewPostgres(pool), storage.NewPlays(pool), game.BetLimits{Min: 5, Max: 2000}, 2*time.Second)

	errs := make([]error, 20)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = service.Play(t.Context(), playerID, "k"+strconv.Itoa(i), 500, game.BetEven)
		})
	}
	wg.Wait()

	successes := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case !errors.Is(err, game.ErrPlayAlreadyOpen):
			t.Errorf("Play() error = %v; want nil or %v", err, game.ErrPlayAlreadyOpen)
		}
	}
	if successes != 1 {
		t.Errorf("successful plays = %d; want 1", successes)
	}
	state, err := service.Wallet(t.Context(), playerID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Balance != 9500 || state.OpenPlay == nil {
		t.Errorf("wallet = %d with open play %v; want 9500 with one open play", state.Balance, state.OpenPlay)
	}
}
