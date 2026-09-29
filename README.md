# Daedalus

Deterministic 2D dungeon generation for Go. One `Config` and one `Seed` in, one immutable `Layout` out: a grid of cells, rooms, corridors and doors.

![A 96×96 dungeon from seed 20260928: boxes and circles only, each room in its own hue, corridors one or three cells wide in ochre, doors as pale bars matching their opening, and a faint lattice is the grid.](.github/assets/map.png)

## Features

- **Deterministic.** The same effective `Config` and `Seed` reproduce a `Layout` bit for bit, in the SDK and over gRPC, on amd64 and arm64. The output is frozen for the whole v1 major and pinned by golden fixtures.
- **No dependencies in the core.** The root package imports only the standard library, enforced by a test that parses its AST. gRPC, protobuf, fx and zap live outside it.
- **Rooms have shape and their own size.** Five canonical masks — rectangle, L, T, cross and circle — each carrying its own width and height range, placed by Poisson disk over the real footprint. A circle is square, odd and at least 5 across, because a disc on a square grid needs a centre cell.
- **Connected by construction.** The spanning tree is grown against the router, so an edge that cannot be routed is replaced rather than fatal; `ExtraEdgeCount` adds cycles back on top of it.
- **Corridors have a width, and keep to themselves.** A weighted distribution drawn per corridor, and only the widths you declare: ask for 1 and 3 and you never get a 2. Every catalog declares 1, the width any corridor can fall back to. Two corridors never share a cell and never come within one cell of each other, anywhere on the floor.
- **Thematic roles.** Ask for a start, a boss and treasure rooms, and get them placed by distance rather than by luck.
- **Density regions.** Different room spacing per area of the same floor.
- **Three ways to run it.** Import it as a library, run it as a gRPC subprocess that announces itself on stdout, or open the local HTTP debug interface — the map above is a screenshot of it.
- **Bounded.** Grids up to 256×256, 65,536 cells, 256 rooms. A request over a limit is rejected before any allocation, never truncated.

## Input

The `Config` that produced the map above — boxes and circles, corridors of one or three cells:

```go
config := daedalus.Config{
	Width: 96, Height: 96, CellSize: 1, Seed: 20260928,
	MinDistance: 7, MaxAttempts: 30, MaxRooms: 128,
	CorridorOrder:  daedalus.CorridorOrderXThenY,
	ExtraEdgeCount: 6,
	RoomRoleRequests: []daedalus.RoomRoleRequest{
		{Role: daedalus.RoomRoleStart, Count: 1},
		{Role: daedalus.RoomRoleBoss, Count: 1},
		{Role: daedalus.RoomRoleTreasure, Count: 3},
	},
	RoomGeometry: &daedalus.RoomGeometry{
		MaxFootprintCells: 81, MinRoomGap: 3,
		Shapes: []daedalus.RoomShapeWeight{
			{
				Shape: daedalus.RoomShapeRectangle, Weight: 4,
				Width:  daedalus.DimensionRange{Min: 6, Max: 9},
				Height: daedalus.DimensionRange{Min: 6, Max: 9},
			},
			{
				Shape: daedalus.RoomShapeCircle, Weight: 2,
				Width:  daedalus.DimensionRange{Min: 7, Max: 9},
				Height: daedalus.DimensionRange{Min: 7, Max: 9},
			},
		},
	},
	CorridorGeometry: &daedalus.CorridorGeometry{
		Widths: []daedalus.CorridorWidthWeight{
			{Width: 1, Weight: 5},
			{Width: 3, Weight: 2},
		},
	},
}

layout, err := daedalus.Generator{}.Generate(config)
```

Each shape carries its own size, because the shapes disagree about what a legal size is. A rectangle takes anything down to 1×1; a circle must be square, odd and at least 5 across. Asking for a 6×6 circle is `ErrInvalidConfig`, not a silent drop.

Only the widths you declare are used. If 3 does not fit, the corridor falls to the next declared width — never to an undeclared 2. Because degradation walks declared widths and nothing else, the catalog must include width 1: it is the fallback every corridor can always take, and a catalog without it is `ErrInvalidConfig` before anything is allocated. Declaring 1 removes that failure; it does not promise the request generates. If no declared width fits that pair of rooms, the generator connects them another way instead of failing; only a room left with no routable edge at all stops the call, with `ErrUnconnectablePlacement` naming that room.

Over gRPC and HTTP the request is the same thing as ProtoJSON, with the proto field names and `seed` as a decimal string:

```json
{"config": {"width": 96, "height": 96, "seed": "20260928",
            "room_geometry": {"shapes": [{"shape": "ROOM_SHAPE_CIRCLE", "weight": 1,
                                          "width": {"min": 7, "max": 9},
                                          "height": {"min": 7, "max": 9}}]}}}
```

## Output

```go
fmt.Printf("rooms=%d corridors=%d doors=%d cells=%d\n",
	len(layout.Rooms), len(layout.Corridors), len(layout.Doors), len(layout.Grid.Cells))

room := layout.Rooms[0]
fmt.Printf("room %d: shape=%d %dx%d origin=%v cells=%d\n",
	room.ID, room.Shape, room.Width, room.Height, room.Origin, len(room.Cells))

for _, corridor := range layout.Corridors {
	if len(corridor.Cells) > len(corridor.Centerline) {
		door := layout.Doors[corridor.FromDoorID]
		fmt.Printf("corridor %d: centerline=%d band=%d door span=%d\n",
			corridor.ID, len(corridor.Centerline), len(corridor.Cells), door.Span)
		break
	}
}
```

```
rooms=54 corridors=59 doors=118 cells=9216
room 0: shape=0 8x7 origin={47 47} cells=56
corridor 1: centerline=3 band=9 door span=3
```

`Centerline` is the ordered route; `Cells` is the band it occupies, so a three-cell route three wide covers nine. `Span` is how many boundary cells the doorway takes, never zero. `RoomID` is nil on anything but a room cell, and `CorridorIDs` is ascending. Since two corridors never share a cell, a corridor cell names exactly one corridor.

Room 0 is always the start room when one is requested, and it is the room nearest the centre of the grid.

## Reference

- [Package documentation](https://pkg.go.dev/github.com/Otoru/daedalus) — the whole API, and the long form of this contract.
- [`Config`](https://pkg.go.dev/github.com/Otoru/daedalus#Config) is the request, [`Layout`](https://pkg.go.dev/github.com/Otoru/daedalus#Layout) is the reply.
- [Examples](https://pkg.go.dev/github.com/Otoru/daedalus#pkg-examples) — runnable, and part of the test suite.

## Install

```
go get github.com/Otoru/daedalus
```

The subprocess and the debug interface:

```
go build -o bin/daedalus ./cmd/daedalus
./bin/daedalus --http-debug-enabled
```

Contributing and local tooling: [CONTRIBUTING.md](CONTRIBUTING.md). Licence: [MIT](LICENSE).
