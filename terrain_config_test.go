package daedalus

import (
	"errors"
	"testing"
)

func TestTerrainConfigCanonicalizesAndCopiesAllInputSlices(t *testing.T) {
	first := Config{
		Width:  8,
		Height: 8,
		Terrain: &TerrainConfig{
			Definitions: []TerrainDefinition{
				{ID: "water", EntryCost: 7},
				{ID: "grass", EntryCost: 2, Transparent: true},
			},
			Rooms: &TerrainDistribution{
				NoneWeight: 3,
				Terrains:   []TerrainWeight{{TerrainID: "water", Weight: 5}, {TerrainID: "grass", Weight: 2}},
			},
		},
	}
	second := Config{
		Width:  8,
		Height: 8,
		Terrain: &TerrainConfig{
			Definitions: []TerrainDefinition{
				{ID: "grass", EntryCost: 2, Transparent: true},
				{ID: "water", EntryCost: 7},
			},
			Rooms: &TerrainDistribution{
				NoneWeight: 3,
				Terrains:   []TerrainWeight{{TerrainID: "grass", Weight: 2}, {TerrainID: "water", Weight: 5}},
			},
		},
	}

	firstEffective, err := normalizeConfig(first)
	if err != nil {
		t.Fatal(err)
	}
	secondEffective, err := normalizeConfig(second)
	if err != nil {
		t.Fatal(err)
	}
	if !equalTerrainConfig(firstEffective.terrain, secondEffective.terrain) {
		t.Fatalf("canonical terrain configs differ:\nfirst=%#v\nsecond=%#v", firstEffective.terrain, secondEffective.terrain)
	}
	wantDefinitions := []TerrainDefinition{
		{ID: "grass", EntryCost: 2, Transparent: true},
		{ID: "water", EntryCost: 7},
	}
	if got := firstEffective.terrain.Definitions; !equalTerrainDefinitions(got, wantDefinitions) {
		t.Fatalf("definitions = %#v, want %#v", got, wantDefinitions)
	}
	wantWeights := []TerrainWeight{{TerrainID: "grass", Weight: 2}, {TerrainID: "water", Weight: 5}}
	if got := firstEffective.terrain.Rooms.Terrains; !equalTerrainWeights(got, wantWeights) {
		t.Fatalf("weights = %#v, want %#v", got, wantWeights)
	}

	first.Terrain.Definitions[0].ID = "changed"
	first.Terrain.Rooms.Terrains[0].TerrainID = "changed"
	first.Terrain.Rooms.Terrains[0].Weight = 99
	if firstEffective.terrain.Definitions[1].ID != "water" || firstEffective.terrain.Rooms.Terrains[1].Weight != 5 {
		t.Fatal("normalized terrain must not alias caller-owned slices")
	}
}

func TestTerrainConfigValidationRejectsMalformedConfigurations(t *testing.T) {
	validDefinitions := []TerrainDefinition{{ID: "a"}, {ID: "b", EntryCost: 1}}
	cases := []struct {
		name     string
		config   *TerrainConfig
		sentinel error
	}{
		{"empty definitions", &TerrainConfig{Rooms: &TerrainDistribution{NoneWeight: 1}}, ErrInvalidConfig},
		{"no distributions", &TerrainConfig{Definitions: validDefinitions}, ErrInvalidConfig},
		{"empty ID", &TerrainConfig{Definitions: []TerrainDefinition{{ID: ""}}, Rooms: &TerrainDistribution{NoneWeight: 1}}, ErrInvalidConfig},
		{"invalid UTF-8", &TerrainConfig{Definitions: []TerrainDefinition{{ID: TerrainID(string([]byte{0xff}))}}, Rooms: &TerrainDistribution{NoneWeight: 1}}, ErrInvalidConfig},
		{"duplicate definition", &TerrainConfig{Definitions: []TerrainDefinition{{ID: "a"}, {ID: "a"}}, Rooms: &TerrainDistribution{NoneWeight: 1}}, ErrInvalidConfig},
		{"zero terrain weight", &TerrainConfig{Definitions: validDefinitions, Rooms: &TerrainDistribution{Terrains: []TerrainWeight{{TerrainID: "a"}}}}, ErrInvalidConfig},
		{"duplicate terrain weight", &TerrainConfig{Definitions: validDefinitions, Rooms: &TerrainDistribution{NoneWeight: 1, Terrains: []TerrainWeight{{TerrainID: "a", Weight: 1}, {TerrainID: "a", Weight: 2}}}}, ErrInvalidConfig},
		{"unknown terrain weight", &TerrainConfig{Definitions: validDefinitions, Rooms: &TerrainDistribution{NoneWeight: 1, Terrains: []TerrainWeight{{TerrainID: "missing", Weight: 1}}}}, ErrInvalidConfig},
		{"zero total", &TerrainConfig{Definitions: validDefinitions, Rooms: &TerrainDistribution{}}, ErrInvalidConfig},
		{"only impassable terrain", &TerrainConfig{Definitions: []TerrainDefinition{{ID: "wall", EntryCost: 0}}, Rooms: &TerrainDistribution{Terrains: []TerrainWeight{{TerrainID: "wall", Weight: 1}}}}, ErrInvalidConfig},
		{"only one patch bound", &TerrainConfig{Definitions: validDefinitions, Rooms: &TerrainDistribution{NoneWeight: 1, MinPatchCells: 4}}, ErrInvalidConfig},
		{"patch bounds reversed", &TerrainConfig{Definitions: validDefinitions, Rooms: &TerrainDistribution{NoneWeight: 1, MinPatchCells: 9, MaxPatchCells: 4}}, ErrInvalidConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeConfig(Config{Width: 8, Height: 8, Terrain: tc.config})
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error = %v, want %v", err, tc.sentinel)
			}
		})
	}
}

func TestTerrainConfigValidationRejectsProductLimits(t *testing.T) {
	tooMany := make([]TerrainDefinition, MaxTerrainKinds+1)
	for index := range tooMany {
		tooMany[index].ID = TerrainID("id-" + string(rune(0x100+index)))
	}
	if _, err := normalizeConfig(Config{Width: 1, Height: 1, Terrain: &TerrainConfig{
		Definitions: tooMany, Rooms: &TerrainDistribution{NoneWeight: 1},
	}}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("definition count error = %v, want ErrLimitExceeded", err)
	}

	longID := make([]byte, MaxTerrainPaletteBytes+1)
	for index := range longID {
		longID[index] = 'x'
	}
	if _, err := normalizeConfig(Config{Width: 1, Height: 1, Terrain: &TerrainConfig{
		Definitions: []TerrainDefinition{{ID: TerrainID(longID)}},
		Rooms:       &TerrainDistribution{NoneWeight: 1},
	}}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("definition byte error = %v, want ErrLimitExceeded", err)
	}
}

func TestInvalidTerrainConfigIsRejectedBeforeGenerationStarts(t *testing.T) {
	called := false
	generator := Generator{Placer: PlacerFunc(func(PlacementRequest) ([]RoomPlacement, error) {
		called = true
		t.Fatal("invalid terrain configuration reached the placer")
		return nil, nil
	})}
	_, err := generator.Generate(Config{
		Width: 8, Height: 8,
		Terrain: &TerrainConfig{
			Definitions: []TerrainDefinition{{ID: "water"}},
			Rooms:       &TerrainDistribution{Terrains: []TerrainWeight{{TerrainID: "water"}}},
		},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("error = %v, want ErrInvalidConfig", err)
	}
	if called {
		t.Fatal("invalid terrain configuration must be rejected before generation")
	}
}

func equalTerrainConfig(first, second *TerrainConfig) bool {
	if first == nil || second == nil {
		return first == second
	}
	return equalTerrainDefinitions(first.Definitions, second.Definitions) &&
		equalTerrainDistribution(first.Rooms, second.Rooms) &&
		equalTerrainDistribution(first.Corridors, second.Corridors)
}

func equalTerrainDistribution(first, second *TerrainDistribution) bool {
	if first == nil || second == nil {
		return first == second
	}
	return first.NoneWeight == second.NoneWeight && equalTerrainWeights(first.Terrains, second.Terrains) &&
		first.MinPatchCells == second.MinPatchCells && first.MaxPatchCells == second.MaxPatchCells
}

func equalTerrainDefinitions(first, second []TerrainDefinition) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func equalTerrainWeights(first, second []TerrainWeight) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}
