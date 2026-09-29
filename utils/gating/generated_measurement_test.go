package gating

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	daedalus "github.com/Otoru/daedalus"
)

func TestGeneratedLayoutGatingMeasurements(t *testing.T) {
	loads := []struct {
		name            string
		width, maxRooms uint32
		treasures       uint32
	}{
		{name: "64x64/64/8 Treasure", width: 64, maxRooms: 64, treasures: 8},
		{name: "96x96/128/8 Treasure", width: 96, maxRooms: 128, treasures: 8},
		{name: "96x96/128/24 Treasure", width: 96, maxRooms: 128, treasures: 24},
		{name: "128x128/256/24 Treasure", width: 128, maxRooms: 256, treasures: 24},
	}
	requests := []struct {
		name           string
		main, optional uint32
	}{
		{name: "1+1", main: 1, optional: 1},
		{name: "2+2", main: 2, optional: 2},
		{name: "4+4", main: 4, optional: 4},
		{name: "8+8", main: 8, optional: 8},
	}
	for _, load := range loads {
		for _, request := range requests {
			var generatedFailures, success, insufficient, noBridgeCapacity, noTreasureCapacity, noDistinct, sharedDoors int
			var totalBridges, totalTreasures int
			var firstDistinct string
			started := time.Now()
			for seed := uint64(0); seed < 20; seed++ {
				config := diagnosticConfig(seed+1, load.width, load.maxRooms, load.treasures)
				layout, err := (daedalus.Generator{}).Generate(config)
				if err != nil {
					generatedFailures++
					continue
				}
				g, err := buildGraph(context.Background(), layout)
				if err != nil {
					t.Fatalf("%s %s seed %d graph: %v", load.name, request.name, seed, err)
				}
				bridgeCount, treasureCount := 0, 0
				for _, bridge := range g.bridges {
					if bridge {
						bridgeCount++
					}
				}
				for _, room := range layout.Rooms {
					if room.Role != nil && *room.Role == daedalus.RoomRoleTreasure {
						treasureCount++
					}
				}
				totalBridges += bridgeCount
				totalTreasures += treasureCount
				for _, door := range layout.Doors {
					if len(door.CorridorIDs) > 1 {
						sharedDoors++
					}
				}
				if bridgeCount < int(request.main+request.optional) {
					noBridgeCapacity++
				}
				if treasureCount < int(request.optional) {
					noTreasureCapacity++
				}
				_, err = Build(context.Background(), layout, Request{Seed: daedalus.Seed(seed), StartRoomID: 0, MainGateCount: request.main, OptionalGateCount: request.optional})
				if err == nil {
					success++
				} else if errors.Is(err, daedalus.ErrInsufficientGates) {
					insufficient++
					if strings.Contains(err.Error(), "no distinct") {
						noDistinct++
						if firstDistinct == "" {
							firstDistinct = err.Error()
						}
					}
				}
			}
			t.Logf("%s %s: seeds=20 generated_failures=%d success=%d insufficient=%d no_distinct=%d shared_doors=%d no_bridge_capacity=%d no_treasure_capacity=%d avg_bridges=%.1f avg_treasures=%.1f first_no_distinct=%q elapsed=%s", load.name, request.name, generatedFailures, success, insufficient, noDistinct, sharedDoors/20, noBridgeCapacity, noTreasureCapacity, float64(totalBridges)/20, float64(totalTreasures)/20, firstDistinct, time.Since(started))
		}
	}
}
