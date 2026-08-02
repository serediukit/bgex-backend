package ttr

import (
	"fmt"
	"slices"
	"sort"

	pb "github.com/serediukit/bgex-backend/internal/games/ttr/pb"
)

// Final-scoring point constants (rules §3.1, §13.3, §13.5).
const (
	unbuiltStationPoints   = 4  // UNUSED_STATION_POINTS: +4 per station never built (§13.5)
	longestPathBonusPoints = 10 // LONGEST_PATH_BONUS: awarded whole to every tied player (§13.3)

	// noBorrow marks a station-assignment option that borrows no route. Real
	// route ids in every map document start at 1, so 0 is never a valid id.
	noBorrow = int32(0)
)

// FinalScore computes the complete §13 final scoring for every player in gs,
// including tiebreak ranking per §13.6. It operates on m (the parsed board)
// and gs (the persisted hot state) but builds its own plain Go graph
// structures internally (see longestTrail / bestStationAssignment) rather
// than reasoning about the protobuf messages directly, per plan.md's
// "Longest path / station optimizer placement" note.
//
// Resigned players (plan Q14) get an entirely zeroed ScoreBreakdown — every
// points field 0, Rank fixed at len(gs.Players) (tied at the bottom-most
// slot regardless of how many players resigned) — rather than a real
// breakdown with Total overridden to 0: that keeps the "sum of a
// breakdown's components equals its Total" checksum true universally,
// including for resigned players, and matches the shape the Step-11
// placeholder (turnflow.go's now-deleted placeholderScoreBreakdown)
// established as the contract every caller of finalizeGame already relies
// on.
func FinalScore(m *Map, gs *pb.GameState) ([]*pb.ScoreBreakdown, error) {
	results := make([]*pb.ScoreBreakdown, len(gs.Players))
	longest, maxLongest := longestTrailsByActiveSeat(m, gs)

	ranked := make([]rankable, 0, len(gs.Players))
	for i, p := range gs.Players {
		if p.Resigned {
			results[i] = &pb.ScoreBreakdown{SeatIndex: p.SeatIndex, Rank: int32(len(gs.Players))} // #nosec G115 -- bounded by maxSeats (5)
			continue
		}
		sb, err := buildBreakdown(m, gs, p, longest[p.SeatIndex], maxLongest)
		if err != nil {
			return nil, fmt.Errorf("seat %d: %w", p.SeatIndex, err)
		}
		results[i] = sb
		ranked = append(ranked, rankableFor(i, sb, p))
	}

	assignRanks(results, ranked)
	return results, nil
}

// longestTrailsByActiveSeat computes each active (non-resigned) player's
// §13.3 longest-trail length and the maximum across all of them. Resigned
// players are excluded from both the map and the max, per the rules
// restated in the step brief ("Resigned players are excluded from the
// comparison").
func longestTrailsByActiveSeat(m *Map, gs *pb.GameState) (map[int32]int, int) {
	longest := make(map[int32]int, len(gs.Players))
	maxLongest := 0
	for _, p := range gs.Players {
		if p.Resigned {
			continue
		}
		l := longestTrail(m, p.ClaimedRouteIds)
		longest[p.SeatIndex] = l
		if l > maxLongest {
			maxLongest = l
		}
	}
	return longest, maxLongest
}

// buildBreakdown computes the full ScoreBreakdown for one active player p:
// route points (§13.1, propagating ErrInvalidRouteLength loudly per §12),
// the station-optimized ticket +/- (§13.2, §13.4), the unbuilt-station bonus
// (§13.5), and the longest-path bonus (§13.3, awarded iff p's own length
// ties the active-player maximum).
func buildBreakdown(m *Map, gs *pb.GameState, p *pb.PlayerState, length, maxLongest int) (*pb.ScoreBreakdown, error) {
	routePts, err := sumRoutePoints(m, p.ClaimedRouteIds)
	if err != nil {
		return nil, err
	}

	assignment, _, completed, missed := bestStationAssignment(m, gs, p)
	completedPts := sumTicketPoints(m, completed)
	missedPts := sumTicketPoints(m, missed)

	var bonus int32
	if maxLongest > 0 && length == maxLongest {
		bonus = longestPathBonusPoints
	}

	sb := &pb.ScoreBreakdown{
		SeatIndex:              p.SeatIndex,
		RoutePoints:            int32(routePts),     // #nosec G115 -- bounded by map-authored route lengths
		TicketsCompletedPoints: int32(completedPts), // #nosec G115 -- bounded by map-authored ticket points
		TicketsMissedPoints:    int32(missedPts),    // #nosec G115 -- bounded by map-authored ticket points
		StationBonus:           p.StationsLeft * unbuiltStationPoints,
		LongestPathBonus:       bonus,
		LongestPathLength:      int32(length), // #nosec G115 -- bounded by total map route length
		CompletedTicketIds:     completed,
		MissedTicketIds:        missed,
		BorrowedRoutes:         assignment,
	}
	sb.Total = sb.RoutePoints + sb.TicketsCompletedPoints - sb.TicketsMissedPoints + sb.StationBonus + sb.LongestPathBonus
	return sb, nil
}

// sumRoutePoints sums PointsForLength(route.Length) over routeIDs, failing
// loudly (rules §12) if any claimed route has an undefined length. This is
// the same computation applyClaimRoute performs incrementally into
// PlayerState.RouteScore (claim.go) — recomputing it here from
// ClaimedRouteIds is the §13.1 checksum.
func sumRoutePoints(m *Map, routeIDs []int32) (int, error) {
	total := 0
	for _, rid := range routeIDs {
		r := m.RouteByID[rid]
		if r == nil {
			return 0, fmt.Errorf("%w: unknown route %d", ErrInvalidRouteLength, rid)
		}
		pts, err := PointsForLength(r.Length)
		if err != nil {
			return 0, err
		}
		total += pts
	}
	return total, nil
}

// sumTicketPoints sums the Points of every ticket id in ids, skipping ids
// unknown on m (defensive; should not happen for a validly-derived ticket
// list).
func sumTicketPoints(m *Map, ids []int32) int {
	total := 0
	for _, id := range ids {
		if tk := m.TicketByID[id]; tk != nil {
			total += tk.Points
		}
	}
	return total
}

// trailEdge is one edge of a player's own-route subgraph, as used by
// longestTrail: a plain Go struct independent of the protobuf/map types so
// the DFS below only ever touches city-id strings and integer lengths.
type trailEdge struct {
	a, b   string
	length int
}

// longestTrail returns the length (in train cars) of the longest trail —
// edge-distinct walk, cities may repeat, loops allowed — through the
// player's own route subgraph (rules §13.3). Stations and borrowed opponent
// routes are excluded by construction: routeIDs must be exactly
// PlayerState.ClaimedRouteIds. Unknown routes are skipped defensively.
//
// The search is a DFS over edges from every vertex, tracking the maximum
// weighted length reached, exactly as the rules' own pseudocode describes.
// It is memoized on (vertex, used-edge-bitset) only once the player owns
// more than 20 routes (rules: "tiny in practice... memoize... if profiling
// demands it") — testMap()'s largest per-player subgraph is far below that,
// so the plain, unmemoized DFS is what every test in this package actually
// exercises; the memoized path exists for a future large map/bot.
//
// The used-edge SET, however, is a []bool shared across the whole search
// (backtracking: mark before recursing, unmark after) — correctness never
// depends on len(edges) or on whether memoization is enabled. Only the memo
// KEY additionally packs that same set into a uint64 bitmask, and only when
// len(edges) <= 64 (a map with a route id 65+ never triggers memoization —
// see newTrailDFS). A previous version used the uint64 bitmask as the used
// SET itself on both the memoized and unmemoized paths: for eIdx >= 64,
// uint64(1)<<eIdx is 0 in Go, so that edge was never marked used and the DFS
// recursed across it forever, a fatal (unrecoverable) stack overflow that
// takes the whole process down (C1 in the scoring/redaction review).
// maploader.go's validatePlayerBounds now also caps trains_per_player at 64,
// which bounds any one player's claimed-route count in practice, but this
// function must be correct independent of that loader guard.
func longestTrail(m *Map, routeIDs []int32) int {
	edges, adjacency := buildTrailGraph(m, routeIDs)
	if len(edges) == 0 {
		return 0
	}

	dfs := newTrailDFS(edges, adjacency)

	best := 0
	seen := make(map[string]bool, len(adjacency))
	used := make([]bool, len(edges))
	for _, e := range edges {
		for _, v := range [2]string{e.a, e.b} {
			if seen[v] {
				continue
			}
			seen[v] = true
			if r := dfs(v, used); r > best {
				best = r
			}
		}
	}
	return best
}

// buildTrailGraph converts routeIDs into a plain edge list plus a
// city-id -> incident-edge-index adjacency map, for longestTrail's DFS.
func buildTrailGraph(m *Map, routeIDs []int32) ([]trailEdge, map[string][]int) {
	edges := make([]trailEdge, 0, len(routeIDs))
	adjacency := make(map[string][]int, len(routeIDs))
	for _, rid := range routeIDs {
		r := m.RouteByID[rid]
		if r == nil {
			continue
		}
		idx := len(edges)
		edges = append(edges, trailEdge{a: r.A, b: r.B, length: r.Length})
		adjacency[r.A] = append(adjacency[r.A], idx)
		adjacency[r.B] = append(adjacency[r.B], idx)
	}
	return edges, adjacency
}

// trailState is the memoization key for the <=64-edge DFS path: a vertex
// plus a uint64 bitset of edges already used. The best additional length
// reachable from a given state depends only on which edges remain
// available, never on how that state was reached, so this key is sound. It
// is only ever constructed when len(edges) <= 64 (see newTrailDFS) — the
// used EDGE SET during traversal is a separate, unbounded []bool (see
// longestTrail's doc comment on why the two must not be conflated).
type trailState struct {
	vertex string
	used   uint64
}

// newTrailDFS builds a dfs(vertex, used) closure over edges/adjacency,
// memoized once len(edges) exceeds 20 and is at most 64 (the rules describe
// "≤ ~30 routes per player" as the practical ceiling, well under that
// limit). used is the caller-owned, shared-and-restored []bool backtracking
// set (longestTrail allocates it once per player and reuses it across every
// starting vertex): dfs marks used[eIdx] before recursing and unmarks it
// after, so it is always all-false again once a top-level call returns.
//
// The memo key packs that same set into a uint64 bitmask ONLY when
// len(edges) <= 64 — computing the bitmask is what used to double as the
// traversal's used-set check, which broke silently for edge index 64+ (see
// longestTrail's doc comment). Now the two are independent: correctness
// (the used []bool check) never depends on len(edges), and only the memo
// key's bitmask construction is gated by the <=64 bound.
func newTrailDFS(edges []trailEdge, adjacency map[string][]int) func(vertex string, used []bool) int {
	useMemo := len(edges) > 20 && len(edges) <= 64
	var memo map[trailState]int
	if useMemo {
		memo = make(map[trailState]int)
	}

	// bitsetOf packs used into a uint64 for the memo key. Only ever called
	// when useMemo is true, i.e. len(edges) <= 64, so eIdx always fits.
	bitsetOf := func(used []bool) uint64 {
		var bits uint64
		for eIdx, u := range used {
			if u {
				bits |= uint64(1) << uint(eIdx) // #nosec G115 -- eIdx < len(edges) <= 64 here (useMemo guard)
			}
		}
		return bits
	}

	var dfs func(vertex string, used []bool) int
	dfs = func(vertex string, used []bool) int {
		var key trailState
		if useMemo {
			key = trailState{vertex: vertex, used: bitsetOf(used)}
			if v, ok := memo[key]; ok {
				return v
			}
		}
		best := 0
		for _, eIdx := range adjacency[vertex] {
			if used[eIdx] {
				continue
			}
			e := edges[eIdx]
			next := e.b
			if vertex == e.b {
				next = e.a
			}
			used[eIdx] = true
			if cand := e.length + dfs(next, used); cand > best {
				best = cand
			}
			used[eIdx] = false
		}
		if useMemo {
			memo[key] = best
		}
		return best
	}
	return dfs
}

// stationOption is one candidate for a single station city: either
// borrowing a specific opponent-owned route incident to that city, or
// borrowing nothing (routeID == noBorrow).
type stationOption struct {
	city    string
	routeID int32
}

// bestStationAssignment brute-forces every assignment of (station city ->
// one incident opponent route, or none) shared across ALL of p's tickets,
// and returns the assignment maximizing p's net ticket score (rules §13.4),
// plus that net score and the completed/missed ticket id split it produces.
//
// Branching is the product, over p's <= stationsPerPlayer built stations, of
// (1 + incident-opponent-route-count) for that city — bounded in practice by
// <=3 stations x a handful of incident routes each (rules §13.4: "typically
// <=5"), so exhaustive search is fast and correct. Both testMap() (max
// degree per city is small — no city touches more than 4 of the 15 fixture
// routes) and the real Europe map (a handful of routes per city at most)
// stay far inside this bound; a pathological custom map with many routes
// converging on one city would only multiply, not exponentiate, the branch
// count per additional station (<=3 stations total), so even a city with
// dozens of incident routes keeps the search in the thousands of
// combinations, not a blowup.
func bestStationAssignment(m *Map, gs *pb.GameState, p *pb.PlayerState) (map[string]int32, int, []int32, []int32) {
	options := stationOptionSets(m, gs, p)
	ticketIDs := sortedTicketIDs(p)

	var best struct {
		set               bool
		net               int
		assignment        map[string]int32
		completed, missed []int32
	}

	var combo []stationOption
	var recurse func(i int)
	recurse = func(i int) {
		if i == len(options) {
			assignment, net, completed, missed := evaluateAssignment(m, p, combo, ticketIDs)
			if !best.set || net > best.net {
				best.set, best.net = true, net
				best.assignment, best.completed, best.missed = assignment, completed, missed
			}
			return
		}
		for _, opt := range options[i] {
			combo = append(combo, opt)
			recurse(i + 1)
			combo = combo[:len(combo)-1]
		}
	}
	recurse(0)

	return best.assignment, best.net, best.completed, best.missed
}

// stationOptionSets returns, per station city p has built (in
// PlayerState.StationCities order), the sorted list of candidate options:
// "borrow nothing" first, then every opponent-owned route incident to that
// city in ascending route-id order. "Opponent-owned" means owned (per
// gs.RouteOwner) by any seat other than p's — including a resigned player's
// seat, since a resigned player's routes stay on the board and keep
// blocking/serving exactly as an active opponent's would (plan Q14). A
// closed-but-unclaimed double-route sibling is never a candidate: it has no
// entry in gs.RouteOwner at all.
func stationOptionSets(m *Map, gs *pb.GameState, p *pb.PlayerState) [][]stationOption {
	sets := make([][]stationOption, len(p.StationCities))
	for i, city := range p.StationCities {
		ids := append([]int32(nil), m.RoutesByCity[city]...)
		slices.Sort(ids)

		opts := []stationOption{{city: city, routeID: noBorrow}}
		for _, rid := range ids {
			owner, owned := gs.RouteOwner[rid]
			if !owned || owner == p.SeatIndex {
				continue
			}
			opts = append(opts, stationOption{city: city, routeID: rid})
		}
		sets[i] = opts
	}
	return sets
}

// sortedTicketIDs returns p's kept ticket ids in ascending order, so
// completed/missed splits come out in a deterministic order independent of
// protobuf field iteration.
func sortedTicketIDs(p *pb.PlayerState) []int32 {
	ids := append([]int32(nil), p.TicketIds...)
	slices.Sort(ids)
	return ids
}

// evaluateAssignment scores p's tickets under one candidate combo of
// station borrows: build a union-find over p's own claimed routes plus the
// combo's borrowed routes, then check each ticket's connectivity (rules
// §13.2). It returns the borrowed-routes assignment map (only entries where
// a route was actually borrowed), the net +/- score, and the completed /
// missed ticket id lists.
func evaluateAssignment(m *Map, p *pb.PlayerState, combo []stationOption, ticketIDs []int32) (map[string]int32, int, []int32, []int32) {
	uf := newUnionFind()
	for _, rid := range p.ClaimedRouteIds {
		if r := m.RouteByID[rid]; r != nil {
			uf.union(r.A, r.B)
		}
	}

	assignment := make(map[string]int32, len(combo))
	for _, opt := range combo {
		if opt.routeID == noBorrow {
			continue
		}
		if r := m.RouteByID[opt.routeID]; r != nil {
			uf.union(r.A, r.B)
			assignment[opt.city] = opt.routeID
		}
	}

	net := 0
	var completed, missed []int32
	for _, tid := range ticketIDs {
		tk := m.TicketByID[tid]
		if tk == nil {
			continue
		}
		if uf.connected(tk.A, tk.B) {
			net += tk.Points
			completed = append(completed, tid)
		} else {
			net -= tk.Points
			missed = append(missed, tid)
		}
	}
	return assignment, net, completed, missed
}

// unionFind is a simple string-keyed disjoint-set structure used to test
// destination-ticket connectivity for one candidate station-borrow
// assignment (rules §13.2, §13.4). It is rebuilt fresh per candidate
// combination in bestStationAssignment/evaluateAssignment — the graphs
// involved are tiny (a player's own routes plus <=3 borrowed edges), so no
// union-by-rank/path-compression sophistication is needed beyond basic path
// compression in find.
type unionFind struct {
	parent map[string]string
}

func newUnionFind() *unionFind {
	return &unionFind{parent: make(map[string]string)}
}

// find returns the representative of x's set, creating a fresh singleton
// set for x if it has not been seen before.
func (u *unionFind) find(x string) string {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
		return x
	}
	if u.parent[x] != x {
		u.parent[x] = u.find(u.parent[x])
	}
	return u.parent[x]
}

// union merges the sets containing a and b.
func (u *unionFind) union(a, b string) {
	ra, rb := u.find(a), u.find(b)
	if ra != rb {
		u.parent[ra] = rb
	}
}

// connected reports whether a and b are in the same set.
func (u *unionFind) connected(a, b string) bool {
	return u.find(a) == u.find(b)
}

// rankable is the sort/tiebreak view of one active player's ScoreBreakdown,
// used only by assignRanks (rules §13.6).
type rankable struct {
	idx           int // index into the FinalScore results slice
	total         int32
	completed     int
	builtStations int
	hasLongest    bool
}

// rankableFor builds the rankable view of sb/p for assignRanks.
func rankableFor(idx int, sb *pb.ScoreBreakdown, p *pb.PlayerState) rankable {
	return rankable{
		idx:           idx,
		total:         sb.Total,
		completed:     len(sb.CompletedTicketIds),
		builtStations: len(p.StationCities),
		hasLongest:    sb.LongestPathBonus > 0,
	}
}

// assignRanks implements the §13.6 tiebreak order over the active-player
// entries in ranked, writing Rank (and, for a tied-for-first group,
// SharedVictory) directly into the corresponding results entries:
//
//  1. Highest total.
//  2. Most completed tickets.
//  3. Fewest stations built.
//  4. Holds the longest path (LongestPathBonus > 0).
//  5. Still tied -> same rank; SharedVictory is set on each rank-1 entry
//     only (a tie surviving all four criteria anywhere lower in the
//     standings still shares a rank number, just not the shared-victory
//     flag, which by definition marks a shared *win*).
//
// Ranks follow standard competition ranking (ties share a rank; the next
// distinct entry's rank is its 1-based position, i.e. "1,1,3,4").
func assignRanks(results []*pb.ScoreBreakdown, ranked []rankable) {
	sort.SliceStable(ranked, func(i, j int) bool { return rankLess(ranked[i], ranked[j]) })

	for i, r := range ranked {
		rank := int32(i + 1) // #nosec G115 -- bounded by maxSeats (5)
		if i > 0 && rankTie(ranked[i-1], r) {
			rank = results[ranked[i-1].idx].Rank
		}
		results[r.idx].Rank = rank
	}

	firstPlace := make([]*pb.ScoreBreakdown, 0, len(ranked))
	for _, r := range ranked {
		if results[r.idx].Rank == 1 {
			firstPlace = append(firstPlace, results[r.idx])
		}
	}
	if len(firstPlace) > 1 {
		for _, sb := range firstPlace {
			sb.SharedVictory = true
		}
	}
}

// rankLess reports whether a ranks strictly ahead of b under the §13.6
// tiebreak order.
func rankLess(a, b rankable) bool {
	if a.total != b.total {
		return a.total > b.total
	}
	if a.completed != b.completed {
		return a.completed > b.completed
	}
	if a.builtStations != b.builtStations {
		return a.builtStations < b.builtStations
	}
	if a.hasLongest != b.hasLongest {
		return a.hasLongest
	}
	return false
}

// rankTie reports whether a and b are indistinguishable under every §13.6
// criterion (and therefore share a rank).
func rankTie(a, b rankable) bool {
	return a.total == b.total &&
		a.completed == b.completed &&
		a.builtStations == b.builtStations &&
		a.hasLongest == b.hasLongest
}
