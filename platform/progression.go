package platform

import (
	"context"
	"sort"
)

// AbilityGrant is one acquisition: the step that grants it and the room the
// character must be able to reach before any edge that requires it.
type AbilityGrant struct {
	Step   int
	Room   RoomID
	Grants AbilitySet
}

// RegionLock is a region that must stay unreachable without Ability. Gate is
// the authored opening that is supposed to be the way in.
type RegionLock struct {
	Step    int
	Ability Ability
	Gate    TransitionID
	Rooms   []RoomID
}

// StageRoute is reachability under one cumulative moveset. Objective is the
// next grant's room, or the goal once every grant is held. GoalReachable is
// recorded separately so a stage that can collect its grant and still cannot
// finish is visible.
type StageRoute struct {
	Index              int
	Abilities          AbilitySet
	Objective          RoomID
	ObjectiveIsGoal    bool
	ObjectiveReachable bool
	GoalReachable      bool
	Edges              []MotionEdgeID
}

// RouteReport answers "does a route exist", stage by stage, on the authored
// directed graph only. It does not look at unplanned geometry and it does not
// say whether a fall is a trap; those are LockReport and SoftlockReport.
type RouteReport struct {
	Stages []StageRoute
	Exists bool
}

// GrantFinding is one mandatory ability. Obtainable means the grant room is
// reachable with the moveset from before the grant. OnceHeld means it becomes
// reachable if the ability is pretended already owned, which is the signature
// of a key sitting behind its own door.
type GrantFinding struct {
	Step       int
	Ability    Ability
	Room       RoomID
	Obtainable bool
	OnceHeld   bool
}

// GrantReport answers "is every mandatory ability obtainable before the gate
// that requires it". It is not the route report.
type GrantReport struct {
	Grants     []GrantFinding
	Obtainable bool
}

// LockFinding is one region that was supposed to stay shut. Leaked lists the
// rooms of that region reachable without Ability. ViaGeometric is set when a
// leak uses a boundary the authored transitions do not cover. AuthoredLeak is
// set when the authored graph itself already enters the region.
type LockFinding struct {
	Step         int
	Ability      Ability
	Gate         TransitionID
	Rooms        []RoomID
	Leaked       []RoomID
	ViaGeometric bool
	AuthoredLeak bool
	Held         bool
}

// LockReport answers "does the lock actually hold", including a walk or a
// fall through air that no transition claims.
type LockReport struct {
	Locks []LockFinding
	Held  bool
}

// HazardFinding is one fall or other one-way edge the character can reach.
// CanWin and CanReturn are independent of whether some other route reaches
// the goal: a pit off to the side is a softlock even when the goal is open.
type HazardFinding struct {
	Edge      MotionEdgeID
	From      MotionNodeID
	To        MotionNodeID
	Kind      MotionEdgeKind
	Mandatory bool
	CanWin    bool
	CanReturn bool
}

// Trapped reports that the landing can neither reach the goal nor get back
// to a state that was reachable without taking the hazard.
func (h HazardFinding) Trapped() bool { return !h.CanWin && !h.CanReturn }

// SoftlockReport answers "can every fall and every one-way still be survived".
// Clear is not RouteReport.Exists.
type SoftlockReport struct {
	Hazards []HazardFinding
	Clear   bool
}

// MacroAudit is the four reports, kept apart on purpose.
type MacroAudit struct {
	Route     RouteReport
	Grants    GrantReport
	Locks     LockReport
	Softlocks SoftlockReport
}

// AuditMacro runs the four checks. A failure of one does not suppress the others.
func AuditMacro(ctx context.Context, macro Macro) (MacroAudit, error) {
	var zero MacroAudit
	if len(macro.Graph.Nodes) != len(macro.Plane.Rooms) {
		return zero, queryError("macro graph has %d nodes for %d rooms", len(macro.Graph.Nodes), len(macro.Plane.Rooms))
	}
	route, err := AuditRoute(ctx, &macro.Graph, macro.Plane.Spawn.Room, macro.Plane.Goal.Room, macro.Plan, macro.Grants)
	if err != nil {
		return zero, err
	}
	grants, err := AuditGrants(ctx, &macro.Graph, macro.Plane.Spawn.Room, macro.Plan, macro.Grants)
	if err != nil {
		return zero, err
	}
	locks, err := AuditLocks(ctx, macro.Plane, &macro.Graph, macro.Plane.Spawn.Room, macro.Plan, macro.Locks, macro.Profile)
	if err != nil {
		return zero, err
	}
	soft, err := AuditSoftlocks(ctx, &macro.Graph, macro.Plane.Spawn.Room, macro.Plane.Goal.Room, macro.Plan)
	if err != nil {
		return zero, err
	}
	return MacroAudit{Route: route, Grants: grants, Locks: locks, Softlocks: soft}, nil
}

type edgeRef struct {
	to         int
	id         int
	requires   AbilitySet
	kind       MotionEdgeKind
	transition TransitionID
	passage    PassageID
}

func graphAdj(g *JumpGraph, extra []MotionEdge) [][]edgeRef {
	n := 0
	if g != nil {
		n = len(g.Nodes)
	}
	for _, e := range extra {
		if int(e.From)+1 > n {
			n = int(e.From) + 1
		}
		if int(e.To)+1 > n {
			n = int(e.To) + 1
		}
	}
	adj := make([][]edgeRef, n)
	add := func(e MotionEdge) {
		if int(e.From) >= len(adj) || int(e.To) >= len(adj) {
			return
		}
		adj[e.From] = append(adj[e.From], edgeRef{
			to: int(e.To), id: int(e.ID), requires: e.Requires, kind: e.Kind,
			transition: e.Transition, passage: e.Passage,
		})
	}
	if g != nil {
		for _, e := range g.Edges {
			add(e)
		}
	}
	for _, e := range extra {
		add(e)
	}
	return adj
}

func bfs(adj [][]edgeRef, start int, allow func(edgeRef) bool) (reached []bool, parent []int, via []int) {
	n := len(adj)
	reached = make([]bool, n)
	parent = make([]int, n)
	via = make([]int, n)
	for i := range parent {
		parent[i] = -1
		via[i] = -1
	}
	if start < 0 || start >= n {
		return reached, parent, via
	}
	reached[start] = true
	parent[start] = start
	queue := []int{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range adj[cur] {
			if reached[e.to] || (allow != nil && !allow(e)) {
				continue
			}
			reached[e.to] = true
			parent[e.to] = cur
			via[e.to] = e.id
			queue = append(queue, e.to)
		}
	}
	return reached, parent, via
}

func authored(moveset AbilitySet) func(edgeRef) bool {
	return func(e edgeRef) bool {
		if e.kind != MotionEdgeKindTransition && e.kind != MotionEdgeKindFall {
			return false
		}
		return moveset.Contains(e.requires)
	}
}

func usable(moveset AbilitySet) func(edgeRef) bool {
	return func(e edgeRef) bool { return moveset.Contains(e.requires) }
}

func skipEdge(id int, allow func(edgeRef) bool) func(edgeRef) bool {
	return func(e edgeRef) bool {
		if e.id == id {
			return false
		}
		return allow(e)
	}
}

func pathEdges(parent, via []int, start, target int) []MotionEdgeID {
	if parent[target] < 0 {
		return nil
	}
	var rev []MotionEdgeID
	cur := target
	guard := 0
	for cur != start {
		if via[cur] < 0 || parent[cur] < 0 {
			return nil
		}
		rev = append(rev, MotionEdgeID(via[cur]))
		cur = parent[cur]
		guard++
		if guard > len(parent) {
			return nil
		}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// AuditRoute checks the authored graph under each stage's moveset. Exists is
// true only when every stage reaches its own objective: the next grant, then
// the goal.
func AuditRoute(ctx context.Context, graph *JumpGraph, spawn, goal RoomID, plan ProgressionPlan, grants []AbilityGrant) (RouteReport, error) {
	var zero RouteReport
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if graph == nil {
		return zero, queryError("route audit requires a graph")
	}
	if err := checkGrants(plan, grants); err != nil {
		return zero, err
	}
	if int(spawn) >= len(graph.Nodes) || int(goal) >= len(graph.Nodes) {
		return zero, queryError("spawn %d or goal %d is outside the graph", spawn, goal)
	}
	adj := graphAdj(graph, nil)
	stages := plan.Stages()
	report := RouteReport{Stages: make([]StageRoute, len(stages)), Exists: true}
	for i, moveset := range stages {
		objective := goal
		isGoal := true
		if i < len(grants) {
			objective = grants[i].Room
			isGoal = false
		}
		if int(objective) >= len(graph.Nodes) {
			return zero, queryError("stage %d objective room %d is outside the graph", i, objective)
		}
		reached, parent, via := bfs(adj, int(spawn), authored(moveset))
		stage := StageRoute{
			Index:              i,
			Abilities:          moveset,
			Objective:          objective,
			ObjectiveIsGoal:    isGoal,
			ObjectiveReachable: reached[objective],
			GoalReachable:      reached[goal],
		}
		if stage.ObjectiveReachable {
			stage.Edges = pathEdges(parent, via, int(spawn), int(objective))
		} else {
			report.Exists = false
		}
		report.Stages[i] = stage
	}
	return report, nil
}

func checkGrants(plan ProgressionPlan, grants []AbilityGrant) error {
	if len(grants) != len(plan.Steps) {
		return progressionError("plan has %d steps and %d grants", len(plan.Steps), len(grants))
	}
	for i, grant := range grants {
		if grant.Step != i {
			return progressionError("grant %d names step %d", i, grant.Step)
		}
		if grant.Grants != plan.Steps[i].Grants {
			return progressionError("grant %d does not match step %d", i, i)
		}
	}
	return nil
}

// AuditGrants checks each mandatory ability against the moveset from before
// its step. It does not ask whether the goal is reachable.
func AuditGrants(ctx context.Context, graph *JumpGraph, spawn RoomID, plan ProgressionPlan, grants []AbilityGrant) (GrantReport, error) {
	var zero GrantReport
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if graph == nil {
		return zero, queryError("grant audit requires a graph")
	}
	if err := checkGrants(plan, grants); err != nil {
		return zero, err
	}
	if int(spawn) >= len(graph.Nodes) {
		return zero, queryError("spawn %d is outside the graph", spawn)
	}
	adj := graphAdj(graph, nil)
	stages := plan.Stages()
	report := GrantReport{Obtainable: true}
	for _, grant := range grants {
		if grant.Step >= len(stages) || int(grant.Room) >= len(graph.Nodes) {
			return zero, progressionError("grant step %d room %d is outside the plan or the graph", grant.Step, grant.Room)
		}
		prior := stages[grant.Step]
		reached, _, _ := bfs(adj, int(spawn), authored(prior))
		held := prior.Union(grant.Grants)
		once, _, _ := bfs(adj, int(spawn), authored(held))
		for _, ability := range grant.Grants.Abilities() {
			finding := GrantFinding{
				Step:       grant.Step,
				Ability:    ability,
				Room:       grant.Room,
				Obtainable: reached[grant.Room],
				OnceHeld:   once[grant.Room],
			}
			if !finding.Obtainable {
				report.Obtainable = false
			}
			report.Grants = append(report.Grants, finding)
		}
	}
	return report, nil
}

// AuditLocks floods from the spawn without each lock's ability, once on the
// authored graph and once with geometric bypasses added. A region that either
// flood enters is not held.
func AuditLocks(ctx context.Context, plane Plane, graph *JumpGraph, spawn RoomID, plan ProgressionPlan, locks []RegionLock, profile MovementProfile) (LockReport, error) {
	var zero LockReport
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if graph == nil {
		return zero, queryError("lock audit requires a graph")
	}
	if int(spawn) >= len(graph.Nodes) {
		return zero, queryError("spawn %d is outside the graph", spawn)
	}
	bypasses, err := GeometricBypasses(plane, profile)
	if err != nil {
		return zero, err
	}
	final := plan.Final()
	authoredAdj := graphAdj(graph, nil)
	combined := graphAdj(graph, bypasses)
	report := LockReport{Locks: make([]LockFinding, len(locks)), Held: true}
	for i, lock := range locks {
		if !lock.Ability.IsKnown() {
			return zero, progressionError("lock %d requires %s, which is not a declared ability", i, lock.Ability)
		}
		without := final.Without(lock.Ability)
		sealed, _, _ := bfs(authoredAdj, int(spawn), usable(without))
		opened, _, _ := bfs(combined, int(spawn), usable(without))
		finding := LockFinding{
			Step: lock.Step, Ability: lock.Ability, Gate: lock.Gate, Rooms: lock.Rooms, Held: true,
		}
		if len(lock.Rooms) == 0 {
			finding.Held = false
		}
		for _, room := range lock.Rooms {
			if int(room) >= len(opened) {
				return zero, queryError("lock %d names room %d, which is outside the graph", i, room)
			}
			if !opened[room] {
				continue
			}
			finding.Leaked = append(finding.Leaked, room)
			finding.Held = false
			if sealed[room] {
				finding.AuthoredLeak = true
			} else {
				finding.ViaGeometric = true
			}
		}
		if !finding.Held {
			report.Held = false
		}
		report.Locks[i] = finding
	}
	return report, nil
}

// AuditSoftlocks walks every fall and every one-way edge that a stage can
// reach, and asks whether the landing can still win or get back. The earliest
// stage that can take the edge is the one that counts: a later ability does
// not forgive a pit the character can already fall into.
func AuditSoftlocks(ctx context.Context, graph *JumpGraph, spawn, goal RoomID, plan ProgressionPlan) (SoftlockReport, error) {
	var zero SoftlockReport
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if graph == nil {
		return zero, queryError("softlock audit requires a graph")
	}
	if int(spawn) >= len(graph.Nodes) || int(goal) >= len(graph.Nodes) {
		return zero, queryError("spawn %d or goal %d is outside the graph", spawn, goal)
	}
	adj := graphAdj(graph, nil)
	stages := plan.Stages()
	report := SoftlockReport{Clear: true}
	for _, edge := range graph.Edges {
		if !isHazard(graph, edge) {
			continue
		}
		stage, reached, ok := stageThatReaches(adj, stages, int(spawn), edge)
		if !ok {
			continue
		}
		allow := authored(stage)
		without, _, _ := bfs(adj, int(spawn), skipEdge(int(edge.ID), allow))
		fromLanding, _, _ := bfs(adj, int(edge.To), allow)
		finding := HazardFinding{
			Edge:      edge.ID,
			From:      edge.From,
			To:        edge.To,
			Kind:      edge.Kind,
			Mandatory: reached[goal] && !without[goal],
			CanWin:    fromLanding[goal],
		}
		for i := range fromLanding {
			if fromLanding[i] && without[i] {
				finding.CanReturn = true
				break
			}
		}
		if finding.Trapped() {
			report.Clear = false
		}
		report.Hazards = append(report.Hazards, finding)
	}
	return report, nil
}

func stageThatReaches(adj [][]edgeRef, stages []AbilitySet, spawn int, edge MotionEdge) (AbilitySet, []bool, bool) {
	for _, stage := range stages {
		if !stage.Contains(edge.Requires) {
			continue
		}
		reached, _, _ := bfs(adj, spawn, authored(stage))
		if reached[edge.From] {
			return stage, reached, true
		}
	}
	return 0, nil, false
}

func isHazard(g *JumpGraph, e MotionEdge) bool {
	if e.Kind == MotionEdgeKindFall {
		return true
	}
	if e.Passage == 0 {
		return !hasReverse(g, e)
	}
	return len(g.Passage(e.Passage)) < 2
}

func hasReverse(g *JumpGraph, e MotionEdge) bool {
	for _, other := range g.Edges {
		if other.From == e.To && other.To == e.From {
			return true
		}
	}
	return false
}

type bypassRun struct {
	from     RoomID
	neighbor RoomID
	side     TransitionSide
	along    int32
	length   int32
}

// GeometricBypasses returns directed edges for air on a shared wall that no
// transition covers and that the body can pass. A vertical run is a walk in
// both senses. A run along a floor is a fall downward only: climbing back
// through an unplanned hole is not free. The edges are not part of the
// authored graph; the lock audit adds them.
func GeometricBypasses(plane Plane, profile MovementProfile) ([]MotionEdge, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	index, err := transitionIndex(plane)
	if err != nil {
		return nil, err
	}
	var runs []bypassRun
	for i := range plane.Rooms {
		room := plane.Rooms[i]
		scanBoundary(&runs, plane, room, index, TransitionSideLeft)
		scanBoundary(&runs, plane, room, index, TransitionSideRight)
		scanBoundary(&runs, plane, room, index, TransitionSideBottom)
	}
	var edges []MotionEdge
	for _, run := range runs {
		if !runUsable(run.side, run.length, profile) {
			continue
		}
		if run.side.IsVertical() {
			if run.from >= run.neighbor {
				continue
			}
			edges = append(edges, MotionEdge{From: MotionNodeID(run.from), To: MotionNodeID(run.neighbor), Kind: MotionEdgeKindWalk})
			edges = append(edges, MotionEdge{From: MotionNodeID(run.neighbor), To: MotionNodeID(run.from), Kind: MotionEdgeKindWalk})
			continue
		}
		edges = append(edges, MotionEdge{From: MotionNodeID(run.from), To: MotionNodeID(run.neighbor), Kind: MotionEdgeKindFall})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
	for i := range edges {
		edges[i].ID = MotionEdgeID(i)
	}
	return edges, nil
}

func runUsable(side TransitionSide, length int32, profile MovementProfile) bool {
	if side.IsVertical() {
		return float64(length) >= profile.BodyHeight
	}
	return float64(length) >= 2*profile.BodyHalfWidth
}

func scanBoundary(dst *[]bypassRun, plane Plane, room Room, index map[TransitionID]Transition, side TransitionSide) {
	length := int32(room.Grid.Height)
	if !side.IsVertical() {
		length = int32(room.Grid.Width)
	}
	var cur *bypassRun
	flush := func() {
		if cur != nil && cur.length > 0 {
			*dst = append(*dst, *cur)
		}
		cur = nil
	}
	for i := int32(0); i < length; i++ {
		lx, ly, nx, ny, along := borderPoint(room, side, i)
		kind, ok := room.Grid.At(Cell{X: lx, Y: ly})
		if !ok || kind != CellKindEmpty {
			flush()
			continue
		}
		neighbor, local, ok := roomAt(plane, nx, ny)
		if !ok || neighbor == room.ID {
			flush()
			continue
		}
		nk, nok := plane.Rooms[neighbor].Grid.At(local)
		if !nok || nk != CellKindEmpty {
			flush()
			continue
		}
		if boundaryCovered(room, side, neighbor, along, index) {
			flush()
			continue
		}
		if cur != nil && cur.neighbor == neighbor && cur.along+cur.length == along {
			cur.length++
			continue
		}
		flush()
		cur = &bypassRun{from: room.ID, neighbor: neighbor, side: side, along: along, length: 1}
	}
	flush()
}

func borderPoint(room Room, side TransitionSide, i int32) (lx, ly, nx, ny, along int32) {
	switch side {
	case TransitionSideLeft:
		return 0, i, room.Origin.X - 1, room.Origin.Y + i, room.Origin.Y + i
	case TransitionSideRight:
		return int32(room.Grid.Width) - 1, i, room.Origin.X + int32(room.Grid.Width), room.Origin.Y + i, room.Origin.Y + i
	case TransitionSideTop:
		return i, 0, room.Origin.X + i, room.Origin.Y - 1, room.Origin.X + i
	default:
		return i, int32(room.Grid.Height) - 1, room.Origin.X + i, room.Origin.Y + int32(room.Grid.Height), room.Origin.X + i
	}
}

func boundaryCovered(room Room, side TransitionSide, neighbor RoomID, along int32, index map[TransitionID]Transition) bool {
	for _, t := range room.Transitions {
		if t.Side != side {
			continue
		}
		partner, ok := index[t.To]
		if !ok || partner.Room != neighbor {
			continue
		}
		sp, ok := openingSpan(room, t)
		if !ok {
			continue
		}
		if along >= sp.start && along < sp.start+sp.extent {
			return true
		}
	}
	return false
}

func roomAt(plane Plane, x, y int32) (RoomID, Cell, bool) {
	if x < 0 || y < 0 || uint32(x) >= plane.Width || uint32(y) >= plane.Height {
		return 0, Cell{}, false
	}
	for i := range plane.Rooms {
		room := plane.Rooms[i]
		lx := x - room.Origin.X
		ly := y - room.Origin.Y
		if lx < 0 || ly < 0 || uint32(lx) >= room.Grid.Width || uint32(ly) >= room.Grid.Height {
			continue
		}
		return room.ID, Cell{X: lx, Y: ly}, true
	}
	return 0, Cell{}, false
}

func transitionIndex(plane Plane) (map[TransitionID]Transition, error) {
	index := make(map[TransitionID]Transition)
	for _, room := range plane.Rooms {
		for _, t := range room.Transitions {
			if _, ok := index[t.ID]; ok {
				return nil, geometryError("transition %d is listed twice", t.ID)
			}
			index[t.ID] = t
		}
	}
	return index, nil
}

func bindProgression(ctx context.Context, plane Plane, asm assembly, steps []ProgressionStep, profile MovementProfile) (Macro, error) {
	var zero Macro
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := profile.Validate(); err != nil {
		return zero, err
	}
	if int(asm.goal) >= len(plane.Rooms) {
		return zero, geometryError("goal room %d is outside the plane", asm.goal)
	}
	plane.Goal = Anchor{Room: asm.goal, At: standingCell(plane.Rooms[asm.goal])}
	plan := ProgressionPlan{Steps: append([]ProgressionStep(nil), steps...)}
	grants, err := placeGrants(plane, plan)
	if err != nil {
		return zero, err
	}
	locks, err := lockRegions(plane, asm)
	if err != nil {
		return zero, err
	}
	graph, err := macroGraph(plane, profile, plan.Final())
	if err != nil {
		return zero, err
	}
	return Macro{
		Plane: plane, Plan: plan, Grants: grants, Locks: locks, Graph: graph, Profile: profile,
	}, nil
}

func placeGrants(plane Plane, plan ProgressionPlan) ([]AbilityGrant, error) {
	if len(plan.Steps) == 0 {
		return nil, nil
	}
	adj, err := directedLinks(plane)
	if err != nil {
		return nil, err
	}
	stages := plan.Stages()
	used := make([]bool, len(plane.Rooms))
	grants := make([]AbilityGrant, len(plan.Steps))
	spawn := int(plane.Spawn.Room)
	for step := range plan.Steps {
		moveset := stages[step]
		reached, dist := linkFlood(adj, spawn, moveset)
		best := -1
		for room := range reached {
			if !reached[room] {
				continue
			}
			if best >= 0 && room == spawn {
				continue
			}
			if best < 0 || dist[room] > dist[best] || (dist[room] == dist[best] && !used[room] && used[best]) || (dist[room] == dist[best] && used[room] == used[best] && room < best) {
				if room == spawn && best >= 0 {
					continue
				}
				best = room
			}
		}
		if best < 0 {
			return nil, progressionError("step %d has nowhere to put its grant", step)
		}
		// Prefer an unused room at the same distance. The comparison above
		// already biases away from used rooms when distances tie. If the
		// farthest is used and some nearer room is free, keep the farthest:
		// two grants may share a room only when nothing else is reachable.
		if used[best] {
			alt := -1
			for room := range reached {
				if !reached[room] || used[room] || room == spawn {
					continue
				}
				if alt < 0 || dist[room] > dist[alt] || (dist[room] == dist[alt] && room < alt) {
					alt = room
				}
			}
			if alt >= 0 {
				best = alt
			}
		}
		used[best] = true
		grants[step] = AbilityGrant{Step: step, Room: RoomID(best), Grants: plan.Steps[step].Grants}
	}
	return grants, nil
}

type roomLink struct {
	to       int
	requires AbilitySet
}

func directedLinks(plane Plane) ([][]roomLink, error) {
	index, err := transitionIndex(plane)
	if err != nil {
		return nil, err
	}
	adj := make([][]roomLink, len(plane.Rooms))
	for ri := range plane.Rooms {
		for _, t := range plane.Rooms[ri].Transitions {
			if t.Outbound == nil {
				continue
			}
			partner, ok := index[t.To]
			if !ok {
				return nil, geometryError("transition %d pairs with %d, which is missing", t.ID, t.To)
			}
			adj[ri] = append(adj[ri], roomLink{to: int(partner.Room), requires: t.Outbound.Requires})
		}
	}
	return adj, nil
}

func linkFlood(adj [][]roomLink, start int, moveset AbilitySet) ([]bool, []int) {
	n := len(adj)
	reached := make([]bool, n)
	dist := make([]int, n)
	if start < 0 || start >= n {
		return reached, dist
	}
	reached[start] = true
	queue := []int{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, link := range adj[cur] {
			if reached[link.to] || !moveset.Contains(link.requires) {
				continue
			}
			reached[link.to] = true
			dist[link.to] = dist[cur] + 1
			queue = append(queue, link.to)
		}
	}
	return reached, dist
}

func lockRegions(plane Plane, asm assembly) ([]RegionLock, error) {
	var locks []RegionLock
	for _, gate := range asm.gates {
		rooms, err := beyond(plane, gate.pair, gate.forward, plane.Spawn.Room)
		if err != nil {
			return nil, err
		}
		if len(rooms) == 0 || containsRoom(rooms, plane.Spawn.Room) {
			return nil, progressionError("gate between %d and %d does not separate a region from the spawn", gate.pair.lo, gate.pair.hi)
		}
		for _, ability := range gate.requires.Abilities() {
			copied := append([]RoomID(nil), rooms...)
			locks = append(locks, RegionLock{
				Step: gate.step, Ability: ability, Gate: gate.gate, Rooms: copied,
			})
		}
	}
	return locks, nil
}

func beyond(plane Plane, block roomPair, forward, spawn RoomID) ([]RoomID, error) {
	index, err := transitionIndex(plane)
	if err != nil {
		return nil, err
	}
	n := len(plane.Rooms)
	adj := make([][]int, n)
	for ri := range plane.Rooms {
		for _, t := range plane.Rooms[ri].Transitions {
			partner, ok := index[t.To]
			if !ok || partner.Room == RoomID(ri) {
				continue
			}
			if orderedPair(RoomID(ri), partner.Room) == block {
				continue
			}
			if partner.Room > RoomID(ri) {
				adj[ri] = append(adj[ri], int(partner.Room))
				adj[partner.Room] = append(adj[partner.Room], ri)
			}
		}
	}
	for i := range adj {
		sort.Ints(adj[i])
	}
	if int(forward) >= n {
		return nil, geometryError("gate forward room %d is outside the plane", forward)
	}
	seen := make([]bool, n)
	var rooms []RoomID
	queue := []int{int(forward)}
	seen[forward] = true
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		rooms = append(rooms, RoomID(cur))
		for _, nxt := range adj[cur] {
			if seen[nxt] {
				continue
			}
			seen[nxt] = true
			queue = append(queue, nxt)
		}
	}
	sort.Slice(rooms, func(i, j int) bool { return rooms[i] < rooms[j] })
	if containsRoom(rooms, spawn) {
		return nil, progressionError("closing the gate still leaves the spawn inside the locked region")
	}
	return rooms, nil
}

func containsRoom(rooms []RoomID, id RoomID) bool {
	for _, room := range rooms {
		if room == id {
			return true
		}
	}
	return false
}

func macroGraph(plane Plane, profile MovementProfile, abilities AbilitySet) (JumpGraph, error) {
	index, err := transitionIndex(plane)
	if err != nil {
		return JumpGraph{}, err
	}
	full := profile.FullResources()
	nodes := make([]MotionNode, len(plane.Rooms))
	for i, room := range plane.Rooms {
		nodes[i] = MotionNode{
			ID:        MotionNodeID(i),
			Height:    1,
			Footing:   Point(float64(room.Grid.Width) / 2),
			Velocity:  Span{Lo: 0, Hi: 0},
			Mode:      MotionModeGrounded,
			Resources: full,
		}
		if !nodes[i].IsRest(profile) {
			return JumpGraph{}, geometryError("room %d did not become a rest node", i)
		}
	}
	count := 0
	for _, room := range plane.Rooms {
		count += len(room.Transitions)
	}
	passage := make([]PassageID, count)
	nextPassage := PassageID(1)
	for id := 0; id < count; id++ {
		if passage[id] != 0 {
			continue
		}
		t, ok := index[TransitionID(id)]
		if !ok {
			return JumpGraph{}, geometryError("transition ids skip %d", id)
		}
		if int(t.To) >= count {
			return JumpGraph{}, geometryError("transition %d pairs outside the id range", t.ID)
		}
		passage[id] = nextPassage
		passage[t.To] = nextPassage
		nextPassage++
	}
	var edges []MotionEdge
	for ri := range plane.Rooms {
		for _, t := range plane.Rooms[ri].Transitions {
			if t.Outbound == nil {
				continue
			}
			partner, ok := index[t.To]
			if !ok {
				return JumpGraph{}, geometryError("transition %d has no partner", t.ID)
			}
			kind := MotionEdgeKindTransition
			if t.Inbound == nil && t.Side == TransitionSideBottom {
				kind = MotionEdgeKindFall
			}
			edges = append(edges, MotionEdge{
				ID:         MotionEdgeID(len(edges)),
				From:       MotionNodeID(ri),
				To:         MotionNodeID(partner.Room),
				Kind:       kind,
				Requires:   t.Outbound.Requires,
				Passage:    passage[t.ID],
				Transition: t.ID,
			})
		}
	}
	return JumpGraph{
		Model:          "daedalus/platform/macro",
		ProfileVersion: profile.Version,
		Abilities:      abilities,
		Discipline:     NodeDisciplineRestOnly,
		Nodes:          nodes,
		Edges:          edges,
	}, nil
}
