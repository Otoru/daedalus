package gating

import (
	"context"
	"errors"
	"testing"

	daedalus "github.com/Otoru/daedalus"
)

func TestGeneratedPlanKeepsOptionalGatesOffMandatoryTargetPath(t *testing.T) {
	layout, err := (daedalus.Generator{}).Generate(diagnosticConfig(1, 128, 256, 24))
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	target := findBossRoom(layout)
	g, err := buildGraph(context.Background(), layout)
	if err != nil {
		t.Fatalf("buildGraph() error = %v", err)
	}
	mandatoryPath, err := targetPath(g, g.blockOf[0], g.blockOf[target])
	if err != nil {
		t.Fatalf("targetPath() error = %v", err)
	}
	mandatoryDoors := make(map[daedalus.DoorID]bool, len(mandatoryPath)*2)
	for _, edge := range mandatoryPath {
		corridor := layout.Corridors[edge.corridor]
		mandatoryDoors[corridor.FromDoorID] = true
		mandatoryDoors[corridor.ToDoorID] = true
	}
	plan, err := Build(context.Background(), layout, Request{Seed: 1, StartRoomID: 0, MainGateCount: 1, OptionalGateCount: 1})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	for _, gate := range plan.Gates {
		if gate.Kind == GateKindOptional && mandatoryDoors[gate.DoorID] {
			t.Fatalf("optional GateID %d uses DoorID %d on the mandatory target path", gate.ID, gate.DoorID)
		}
	}
}

func TestBuildTwoRoomGateAndValidate(t *testing.T) {
	layout := twoRoomLayout()
	plan, err := Build(context.Background(), layout, Request{StartRoomID: 0, MainGateCount: 1})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(plan.Gates) != 1 || plan.Gates[0].DoorID != 0 || plan.Gates[0].KeyRoomID != 0 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if err := Validate(context.Background(), layout, plan, 0); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsKeyBehindItsOwnDoor(t *testing.T) {
	layout := twoRoomLayout()
	plan, err := Build(context.Background(), layout, Request{StartRoomID: 0, MainGateCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	plan.Gates[0].KeyRoomID = 1
	err = Validate(context.Background(), layout, plan, 0)
	t.Logf("controlled key-behind-own-lock mutation: %v", err)
	if !errors.Is(err, daedalus.ErrInvalidGating) {
		t.Fatalf("Validate() error = %v, want ErrInvalidGating", err)
	}
}

func TestBuildRejectsLoopAsSingleDoorGate(t *testing.T) {
	layout := twoRoomLayout()
	layout.Rooms = append(layout.Rooms, daedalus.Room{ID: 2, DoorIDs: []daedalus.DoorID{3, 4}})
	layout.Rooms[0].DoorIDs = []daedalus.DoorID{0, 5}
	layout.Rooms[1].DoorIDs = []daedalus.DoorID{1, 2}
	layout.Corridors = append(layout.Corridors,
		daedalus.Corridor{ID: 1, FromRoomID: 1, ToRoomID: 2, FromDoorID: 2, ToDoorID: 3},
		daedalus.Corridor{ID: 2, FromRoomID: 2, ToRoomID: 0, FromDoorID: 4, ToDoorID: 5},
	)
	layout.Doors = append(layout.Doors,
		daedalus.Door{ID: 2, RoomID: 1, CorridorIDs: []daedalus.CorridorID{1}},
		daedalus.Door{ID: 3, RoomID: 2, CorridorIDs: []daedalus.CorridorID{1}},
		daedalus.Door{ID: 4, RoomID: 2, CorridorIDs: []daedalus.CorridorID{2}},
		daedalus.Door{ID: 5, RoomID: 0, CorridorIDs: []daedalus.CorridorID{2}},
	)
	if _, err := Build(context.Background(), layout, Request{StartRoomID: 0, TargetRoomID: ptrRoom(1), MainGateCount: 1}); !errors.Is(err, daedalus.ErrInsufficientGates) {
		t.Fatalf("Build() error = %v, want ErrInsufficientGates", err)
	}
}

func ptrRoom(room daedalus.RoomID) *daedalus.RoomID { return &room }

func chainLayout(count int) daedalus.Layout {
	rooms := make([]daedalus.Room, count)
	doors := make([]daedalus.Door, 0, count*2)
	corridors := make([]daedalus.Corridor, 0, count-1)
	for room := 0; room < count; room++ {
		rooms[room].ID = daedalus.RoomID(room)
		if room > 0 {
			left := daedalus.DoorID(len(doors))
			doors = append(doors, daedalus.Door{ID: left, RoomID: daedalus.RoomID(room), CorridorIDs: []daedalus.CorridorID{daedalus.CorridorID(room - 1)}})
			rooms[room].DoorIDs = append(rooms[room].DoorIDs, left)
		}
		if room+1 < count {
			right := daedalus.DoorID(len(doors))
			doors = append(doors, daedalus.Door{ID: right, RoomID: daedalus.RoomID(room), CorridorIDs: []daedalus.CorridorID{daedalus.CorridorID(room)}})
			rooms[room].DoorIDs = append(rooms[room].DoorIDs, right)
			toDoor := daedalus.DoorID(len(doors))
			corridors = append(corridors, daedalus.Corridor{ID: daedalus.CorridorID(room), FromRoomID: daedalus.RoomID(room), ToRoomID: daedalus.RoomID(room + 1), FromDoorID: right, ToDoorID: toDoor})
		}
	}
	return daedalus.Layout{Rooms: rooms, Corridors: corridors, Doors: doors}
}

func BenchmarkBuildGatingPlan(b *testing.B) {
	loads := []struct {
		name  string
		rooms int
		gates int
	}{
		{name: "Small", rooms: 32, gates: 3},
		{name: "Typical", rooms: 128, gates: 8},
		{name: "Maximum_v1", rooms: 256, gates: 255},
	}
	for _, load := range loads {
		load := load
		b.Run(load.name, func(b *testing.B) {
			layout := chainLayout(load.rooms)
			request := Request{Seed: 7, StartRoomID: 0, TargetRoomID: ptrRoom(daedalus.RoomID(load.rooms - 1)), MainGateCount: uint32(load.gates)}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := Build(context.Background(), layout, request); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func twoRoomLayout() daedalus.Layout {
	return daedalus.Layout{
		Rooms: []daedalus.Room{
			{ID: 0, DoorIDs: []daedalus.DoorID{0}},
			{ID: 1, DoorIDs: []daedalus.DoorID{1}},
		},
		Corridors: []daedalus.Corridor{{ID: 0, FromRoomID: 0, ToRoomID: 1, FromDoorID: 0, ToDoorID: 1}},
		Doors: []daedalus.Door{
			{ID: 0, RoomID: 0, CorridorIDs: []daedalus.CorridorID{0}},
			{ID: 1, RoomID: 1, CorridorIDs: []daedalus.CorridorID{0}},
		},
	}
}
