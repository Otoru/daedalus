package platform

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strconv"

	"github.com/Otoru/daedalus/core"
)

// Synthesis is the pass that stitches the other fronts together: the macro
// front places rooms and binds the progression, the rhythm front turns a
// spine into per-beat geometry, placement stamps that geometry into the
// rooms, and the oracle is asked for a verdict about the result. Nothing
// here computes motion; everything here composes answers.
//
// # What the verdict means
//
// The per-beat certificates the rhythm front collected do not add up to a
// certificate about the map. Each one was issued against the beat's own local
// grid, where there is no ceiling, no side wall and no neighbouring platform.
// Once the beats share a room, two steps that were individually certified may
// no longer compose — which is point 3 of the plan's §1 and the reason this
// pass exists at all. So synthesis asks the oracle twice more per room: once
// to build the room's directed jump graph over the FINAL geometry, and once
// for a route across the run that was stamped into it.
//
// A layout is Certified when every room's graph was built exhaustively within
// the model and every stamped run is crossable end to end. It is Unknown when
// any of those searches ran out of budget, and the partial graph comes back
// with it. It is Rejected when a search completed and the answer was no.
//
// # Error against verdict
//
// The line is the contract's, in errors.go, and this pass does not move it. A
// malformed question — an invalid Config, a profile the plane cannot hold, a
// cancelled context, a product limit — is an error, returned before anything
// is drawn. A map that was drawn and then refused is a verdict, returned with
// the map. In particular a budget that ran out is never an error and never a
// rejection; it is VerdictUnknown with the BudgetReport that says so.
//
// # Retry
//
// A rejection is about one draw, not about the request, so synthesis re-seeds
// and draws again, up to synthMaxAttempts times. A budget that ran out is NOT
// a retry reason: the next attempt would run under the same ceilings and the
// honest answer is already available. When no attempt is certified the last
// attempt's layout is returned as it is — Rejected, with the counterexample
// still in it — because the request did produce a map and this pass knows
// exactly what is wrong with it. Reporting Unknown there would hide a
// rejection that was actually decided.

const (
	// synthMaxAttempts is how many times a request is drawn before synthesis
	// gives up and returns the last rejection. It is a count rather than a
	// deadline for the same reason SearchBudget is: a wall clock would make
	// the output depend on the machine.
	synthMaxAttempts = 4

	// synthBeatsPerRoom is how many of the widest beats in the vocabulary the
	// LARGEST room has to hold. Two is the smallest number with a composition
	// to check at all: one beat was already certified by the rhythm front
	// against the same two platforms, and it takes a second step for two
	// individually possible jumps to fail to compose. Rooms are drawn between
	// one beat wide and this many, so most hold more than two of the narrower
	// beats actually drawn.
	synthBeatsPerRoom uint32 = 2

	// synthMinRoomSlack is how many rooms a map carries beyond one per
	// progression step: a spawn room and a goal room that are not gates.
	synthMinRoomSlack uint32 = 2

	// synthFootHeightTolerance is how far, in cells, a node's foot height may
	// sit from a stamped platform's and still be that platform's node. It is
	// below half a cell because feet heights are integers in this frame.
	synthFootHeightTolerance = 0.25
)

// PlatformLayout is one platform-generation request's whole answer: the map,
// the directed graph derived from it, and the three-valued verdict about it.
//
// The first four fields are the hand-off shape the wire front already
// exports, in the same order, so that converting one to the other stays a
// copy of four fields rather than a translation. Everything after them is
// synthesis provenance: what was asked, what was drawn, and what each room
// cost.
//
// # Two graphs, deliberately not merged
//
// JumpGraph is the oracle's: one directed graph over every room's geometry,
// every edge certified and carrying a witness. It contains NO transition
// edge. RoomGraph is the macro front's: one rest node per room and one edge
// per traversable sense of a transition, authored rather than certified, with
// no witness anywhere.
//
// They are kept apart on purpose. Merging them would put an unwitnessed edge
// inside a graph whose judgement says Certified, and the contract is explicit
// that a certified edge without a witness is not certified. Crossing a room
// boundary needs both rooms and the pairing of their openings, which the
// oracle does not model; until something certifies that crossing, saying so
// is better than hiding it behind an edge that looks like the others.
type PlatformLayout struct {
	// Config is the request, after Normalize.
	Config Config
	// Plane is the generated map: rooms at explicit origins, with the beat
	// geometry stamped into them.
	Plane Plane
	// JumpGraph is every room's certified jump graph, merged into one. IDs
	// are renumbered; RoomSynthesis records each room's range.
	JumpGraph JumpGraph
	// Judgement is the verdict about the whole layout.
	Judgement Judgement

	// RoomGraph is the macro front's room-level directed graph. It is not a
	// certificate and carries no witness.
	RoomGraph JumpGraph
	// Plan is the progression the map was built around.
	Plan ProgressionPlan
	// Grants lists where each ability is obtained.
	Grants []AbilityGrant
	// Locks lists the regions each ability is supposed to open.
	Locks []RegionLock
	// Audit is the macro front's four reports, re-run against the FINAL
	// geometry rather than the shells the macro front audited.
	Audit MacroAudit
	// Rooms is one entry per room, in ascending RoomID order.
	Rooms []RoomSynthesis
	// Attempts is how many draws it took. It is at least one.
	Attempts int
	// AttemptSeeds lists the seed of each attempt, in order, so a rejected
	// draw can be replayed on its own.
	AttemptSeeds []Seed
}

// RoomSynthesis is what synthesis produced for one room: the moveset it was
// authored for, the rhythm it was given, where the beats landed, and where
// the room's part of the merged graph begins.
type RoomSynthesis struct {
	// Room is the room this entry describes.
	Room RoomID
	// Abilities is the moveset the room's geometry was authored under: the
	// progression stage at which the room first becomes reachable, which is
	// the weakest moveset that has to be able to cross it.
	Abilities AbilitySet
	// Rhythm is the spine and the realized beats the rhythm front produced.
	Rhythm Rhythm
	// Placements lists the beats that were stamped, in run order.
	Placements []BeatPlacement
	// Dropped is how many beats of the run did not fit and were not stamped.
	// They are dropped rather than clipped: a clipped beat is geometry the
	// oracle never saw.
	Dropped int
	// SurfaceBase, NodeBase and EdgeBase are where this room's surfaces,
	// nodes and edges start inside PlatformLayout.JumpGraph, and the Count
	// fields are how many there are.
	SurfaceBase  SurfaceID
	SurfaceCount uint32
	NodeBase     MotionNodeID
	NodeCount    uint32
	EdgeBase     MotionEdgeID
	EdgeCount    uint32
	// Build is the judgement the graph build returned for this room.
	Build Judgement
	// Composition is the judgement for "the stamped run is crossable from its
	// first platform to its last". It is the certificate the per-beat answers
	// do not give. A run of fewer than two beats has nothing to compose and
	// is certified as trivial.
	Composition Judgement
	// Entry and Exit are the nodes the composition check ran between, in the
	// merged graph's numbering. They are meaningful only when a run was
	// stamped.
	Entry MotionNodeID
	Exit  MotionNodeID
}

// Generate draws a platform map for config and judges it with oracle.
//
// oracle is a parameter rather than a package default so that the caller
// decides whose certificate it is holding: M1Oracle produces ModelM1 and
// FakeOracle produces ModelFake, and a consumer that treats a certificate as
// binding checks Judgement.Model. A nil oracle is a malformed question, not a
// request for a default.
//
// The returned error is always about the question. A map that was drawn and
// refused comes back with a nil error and a Judgement that says why; so does
// a map whose analysis ran out of budget.
func Generate(ctx context.Context, oracle Oracle, config Config) (PlatformLayout, error) {
	var zero PlatformLayout
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if oracle == nil {
		return zero, queryError("Generate needs an oracle; nil is not a request for a default")
	}
	if err := config.Validate(); err != nil {
		return zero, err
	}
	config = config.Normalize()

	// The macro config is validated here, before the first draw, so that
	// every later failure of the macro front is about the draw and not about
	// the request. That split is what makes a retry legitimate: retrying a
	// plane too small to hold a room would just burn attempts. The seed
	// passed in is a placeholder — MacroConfig.Validate checks sizes, counts
	// and steps, none of which the seed can change, and the seed each attempt
	// really uses is derived from that attempt.
	if err := macroConfigFor(config, config.Seed).Validate(); err != nil {
		return zero, err
	}

	attempts := core.NewSplitMix64(config.Seed, synthAttemptSalt)
	seeds := make([]Seed, 0, synthMaxAttempts)
	var last PlatformLayout
	var decided bool
	var drawErr error

	for attempt := 0; attempt < synthMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		seed := Seed(attempts.Next())
		seeds = append(seeds, seed)

		layout, err := synthesizeOnce(ctx, oracle, config, seed)
		if err != nil {
			if !isDrawFailure(err) {
				return zero, err
			}
			// The macro front reports a map it refuses to return as an error
			// rather than as a verdict. That is a rejection of the draw, so
			// it costs an attempt and not the request. It does NOT discard a
			// rejected map an earlier attempt produced: a map this pass can
			// point at is a better answer than an error, so the error is kept
			// only for the case where no attempt ever got that far.
			drawErr = err
			continue
		}

		last, decided = layout, true
		if verdict := layout.Judgement.Verdict; verdict == VerdictCertified || verdict == VerdictUnknown {
			// Certified is the answer. Unknown is also the answer: a ceiling
			// decides nothing, and the next attempt would run under the same
			// ceiling and learn the same thing.
			last.Attempts = attempt + 1
			last.AttemptSeeds = seeds
			return last, nil
		}
	}

	if !decided {
		if drawErr != nil {
			return zero, drawErr
		}
		return zero, geometryError("no attempt produced a map")
	}
	// Every attempt was drawn, so the count is the ceiling even though the
	// layout being returned came from an earlier one.
	last.Attempts = synthMaxAttempts
	last.AttemptSeeds = seeds
	last.Judgement.Detail = describeExhaustedRetries(last.Judgement.Detail, synthMaxAttempts)
	return last, nil
}

// isDrawFailure reports whether err describes the map that was drawn rather
// than the request that asked for it. Only those are worth another draw.
//
// The macro front fails closed: a plane it cannot fill, or a map that fails
// one of its four audits, comes back as ErrInvalidGeometry or
// ErrInvalidProgression. Because Generate validates the macro config itself
// before the first draw, neither sentinel can still mean "you asked wrong" by
// the time it is seen here.
func isDrawFailure(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.Is(err, ErrInvalidGeometry) || errors.Is(err, ErrInvalidProgression)
}

func describeExhaustedRetries(detail string, attempts int) string {
	suffix := "; " + strconv.Itoa(attempts) + " attempts were drawn and every one was rejected"
	if detail == "" {
		return "no draw survived" + suffix
	}
	return detail + suffix
}

// synthesizeOnce draws and judges exactly one map.
func synthesizeOnce(ctx context.Context, oracle Oracle, config Config, seed Seed) (PlatformLayout, error) {
	var zero PlatformLayout
	streams := newSynthStreams(seed)

	macro, err := GenerateMacro(ctx, macroConfigFor(config, streams.macro))
	if err != nil {
		return zero, err
	}

	stages := roomStages(macro)
	rooms := make([]RoomSynthesis, len(macro.Plane.Rooms))
	graphs := make([]JumpGraph, len(macro.Plane.Rooms))

	for index := range macro.Plane.Rooms {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		room := &macro.Plane.Rooms[index]
		abilities := stages[index]

		roomConfig := config
		roomConfig.Seed = Seed(streams.rhythm.Next())
		rhythm, err := GenerateRhythm(ctx, oracle, roomConfig, abilities)
		if err != nil {
			return zero, err
		}

		placements, dropped, err := placeRhythm(room, rhythm, &streams.placement)
		if err != nil {
			return zero, err
		}

		rooms[index] = RoomSynthesis{
			Room:       room.ID,
			Abilities:  abilities,
			Rhythm:     rhythm,
			Placements: placements,
			Dropped:    dropped,
		}
	}

	// Openings and anchors were chosen on the empty shell. The stamp writes
	// the interior afterwards and can seal a mouth or bury an anchor. Seating
	// re-decides both against the stamped geometry. A seating rejection is a
	// verdict about the draw, applied after the oracle: a search that ran out
	// of budget is still Unknown, and is not overwritten by the seating.
	seatReason, seatDetail, seatRejected := seatShell(&macro.Plane, config.Profile, stages)

	// The stamp only ever writes a room's interior. Seating may slide an
	// opening along the border or cut a footing up to a mouth. Re-validating
	// says the plane still describes space.
	if err := ValidatePlane(macro.Plane); err != nil {
		return zero, err
	}

	for index := range macro.Plane.Rooms {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		room := macro.Plane.Rooms[index]
		result, err := oracle.BuildGraph(ctx, GraphQuery{
			Grid:        room.Grid,
			Room:        room.ID,
			Profile:     config.Profile,
			Abilities:   config.Progression.Final(),
			Discipline:  config.Discipline,
			Budget:      config.Budget,
			OmitWitness: config.OmitWitness,
		})
		if err != nil {
			return zero, err
		}
		graphs[index] = result.Graph
		rooms[index].Build = result.Judgement
	}

	merged, spans, err := mergeRoomGraphs(graphs, oracle.Model(), config)
	if err != nil {
		return zero, err
	}
	for index := range rooms {
		rooms[index].SurfaceBase = spans[index].surfaceBase
		rooms[index].SurfaceCount = spans[index].surfaceCount
		rooms[index].NodeBase = spans[index].nodeBase
		rooms[index].NodeCount = spans[index].nodeCount
		rooms[index].EdgeBase = spans[index].edgeBase
		rooms[index].EdgeCount = spans[index].edgeCount
	}

	// The composition check runs on each room's own graph, before the
	// renumbering, and the resulting node ids are shifted into the merged
	// graph's frame afterwards.
	for index := range rooms {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		composition, entry, exit, err := checkComposition(ctx, oracle, config, &graphs[index], rooms[index])
		if err != nil {
			return zero, err
		}
		rooms[index].Composition = composition
		rooms[index].Entry = entry + spans[index].nodeBase
		rooms[index].Exit = exit + spans[index].nodeBase
	}

	audit, err := AuditMacro(ctx, macro)
	if err != nil {
		return zero, err
	}

	layout := PlatformLayout{
		Config:    config,
		Plane:     macro.Plane,
		JumpGraph: merged,
		RoomGraph: macro.Graph,
		Plan:      macro.Plan,
		Grants:    macro.Grants,
		Locks:     macro.Locks,
		Audit:     audit,
		Rooms:     rooms,
	}
	layout.Judgement = judgeLayout(oracle.Model(), rooms, audit)
	if seatRejected && layout.Judgement.Verdict != VerdictUnknown {
		version := config.Profile.Version
		if version == "" {
			version = ProfileVersionM1
		}
		layout.Judgement.Verdict = VerdictRejected
		layout.Judgement.Reason = seatReason
		layout.Judgement.Model = oracle.Model()
		layout.Judgement.ProfileVersion = version
		layout.Judgement.Detail = seatDetail
	}
	if err := layout.Judgement.Validate(); err != nil {
		return zero, err
	}
	return layout, nil
}

// judgeLayout folds every room's two judgements and the macro audit into one.
//
// The order of the checks is the order of honesty. A ceiling that stopped a
// search means nothing after it was decided, so budget exhaustion wins over
// every rejection: claiming "no" on the strength of a search that never
// finished is the one failure this package exists to prevent.
func judgeLayout(model string, rooms []RoomSynthesis, audit MacroAudit) Judgement {
	judgement := Judgement{
		Verdict:        VerdictCertified,
		Reason:         ReasonWitnessFound,
		Model:          model,
		ProfileVersion: profileVersionOf(rooms),
	}
	for _, room := range rooms {
		judgement.Budget.CandidateEdges += room.Build.Budget.CandidateEdges + room.Composition.Budget.CandidateEdges
		judgement.Budget.ExpandedNodes += room.Build.Budget.ExpandedNodes + room.Composition.Budget.ExpandedNodes
		judgement.Budget.CollisionTests += room.Build.Budget.CollisionTests + room.Composition.Budget.CollisionTests
	}

	for _, room := range rooms {
		for _, part := range [2]Judgement{room.Build, room.Composition} {
			if part.Verdict != VerdictUnknown {
				continue
			}
			judgement.Verdict = VerdictUnknown
			judgement.Reason = part.Reason
			judgement.Detail = "room " + strconv.Itoa(int(room.Room)) + ": " + part.Detail
			judgement.Budget.Exhausted = judgement.Budget.Exhausted || part.Budget.Exhausted
			return judgement
		}
	}

	for _, room := range rooms {
		for _, part := range [2]Judgement{room.Build, room.Composition} {
			if part.Verdict != VerdictRejected {
				continue
			}
			judgement.Verdict = VerdictRejected
			judgement.Reason = part.Reason
			judgement.Detail = "room " + strconv.Itoa(int(room.Room)) + ": " + part.Detail
			return judgement
		}
	}

	// The macro front audits the shells it placed; this audit runs against
	// the geometry that was actually stamped into them. The frozen reason
	// vocabulary describes manoeuvres and has no word for "the authored gate
	// leaks", so every audit failure is reported as ReasonDisconnected — the
	// only rejection reason that is about the map rather than about one jump
	// — and Detail names which of the four reports failed. The reports
	// themselves are in PlatformLayout.Audit, so nothing is lost.
	if detail, ok := auditFailure(audit); ok {
		judgement.Verdict = VerdictRejected
		judgement.Reason = ReasonDisconnected
		judgement.Detail = detail
		return judgement
	}

	judgement.Detail = "every room's graph was built exhaustively and every stamped run is crossable"
	return judgement
}

func auditFailure(audit MacroAudit) (string, bool) {
	switch {
	case !audit.Route.Exists:
		return "the route report finds no path across the stage objectives", true
	case !audit.Grants.Obtainable:
		return "the grant report finds an ability behind the gate that requires it", true
	case !audit.Locks.Held:
		return "the lock report finds a region reachable without its ability", true
	case !audit.Softlocks.Clear:
		return "the softlock report finds a fall that cannot be recovered from", true
	}
	return "", false
}

func profileVersionOf(rooms []RoomSynthesis) string {
	for _, room := range rooms {
		if room.Build.ProfileVersion != "" {
			return room.Build.ProfileVersion
		}
	}
	return ProfileVersionM1
}

// checkComposition asks whether the run stamped into one room can be walked
// from its first platform to its last. It returns the judgement and the two
// node ids, in the room graph's own numbering.
//
// A run of fewer than two beats is certified as trivial: a single beat was
// already certified by the rhythm front against the same two platforms, and
// there is no second step for it to fail to compose with.
func checkComposition(ctx context.Context, finder RouteFinder, config Config, graph *JumpGraph, room RoomSynthesis) (Judgement, MotionNodeID, MotionNodeID, error) {
	trivial := Judgement{
		Verdict:        VerdictCertified,
		Reason:         ReasonTrivial,
		Model:          graph.Model,
		ProfileVersion: graph.ProfileVersion,
		Detail:         "the room holds fewer than two stamped beats, so there is nothing to compose",
	}
	if len(room.Placements) < 2 {
		return trivial, 0, 0, nil
	}

	first := room.Placements[0]
	last := room.Placements[len(room.Placements)-1]
	entry, okEntry := groundedNodeAt(graph, first.DepartureX, first.DepartureHeight)
	exit, okExit := groundedNodeAt(graph, last.ArrivalX, last.ArrivalHeight)
	if !okEntry || !okExit {
		// The platform was stamped and the oracle found nowhere to stand on
		// it: too narrow for the body, or with the ceiling too close. That is
		// a decided no about this geometry, not a gap in the search.
		which := "departure"
		if okEntry {
			which = "arrival"
		}
		return Judgement{
			Verdict:        VerdictRejected,
			Reason:         ReasonLandingUnsupported,
			Model:          graph.Model,
			ProfileVersion: graph.ProfileVersion,
			Detail:         "the run's " + which + " platform carries no standing node",
		}, 0, 0, nil
	}
	if entry == exit {
		return trivial, entry, exit, nil
	}

	result, err := finder.FindRoute(ctx, RouteQuery{
		Graph:     graph,
		From:      entry,
		To:        exit,
		Abilities: room.Abilities,
		Budget:    config.Budget,
	})
	if err != nil {
		return Judgement{}, 0, 0, err
	}
	return result.Judgement, entry, exit, nil
}

// groundedNodeAt finds the standing node of the platform at (x, height). It
// prefers the node whose footing is nearest x, and breaks a tie by the lowest
// id, so the choice is a function of the graph and not of iteration order.
func groundedNodeAt(graph *JumpGraph, x, height float64) (MotionNodeID, bool) {
	var best MotionNodeID
	var bestDistance float64
	found := false
	for _, node := range graph.Nodes {
		if node.Mode != MotionModeGrounded || node.Footing.IsEmpty() {
			continue
		}
		if math.Abs(node.Height-height) > synthFootHeightTolerance {
			continue
		}
		distance := distanceToSpan(node.Footing, x)
		if !found || distance < bestDistance {
			best, bestDistance, found = node.ID, distance, true
		}
	}
	return best, found
}

func distanceToSpan(span Span, x float64) float64 {
	switch {
	case x < span.Lo:
		return span.Lo - x
	case x > span.Hi:
		return x - span.Hi
	}
	return 0
}

// graphSpan is where one room's part of the merged graph begins and how long
// it is.
type graphSpan struct {
	surfaceBase  SurfaceID
	surfaceCount uint32
	nodeBase     MotionNodeID
	nodeCount    uint32
	edgeBase     MotionEdgeID
	edgeCount    uint32
}

// mergeRoomGraphs concatenates the per-room graphs into one, renumbering
// surfaces, nodes, edges and passages by a running offset. Rooms are visited
// in ascending RoomID order and nothing is sorted, so the merged ids are a
// function of the rooms alone.
//
// A passage id of zero means "this edge belongs to no passage" and is left
// alone; every other one is shifted with the room it came from, so two senses
// of the same geometry stay paired and two rooms cannot collide on an id.
func mergeRoomGraphs(graphs []JumpGraph, model string, config Config) (JumpGraph, []graphSpan, error) {
	merged := JumpGraph{
		Model:          model,
		ProfileVersion: ProfileVersionM1,
		Abilities:      config.Progression.Final(),
		Discipline:     config.Discipline,
	}
	spans := make([]graphSpan, len(graphs))

	var surfaceBase, nodeBase, edgeBase, passageBase uint64
	for index, graph := range graphs {
		// Every room is built under the same profile, so the first room that
		// names a version names the graph's.
		if index == 0 && graph.ProfileVersion != "" {
			merged.ProfileVersion = graph.ProfileVersion
		}
		spans[index] = graphSpan{
			surfaceBase:  SurfaceID(surfaceBase),
			surfaceCount: uint32(len(graph.Surfaces)),
			nodeBase:     MotionNodeID(nodeBase),
			nodeCount:    uint32(len(graph.Nodes)),
			edgeBase:     MotionEdgeID(edgeBase),
			edgeCount:    uint32(len(graph.Edges)),
		}

		for _, surface := range graph.Surfaces {
			surface.ID += SurfaceID(surfaceBase)
			merged.Surfaces = append(merged.Surfaces, surface)
		}
		for _, node := range graph.Nodes {
			node.ID += MotionNodeID(nodeBase)
			node.Surface += SurfaceID(surfaceBase)
			merged.Nodes = append(merged.Nodes, node)
		}
		var highestPassage uint64
		for _, edge := range graph.Edges {
			if uint64(edge.Passage) > highestPassage {
				highestPassage = uint64(edge.Passage)
			}
			edge.ID += MotionEdgeID(edgeBase)
			edge.From += MotionNodeID(nodeBase)
			edge.To += MotionNodeID(nodeBase)
			if edge.Passage != 0 {
				edge.Passage += PassageID(passageBase)
			}
			merged.Edges = append(merged.Edges, edge)
		}

		surfaceBase += uint64(len(graph.Surfaces))
		nodeBase += uint64(len(graph.Nodes))
		edgeBase += uint64(len(graph.Edges))
		passageBase += highestPassage

		if nodeBase > MaxMotionNodes {
			return JumpGraph{}, nil, limitError("the merged jump graph holds %d nodes, above the ceiling of %d", nodeBase, MaxMotionNodes)
		}
		if edgeBase > MaxMotionEdges {
			return JumpGraph{}, nil, limitError("the merged jump graph holds %d edges, above the ceiling of %d", edgeBase, MaxMotionEdges)
		}
	}
	return merged, spans, nil
}

// macroConfigFor maps a platform Config onto the macro front's input.
//
// The room-size bounds are DERIVED FROM THE BEAT VOCABULARY, not fixed. The
// macro front's own defaults fit one canonical beat (26 cells, plus the
// opening and its landing). Sizing the room to the vocabulary instead means
// the caller's declared MinCells and MaxCells really do decide how big a room
// is, which is the direction that keeps the caller's declaration honoured.
// The alternative — quietly shrinking the caller's beat bounds so they fit a
// fixed room — would make a declared extent a suggestion.
//
// The LARGEST room shrinks to fit a small plane. The SMALLEST one does not:
// it is one beat wide, and a room narrower than that holds no beat at all, so
// shrinking it would produce a plane of empty shells that still reported
// itself as a certified platform map. A plane that cannot hold that smallest
// room instead leaves MinWidth above the plane's own width, and
// MacroConfig.Validate — which Generate calls before the first draw — reports
// it as the configuration error it is, naming both sizes.
func macroConfigFor(config Config, seed Seed) MacroConfig {
	beatWidth := widestBeatCells(config)
	beatHeight := tallestBeatCells(config)

	minWidth := clampRoomSide(beatWidth+2+synthMaxLeadingPad, MaxRoomSide)
	maxWidth := clampRoomSide(synthBeatsPerRoom*beatWidth+2+synthMaxLeadingPad, config.Width)
	if maxWidth < minWidth {
		maxWidth = minWidth
	}
	minHeight := clampRoomSide(beatHeight+2+synthMaxFloorReserve, MaxRoomSide)
	maxHeight := clampRoomSide(minHeight+GateBandCells, config.Height)
	if maxHeight < minHeight {
		maxHeight = minHeight
	}
	return MacroConfig{
		Seed:      seed,
		Width:     config.Width,
		Height:    config.Height,
		Rooms:     synthRoomCount(config, maxWidth, maxHeight),
		MinWidth:  minWidth,
		MaxWidth:  maxWidth,
		MinHeight: minHeight,
		MaxHeight: maxHeight,
		Steps:     config.Progression.Steps,
	}
}

// synthRoomCount is how many rooms to ask the macro front for. Config.MaxRooms
// is a ceiling and its zero value is the product ceiling of 512, which is not
// an instruction to fill the plane with 512 rooms. The count is therefore a
// conservative tiling estimate — the plane divided by the largest room —
// clamped below by one room per progression step plus a spawn and a goal, and
// above by the ceiling the caller declared.
func synthRoomCount(config Config, maxWidth, maxHeight uint32) uint32 {
	var capacity uint64
	if maxWidth > 0 && maxHeight > 0 {
		capacity = uint64(config.Width/maxWidth) * uint64(config.Height/maxHeight)
	}
	floor := uint64(len(config.Progression.Steps)) + uint64(synthMinRoomSlack)
	if capacity < floor {
		capacity = floor
	}
	ceiling := uint64(config.MaxRooms)
	if ceiling == 0 || ceiling > MaxRooms {
		ceiling = MaxRooms
	}
	if capacity > ceiling {
		capacity = ceiling
	}
	if capacity == 0 {
		capacity = 1
	}
	return uint32(capacity)
}

// roomStages returns, per room, the moveset the room's geometry is authored
// for: the stage at which the room first becomes reachable.
//
// The source is the macro front's own RegionLock list, so the answer is the
// map's authored intent rather than a second guess at it. A room behind the
// dash gate is built for a character that has the dash; a room in no locked
// region is built for the base moveset, which is the weakest moveset that
// ever has to cross it.
//
// Because a ProgressionPlan only grants, the stages are a chain under
// inclusion and "the highest lock containing the room" is well defined. That
// is the hypothesis the plan type documents, used here deliberately and not
// by accident.
func roomStages(macro Macro) []AbilitySet {
	stages := macro.Plan.Stages()
	out := make([]AbilitySet, len(macro.Plane.Rooms))
	for index := range out {
		out[index] = macro.Plan.Base
	}
	for _, lock := range macro.Locks {
		if lock.Step < 0 || lock.Step+1 >= len(stages) {
			continue
		}
		set := stages[lock.Step+1]
		for _, room := range lock.Rooms {
			if int(room) >= len(out) {
				continue
			}
			if !out[room].Contains(set) {
				out[room] = set
			}
		}
	}
	return out
}

// Canonical appends the layout's identity as a big-endian byte string. Two
// layouts generated from the same Config and the same Seed encode to the same
// bytes, and the encoding visits no map and no pointer address.
//
// Floating-point values are encoded as their IEEE-754 bits rather than
// formatted, so the comparison is exact and a NaN that appeared twice would
// still be reported as equal to itself — which is what a determinism check
// wants and what a numeric comparison would get wrong.
//
// What is encoded is what a consumer can observe: the request's seed, the
// geometry, the directed graph including every witness, and the verdict. The
// synthesis provenance — how many attempts it took, each room's rhythm — is
// deliberately NOT encoded: it is a record of how the map was found, and two
// generators that found the same map by different routes should still be
// recognised as having produced the same map.
func (l PlatformLayout) Canonical() []byte {
	out := make([]byte, 0, 1024)
	out = appendU64(out, uint64(l.Config.Seed))
	out = appendU32(out, l.Config.Width)
	out = appendU32(out, l.Config.Height)
	out = appendPlane(out, l.Plane)
	out = appendGraph(out, l.JumpGraph)
	out = appendJudgement(out, l.Judgement)
	return out
}

func appendPlane(out []byte, plane Plane) []byte {
	out = appendU32(out, plane.Width)
	out = appendU32(out, plane.Height)
	out = appendU32(out, uint32(plane.Spawn.Room))
	out = appendI32(out, plane.Spawn.At.X)
	out = appendI32(out, plane.Spawn.At.Y)
	out = appendU32(out, uint32(plane.Goal.Room))
	out = appendI32(out, plane.Goal.At.X)
	out = appendI32(out, plane.Goal.At.Y)
	out = appendU32(out, uint32(len(plane.Rooms)))
	for _, room := range plane.Rooms {
		out = appendU32(out, uint32(room.ID))
		out = appendI32(out, room.Origin.X)
		out = appendI32(out, room.Origin.Y)
		out = appendU32(out, room.Grid.Width)
		out = appendU32(out, room.Grid.Height)
		for _, cell := range room.Grid.Cells {
			out = append(out, byte(cell))
		}
		out = appendU32(out, uint32(len(room.Transitions)))
		for _, transition := range room.Transitions {
			out = appendU32(out, uint32(transition.ID))
			out = appendU32(out, uint32(transition.Room))
			out = appendI32(out, int32(transition.Side))
			out = appendU32(out, transition.Index)
			out = appendU32(out, transition.Offset)
			out = appendU32(out, transition.Extent)
			out = appendI32(out, int32(transition.Exit))
			out = appendU32(out, uint32(transition.To))
			out = appendTraversal(out, transition.Outbound)
			out = appendTraversal(out, transition.Inbound)
		}
	}
	return out
}

func appendTraversal(out []byte, traversal *Traversal) []byte {
	if traversal == nil {
		return append(out, 0)
	}
	out = append(out, 1)
	out = appendU32(out, uint32(traversal.Requires))
	return appendString(out, traversal.Note)
}

func appendGraph(out []byte, graph JumpGraph) []byte {
	out = appendString(out, graph.Model)
	out = appendString(out, graph.ProfileVersion)
	out = appendU32(out, uint32(graph.Abilities))
	out = appendI32(out, int32(graph.Discipline))
	out = appendU32(out, uint32(len(graph.Surfaces)))
	for _, surface := range graph.Surfaces {
		out = appendU32(out, uint32(surface.ID))
		out = appendU32(out, uint32(surface.Room))
		out = appendI32(out, int32(surface.Kind))
		out = appendSpan(out, surface.Extent)
		out = appendF64(out, surface.At)
		out = appendU32(out, uint32(len(surface.Intervals)))
		for _, interval := range surface.Intervals {
			out = appendSpan(out, interval.Footing)
			out = appendF64(out, interval.Headroom)
			out = appendBool(out, interval.Hazard)
		}
	}
	out = appendU32(out, uint32(len(graph.Nodes)))
	for _, node := range graph.Nodes {
		out = appendU32(out, uint32(node.ID))
		out = appendU32(out, uint32(node.Surface))
		out = appendU32(out, node.Interval)
		out = appendF64(out, node.Height)
		out = appendSpan(out, node.Footing)
		out = appendSpan(out, node.Velocity)
		out = appendI32(out, int32(node.Mode))
		out = appendResources(out, node.Resources)
	}
	out = appendU32(out, uint32(len(graph.Edges)))
	for _, edge := range graph.Edges {
		out = appendU32(out, uint32(edge.ID))
		out = appendU32(out, uint32(edge.From))
		out = appendU32(out, uint32(edge.To))
		out = appendI32(out, int32(edge.Kind))
		out = appendU32(out, uint32(edge.Requires))
		out = appendU32(out, uint32(edge.Passage))
		out = appendU32(out, uint32(edge.Transition))
		out = appendF64(out, edge.Duration)
		out = appendWitness(out, edge.Witness)
	}
	return out
}

func appendWitness(out []byte, witness *Witness) []byte {
	if witness == nil {
		return append(out, 0)
	}
	out = append(out, 1)
	out = appendF64(out, witness.ControlRate)
	out = appendU32(out, uint32(len(witness.Phases)))
	for _, phase := range witness.Phases {
		out = appendI32(out, int32(phase.Mode))
		out = appendState(out, phase.Start)
		out = appendState(out, phase.End)
		out = appendF64(out, phase.Duration)
		out = appendF64(out, phase.AccelX)
		out = appendF64(out, phase.AccelY)
	}
	out = appendU32(out, uint32(len(witness.Commands)))
	for _, command := range witness.Commands {
		out = appendF64(out, command.At)
		out = append(out, byte(command.Hold))
	}
	return out
}

func appendState(out []byte, state MotionState) []byte {
	out = appendF64(out, state.X)
	out = appendF64(out, state.Y)
	out = appendF64(out, state.VX)
	out = appendF64(out, state.VY)
	return out
}

func appendResources(out []byte, resources Resources) []byte {
	out = append(out, resources.AirJumps, resources.DashCharges)
	out = appendF64(out, resources.DashCooldown)
	out = append(out, resources.WallJumpsSinceGround, byte(resources.LastWall))
	return appendF64(out, resources.ClingRemaining)
}

func appendJudgement(out []byte, judgement Judgement) []byte {
	out = appendI32(out, int32(judgement.Verdict))
	out = appendI32(out, int32(judgement.Reason))
	out = appendString(out, judgement.Model)
	out = appendString(out, judgement.ProfileVersion)
	out = appendU64(out, judgement.Budget.CandidateEdges)
	out = appendU64(out, judgement.Budget.ExpandedNodes)
	out = appendU64(out, judgement.Budget.CollisionTests)
	return appendBool(out, judgement.Budget.Exhausted)
}

func appendSpan(out []byte, span Span) []byte {
	out = appendF64(out, span.Lo)
	return appendF64(out, span.Hi)
}

func appendU64(dst []byte, value uint64) []byte {
	return binary.BigEndian.AppendUint64(dst, value)
}

func appendF64(dst []byte, value float64) []byte {
	return appendU64(dst, math.Float64bits(value))
}

func appendBool(dst []byte, value bool) []byte {
	if value {
		return append(dst, 1)
	}
	return append(dst, 0)
}

func appendString(dst []byte, value string) []byte {
	dst = appendU32(dst, uint32(len(value)))
	return append(dst, value...)
}
