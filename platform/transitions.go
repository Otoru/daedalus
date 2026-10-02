package platform

import (
	"context"
	"math"
	"sort"

	"github.com/Otoru/daedalus/core"
)

// targetEndpointsPerRoom is the reference randomiser's mean, 890 endpoints
// over 368 scenes. Those endpoints are distinct transitions: a second opening
// between the same two rooms is not one of them. The mean is what a map is
// measured against. It is not a count this generator forges by punching a
// parallel hole in a wall that already joins those rooms.
const targetEndpointsPerRoom = 890.0 / 368.0

// targetAsymmetricFraction is 16 one-way pairs over 445, counting the three
// patterns the reference marks: a fall, a bottom exit whose return is another
// path, and the same opening with a different ability in each sense.
const targetAsymmetricFraction = 16.0 / 445.0

// TransitionStats is the calibration reading of one plane.
type TransitionStats struct {
	Rooms             int
	Endpoints         int
	Pairs             int
	MeanDegree        float64
	OneWayPairs       int
	ConditionalPairs  int
	AsymmetricPairs   int
	MultiOpeningSides int
	// DistinctPairs is room pairs, not openings. Two openings between the
	// same rooms count once. ParallelPairs is how many of those room pairs
	// are joined by more than one opening.
	DistinctPairs int
	ParallelPairs int
}

// TransitionStatistics counts endpoints, pairs and the two asymmetry shapes.
// A pair is conditional when both senses exist and disagree, and one-way only
// when a sense is missing. Those are different and are counted apart; their
// sum is what the reference graph folds into one flag.
func TransitionStatistics(plane Plane) TransitionStats {
	stats := TransitionStats{Rooms: len(plane.Rooms)}
	index := map[TransitionID]Transition{}
	for _, room := range plane.Rooms {
		stats.Endpoints += len(room.Transitions)
		var perSide [6]int
		for _, t := range room.Transitions {
			index[t.ID] = t
			if t.Side >= 0 && int(t.Side) < len(perSide) {
				perSide[t.Side]++
			}
		}
		for _, count := range perSide {
			if count >= 2 {
				stats.MultiOpeningSides++
			}
		}
	}
	if stats.Rooms > 0 {
		stats.MeanDegree = float64(stats.Endpoints) / float64(stats.Rooms)
	}
	seen := map[TransitionID]bool{}
	roomPairs := map[roomPair]int{}
	for _, t := range index {
		if seen[t.ID] {
			continue
		}
		partner, ok := index[t.To]
		if !ok || seen[partner.ID] {
			continue
		}
		seen[t.ID] = true
		seen[partner.ID] = true
		stats.Pairs++
		roomPairs[orderedPair(t.Room, partner.Room)]++
		switch {
		case t.IsOneWay() || partner.IsOneWay():
			stats.OneWayPairs++
		case t.IsConditional() || partner.IsConditional():
			stats.ConditionalPairs++
		}
	}
	stats.DistinctPairs = len(roomPairs)
	for _, n := range roomPairs {
		if n > 1 {
			stats.ParallelPairs++
		}
	}
	stats.AsymmetricPairs = stats.OneWayPairs + stats.ConditionalPairs
	return stats
}

func targetPairCount(rooms int) int {
	if rooms <= 1 {
		return 0
	}
	n := int(math.Round(float64(rooms) * 445.0 / 368.0))
	if n < rooms-1 {
		n = rooms - 1
	}
	return n
}

func targetAsymmetricCount(pairs int) int {
	if pairs <= 0 {
		return 0
	}
	return int(math.Round(float64(pairs) * targetAsymmetricFraction))
}

type sensePattern int

const (
	patternSymmetric sensePattern = iota
	patternFall
	patternBotReturn
	patternConditional
)

type roomPair struct {
	lo, hi RoomID
}

func orderedPair(a, b RoomID) roomPair {
	if a > b {
		a, b = b, a
	}
	return roomPair{lo: a, hi: b}
}

type roomContact struct {
	a, b     RoomID
	sideA    TransitionSide
	axis0    int32
	axis1    int32
	second   bool
	pattern  sensePattern
	requires AbilitySet
	forward  RoomID
}

type gatePlan struct {
	pair      roomPair
	step      int
	requires  AbilitySet
	forward   RoomID
	spawnSide RoomID
	gate      TransitionID
	haveGate  bool
}

type assembly struct {
	contacts []roomContact
	gates    []gatePlan
	spine    []RoomID
	goal     RoomID
}

type axisSpan struct {
	start  int32
	extent int32
}

func wireTransitions(ctx context.Context, plane Plane, parents []int, steps []ProgressionStep) (Plane, assembly, error) {
	var zero assembly
	if err := ctx.Err(); err != nil {
		return plane, zero, err
	}
	geometric := geometricContacts(plane)
	chosen, extras, err := treeContacts(geometric, parents)
	if err != nil {
		return plane, zero, err
	}
	spine, err := spinePath(parents)
	if err != nil {
		return plane, zero, err
	}
	gates, err := placeGates(chosen, spine, steps)
	if err != nil {
		return plane, zero, err
	}
	chosen, err = fillOpenings(chosen, extras, gates, len(plane.Rooms))
	if err != nil {
		return plane, zero, err
	}
	chosen = spliceReturn(chosen, extras, gates, len(plane.Rooms))
	goal := RoomID(0)
	if len(spine) > 0 {
		goal = spine[len(spine)-1]
	}
	addFalls(chosen, gates, goal)
	sort.Slice(chosen, func(i, j int) bool {
		if chosen[i].a != chosen[j].a {
			return chosen[i].a < chosen[j].a
		}
		if chosen[i].b != chosen[j].b {
			return chosen[i].b < chosen[j].b
		}
		return chosen[i].sideA < chosen[j].sideA
	})
	plane, gates, err = materialize(plane, chosen, gates)
	if err != nil {
		return plane, zero, err
	}
	return plane, assembly{contacts: chosen, gates: gates, spine: spine, goal: goal}, nil
}

func geometricContacts(plane Plane) []roomContact {
	var out []roomContact
	for i := range plane.Rooms {
		for j := i + 1; j < len(plane.Rooms); j++ {
			if c, ok := contactBetween(plane.Rooms[i], plane.Rooms[j]); ok {
				out = append(out, c)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].a != out[j].a {
			return out[i].a < out[j].a
		}
		return out[i].b < out[j].b
	})
	return out
}

func contactBetween(a, b Room) (roomContact, bool) {
	aw, ah := int32(a.Grid.Width), int32(a.Grid.Height)
	bw, bh := int32(b.Grid.Width), int32(b.Grid.Height)
	switch {
	case a.Origin.X+aw == b.Origin.X:
		y0, y1 := overlap(a.Origin.Y, ah, b.Origin.Y, bh)
		if y1-y0 >= minOverlapCells {
			return roomContact{a: a.ID, b: b.ID, sideA: TransitionSideRight, axis0: y0, axis1: y1}, true
		}
	case b.Origin.X+bw == a.Origin.X:
		y0, y1 := overlap(a.Origin.Y, ah, b.Origin.Y, bh)
		if y1-y0 >= minOverlapCells {
			return roomContact{a: b.ID, b: a.ID, sideA: TransitionSideRight, axis0: y0, axis1: y1}, true
		}
	case a.Origin.Y+ah == b.Origin.Y:
		x0, x1 := overlap(a.Origin.X, aw, b.Origin.X, bw)
		if x1-x0 >= minOverlapCells {
			return roomContact{a: a.ID, b: b.ID, sideA: TransitionSideBottom, axis0: x0, axis1: x1}, true
		}
	case b.Origin.Y+bh == a.Origin.Y:
		x0, x1 := overlap(a.Origin.X, aw, b.Origin.X, bw)
		if x1-x0 >= minOverlapCells {
			return roomContact{a: b.ID, b: a.ID, sideA: TransitionSideBottom, axis0: x0, axis1: x1}, true
		}
	}
	return roomContact{}, false
}

func overlap(a0, aSpan, b0, bSpan int32) (int32, int32) {
	return max(a0, b0), min(a0+aSpan, b0+bSpan)
}

func treeContacts(geometric []roomContact, parents []int) (chosen, extras []roomContact, err error) {
	want := map[roomPair]bool{}
	for child, parent := range parents {
		if parent < 0 {
			continue
		}
		want[orderedPair(RoomID(child), RoomID(parent))] = true
	}
	for _, c := range geometric {
		key := orderedPair(c.a, c.b)
		if want[key] {
			chosen = append(chosen, c)
			delete(want, key)
			continue
		}
		extras = append(extras, c)
	}
	if len(want) > 0 {
		return nil, nil, geometryError("a tree edge has no shared wall long enough for an opening")
	}
	return chosen, extras, nil
}

func spinePath(parents []int) ([]RoomID, error) {
	n := len(parents)
	adj := make([][]int, n)
	for child, parent := range parents {
		if parent < 0 {
			continue
		}
		adj[parent] = append(adj[parent], child)
		adj[child] = append(adj[child], parent)
	}
	for i := range adj {
		sort.Ints(adj[i])
	}
	goal, parent := farthest(0, adj)
	if parent[goal] < 0 {
		return nil, geometryError("room %d is disconnected from the spawn", goal)
	}
	var rev []RoomID
	for cur := goal; ; cur = parent[cur] {
		rev = append(rev, RoomID(cur))
		if cur == 0 {
			break
		}
		if parent[cur] == cur || parent[cur] < 0 {
			return nil, geometryError("spine walk from room %d missed the spawn", goal)
		}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, nil
}

func farthest(start int, adj [][]int) (int, []int) {
	n := len(adj)
	dist := make([]int, n)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = -1
		dist[i] = -1
	}
	dist[start] = 0
	parent[start] = start
	queue := []int{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nxt := range adj[cur] {
			if dist[nxt] >= 0 {
				continue
			}
			dist[nxt] = dist[cur] + 1
			parent[nxt] = cur
			queue = append(queue, nxt)
		}
	}
	best := start
	for i := 1; i < n; i++ {
		if dist[i] > dist[best] || (dist[i] == dist[best] && dist[i] >= 0 && i < best) {
			best = i
		}
	}
	return best, parent
}

func placeGates(contacts []roomContact, spine []RoomID, steps []ProgressionStep) ([]gatePlan, error) {
	edges := len(spine) - 1
	if edges < 0 {
		edges = 0
	}
	indices, err := pickGateIndices(edges, len(steps))
	if err != nil {
		return nil, err
	}
	gates := make([]gatePlan, len(indices))
	for step, edge := range indices {
		spawnSide := spine[edge]
		forward := spine[edge+1]
		at := findContact(contacts, spawnSide, forward)
		if at < 0 {
			return nil, geometryError("spine edge %d→%d is not a placed wall", spawnSide, forward)
		}
		contacts[at].pattern = patternConditional
		contacts[at].requires = steps[step].Grants
		contacts[at].forward = forward
		gates[step] = gatePlan{
			pair:      orderedPair(spawnSide, forward),
			step:      step,
			requires:  steps[step].Grants,
			forward:   forward,
			spawnSide: spawnSide,
		}
	}
	return gates, nil
}

func pickGateIndices(edges, steps int) ([]int, error) {
	if steps == 0 {
		return nil, nil
	}
	if edges < steps {
		return nil, progressionError("spine has %d edges and the plan needs %d gates", edges, steps)
	}
	used := make([]bool, edges)
	out := make([]int, 0, steps)
	for ordinal := 1; ordinal <= steps; ordinal++ {
		idx := (ordinal * edges) / (steps + 1)
		if idx >= edges {
			idx = edges - 1
		}
		if used[idx] {
			found := -1
			for j := idx; j < edges; j++ {
				if !used[j] {
					found = j
					break
				}
			}
			if found < 0 {
				for j := 0; j < idx; j++ {
					if !used[j] {
						found = j
						break
					}
				}
			}
			if found < 0 {
				return nil, progressionError("could not place %d distinct gates on %d edges", steps, edges)
			}
			idx = found
		}
		used[idx] = true
		out = append(out, idx)
	}
	sort.Ints(out)
	return out, nil
}

func findContact(contacts []roomContact, a, b RoomID) int {
	want := orderedPair(a, b)
	for i, c := range contacts {
		if orderedPair(c.a, c.b) == want {
			return i
		}
	}
	return -1
}

func fillOpenings(chosen, extras []roomContact, gates []gatePlan, rooms int) ([]roomContact, error) {
	gated := map[roomPair]bool{}
	for _, gate := range gates {
		gated[gate.pair] = true
	}
	count := openingCount(chosen)
	target := targetPairCount(rooms)
	// The second span of a contact that already joins two rooms is a parallel
	// edge: the player arrives in the same room through either hole, and on a
	// short overlap the two holes are one opening with a tooth. Hollow Knight's
	// left1/left2 on one wall go to different places. The target above stays
	// the reference count of distinct transitions. Extra contacts are new room
	// pairs, so they are the only thing spent against it. When the plane has
	// no more shared walls, the map keeps the pairs it has. Missing the count
	// is the measurement, not a reason to punch the parallel hole.
	labels := labelsSkipping(rooms, chosen, gated)
	for _, extra := range extras {
		if count >= target {
			break
		}
		if int(extra.a) >= len(labels) || int(extra.b) >= len(labels) {
			continue
		}
		if labels[extra.a] < 0 || labels[extra.a] != labels[extra.b] {
			continue
		}
		chosen = append(chosen, extra)
		count++
	}
	return chosen, nil
}

func openingCount(contacts []roomContact) int {
	n := 0
	for _, c := range contacts {
		n += len(contactSpans(c, c.second))
	}
	return n
}

func labelsSkipping(n int, contacts []roomContact, skip map[roomPair]bool) []int {
	adj := make([][]int, n)
	for _, c := range contacts {
		if skip[orderedPair(c.a, c.b)] {
			continue
		}
		adj[c.a] = append(adj[c.a], int(c.b))
		adj[c.b] = append(adj[c.b], int(c.a))
	}
	for i := range adj {
		sort.Ints(adj[i])
	}
	label := make([]int, n)
	for i := range label {
		label[i] = -1
	}
	next := 0
	for i := 0; i < n; i++ {
		if label[i] >= 0 {
			continue
		}
		queue := []int{i}
		label[i] = next
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, nxt := range adj[cur] {
				if label[nxt] >= 0 {
					continue
				}
				label[nxt] = next
				queue = append(queue, nxt)
			}
		}
		next++
	}
	return label
}

func addFalls(contacts []roomContact, gates []gatePlan, goal RoomID) error {
	pairs := openingCount(contacts)
	want := targetAsymmetricCount(pairs) - len(gates)
	if want <= 0 {
		return nil
	}
	gated := map[roomPair]bool{}
	for _, gate := range gates {
		gated[gate.pair] = true
	}
	added := 0
	for i := range contacts {
		if added >= want {
			break
		}
		c := contacts[i]
		if c.sideA != TransitionSideBottom || c.pattern != patternSymmetric {
			continue
		}
		if gated[orderedPair(c.a, c.b)] {
			continue
		}
		// Pattern 2: the bottom exit is one-way and another path returns.
		if !reaches(contacts, c.b, c.a, orderedPair(c.a, c.b)) {
			continue
		}
		contacts[i].pattern = patternBotReturn
		added++
	}
	spawnFree := freeComponent(contacts, 0, roomPair{lo: 0, hi: 0}, false)
	for i := range contacts {
		if added >= want {
			break
		}
		c := contacts[i]
		if c.sideA != TransitionSideBottom || c.pattern != patternSymmetric {
			continue
		}
		if gated[orderedPair(c.a, c.b)] {
			continue
		}
		// Pattern 1: a fall that does not climb, and only once the ledge is
		// already behind an ability. The landing must still reach the goal
		// with no further gate, or the drop is a softlock.
		block := orderedPair(c.a, c.b)
		if reaches(contacts, c.b, c.a, block) || spawnFree[c.a] {
			continue
		}
		south := freeComponent(contacts, c.b, block, true)
		if !south[goal] {
			continue
		}
		contacts[i].pattern = patternFall
		added++
	}
	return nil
}

// freeComponent is the rooms reachable from start without crossing block and
// without crossing a conditional gate. block is ignored when useBlock is false.
func freeComponent(contacts []roomContact, start RoomID, block roomPair, useBlock bool) map[RoomID]bool {
	n := 0
	for _, c := range contacts {
		if int(c.a) >= n {
			n = int(c.a) + 1
		}
		if int(c.b) >= n {
			n = int(c.b) + 1
		}
	}
	adj := make([][]int, n)
	for _, c := range contacts {
		if c.pattern == patternConditional {
			continue
		}
		if useBlock && orderedPair(c.a, c.b) == block {
			continue
		}
		adj[c.a] = append(adj[c.a], int(c.b))
		adj[c.b] = append(adj[c.b], int(c.a))
	}
	seen := map[RoomID]bool{}
	if int(start) >= n {
		return seen
	}
	queue := []int{int(start)}
	seen[start] = true
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nxt := range adj[cur] {
			if seen[RoomID(nxt)] {
				continue
			}
			seen[RoomID(nxt)] = true
			queue = append(queue, nxt)
		}
	}
	return seen
}

// spliceReturn adds one extra bottom contact inside a single lock component,
// so a one-way drop has a cycle to come back on. The contact is a new room
// pair. It is not paid for by deleting a second hole in a wall that already
// joins two rooms.
func spliceReturn(chosen, extras []roomContact, gates []gatePlan, rooms int) []roomContact {
	if targetAsymmetricCount(openingCount(chosen)) <= len(gates) {
		return chosen
	}
	gated := map[roomPair]bool{}
	for _, gate := range gates {
		gated[gate.pair] = true
	}
	labels := labelsSkipping(rooms, chosen, gated)
	extraAt := -1
	for i, c := range extras {
		if c.sideA != TransitionSideBottom {
			continue
		}
		if int(c.a) >= len(labels) || int(c.b) >= len(labels) {
			continue
		}
		if labels[c.a] < 0 || labels[c.a] != labels[c.b] {
			continue
		}
		extraAt = i
		break
	}
	if extraAt < 0 {
		return chosen
	}
	extra := extras[extraAt]
	want := orderedPair(extra.a, extra.b)
	for _, c := range chosen {
		if orderedPair(c.a, c.b) == want {
			return chosen
		}
	}
	return append(chosen, extra)
}

func reaches(contacts []roomContact, from, to RoomID, block roomPair) bool {
	if from == to {
		return true
	}
	n := 0
	for _, c := range contacts {
		if int(c.a) >= n {
			n = int(c.a) + 1
		}
		if int(c.b) >= n {
			n = int(c.b) + 1
		}
	}
	adj := make([][]int, n)
	for _, c := range contacts {
		if orderedPair(c.a, c.b) == block {
			continue
		}
		adj[c.a] = append(adj[c.a], int(c.b))
		adj[c.b] = append(adj[c.b], int(c.a))
	}
	seen := make([]bool, n)
	queue := []int{int(from)}
	seen[from] = true
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, nxt := range adj[cur] {
			if seen[nxt] {
				continue
			}
			if RoomID(nxt) == to {
				return true
			}
			seen[nxt] = true
			queue = append(queue, nxt)
		}
	}
	return false
}

func contactSpans(c roomContact, second bool) []axisSpan {
	if c.sideA.IsVertical() {
		floor, ok := floorSpan(c.axis0, c.axis1)
		if !ok {
			return nil
		}
		if !second {
			return []axisSpan{floor}
		}
		high, ok := highSpan(c.axis0, c.axis1)
		if !ok || spansOverlap(floor, high) || spanSolidGap(floor, high) < int32(OpeningExtent) {
			return []axisSpan{floor}
		}
		return []axisSpan{floor, high}
	}
	primary, ok := centerSpan(c.axis0, c.axis1)
	if !ok {
		return nil
	}
	if !second {
		return []axisSpan{primary}
	}
	left, right, ok := endSpans(c.axis0, c.axis1)
	if !ok || spansOverlap(left, right) || spanSolidGap(left, right) < int32(OpeningExtent) {
		return []axisSpan{primary}
	}
	return []axisSpan{left, right}
}

// spanSolidGap is the solid cells between two air spans on the same axis.
// A negative gap means the spans overlap.
func spanSolidGap(a, b axisSpan) int32 {
	if a.start > b.start {
		a, b = b, a
	}
	return b.start - (a.start + a.extent)
}

func floorSpan(y0, y1 int32) (axisSpan, bool) {
	extent := int32(OpeningExtent)
	if y1-y0 < extent+1 {
		return axisSpan{}, false
	}
	return axisSpan{start: y1 - 1 - extent, extent: extent}, true
}

func highSpan(y0, y1 int32) (axisSpan, bool) {
	span := y1 - y0
	if span < minOverlapCells {
		return axisSpan{}, false
	}
	extent := int32(OpeningExtent)
	bands := int(span) / GateBandCells
	if bands < 2 {
		return axisSpan{}, false
	}
	land := y1 - 1 - int32(bands-1)*GateBandCells
	start := land - extent
	if start < y0 || land >= y1 {
		return axisSpan{}, false
	}
	return axisSpan{start: start, extent: extent}, true
}

func centerSpan(x0, x1 int32) (axisSpan, bool) {
	extent := int32(OpeningExtent)
	if x1-x0 < extent {
		return axisSpan{}, false
	}
	return axisSpan{start: x0 + (x1-x0-extent)/2, extent: extent}, true
}

func endSpans(x0, x1 int32) (left, right axisSpan, ok bool) {
	extent := int32(OpeningExtent)
	if x1-x0 < 2*extent+1 {
		return axisSpan{}, axisSpan{}, false
	}
	return axisSpan{start: x0, extent: extent}, axisSpan{start: x1 - extent, extent: extent}, true
}

func spansOverlap(a, b axisSpan) bool {
	return a.start < b.start+b.extent && b.start < a.start+a.extent
}

func materialize(plane Plane, contacts []roomContact, gates []gatePlan) (Plane, []gatePlan, error) {
	gateAt := map[roomPair]int{}
	for i, gate := range gates {
		gateAt[gate.pair] = i
	}
	var all []Transition
	for _, c := range contacts {
		spans := contactSpans(c, c.second)
		if len(spans) == 0 {
			return plane, nil, geometryError("rooms %d and %d share a wall with no place for an opening", c.a, c.b)
		}
		roomA := plane.Rooms[c.a]
		roomB := plane.Rooms[c.b]
		sideB := oppositeSide(c.sideA)
		for spanIndex, sp := range spans {
			idA := TransitionID(len(all))
			idB := idA + 1
			outA, inA := senses(c, c.a)
			outB, inB := senses(c, c.b)
			tA, okA := newTransition(roomA, idA, c.sideA, sp, idB, outA, inA)
			tB, okB := newTransition(roomB, idB, sideB, sp, idA, outB, inB)
			if !okA || !okB {
				return plane, nil, geometryError("opening between rooms %d and %d does not land on both walls", c.a, c.b)
			}
			all = append(all, tA, tB)
			if gi, ok := gateAt[orderedPair(c.a, c.b)]; ok && spanIndex == 0 {
				if tA.Room == gates[gi].spawnSide {
					gates[gi].gate = tA.ID
				} else {
					gates[gi].gate = tB.ID
				}
				gates[gi].haveGate = true
			}
		}
	}
	for i := range gates {
		if !gates[i].haveGate {
			return plane, nil, geometryError("gate %d did not receive an opening", i)
		}
	}
	buckets := make([][]Transition, len(plane.Rooms))
	for _, t := range all {
		buckets[t.Room] = append(buckets[t.Room], t)
	}
	for i := range plane.Rooms {
		plane.Rooms[i].Transitions = buckets[i]
	}
	assignIndices(plane.Rooms)
	for i := range plane.Rooms {
		for _, t := range plane.Rooms[i].Transitions {
			punchOpening(&plane.Rooms[i], t)
		}
	}
	return plane, gates, nil
}

func senses(c roomContact, room RoomID) (*Traversal, *Traversal) {
	switch c.pattern {
	case patternFall:
		if room == c.a {
			return &Traversal{Note: "fall"}, nil
		}
		return nil, &Traversal{Note: "fall"}
	case patternBotReturn:
		if room == c.a {
			return &Traversal{Note: "bot-return"}, nil
		}
		return nil, &Traversal{Note: "bot-return"}
	case patternConditional:
		need := &Traversal{Requires: c.requires, Note: "ability"}
		free := &Traversal{}
		if room == c.forward {
			return free, need
		}
		return need, free
	default:
		return &Traversal{}, &Traversal{}
	}
}

func newTransition(room Room, id TransitionID, side TransitionSide, sp axisSpan, to TransitionID, out, in *Traversal) (Transition, bool) {
	offset, ok := offsetFromSpan(room, side, sp)
	if !ok {
		return Transition{}, false
	}
	exit, ok := sideExit(side)
	if !ok {
		return Transition{}, false
	}
	return Transition{
		ID:       id,
		Room:     room.ID,
		Side:     side,
		Offset:   offset,
		Extent:   uint32(sp.extent),
		Exit:     exit,
		To:       to,
		Outbound: out,
		Inbound:  in,
	}, true
}

func offsetFromSpan(room Room, side TransitionSide, sp axisSpan) (uint32, bool) {
	var offset int32
	switch side {
	case TransitionSideLeft, TransitionSideRight:
		offset = room.Origin.Y + int32(room.Grid.Height) - sp.extent - sp.start
	case TransitionSideTop, TransitionSideBottom:
		offset = sp.start - room.Origin.X
	default:
		return 0, false
	}
	if offset < 0 {
		return 0, false
	}
	return uint32(offset), true
}

func openingSpan(room Room, t Transition) (axisSpan, bool) {
	if t.Extent == 0 || !t.Side.IsCardinal() {
		return axisSpan{}, false
	}
	switch t.Side {
	case TransitionSideLeft, TransitionSideRight:
		start := room.Origin.Y + int32(room.Grid.Height) - int32(t.Offset) - int32(t.Extent)
		return axisSpan{start: start, extent: int32(t.Extent)}, true
	default:
		return axisSpan{start: room.Origin.X + int32(t.Offset), extent: int32(t.Extent)}, true
	}
}

func oppositeSide(side TransitionSide) TransitionSide {
	switch side {
	case TransitionSideLeft:
		return TransitionSideRight
	case TransitionSideRight:
		return TransitionSideLeft
	case TransitionSideTop:
		return TransitionSideBottom
	case TransitionSideBottom:
		return TransitionSideTop
	default:
		return TransitionSideUnspecified
	}
}

func sideExit(side TransitionSide) (Direction, bool) {
	switch side {
	case TransitionSideLeft:
		return core.DirectionWest, true
	case TransitionSideRight:
		return core.DirectionEast, true
	case TransitionSideTop:
		return core.DirectionNorth, true
	case TransitionSideBottom:
		return core.DirectionSouth, true
	default:
		return 0, false
	}
}

func assignIndices(rooms []Room) {
	for ri := range rooms {
		var buckets [6][]int
		for ti := range rooms[ri].Transitions {
			side := rooms[ri].Transitions[ti].Side
			if side < 0 || int(side) >= len(buckets) {
				continue
			}
			buckets[side] = append(buckets[side], ti)
		}
		for _, idxs := range buckets {
			sort.Slice(idxs, func(i, j int) bool {
				return rooms[ri].Transitions[idxs[i]].Offset < rooms[ri].Transitions[idxs[j]].Offset
			})
			for rank, ti := range idxs {
				rooms[ri].Transitions[ti].Index = uint32(rank + 1)
			}
		}
		sort.Slice(rooms[ri].Transitions, func(i, j int) bool {
			return rooms[ri].Transitions[i].ID < rooms[ri].Transitions[j].ID
		})
	}
}

func punchOpening(room *Room, t Transition) {
	sp, ok := openingSpan(*room, t)
	if !ok {
		return
	}
	switch t.Side {
	case TransitionSideLeft:
		for i := int32(0); i < sp.extent; i++ {
			setCell(&room.Grid, 0, sp.start+i-room.Origin.Y, CellKindEmpty)
		}
	case TransitionSideRight:
		x := int32(room.Grid.Width) - 1
		for i := int32(0); i < sp.extent; i++ {
			setCell(&room.Grid, x, sp.start+i-room.Origin.Y, CellKindEmpty)
		}
	case TransitionSideTop:
		for i := int32(0); i < sp.extent; i++ {
			setCell(&room.Grid, sp.start+i-room.Origin.X, 0, CellKindEmpty)
		}
	default:
		y := int32(room.Grid.Height) - 1
		for i := int32(0); i < sp.extent; i++ {
			setCell(&room.Grid, sp.start+i-room.Origin.X, y, CellKindEmpty)
		}
	}
}

func validateTransitions(plane Plane) error {
	count := 0
	var maxID TransitionID
	seen := map[TransitionID]struct{ room, index int }{}
	for ri := range plane.Rooms {
		var previous TransitionID
		for ti := range plane.Rooms[ri].Transitions {
			t := plane.Rooms[ri].Transitions[ti]
			count++
			if t.Room != plane.Rooms[ri].ID {
				return geometryError("transition %d is listed on room %d but names room %d", t.ID, plane.Rooms[ri].ID, t.Room)
			}
			if _, ok := seen[t.ID]; ok {
				return geometryError("transition %d is listed twice", t.ID)
			}
			seen[t.ID] = struct{ room, index int }{ri, ti}
			if count == 1 || t.ID > maxID {
				maxID = t.ID
			}
			if ti > 0 && t.ID < previous {
				return geometryError("room %d lists transition %d before %d", plane.Rooms[ri].ID, previous, t.ID)
			}
			previous = t.ID
			if err := validateOneTransition(plane.Rooms[ri], t); err != nil {
				return err
			}
		}
		if err := validateSideIndices(plane.Rooms[ri]); err != nil {
			return err
		}
	}
	if count == 0 {
		return nil
	}
	if int(maxID) != count-1 || len(seen) != count {
		return geometryError("transition ids are not the dense range 0..%d", count-1)
	}
	for _, loc := range seen {
		t := plane.Rooms[loc.room].Transitions[loc.index]
		if t.To == t.ID {
			return geometryError("transition %d pairs with itself", t.ID)
		}
		other, ok := seen[t.To]
		if !ok {
			return geometryError("transition %d pairs with %d, which is not in the plane", t.ID, t.To)
		}
		partner := plane.Rooms[other.room].Transitions[other.index]
		if partner.To != t.ID {
			return geometryError("transition %d pairs with %d, which pairs with %d", t.ID, partner.ID, partner.To)
		}
		if t.ID > partner.ID {
			continue
		}
		if err := validatePair(plane.Rooms[loc.room], t, plane.Rooms[other.room], partner); err != nil {
			return err
		}
	}
	return nil
}

func validateOneTransition(room Room, t Transition) error {
	if t.Side == TransitionSideUnspecified {
		return geometryError("transition %d has no side", t.ID)
	}
	if t.Extent == 0 {
		return geometryError("transition %d has an empty extent", t.ID)
	}
	if !knownDirection(t.Exit) {
		return geometryError("transition %d exits along %d, which is not a cardinal direction", t.ID, t.Exit)
	}
	if t.Side == TransitionSideDoor {
		return nil
	}
	if !t.Side.IsCardinal() {
		return geometryError("transition %d has side %s", t.ID, t.Side)
	}
	exit, _ := sideExit(t.Side)
	if t.Exit != exit {
		return geometryError("transition %d is on the %s wall but exits %s", t.ID, t.Side, directionName(t.Exit))
	}
	limit := room.Grid.Height
	if !t.Side.IsVertical() {
		limit = room.Grid.Width
	}
	if uint64(t.Offset)+uint64(t.Extent) > uint64(limit) {
		return geometryError("transition %d offset %d extent %d does not fit a %s wall of %d", t.ID, t.Offset, t.Extent, t.Side, limit)
	}
	return nil
}

func validateSideIndices(room Room) error {
	var buckets [6][]Transition
	for _, t := range room.Transitions {
		if t.Side < 0 || int(t.Side) >= len(buckets) {
			continue
		}
		buckets[t.Side] = append(buckets[t.Side], t)
	}
	for side, group := range buckets {
		if len(group) > MaxTransitionsPerSide {
			return limitError("room %d side %s has %d openings, above the ceiling of %d", room.ID, TransitionSide(side), len(group), MaxTransitionsPerSide)
		}
		sort.Slice(group, func(i, j int) bool { return group[i].Offset < group[j].Offset })
		for rank, t := range group {
			if t.Index != uint32(rank+1) {
				return geometryError("transition %d has index %d, want %d for its offset order on %s", t.ID, t.Index, rank+1, t.Side)
			}
			if rank > 0 && group[rank-1].Offset+group[rank-1].Extent > t.Offset {
				return geometryError("openings %d and %d overlap on room %d %s", group[rank-1].ID, t.ID, room.ID, t.Side)
			}
		}
	}
	return nil
}

func validatePair(roomA Room, a Transition, roomB Room, b Transition) error {
	if a.Side == TransitionSideDoor || b.Side == TransitionSideDoor {
		if a.Side != TransitionSideDoor || b.Side != TransitionSideDoor {
			return geometryError("transition %d is a door but its partner %d is %s", a.ID, b.ID, b.Side)
		}
		return nil
	}
	if b.Side != oppositeSide(a.Side) {
		return geometryError("transition %d on %s pairs with %d on %s", a.ID, a.Side, b.ID, b.Side)
	}
	spanA, okA := openingSpan(roomA, a)
	spanB, okB := openingSpan(roomB, b)
	if !okA || !okB || spanA != spanB {
		return geometryError("transitions %d and %d do not occupy the same run of the shared wall", a.ID, b.ID)
	}
	return nil
}

func knownDirection(d Direction) bool {
	switch d {
	case core.DirectionNorth, core.DirectionEast, core.DirectionSouth, core.DirectionWest:
		return true
	default:
		return false
	}
}

func directionName(d Direction) string {
	switch d {
	case core.DirectionNorth:
		return "north"
	case core.DirectionEast:
		return "east"
	case core.DirectionSouth:
		return "south"
	case core.DirectionWest:
		return "west"
	default:
		return "unspecified"
	}
}
