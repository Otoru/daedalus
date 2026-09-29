package daedalus

import (
	"context"
	"errors"
	"testing"
)

func TestRoleCeilingOneIsValidAndPreserved(t *testing.T) {
	effective, err := normalizeConfig(Config{Width: 16, Height: 16, MaxRooms: 8, RoomRoleRequests: []RoomRoleRequest{{Role: RoomRoleTreasure, Count: 1, MaxRoomEdges: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if effective.roomRoleRequests[0].MaxRoomEdges != 1 {
		t.Fatalf("ceiling was not copied")
	}
	if _, err := normalizeConfig(Config{Width: 16, Height: 16, MaxRoomEdges: 1, RoomRoleRequests: []RoomRoleRequest{{Role: RoomRoleTreasure, Count: 1, MaxRoomEdges: 1}}}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("global ceiling should retain its own validation: %v", err)
	}
}

func TestGeneratedRoleCeilingNeverExceedsOne(t *testing.T) {
	for seed := Seed(0); seed < 20; seed++ {
		layout, err := (Generator{}).Generate(Config{Width: 48, Height: 40, Seed: seed, MaxRooms: 28, ExtraEdgeCount: 3, RoomRoleRequests: []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}, {Role: RoomRoleBoss, Count: 1, MaxRoomEdges: 1}}})
		if err != nil {
			if errors.Is(err, ErrUnsatisfiedRoleConstraint) || errors.Is(err, ErrUnconnectablePlacement) || errors.Is(err, ErrUnroutableEdge) {
				continue
			}
			t.Fatal(err)
		}
		degrees := make(map[RoomID]int)
		for _, corridor := range layout.Corridors {
			degrees[corridor.FromRoomID]++
			degrees[corridor.ToRoomID]++
		}
		for _, room := range layout.Rooms {
			if room.Role != nil && *room.Role == RoomRoleBoss && degrees[room.ID] > 1 {
				t.Fatalf("seed %d boss degree %d", seed, degrees[room.ID])
			}
		}
	}
}

func TestRoleConstraintErrorIsAtomic(t *testing.T) {
	layout, err := (Generator{}).GenerateContext(context.Background(), Config{Width: 24, Height: 24, Seed: 3, MaxRooms: 12, RoomRoleRequests: []RoomRoleRequest{{Role: RoomRoleStart, Count: 1, MaxRoomEdges: 1}}})
	if err != nil && !errors.Is(err, ErrUnsatisfiedRoleConstraint) {
		t.Fatal(err)
	}
	if errors.Is(err, ErrUnsatisfiedRoleConstraint) && (layout.Rooms != nil || layout.Corridors != nil) {
		t.Fatal("constraint failure returned partial layout")
	}
}
