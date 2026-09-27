package daedalus_test

import (
	"context"
	"fmt"
	"time"

	"github.com/Otoru/daedalus"
)

// A game asks for one floor with a width, a height, and a seed. The zero
// Generator is ready to use, and the same seed repeats the same Layout.
func ExampleGenerator_Generate() {
	layout, err := daedalus.Generator{}.Generate(daedalus.Config{
		Width: 20, Height: 16, Seed: 7,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	room := layout.Rooms[0]
	fmt.Printf("rooms=%d corridors=%d doors=%d\n", len(layout.Rooms), len(layout.Corridors), len(layout.Doors))
	fmt.Printf("first %s %dx%d origin=(%d,%d) cells=%d\n",
		shapeName(room.Shape), room.Width, room.Height, room.Origin.X, room.Origin.Y, len(room.Cells))

	// Output:
	// rooms=4 corridors=3 doors=6
	// first rectangle 9x5 origin=(9,7) cells=45
}

// GenerateContext uses the caller's deadline. A live deadline returns the
// Layout; a deadline that has already passed returns that error and no Layout.
func ExampleGenerator_GenerateContext() {
	config := daedalus.Config{Width: 20, Height: 16, Seed: 7}
	generator := daedalus.Generator{}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	layout, err := generator.GenerateContext(ctx, config)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("rooms=%d corridors=%d\n", len(layout.Rooms), len(layout.Corridors))

	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer stop()
	_, err = generator.GenerateContext(expired, config)
	fmt.Println(err)

	// Output:
	// rooms=4 corridors=3
	// context deadline exceeded
}

// An absent RoomGeometry is the default profile, whose Rooms occupy more than
// one Cell. A geometry that asks only for 1×1 rectangles is a different
// request and produces one-Cell Rooms.
func ExampleConfig_roomGeometry() {
	generator := daedalus.Generator{}
	absent, err := generator.Generate(daedalus.Config{
		Width: 24, Height: 24, Seed: 4, MaxRooms: 4,
	})
	if err != nil {
		fmt.Println("absent:", err)
		return
	}
	oneCell, err := generator.Generate(daedalus.Config{
		Width: 24, Height: 24, Seed: 4, MaxRooms: 4, MinDistance: 4,
		RoomGeometry: &daedalus.RoomGeometry{
			MinWidth: 1, MaxWidth: 1, MinHeight: 1, MaxHeight: 1,
			MaxFootprintCells: 1, MinRoomGap: 1,
			Shapes: []daedalus.RoomShapeWeight{{
				Shape: daedalus.RoomShapeRectangle, Weight: 1,
			}},
		},
	})
	if err != nil {
		fmt.Println("one-cell:", err)
		return
	}

	first := absent.Rooms[0]
	fmt.Printf("absent rooms=%d first=%s %dx%d cells=%d\n",
		len(absent.Rooms), shapeName(first.Shape), first.Width, first.Height, len(first.Cells))
	cell := oneCell.Rooms[0]
	fmt.Printf("one-cell rooms=%d first=%s %dx%d origin=(%d,%d) cells=%d\n",
		len(oneCell.Rooms), shapeName(cell.Shape), cell.Width, cell.Height, cell.Origin.X, cell.Origin.Y, len(cell.Cells))

	// Output:
	// absent rooms=4 first=cross 3x6 cells=8
	// one-cell rooms=4 first=rectangle 1x1 origin=(11,11) cells=1
}

// Start, Boss, and Treasure are requested by name and read back from the
// Rooms that received them. Start is Room 0.
func ExampleGenerator_roomRoles() {
	layout, err := daedalus.Generator{}.Generate(daedalus.Config{
		Width: 40, Height: 40, Seed: 11, MaxRooms: 8,
		RoomRoleRequests: []daedalus.RoomRoleRequest{
			{Role: daedalus.RoomRoleStart, Count: 1},
			{Role: daedalus.RoomRoleBoss, Count: 1},
			{Role: daedalus.RoomRoleTreasure, Count: 2},
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, room := range layout.Rooms {
		if room.Role == nil {
			continue
		}
		fmt.Printf("room %d %s at (%d,%d)\n", room.ID, roleName(*room.Role), room.At.X, room.At.Y)
	}

	// Output:
	// room 0 start at (19,19)
	// room 1 boss at (11,26)
	// room 2 treasure at (9,15)
	// room 3 treasure at (17,7)
}

// A PlantCatalog selects asset metadata. The Layout stores each PlantID and
// its tags; it does not resolve them to a scene or a prefab.
func ExampleConfig_plantCatalog() {
	layout, err := daedalus.Generator{}.Generate(daedalus.Config{
		Width: 24, Height: 24, Seed: 5, MaxRooms: 4,
		PlantCatalog: &daedalus.PlantCatalog{
			Rooms: []daedalus.RoomPlant{{
				ID:     "crypt",
				Tags:   []string{"stone", "dark"},
				Weight: 1,
				DoorDirections: []daedalus.Direction{
					daedalus.DirectionNorth,
					daedalus.DirectionEast,
					daedalus.DirectionSouth,
					daedalus.DirectionWest,
				},
			}},
			Corridors: []daedalus.CorridorPlant{{
				ID: "passage", Tags: []string{"damp"}, Weight: 1,
			}},
		},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	room := layout.Rooms[0]
	fmt.Printf("room %s %v\n", room.PlantID, room.Tags)
	corridor := layout.Corridors[0]
	fmt.Printf("corridor %s %v\n", corridor.PlantID, corridor.Tags)

	// Output:
	// room crypt [stone dark]
	// corridor passage [damp]
}

func shapeName(shape daedalus.RoomShape) string {
	switch shape {
	case daedalus.RoomShapeRectangle:
		return "rectangle"
	case daedalus.RoomShapeL:
		return "L"
	case daedalus.RoomShapeT:
		return "T"
	case daedalus.RoomShapeCross:
		return "cross"
	case daedalus.RoomShapeCircle:
		return "circle"
	default:
		return "unknown"
	}
}

func roleName(role daedalus.RoomRole) string {
	switch role {
	case daedalus.RoomRoleStart:
		return "start"
	case daedalus.RoomRoleBoss:
		return "boss"
	case daedalus.RoomRoleTreasure:
		return "treasure"
	default:
		return "unknown"
	}
}
