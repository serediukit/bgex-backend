// Package eval evaluates the best 5-card poker hand out of 5–7 cards and yields
// a comparable value, so showdown logic can rank and compare players' hands.
package eval

import (
	"sort"

	"github.com/serediukit/bgex-backend/internal/games/engine"
)

// Category is a poker hand category, ordered so a larger value always beats a
// smaller one.
type Category int

const (
	HighCard Category = iota
	OnePair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
)

var categoryNames = map[Category]string{
	HighCard:      "High Card",
	OnePair:       "One Pair",
	TwoPair:       "Two Pair",
	ThreeOfAKind:  "Three of a Kind",
	Straight:      "Straight",
	Flush:         "Flush",
	FullHouse:     "Full House",
	FourOfAKind:   "Four of a Kind",
	StraightFlush: "Straight Flush",
}

// HandValue is a fully-ordered poker hand strength: primarily the Category,
// then Tiebreak values (most-significant first) to break ties within a category.
type HandValue struct {
	Category Category
	Tiebreak []int
}

// String describes the hand category (e.g. "Full House").
func (h HandValue) String() string { return categoryNames[h.Category] }

// Compare returns 1 if h beats o, -1 if o beats h, 0 if they tie.
func (h HandValue) Compare(o HandValue) int {
	if h.Category != o.Category {
		if h.Category > o.Category {
			return 1
		}
		return -1
	}
	for i := 0; i < len(h.Tiebreak) && i < len(o.Tiebreak); i++ {
		if h.Tiebreak[i] != o.Tiebreak[i] {
			if h.Tiebreak[i] > o.Tiebreak[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

// Evaluate returns the best HandValue for the given cards (5, 6, or 7 of them).
func Evaluate(cards []engine.Card) HandValue {
	rankCount := make(map[int]int)
	suitRanks := make(map[engine.Suit][]int)
	allRanks := make([]int, 0, len(cards))

	for _, c := range cards {
		r := int(c.Rank)
		rankCount[r]++
		suitRanks[c.Suit] = append(suitRanks[c.Suit], r)
		allRanks = append(allRanks, r)
	}

	// Flush suit, if any (5+ of one suit).
	var flushRanks []int
	for _, rs := range suitRanks {
		if len(rs) >= 5 {
			flushRanks = rs
			break
		}
	}

	// Straight flush: a straight within the flush suit.
	if flushRanks != nil {
		if high, ok := straightHigh(flushRanks); ok {
			return HandValue{StraightFlush, []int{high}}
		}
	}

	// Rank groups sorted by count desc, then rank desc.
	type group struct{ rank, count int }
	groups := make([]group, 0, len(rankCount))
	for r, c := range rankCount {
		groups = append(groups, group{r, c})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].count != groups[j].count {
			return groups[i].count > groups[j].count
		}
		return groups[i].rank > groups[j].rank
	})

	uniqueDesc := make([]int, 0, len(rankCount))
	for r := range rankCount {
		uniqueDesc = append(uniqueDesc, r)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(uniqueDesc)))

	kickers := func(exclude map[int]bool, n int) []int {
		out := make([]int, 0, n)
		for _, r := range uniqueDesc {
			if exclude[r] {
				continue
			}
			out = append(out, r)
			if len(out) == n {
				break
			}
		}
		return out
	}

	// Four of a kind.
	if groups[0].count == 4 {
		quad := groups[0].rank
		return HandValue{FourOfAKind, append([]int{quad}, kickers(map[int]bool{quad: true}, 1)...)}
	}

	// Full house (trips + a pair, or two trips).
	if groups[0].count == 3 && len(groups) > 1 && groups[1].count >= 2 {
		return HandValue{FullHouse, []int{groups[0].rank, groups[1].rank}}
	}

	// Flush.
	if flushRanks != nil {
		sorted := append([]int(nil), flushRanks...)
		sort.Sort(sort.Reverse(sort.IntSlice(sorted)))
		return HandValue{Flush, sorted[:5]}
	}

	// Straight.
	if high, ok := straightHigh(allRanks); ok {
		return HandValue{Straight, []int{high}}
	}

	// Three of a kind.
	if groups[0].count == 3 {
		trip := groups[0].rank
		return HandValue{ThreeOfAKind, append([]int{trip}, kickers(map[int]bool{trip: true}, 2)...)}
	}

	// Two pair.
	if groups[0].count == 2 && len(groups) > 1 && groups[1].count == 2 {
		hi, lo := groups[0].rank, groups[1].rank
		return HandValue{TwoPair, append([]int{hi, lo}, kickers(map[int]bool{hi: true, lo: true}, 1)...)}
	}

	// One pair.
	if groups[0].count == 2 {
		pair := groups[0].rank
		return HandValue{OnePair, append([]int{pair}, kickers(map[int]bool{pair: true}, 3)...)}
	}

	// High card.
	return HandValue{HighCard, uniqueDesc[:5]}
}

// straightHigh returns the top card rank of the highest straight found in ranks
// (duplicates allowed), handling the wheel (A-2-3-4-5, high card 5). ok is false
// when there is no straight.
func straightHigh(ranks []int) (int, bool) {
	present := make(map[int]bool, len(ranks))
	for _, r := range ranks {
		present[r] = true
	}
	if present[int(engine.Ace)] {
		present[1] = true // Ace plays low for the wheel.
	}
	for top := int(engine.Ace); top >= 5; top-- {
		run := true
		for k := 0; k < 5; k++ {
			if !present[top-k] {
				run = false
				break
			}
		}
		if run {
			return top, true
		}
	}
	return 0, false
}
