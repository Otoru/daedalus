package gating

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	daedalus "github.com/Otoru/daedalus"
)

type measurementLoad struct {
	name            string
	width, maxRooms uint32
	treasures       uint32
}

type measurementRequest struct {
	name           string
	main, optional uint32
}

func TestGeneratedLayoutGatingMeasurements(t *testing.T) {
	loads := []measurementLoad{
		{name: "64x64/64/8 Treasure", width: 64, maxRooms: 64, treasures: 8},
		{name: "96x96/128/8 Treasure", width: 96, maxRooms: 128, treasures: 8},
		{name: "96x96/128/24 Treasure", width: 96, maxRooms: 128, treasures: 24},
		{name: "128x128/256/24 Treasure", width: 128, maxRooms: 256, treasures: 24},
	}
	requests := []measurementRequest{
		{name: "1+1", main: 1, optional: 1},
		{name: "2+2", main: 2, optional: 2},
		{name: "4+4", main: 4, optional: 4},
		{name: "8+8", main: 8, optional: 8},
	}
	for _, load := range loads {
		for _, request := range requests {
			started := time.Now()
			totals := measureGatingRequest(t, load, request)
			t.Logf("%s %s: seeds=20 generated_failures=%d success=%d insufficient=%d no_distinct=%d shared_doors=%d no_bridge_capacity=%d no_treasure_capacity=%d avg_bridges=%.1f avg_treasures=%.1f first_no_distinct=%q elapsed=%s", load.name, request.name, totals.generatedFailures, totals.success, totals.insufficient, totals.noDistinct, totals.sharedDoors/20, totals.noBridgeCapacity, totals.noTreasureCapacity, float64(totals.totalBridges)/20, float64(totals.totalTreasures)/20, totals.firstDistinct, time.Since(started))
		}
	}
}

type measurementTotals struct {
	generatedFailures, success, insufficient                      int
	noBridgeCapacity, noTreasureCapacity, noDistinct, sharedDoors int
	totalBridges, totalTreasures                                  int
	firstDistinct                                                 string
}

func measureGatingRequest(t *testing.T, load measurementLoad, request measurementRequest) measurementTotals {
	t.Helper()
	var totals measurementTotals
	for seed := uint64(0); seed < 20; seed++ {
		sample := measureGatingSeed(t, load, request, seed)
		totals.generatedFailures += sample.generatedFailures
		totals.success += sample.success
		totals.insufficient += sample.insufficient
		totals.noBridgeCapacity += sample.noBridgeCapacity
		totals.noTreasureCapacity += sample.noTreasureCapacity
		totals.noDistinct += sample.noDistinct
		totals.sharedDoors += sample.sharedDoors
		totals.totalBridges += sample.totalBridges
		totals.totalTreasures += sample.totalTreasures
		if totals.firstDistinct == "" {
			totals.firstDistinct = sample.firstDistinct
		}
	}
	return totals
}

func measureGatingSeed(t *testing.T, load measurementLoad, request measurementRequest, seed uint64) measurementTotals {
	t.Helper()
	var sample measurementTotals
	config := diagnosticConfig(seed+1, load.width, load.maxRooms, load.treasures)
	layout, err := (daedalus.Generator{}).Generate(config)
	if err != nil {
		sample.generatedFailures = 1
		return sample
	}
	g, err := buildGraph(context.Background(), layout)
	if err != nil {
		t.Fatalf("%s %s seed %d graph: %v", load.name, request.name, seed, err)
	}
	for _, bridge := range g.bridges {
		if bridge {
			sample.totalBridges++
		}
	}
	for _, room := range layout.Rooms {
		if room.Role != nil && *room.Role == daedalus.RoomRoleTreasure {
			sample.totalTreasures++
		}
	}
	for _, door := range layout.Doors {
		if len(door.CorridorIDs) > 1 {
			sample.sharedDoors++
		}
	}
	if sample.totalBridges < int(request.main+request.optional) {
		sample.noBridgeCapacity++
	}
	if sample.totalTreasures < int(request.optional) {
		sample.noTreasureCapacity++
	}
	_, err = Build(context.Background(), layout, Request{Seed: daedalus.Seed(seed), StartRoomID: 0, MainGateCount: request.main, OptionalGateCount: request.optional})
	measureGatingResult(&sample, err)
	return sample
}

func measureGatingResult(sample *measurementTotals, err error) {
	if err == nil {
		sample.success++
	} else if errors.Is(err, daedalus.ErrInsufficientGates) {
		sample.insufficient++
		if strings.Contains(err.Error(), "no distinct") {
			sample.noDistinct++
			sample.firstDistinct = err.Error()
		}
	}
}
