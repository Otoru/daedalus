package daedalus

import "errors"

// Sentinel errors name the SDK failure categories. They define categories, not
// the concrete representation: the generator and validation return them wrapped
// with context via fmt.Errorf and %w, and callers must test them exclusively
// with errors.Is. No failure returns a partial Layout.
//
// In the gRPC service, the mapping is: ErrInvalidConfig → InvalidArgument,
// ErrLimitExceeded → ResourceExhausted, ErrNoCompatiblePlant,
// ErrUnroutableEdge and ErrInvalidPlugin → FailedPrecondition, deadline
// expiration → DeadlineExceeded, and caller cancellation → Canceled.
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
)
