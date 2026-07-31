package eval

import (
	"testing"

	"github.com/serediukit/bgex-backend/internal/games/engine"
)

// cards parses compact codes like "Ah Kd Ts 2c" into engine.Cards.
func cards(t *testing.T, codes ...string) []engine.Card {
	t.Helper()
	rankMap := map[byte]engine.Rank{
		'2': engine.Two, '3': engine.Three, '4': engine.Four, '5': engine.Five,
		'6': engine.Six, '7': engine.Seven, '8': engine.Eight, '9': engine.Nine,
		'T': engine.Ten, 'J': engine.Jack, 'Q': engine.Queen, 'K': engine.King, 'A': engine.Ace,
	}
	suitMap := map[byte]engine.Suit{'c': engine.Clubs, 'd': engine.Diamonds, 'h': engine.Hearts, 's': engine.Spades}
	out := make([]engine.Card, 0, len(codes))
	for _, code := range codes {
		if len(code) != 2 {
			t.Fatalf("bad card code %q", code)
		}
		r, ok := rankMap[code[0]]
		if !ok {
			t.Fatalf("bad rank in %q", code)
		}
		s, ok := suitMap[code[1]]
		if !ok {
			t.Fatalf("bad suit in %q", code)
		}
		out = append(out, engine.Card{Rank: r, Suit: s})
	}
	return out
}

func TestEvaluateCategories(t *testing.T) {
	tests := []struct {
		name string
		hand []string
		want Category
	}{
		{"royal/straight flush", []string{"Ah", "Kh", "Qh", "Jh", "Th", "2c", "3d"}, StraightFlush},
		{"wheel straight flush", []string{"Ah", "2h", "3h", "4h", "5h", "Kc", "Qd"}, StraightFlush},
		{"four of a kind", []string{"9h", "9d", "9s", "9c", "Kh", "2d", "3s"}, FourOfAKind},
		{"full house", []string{"9h", "9d", "9s", "Kc", "Kh", "2d", "3s"}, FullHouse},
		{"two trips make full house", []string{"9h", "9d", "9s", "Kc", "Kh", "Kd", "3s"}, FullHouse},
		{"flush", []string{"2h", "5h", "9h", "Jh", "Kh", "2d", "3s"}, Flush},
		{"straight", []string{"5h", "6d", "7s", "8c", "9h", "Kd", "2s"}, Straight},
		{"wheel straight", []string{"Ah", "2d", "3s", "4c", "5h", "Kd", "Qs"}, Straight},
		{"three of a kind", []string{"9h", "9d", "9s", "Kc", "Qh", "2d", "3s"}, ThreeOfAKind},
		{"two pair", []string{"9h", "9d", "Ks", "Kc", "Qh", "2d", "3s"}, TwoPair},
		{"one pair", []string{"9h", "9d", "Ks", "Jc", "Qh", "2d", "3s"}, OnePair},
		{"high card", []string{"9h", "7d", "Ks", "Jc", "Qh", "2d", "4s"}, HighCard},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(cards(t, tt.hand...))
			if got.Category != tt.want {
				t.Fatalf("category = %v (%s), want %v", got.Category, got, tt.want)
			}
		})
	}
}

func TestEvaluateComparisons(t *testing.T) {
	cmp := func(a, b []string) int {
		return Evaluate(cards(t, a...)).Compare(Evaluate(cards(t, b...)))
	}

	// Higher straight beats lower straight.
	if got := cmp(
		[]string{"6h", "7d", "8s", "9c", "Th", "2d", "3s"},
		[]string{"5h", "6d", "7s", "8c", "9h", "2d", "3s"},
	); got != 1 {
		t.Errorf("T-high straight should beat 9-high straight, got %d", got)
	}

	// Ace-high straight beats wheel.
	if got := cmp(
		[]string{"Ah", "Kd", "Qs", "Jc", "Th", "2d", "3s"},
		[]string{"Ah", "2d", "3s", "4c", "5h", "Kd", "Qs"},
	); got != 1 {
		t.Errorf("broadway should beat wheel, got %d", got)
	}

	// Kicker decides between equal pairs.
	if got := cmp(
		[]string{"Ah", "Ad", "Ks", "Qc", "Jh", "2d", "3s"},
		[]string{"Ah", "Ad", "Ks", "Qc", "9h", "2d", "4s"},
	); got != 1 {
		t.Errorf("J kicker should beat 9 kicker, got %d", got)
	}

	// Identical hands (different suits) tie.
	if got := cmp(
		[]string{"Ah", "Ad", "Ks", "Qc", "Jh", "2d", "3s"},
		[]string{"Ac", "As", "Kd", "Qh", "Jc", "2h", "3c"},
	); got != 0 {
		t.Errorf("identical two-card kickers should tie, got %d", got)
	}

	// Flush beats a straight.
	if got := cmp(
		[]string{"2h", "5h", "9h", "Jh", "Kh", "3d", "4s"},
		[]string{"5h", "6d", "7s", "8c", "9h", "Kd", "2s"},
	); got != 1 {
		t.Errorf("flush should beat straight, got %d", got)
	}
}
