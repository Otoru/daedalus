package platform

import (
	"errors"
	"fmt"
)

// Sentinel errors name this package's failure categories. They define
// categories, not representations: a failure is returned wrapped with context
// through fmt.Errorf and %w, and callers must test them exclusively with
// errors.Is. No failure returns a partial result.
//
// The line between an error and a Verdict is the most important rule here, and
// it is not negotiable. An error means the QUESTION was malformed: an invalid
// profile, geometry that does not describe a grid, a query naming a node that
// does not exist, a request over a product limit, a cancelled context. A
// Verdict means the question was well formed and the ANSWER is no, or is not
// known.
//
// A jump that cannot be made is therefore not an error. A search that ran out
// of budget is not an error either; it is VerdictUnknown with
// ReasonBudgetExhausted and a BudgetReport that says Exhausted. Returning an
// error for those would force every caller to distinguish "you asked wrong"
// from "the answer is no" by reading a message.
//
// Cancellation and deadlines use context.Canceled and
// context.DeadlineExceeded directly, without dedicated sentinels.
var (
	// ErrInvalidProfile marks a MovementProfile that cannot describe a
	// character: a non-finite or non-positive quantity, a body with no extent,
	// or a mechanic whose mode was left unspecified. A double jump that does
	// not say whether it resets or adds to vertical velocity is this error,
	// not a default.
	ErrInvalidProfile = errors.New("platform: invalid movement profile")

	// ErrInvalidConfig marks a Config that violates ranges, numeric
	// finiteness, or the agreement between its parts. Validation happens
	// before any allocation and before any random stream is consumed.
	ErrInvalidConfig = errors.New("platform: invalid configuration")

	// ErrInvalidGeometry marks a Grid, Room or Plane that does not describe
	// space: a zero dimension, a cell slice whose length disagrees with
	// width times height, an unknown CellKind, a terrain layer that does not
	// match the grid, a room placed outside its plane, or a transition whose
	// side and exit direction disagree.
	//
	// An unknown CellKind is this error on purpose. A converter that silently
	// turns an unrecognised kind into air produces a map that still looks
	// valid and has lost its one-way platforms, which is the worst shape a
	// failure can take.
	ErrInvalidGeometry = errors.New("platform: invalid geometry")

	// ErrInvalidProgression marks a ProgressionPlan that cannot be walked: a
	// step that grants nothing, a step that grants an ability an earlier step
	// or the base already gave, or an ability outside the declared vocabulary.
	ErrInvalidProgression = errors.New("platform: invalid progression plan")

	// ErrInvalidBeats marks a BeatConfig that cannot drive a generator: an
	// unspecified or unknown BeatKind, a duplicate definition, a weight of
	// zero, a distribution naming a kind that was never defined, or a
	// run-length span whose bounds are inverted.
	ErrInvalidBeats = errors.New("platform: invalid beat configuration")

	// ErrInvalidQuery marks a malformed oracle question: a node identifier
	// outside the graph, a nil graph, an edge query whose endpoints share no
	// profile, or a budget whose ceilings are all zero.
	ErrInvalidQuery = errors.New("platform: invalid oracle query")

	// ErrInvalidJudgement marks a Judgement whose parts disagree: a reason
	// that belongs to another verdict, a missing model, or an exhausted budget
	// reported alongside a decided verdict. It exists so that a consumer can
	// reject a malformed certificate instead of trusting it.
	ErrInvalidJudgement = errors.New("platform: invalid judgement")

	// ErrLimitExceeded marks a request above the product limits: room sides,
	// cells per room, rooms per plane, nodes or edges per jump graph. It is
	// detected before allocation and the request is never silently truncated.
	ErrLimitExceeded = errors.New("platform: product limit exceeded")

	// ErrNotImplemented marks a well-formed request this build cannot serve
	// yet. It is the error a partial implementation returns while the contract
	// is being filled in, and it is deliberately distinct from every verdict:
	// a missing implementation is not an answer about the geometry.
	ErrNotImplemented = errors.New("platform: not implemented")
)

func profileError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidProfile, fmt.Sprintf(format, args...))
}

func configError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, fmt.Sprintf(format, args...))
}

func geometryError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidGeometry, fmt.Sprintf(format, args...))
}

func progressionError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidProgression, fmt.Sprintf(format, args...))
}

func beatsError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidBeats, fmt.Sprintf(format, args...))
}

func queryError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidQuery, fmt.Sprintf(format, args...))
}

func judgementError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidJudgement, fmt.Sprintf(format, args...))
}

func limitError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrLimitExceeded, fmt.Sprintf(format, args...))
}
