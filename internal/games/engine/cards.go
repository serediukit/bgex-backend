// Package engine holds the game-agnostic building blocks shared by every game
// on the platform: a standard playing-card model, the Engine contract each game
// implements, and a registry that lets the lobby/realtime layers stay generic.
//
// Anything reusable across card games (Poker, and future titles) belongs here;
// game-specific rules live in their own package (e.g. internal/games/poker).
package engine

import (
	"math/rand/v2"
)

// Suit is one of the four French-deck suits.
type Suit uint8

const (
	Clubs Suit = iota
	Diamonds
	Hearts
	Spades
)

var suitSymbols = [...]string{"c", "d", "h", "s"}

// String returns the single-letter suit code (c/d/h/s).
func (s Suit) String() string {
	if int(s) < len(suitSymbols) {
		return suitSymbols[s]
	}
	return "?"
}

// Rank is a card rank from Two (2) through Ace (14).
type Rank uint8

const (
	Two Rank = iota + 2
	Three
	Four
	Five
	Six
	Seven
	Eight
	Nine
	Ten
	Jack
	Queen
	King
	Ace
)

var rankSymbols = map[Rank]string{
	Two: "2", Three: "3", Four: "4", Five: "5", Six: "6", Seven: "7",
	Eight: "8", Nine: "9", Ten: "T", Jack: "J", Queen: "Q", King: "K", Ace: "A",
}

// String returns the short rank code (2..9, T, J, Q, K, A).
func (r Rank) String() string {
	if s, ok := rankSymbols[r]; ok {
		return s
	}
	return "?"
}

// Card is a single playing card. The zero value is not a valid card.
type Card struct {
	Rank Rank
	Suit Suit
}

// String returns a compact code such as "Ah" or "Tc".
func (c Card) String() string { return c.Rank.String() + c.Suit.String() }

// CardCount is the number of cards in a standard deck.
const CardCount = 52

// ToInt encodes a card as an index in [0,52): suit*13 + (rank-2).
func (c Card) ToInt() int32 {
	return int32(c.Suit)*13 + int32(c.Rank-Two)
}

// CardFromInt decodes an index in [0,52) back into a Card.
func CardFromInt(i int32) Card {
	return Card{
		Rank: Rank(i%13) + Two,
		Suit: Suit(i / 13),
	}
}

// NewDeck returns the 52 card indices in canonical (unshuffled) order.
func NewDeck() []int32 {
	deck := make([]int32, CardCount)
	for i := range deck {
		deck[i] = int32(i)
	}
	return deck
}

// ShuffledDeck returns a freshly shuffled deck of 52 card indices. It uses
// math/rand/v2's global source, which is seeded from the OS at startup — good
// enough for play-money games (no cryptographic secrecy requirement).
func ShuffledDeck() []int32 {
	deck := NewDeck()
	rand.Shuffle(len(deck), func(i, j int) {
		deck[i], deck[j] = deck[j], deck[i]
	})
	return deck
}

// CardsFromInts decodes a slice of indices into Cards.
func CardsFromInts(ints []int32) []Card {
	cards := make([]Card, len(ints))
	for i, v := range ints {
		cards[i] = CardFromInt(v)
	}
	return cards
}
