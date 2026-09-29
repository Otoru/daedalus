package daedalus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const portraitFixturePath = "testdata/portrait/portrait.json"

type portraitCase struct {
	name string
	cfg  Config
}

type portraitResult struct {
	Name  string `json:"name"`
	Hash  string `json:"hash,omitempty"`
	Error string `json:"error,omitempty"`
}

// TestPortraitHashes freezes one digest per realistic end-to-end configuration
// so a generator change shows exactly which isolated axes moved.
func TestPortraitHashes(t *testing.T) {
	cases := portraitCases()
	actual := make([]portraitResult, len(cases))
	var expected []portraitResult
	if !*updateGoldens {
		expected = readPortraitFixture(t)
		require.Equal(t, len(expected), len(actual), "portrait fixture has a different case count; review the intentional generator change, then regenerate explicitly with go test ./ -run TestPortraitHashes -update")
	}

	for index, testCase := range cases {
		index, testCase := index, testCase
		t.Run(testCase.name, func(t *testing.T) {
			result := generatePortraitResult(testCase)
			actual[index] = result
			if !*updateGoldens && expected[index] != result {
				t.Errorf("portrait case changed: expected hash=%q error=%q, got hash=%q error=%q. Frozen output changed; decide consciously whether to regenerate with go test ./ -run TestPortraitHashes -update; this is not an automatic fix.", expected[index].Hash, expected[index].Error, result.Hash, result.Error)
			}
		})
	}

	if *updateGoldens {
		writePortraitFixture(t, actual)
	}
}

func generatePortraitResult(testCase portraitCase) portraitResult {
	layout, err := (Generator{}).Generate(testCase.cfg)
	if err != nil {
		return portraitResult{Name: testCase.name, Error: portraitErrorClass(err)}
	}
	digest := sha256.Sum256(portraitBytes(layout))
	return portraitResult{Name: testCase.name, Hash: hex.EncodeToString(digest[:])}
}

func portraitErrorClass(err error) string {
	for _, sentinel := range []struct {
		name string
		err  error
	}{
		{"ErrInvalidConfig", ErrInvalidConfig},
		{"ErrLimitExceeded", ErrLimitExceeded},
		{"ErrNoCompatiblePlant", ErrNoCompatiblePlant},
		{"ErrUnroutableEdge", ErrUnroutableEdge},
		{"ErrUnconnectablePlacement", ErrUnconnectablePlacement},
		{"ErrInvalidPlugin", ErrInvalidPlugin},
	} {
		if errors.Is(err, sentinel.err) {
			return sentinel.name
		}
	}
	return fmt.Sprintf("unexpected:%T", err)
}

func portraitBytes(layout Layout) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "seed=%d grid=%dx%d cell=%g cells=%d\n", layout.Seed, layout.Grid.Width, layout.Grid.Height, layout.Grid.CellSize, len(layout.Grid.Cells))
	for _, cell := range layout.Grid.Cells {
		fmt.Fprintf(&b, "c %d %d %d", cell.At.X, cell.At.Y, cell.Kind)
		if cell.RoomID != nil {
			fmt.Fprintf(&b, " room=%d", *cell.RoomID)
		}
		fmt.Fprintf(&b, " corridors=")
		portraitIDs(&b, cell.CorridorIDs)
		b.WriteByte('\n')
	}
	for _, room := range layout.Rooms {
		role := "none"
		if room.Role != nil {
			role = fmt.Sprintf("%d", *room.Role)
		}
		fmt.Fprintf(&b, "R %d shape=%d %dx%d origin=%d,%d at=%d,%d role=%s doors=", room.ID, room.Shape, room.Width, room.Height, room.Origin.X, room.Origin.Y, room.At.X, room.At.Y, role)
		portraitIDs(&b, room.DoorIDs)
		fmt.Fprintf(&b, " cells=%d\n", len(room.Cells))
		for _, cell := range room.Cells {
			fmt.Fprintf(&b, "  %d,%d\n", cell.X, cell.Y)
		}
	}
	for _, corridor := range layout.Corridors {
		fmt.Fprintf(&b, "C %d %d->%d doors=%d,%d line=%d band=%d\n", corridor.ID, corridor.FromRoomID, corridor.ToRoomID, corridor.FromDoorID, corridor.ToDoorID, len(corridor.Centerline), len(corridor.Cells))
		for _, cell := range corridor.Centerline {
			fmt.Fprintf(&b, "  l %d,%d\n", cell.X, cell.Y)
		}
		for _, cell := range corridor.Cells {
			fmt.Fprintf(&b, "  b %d,%d\n", cell.X, cell.Y)
		}
	}
	for _, door := range layout.Doors {
		fmt.Fprintf(&b, "D %d room=%d at=%d,%d dir=%d span=%d corridors=", door.ID, door.RoomID, door.At.X, door.At.Y, door.Direction, door.Span)
		portraitIDs(&b, door.CorridorIDs)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

func portraitIDs[T ~uint32](b *bytes.Buffer, ids []T) {
	for index, id := range ids {
		if index > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(b, "%d", id)
	}
}

func readPortraitFixture(t *testing.T) []portraitResult {
	t.Helper()
	contents, err := os.ReadFile(portraitFixturePath)
	require.NoError(t, err, "fixture missing; regenerate explicitly with go test ./ -run TestPortraitHashes -update")
	var fixture []portraitResult
	require.NoError(t, json.Unmarshal(contents, &fixture))
	return fixture
}

func writePortraitFixture(t *testing.T, fixture []portraitResult) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(portraitFixturePath), 0o755))
	contents, err := json.MarshalIndent(fixture, "", "  ")
	require.NoError(t, err)
	contents = append(contents, '\n')
	require.NoError(t, os.WriteFile(portraitFixturePath, contents, 0o644))
}

func portraitCases() []portraitCase {
	dim := func(lo, hi uint32) DimensionRange { return DimensionRange{Min: lo, Max: hi} }
	shapes := func() *RoomGeometry {
		return &RoomGeometry{
			MaxFootprintCells: 81, MinRoomGap: 2,
			Shapes: []RoomShapeWeight{
				{Shape: RoomShapeRectangle, Weight: 4, Width: dim(3, 9), Height: dim(3, 9)},
				{Shape: RoomShapeL, Weight: 2, Width: dim(3, 9), Height: dim(3, 9)},
				{Shape: RoomShapeT, Weight: 2, Width: dim(3, 9), Height: dim(3, 9)},
				{Shape: RoomShapeCross, Weight: 1, Width: dim(3, 9), Height: dim(3, 9)},
				{Shape: RoomShapeCircle, Weight: 2, Width: dim(5, 9), Height: dim(5, 9)},
			},
		}
	}
	roles := []RoomRoleRequest{{Role: RoomRoleStart, Count: 1}, {Role: RoomRoleBoss, Count: 1}, {Role: RoomRoleTreasure, Count: 3}}
	widths := func(values ...CorridorWidthWeight) *CorridorGeometry { return &CorridorGeometry{Widths: values} }
	return []portraitCase{
		{"01-default-geometry", Config{Width: 64, Height: 64, CellSize: 1, Seed: 4242, MinDistance: 6, MaxAttempts: 30, MaxRooms: 128}},
		{"02-shape-catalog", Config{Width: 64, Height: 64, CellSize: 1, Seed: 4242, MinDistance: 6, MaxAttempts: 30, MaxRooms: 128, RoomGeometry: shapes()}},
		{"03-corridor-widths-1-and-3", Config{Width: 96, Height: 96, CellSize: 1, Seed: 20260928, MinDistance: 7, MaxAttempts: 30, MaxRooms: 128, RoomGeometry: shapes(), CorridorGeometry: widths(CorridorWidthWeight{Width: 1, Weight: 5}, CorridorWidthWeight{Width: 3, Weight: 2})}},
		{"04-corridor-width-3-only", Config{Width: 96, Height: 96, CellSize: 1, Seed: 7, MinDistance: 8, MaxAttempts: 30, MaxRooms: 64, RoomGeometry: shapes(), CorridorGeometry: widths(CorridorWidthWeight{Width: 3, Weight: 1})}},
		{"05-roles", Config{Width: 96, Height: 96, CellSize: 1, Seed: 20260928, MinDistance: 7, MaxAttempts: 30, MaxRooms: 128, RoomGeometry: shapes(), RoomRoleRequests: roles}},
		{"06-density-regions", Config{Width: 80, Height: 80, CellSize: 1, Seed: 991, MinDistance: 8, MaxAttempts: 30, MaxRooms: 128, RoomGeometry: shapes(), DensityRegions: []DensityRegion{{Min: Cell{X: 0, Y: 0}, Max: Cell{X: 39, Y: 79}, MinDistance: 4}}}},
		{"07-extra-edges", Config{Width: 96, Height: 96, CellSize: 1, Seed: 20260928, MinDistance: 7, MaxAttempts: 30, MaxRooms: 128, ExtraEdgeCount: 12, RoomGeometry: shapes(), RoomRoleRequests: roles}},
		{"08-max-room-edges", Config{Width: 96, Height: 96, CellSize: 1, Seed: 20260928, MinDistance: 7, MaxAttempts: 30, MaxRooms: 128, ExtraEdgeCount: 12, MaxRoomEdges: 3, RoomGeometry: shapes(), RoomRoleRequests: roles}},
	}
}
