package gating

import (
	"context"

	daedalus "github.com/Otoru/daedalus"
)

type GateID uint32

type GateKind uint8

const (
	GateKindMain GateKind = iota
	GateKindOptional
)

type Gate struct {
	ID        GateID
	Kind      GateKind
	DoorID    daedalus.DoorID
	KeyRoomID daedalus.RoomID
}

type Plan struct {
	Seed  daedalus.Seed
	Gates []Gate
}

type Request struct {
	Seed              daedalus.Seed
	StartRoomID       daedalus.RoomID
	TargetRoomID      *daedalus.RoomID
	MainGateCount     uint32
	OptionalGateCount uint32
}

// Build constructs an exact-count progression plan without mutating layout.
func Build(ctx context.Context, layout daedalus.Layout, request Request) (Plan, error) {
	return build(ctx, layout, request)
}

// Validate is the public solvability oracle for a progression plan.
func Validate(ctx context.Context, layout daedalus.Layout, plan Plan, startRoomID daedalus.RoomID) error {
	return validatePlan(ctx, layout, plan, startRoomID)
}
