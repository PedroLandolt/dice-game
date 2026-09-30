package game

import (
	"errors"
	"testing"
)

func TestSettle(t *testing.T) {
	tests := []struct {
		rolled     int
		bet        BetType
		wantWon    bool
		wantPayout int64
	}{
		{1, BetEven, false, 0},
		{1, BetOdd, true, 1000},
		{2, BetEven, true, 1000},
		{2, BetOdd, false, 0},
		{3, BetEven, false, 0},
		{3, BetOdd, true, 1000},
		{4, BetEven, true, 1000},
		{4, BetOdd, false, 0},
		{5, BetEven, false, 0},
		{5, BetOdd, true, 1000},
		{6, BetEven, true, 1000},
		{6, BetOdd, false, 0},
	}
	for _, tt := range tests {
		won, payout := Settle(tt.rolled, tt.bet, 500)
		if won != tt.wantWon || payout != tt.wantPayout {
			t.Errorf("Settle(%d, %s, 500) = %v, %d; want %v, %d",
				tt.rolled, tt.bet, won, payout, tt.wantWon, tt.wantPayout)
		}
	}
}

func TestBetLimitsValidate(t *testing.T) {
	limits := BetLimits{Min: 5, Max: 10000}
	tests := []struct {
		name    string
		amount  int64
		bet     BetType
		wantErr error
	}{
		{"zero", 0, BetEven, ErrInvalidBetAmount},
		{"negative", -100, BetEven, ErrInvalidBetAmount},
		{"below min", 4, BetEven, ErrInvalidBetAmount},
		{"at min", 5, BetEven, nil},
		{"at max", 10000, BetOdd, nil},
		{"above max", 10001, BetOdd, ErrInvalidBetAmount},
		{"empty type", 500, "", ErrInvalidBetType},
		{"uppercase type", 500, "EVEN", ErrInvalidBetType},
		{"unknown type", 500, "red", ErrInvalidBetType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := limits.Validate(tt.amount, tt.bet)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate(%d, %q) = %v; want %v", tt.amount, tt.bet, err, tt.wantErr)
			}
		})
	}
}

func TestRollD6(t *testing.T) {
	for range 1000 {
		n, err := RollD6()
		if err != nil {
			t.Fatal(err)
		}
		if n < 1 || n > 6 {
			t.Fatalf("RollD6() = %d; want 1..6", n)
		}
	}
}
