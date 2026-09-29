package daedalus

import (
	"errors"
	"testing"
)

func TestTerrainLayerLookupUsesPaletteIndexAndRowMajorOrder(t *testing.T) {
	room := TerrainDefinition{ID: "room", EntryCost: 3, Transparent: true}
	water := TerrainDefinition{ID: "water", EntryCost: 7}
	grid := Grid{
		Width:  3,
		Height: 2,
		Terrain: &TerrainLayer{
			Palette: []TerrainDefinition{room, water},
			Indices: []byte{0, 1, 2, 2, 0, 1},
		},
	}

	definition, ok := grid.TerrainAt(Cell{X: 1, Y: 0})
	if !ok || *definition != room {
		t.Fatalf("TerrainAt(1,0) = %#v, %t; want %#v, true", definition, ok, room)
	}
	definition, ok = grid.TerrainAt(Cell{X: 0, Y: 1})
	if !ok || *definition != water {
		t.Fatalf("TerrainAt(0,1) = %#v, %t; want %#v, true", definition, ok, water)
	}
	if id, ok := grid.TerrainIDAt(Cell{X: 2, Y: 1}); !ok || id != "room" {
		t.Fatalf("TerrainIDAt(2,1) = %q, %t; want room, true", id, ok)
	}
	if _, ok := grid.TerrainAt(Cell{X: 0, Y: 0}); ok {
		t.Fatal("index zero must mean no terrain")
	}
	if _, ok := grid.TerrainAt(Cell{X: -1, Y: 0}); ok {
		t.Fatal("outside lookup must fail closed")
	}
}

func TestTerrainLayerCloneDoesNotAlias(t *testing.T) {
	original := TerrainLayer{
		Palette: []TerrainDefinition{{ID: "stone", EntryCost: 1}},
		Indices: []byte{1, 0, 1},
	}
	clone := original.Clone()
	clone.Palette[0].EntryCost = 9
	clone.Indices[0] = 0
	if original.Palette[0].EntryCost != 1 || original.Indices[0] != 1 {
		t.Fatal("writing the clone must leave the original untouched")
	}
	original.Palette[0].EntryCost = 4
	original.Indices[1] = 1
	if clone.Palette[0].EntryCost != 9 || clone.Indices[1] != 0 {
		t.Fatal("writing the original must leave the clone untouched")
	}

	var nilLayer *TerrainLayer
	if got := nilLayer.Clone(); got != nil {
		t.Fatalf("nil clone = %#v; want nil", got)
	}
}

func TestTerrainLayerValidateRejectsMalformedPayloads(t *testing.T) {
	cases := []struct {
		name   string
		layer  *TerrainLayer
		width  uint32
		height uint32
	}{
		{name: "zero dimensions", layer: &TerrainLayer{Palette: []TerrainDefinition{{ID: "a"}}}, width: 0, height: 1},
		{name: "empty palette", layer: &TerrainLayer{Indices: []byte{0}}, width: 1, height: 1},
		{name: "empty id", layer: &TerrainLayer{Palette: []TerrainDefinition{{ID: ""}}, Indices: []byte{0}}, width: 1, height: 1},
		{name: "invalid utf8", layer: &TerrainLayer{Palette: []TerrainDefinition{{ID: TerrainID(string([]byte{0xff}))}}, Indices: []byte{0}}, width: 1, height: 1},
		{name: "unsorted palette", layer: &TerrainLayer{Palette: []TerrainDefinition{{ID: "b"}, {ID: "a"}}, Indices: []byte{0}}, width: 1, height: 1},
		{name: "duplicate palette", layer: &TerrainLayer{Palette: []TerrainDefinition{{ID: "a"}, {ID: "a"}}, Indices: []byte{0}}, width: 1, height: 1},
		{name: "wrong length", layer: &TerrainLayer{Palette: []TerrainDefinition{{ID: "a"}}, Indices: []byte{0}}, width: 2, height: 1},
		{name: "out of range index", layer: &TerrainLayer{Palette: []TerrainDefinition{{ID: "a"}}, Indices: []byte{2}}, width: 1, height: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.layer.Validate(tc.width, tc.height); !errors.Is(err, ErrInvalidTerrain) {
				t.Fatalf("Validate() error = %v; want ErrInvalidTerrain", err)
			}
		})
	}
}

func TestTerrainLayerValidateLimitsAndNil(t *testing.T) {
	var nilLayer *TerrainLayer
	if err := nilLayer.Validate(256, 256); err != nil {
		t.Fatalf("nil layer should be valid: %v", err)
	}
	tooMany := make([]TerrainDefinition, MaxTerrainKinds+1)
	for index := range tooMany {
		tooMany[index].ID = TerrainID("id-" + string(rune(0x100+index)))
	}
	if err := (&TerrainLayer{Palette: tooMany}).Validate(1, 1); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("palette count error = %v; want ErrLimitExceeded", err)
	}
	indices := make([]byte, MaxCells)
	if err := (&TerrainLayer{Palette: []TerrainDefinition{{ID: "a"}}, Indices: indices}).Validate(256, 256); err != nil {
		t.Fatalf("maximum layer should validate: %v", err)
	}
	if err := (&TerrainLayer{Palette: []TerrainDefinition{{ID: "a"}}, Indices: []byte{0}}).Validate(256, 256); !errors.Is(err, ErrInvalidTerrain) {
		t.Fatalf("bad size error = %v; want ErrInvalidTerrain", err)
	}
	longID := make([]byte, MaxTerrainPaletteBytes+1)
	for index := range longID {
		longID[index] = 'x'
	}
	if err := (&TerrainLayer{Palette: []TerrainDefinition{{ID: TerrainID(longID)}}, Indices: make([]byte, 1)}).Validate(1, 1); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("palette byte error = %v; want ErrLimitExceeded", err)
	}
}

func TestNilTerrainDoesNotChangeGeneratedLayout(t *testing.T) {
	without, err := (Generator{}).Generate(Config{Width: 16, Height: 16, Seed: 0x1234, MinDistance: 2, MaxRooms: 2})
	if err != nil {
		t.Fatal(err)
	}
	if without.Grid.Terrain != nil {
		t.Fatal("default generation must leave Grid.Terrain nil")
	}
	if err := without.Grid.ValidateTerrain(); err != nil {
		t.Fatal(err)
	}
}
