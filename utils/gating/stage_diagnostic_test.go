package gating

import (
	"context"
	"fmt"
	"testing"

	daedalus "github.com/Otoru/daedalus"
)

func TestOptionalStageDiagnostics(t *testing.T) {
	var firstReachable, secondReachable, firstGlobalUnused, secondGlobalUnused, firstStageAvailable, secondStageAvailable int
	var samples int
	for seed := uint64(0); seed < 20; seed++ {
		layout, err := (daedalus.Generator{}).Generate(diagnosticConfig(seed+1, 128, 256, 24))
		if err != nil {
			t.Fatalf("seed %d generation: %v", seed, err)
		}
		g, err := buildGraph(context.Background(), layout)
		if err != nil {
			t.Fatal(err)
		}
		main, err := selectMain(g, g.blockOf[0], g.blockOf[findBossRoom(layout)], 8)
		if err != nil {
			t.Fatalf("seed %d main selection: %v", seed, err)
		}
		mainPath, err := targetPath(g, g.blockOf[0], g.blockOf[findBossRoom(layout)])
		if err != nil {
			t.Fatal(err)
		}
		optional, err := selectOptional(g, g.blockOf[0], main, mainPath, 8)
		if err != nil {
			t.Fatalf("seed %d optional selection: %v", seed, err)
		}
		for _, gate := range optional {
			for _, pathEdge := range mainPath {
				if gate.edge.corridor == pathEdge.corridor {
					t.Fatalf("seed %d optional gate corridor %d lies on the mandatory target path", seed, gate.edge.corridor)
				}
			}
		}
		stats := diagnoseKeyStages(g, append(main, optional...), 0, daedalus.Seed(seed))
		optionalStats := stats[len(main):]
		if len(optionalStats) < 2 {
			t.Fatalf("seed %d has only %d optional stages", seed, len(optionalStats))
		}
		first := optionalStats[0]
		second := optionalStats[1]
		firstReachable += first.reachable
		secondReachable += second.reachable
		firstGlobalUnused += first.globalUnused
		secondGlobalUnused += second.globalUnused
		firstStageAvailable += first.stageAvailable
		secondStageAvailable += second.stageAvailable
		samples++
		if seed == 0 {
			t.Logf("seed 0 all stages: %+v", stats)
			parent, _, _ := rootedTree(g, g.blockOf[0])
			for index, gate := range main {
				t.Logf("seed 0 main gate %d corridor=%d door=%d fromRoom=%d(block=%d) toRoom=%d(block=%d) parentChild=%d->%d", index, gate.edge.corridor, gate.door, gate.edge.fromRoom, g.blockOf[gate.edge.fromRoom], gate.edge.toRoom, g.blockOf[gate.edge.toRoom], parent[g.blockOf[gate.edge.toRoom]], g.blockOf[gate.edge.toRoom])
			}
			locked := make([]bool, len(layout.Doors))
			for _, gate := range append(main, optional...) {
				locked[gate.door] = true
			}
			before := countTrue(floodRooms(g, locked, 0))
			locked[main[0].door] = false
			after := countTrue(floodRooms(g, locked, 0))
			corridor := layout.Corridors[main[0].edge.corridor]
			t.Logf("seed 0 gate 0 direct unlock: before=%d after=%d corridor endpoints=%d/%d locked=%v/%v", before, after, corridor.FromDoorID, corridor.ToDoorID, locked[corridor.FromDoorID], locked[corridor.ToDoorID])
		}
	}
	t.Logf("128x128/256, 24 Treasure, 8+8, seeds=%d: first reachable=%d global_unused=%d stage_available=%d; second reachable=%d global_unused=%d stage_available=%d", samples, firstReachable/samples, firstGlobalUnused/samples, firstStageAvailable/samples, secondReachable/samples, secondGlobalUnused/samples, secondStageAvailable/samples)
}

type keyStageDiagnostic struct {
	kind                                    GateKind
	gate, stage                             int
	reachable, globalUnused, stageAvailable int
	start                                   int
}

func diagnoseKeyStages(g *graph, selected []selectedGate, start int, seed daedalus.Seed) []keyStageDiagnostic {
	used := make([]bool, len(g.layout.Rooms))
	stats := make([]keyStageDiagnostic, 0, len(selected))
	for index, gate := range selected {
		stage := gate.stage
		if gate.kind == GateKindMain {
			stage = index
		}
		locked := make([]bool, len(g.layout.Doors))
		for otherIndex, other := range selected {
			if other.kind == GateKindMain && otherIndex >= stage {
				locked[other.door] = true
			}
			if other.kind == GateKindOptional {
				locked[other.door] = true
			}
		}
		reachable := floodRooms(g, locked, start)
		globalUnused := 0
		for room, isReachable := range reachable {
			if !isReachable {
				continue
			}
			if !used[room] {
				globalUnused++
			}
		}
		stats = append(stats, keyStageDiagnostic{kind: gate.kind, gate: index, stage: stage, reachable: countTrue(reachable), globalUnused: globalUnused, stageAvailable: countTrue(reachable), start: start})
		distances := roomDistances(g, locked, start)
		best := -1
		for room, isReachable := range reachable {
			if !isReachable || used[room] {
				continue
			}
			if best < 0 || distances[room] > distances[best] || (distances[room] == distances[best] && rank(seed, index, room) < rank(seed, index, best)) || (distances[room] == distances[best] && rank(seed, index, room) == rank(seed, index, best) && room < best) {
				best = room
			}
		}
		if best >= 0 {
			used[best] = true
		}
	}
	return stats
}

func diagnosticConfig(seed uint64, width, maxRooms, treasures uint32) daedalus.Config {
	return daedalus.Config{Width: width, Height: width, Seed: daedalus.Seed(seed), MinDistance: 6, MaxAttempts: 30, MaxRooms: maxRooms, RoomGeometry: &daedalus.RoomGeometry{MaxFootprintCells: 9, MinRoomGap: 1, Shapes: []daedalus.RoomShapeWeight{{Shape: daedalus.RoomShapeRectangle, Weight: 1, Width: daedalus.DimensionRange{Min: 3, Max: 3}, Height: daedalus.DimensionRange{Min: 3, Max: 3}}}}, RoomRoleRequests: []daedalus.RoomRoleRequest{{Role: daedalus.RoomRoleStart, Count: 1}, {Role: daedalus.RoomRoleBoss, Count: 1}, {Role: daedalus.RoomRoleTreasure, Count: treasures}}}
}

func findBossRoom(layout daedalus.Layout) int {
	for index, room := range layout.Rooms {
		if room.Role != nil && *room.Role == daedalus.RoomRoleBoss {
			return index
		}
	}
	panic(fmt.Sprintf("layout has no Boss among %d rooms", len(layout.Rooms)))
}
