package storage

import (
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PedroLandolt/dice-game/internal/game"
)

func TestCreateOpenAndFind(t *testing.T) {
	p := newTestPlays(t)
	playerID := "test-" + rand.Text()

	created, err := p.Create(t.Context(), game.Play{PlayerID: playerID, RequestID: "r1", Amount: 500, BetType: game.BetEven})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Status != game.StatusPending {
		t.Fatalf("Create() = %+v; want an id and status pending", created)
	}

	created.Rolled, created.Won, created.Payout, created.BalanceAfter = 4, true, 1000, 9500
	opened, err := p.MarkOpen(t.Context(), created)
	if err != nil || !opened {
		t.Fatalf("MarkOpen() = %v, %v; want true, nil", opened, err)
	}

	found, ok, err := p.FindOpen(t.Context(), playerID)
	if err != nil || !ok {
		t.Fatalf("FindOpen() = %v, %v; want the open play", ok, err)
	}
	want := created
	want.Status = game.StatusOpen
	if found != want {
		t.Errorf("FindOpen() = %+v; want %+v", found, want)
	}
}

func TestCreateConstraints(t *testing.T) {
	p := newTestPlays(t)
	play := game.Play{PlayerID: "test-" + rand.Text(), RequestID: "r1", Amount: 500, BetType: game.BetOdd}

	if _, err := p.Create(t.Context(), play); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Create(t.Context(), play); !errors.Is(err, game.ErrPlayInProgress) {
		t.Errorf("Create() same request = %v; want %v", err, game.ErrPlayInProgress)
	}
	play.RequestID = "r2"
	if _, err := p.Create(t.Context(), play); !errors.Is(err, game.ErrPlayAlreadyOpen) {
		t.Errorf("Create() second active play = %v; want %v", err, game.ErrPlayAlreadyOpen)
	}
}

func TestFindStale(t *testing.T) {
	p := newTestPlays(t)
	created, err := p.Create(t.Context(), game.Play{PlayerID: "test-" + rand.Text(), RequestID: "r1", Amount: 500, BetType: game.BetEven})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.pool.Exec(t.Context(),
		`UPDATE plays SET created_at = now() - INTERVAL '1 minute' WHERE id = $1`,
		created.ID,
	); err != nil {
		t.Fatal(err)
	}

	stale, err := p.FindStale(t.Context(), game.StatusPending, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, play := range stale {
		if play.ID == created.ID {
			return
		}
	}
	t.Errorf("FindStale() did not return play %s", created.ID)
}

func newTestPlays(t *testing.T) *Plays {
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
	return NewPlays(pool)
}
