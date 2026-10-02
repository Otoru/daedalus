package platform

import "context"

// The oracle is the one thing every other part of platform generation talks
// to. Rhythm turns a beat into geometry by asking whether the geometry is
// crossable; the macro layer decides a gate holds by asking whether a region
// is reachable without an ability; synthesis accepts or retries a map by
// asking for a verdict. None of them needs to know how the answer is computed,
// and all of them can be built against FakeOracle before the real one exists.
//
// The interface is split into four narrow pieces plus the whole. A caller that
// only asks about single manoeuvres depends on EdgeChecker and is unaffected
// by every change to graph building.
//
// # The rules every implementation obeys
//
//  1. Deterministic. The same query returns the same answer, byte for byte,
//     on every supported platform. No map iteration reaches the output, no
//     wall clock bounds a search, and no goroutine completion order decides a
//     tie.
//  2. Pure. A query is not mutated and nothing is cached across calls in a way
//     an observer could detect.
//  3. Safe to share. A single value may be used concurrently by several
//     goroutines.
//  4. Cancellable. ctx is honoured, and a cancelled call returns
//     context.Canceled or context.DeadlineExceeded rather than a verdict.
//  5. Honest about the split between an error and a verdict. A malformed
//     question is an error; an answer of "no" or "not known" is a Judgement.
//     An impossible jump is never an error.
//  6. Honest about provenance. Every Judgement names the Model that produced
//     it and the ProfileVersion it is relative to.

// SurfaceQuery asks for the surfaces derivable from one room's geometry.
type SurfaceQuery struct {
	// Grid is the room's canonical geometry.
	Grid Grid
	// Room is the identifier stamped on the derived surfaces.
	Room RoomID
	// Profile decides the footing policy: the body's half width and the
	// clearance margin inset every supporting edge, and the body's height
	// fragments an edge wherever the ceiling is too low.
	Profile MovementProfile
}

// SurfaceResult is the derived surface index.
type SurfaceResult struct {
	// Surfaces lists the derived surfaces in ascending SurfaceID order.
	Surfaces []Surface
	// Judgement records the model, the profile version and whether the
	// derivation was complete. A derivation that hit a ceiling certifies
	// nothing about the surfaces it did not reach.
	Judgement Judgement
}

// EdgeQuery asks whether one manoeuvre connects two states. It is the
// finest-grained question and the one a rhythm generator asks most: "under
// this profile and this moveset, can the character get from there to here?"
type EdgeQuery struct {
	// Grid is the geometry the manoeuvre happens in.
	Grid Grid
	// Profile is the movement profile.
	Profile MovementProfile
	// Abilities is the moveset the character currently holds.
	Abilities AbilitySet
	// From is the departure state, including its velocity band and its
	// resources. Starting from rest is MovementProfile.FullResources and a
	// velocity span containing zero, spelled out rather than assumed.
	From MotionNode
	// To is the arrival state. A query may leave To.Resources at its zero
	// value and set AnyArrivalResources to ask only about geometry.
	To MotionNode
	// AnyArrivalResources relaxes the arrival test to ignore To.Resources, so
	// that the question is "can the character land there" rather than "can it
	// land there with exactly this much left".
	AnyArrivalResources bool
	// Kind restricts the manoeuvre. MotionEdgeKindUnspecified asks for any
	// manoeuvre the moveset allows, and the answer names the one it found.
	Kind MotionEdgeKind
	// Budget bounds the search. The zero value requests DefaultSearchBudget.
	Budget SearchBudget
	// OmitWitness drops the witness from a certified answer.
	OmitWitness bool
}

// EdgeResult is the answer to an EdgeQuery.
type EdgeResult struct {
	// Edge is the manoeuvre found. It is meaningful only when the judgement
	// certifies; its Kind names which manoeuvre answered an unrestricted
	// query, and its Witness is present unless the query omitted it.
	Edge MotionEdge
	// Judgement is the verdict, the reason, the model and what the search
	// spent.
	Judgement Judgement
}

// GraphQuery asks for the whole directed jump graph of a geometry under a
// moveset.
type GraphQuery struct {
	// Grid is the room's canonical geometry.
	Grid Grid
	// Room is the identifier stamped on the derived surfaces.
	Room RoomID
	// Profile is the movement profile.
	Profile MovementProfile
	// Abilities is the moveset to build for. Edges needing more are not
	// emitted.
	Abilities AbilitySet
	// Discipline selects the node model. The zero value requests
	// NodeDisciplineRefined.
	Discipline NodeDiscipline
	// Budget bounds the build. The zero value requests DefaultSearchBudget.
	Budget SearchBudget
	// OmitWitness drops the witness from every edge.
	OmitWitness bool
}

// GraphResult is the answer to a GraphQuery.
//
// The graph is returned even when the judgement is VerdictUnknown. A build
// that ran out of budget produces a real partial graph: every edge in it is
// still certified, and what is missing is the knowledge of what else exists.
// Discarding it would throw away correct information, so the contract is that
// the graph is always usable and the judgement always says how complete it is.
type GraphResult struct {
	// Graph is the directed jump graph, possibly partial.
	Graph JumpGraph
	// Judgement is VerdictCertified when the build was exhaustive within the
	// model, and VerdictUnknown with ReasonBudgetExhausted when it was cut
	// short.
	Judgement Judgement
}

// RouteQuery asks whether a route exists between two nodes of a built graph
// under a moveset. The moveset is a separate field from the graph's own so
// that one graph answers every stage of a progression: filtering edges by
// MotionEdge.Reachable is the whole of the gating check.
type RouteQuery struct {
	// Graph is the graph to search. It must not be nil.
	Graph *JumpGraph
	// From is the start node.
	From MotionNodeID
	// To is the goal node.
	To MotionNodeID
	// Abilities is the moveset to filter edges by. It must be a subset of the
	// graph's own, since the graph holds no edge beyond that.
	Abilities AbilitySet
	// Budget bounds the search. The zero value requests DefaultSearchBudget.
	Budget SearchBudget
}

// RouteResult is the answer to a RouteQuery.
type RouteResult struct {
	// Route is the path found, meaningful only when the judgement certifies.
	Route Route
	// Judgement is the verdict. A graph with no path gives VerdictRejected
	// with ReasonDisconnected, which rejects within THIS graph and says
	// nothing about geometry the graph does not contain.
	Judgement Judgement
}

// SurfaceDeriver turns geometry into the surface index the rest of the
// analysis is built on.
type SurfaceDeriver interface {
	Surfaces(ctx context.Context, query SurfaceQuery) (SurfaceResult, error)
}

// EdgeChecker answers one manoeuvre at a time. It is the narrowest useful
// dependency and the one a beat-to-geometry pass should ask for.
type EdgeChecker interface {
	CheckEdge(ctx context.Context, query EdgeQuery) (EdgeResult, error)
}

// GraphBuilder builds the directed jump graph of a geometry.
type GraphBuilder interface {
	BuildGraph(ctx context.Context, query GraphQuery) (GraphResult, error)
}

// RouteFinder searches a built graph.
type RouteFinder interface {
	FindRoute(ctx context.Context, query RouteQuery) (RouteResult, error)
}

// Oracle is the whole movement oracle: everything the generator can ask about
// motion. Its rules are listed at the top of this file and are part of the
// contract, not advice.
type Oracle interface {
	// Model names the movement model, which every Judgement repeats. A
	// consumer that treats a certificate as binding compares it against
	// ModelM1 and refuses ModelFake.
	Model() string

	SurfaceDeriver
	EdgeChecker
	GraphBuilder
	RouteFinder
}

// effectiveBudget returns the budget a query actually runs under.
func effectiveBudget(budget SearchBudget) SearchBudget {
	if budget == (SearchBudget{}) {
		return DefaultSearchBudget()
	}
	return budget
}
