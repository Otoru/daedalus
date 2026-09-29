package daedalus

import "errors"

// Sentinel errors name the SDK failure categories. They define categories, not
// the concrete representation: the generator and validation return them wrapped
// with context via fmt.Errorf and %w, and callers must test them exclusively
// with errors.Is. No failure returns a partial Layout.
//
// In the gRPC service, the mapping is: ErrInvalidConfig and
// ErrInvalidNavigation → InvalidArgument, ErrLimitExceeded →
// ResourceExhausted, ErrNoCompatiblePlant, ErrUnroutableEdge,
// ErrUnconnectablePlacement, and ErrInvalidPlugin → FailedPrecondition,
// deadline expiration → DeadlineExceeded, and caller cancellation →
// Canceled.
// Cancellation and deadlines use context.Canceled and
// context.DeadlineExceeded directly, without dedicated sentinels.
var (
	// ErrInvalidConfig marks a Config that violates ranges, numeric finiteness,
	// catalog format, density regions, or role requests. Validation occurs
	// before any random stream is consumed.
	ErrInvalidConfig = errors.New("daedalus: invalid configuration")

	// ErrLimitExceeded marks a Config that exceeds the v1 product limits
	// (MaxCells or MaxRooms). It is detected before allocation, generation, or
	// RNG consumption, and the Config is never silently truncated.
	ErrLimitExceeded = errors.New("daedalus: product limit exceeded")

	// ErrNoCompatiblePlant marks a valid Config whose catalog contains no
	// RoomPlant whose DoorDirections support a Room's required Directions and
	// whose Tags contain the assigned role's RequiredTags.
	ErrNoCompatiblePlant = errors.New("daedalus: no compatible plant")

	// ErrUnroutableEdge marks an edge whose two L-routes cross a third Room and
	// for which the deterministic breadth-first search finds no orthogonal path
	// between the endpoints.
	ErrUnroutableEdge = errors.New("daedalus: edge has no orthogonal route")

	// ErrUnconnectablePlacement marks a Room set whose footprints cannot host
	// a spanning tree once every Corridor keeps a one-Cell Chebyshev gap,
	// including beside a Room. The built-in Connector returns it when a Room
	// has no routable edge left. An edge that lost to a cheaper routable
	// candidate is not this error.
	ErrUnconnectablePlacement = errors.New("daedalus: placement cannot be connected under the separation rule")

	// ErrInvalidPlugin marks a caller-supplied Placer or Connector whose return
	// value the Generator cannot accept: a mask that does not match its Shape,
	// duplicated or disconnected offsets, a placement outside the Grid or
	// otherwise rejected, the wrong number of edges, a self-edge, a duplicate
	// edge, an unknown RoomID, or a disconnected graph. The name is
	// ErrInvalidPlugin because the built-in algorithms already report their
	// own failures through the other sentinels; Placer and Connector output
	// share this one category, and the wrapped message says which one failed
	// and why.
	ErrInvalidPlugin = errors.New("daedalus: invalid plugin output")

	// ErrInvalidNavigation marks a malformed navigation request: a CostGrid whose
	// Costs length disagrees with Width × Height, a zero dimension, a Source
	// outside the Grid, a Source on an impassable Cell, a duplicate Source, or a
	// bias out of range. Count and dimension ceilings are ErrLimitExceeded.
	ErrInvalidNavigation = errors.New("daedalus: invalid navigation request")

	// ErrInvalidVisibility marks a malformed visibility grid: a zero dimension,
	// a Transparent length that disagrees with (Width×Height+7)/8, or a nonzero
	// padding bit in the final byte. A product above MaxCells is ErrLimitExceeded.
	ErrInvalidVisibility = errors.New("daedalus: invalid visibility request")

	// ErrInvalidTerrain marks a malformed standalone TerrainLayer: invalid
	// dimensions, palette, index length, or palette index. A nil layer is valid
	// and means that no terrain was requested.
	ErrInvalidTerrain = errors.New("daedalus: invalid terrain layer")
)
