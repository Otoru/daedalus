# Daedalus

Deterministic 2D dungeon generation for Go. One `Config` and one `Seed` in, one immutable `Layout` out: a grid of cells, rooms, corridors and doors.

![A 96×96 dungeon from seed 20260927: each room has its own hue, corridors are dull ochre, doors are pale marks with a tick, and a faint lattice is the grid.](.github/assets/map.png)

## Features

- **Deterministic.** The same effective `Config` and `Seed` reproduce a `Layout` bit for bit, in the SDK and over gRPC, on amd64 and arm64. The output is frozen for the whole v1 major and pinned by golden fixtures.
- **No dependencies in the core.** The root package imports only the standard library, enforced by a test that parses its AST. gRPC, protobuf, fx and zap live outside it.
- **Rooms have shape.** Five canonical masks — rectangle, L, T, cross and circle — placed by Poisson disk over their real footprint, with a configurable gap between them.
- **Connected by construction.** A Prim spanning tree guarantees every room is reachable; `ExtraEdgeCount` adds cycles back on top of it.
- **Thematic roles.** Ask for a start, a boss and treasure rooms, and get them placed by distance rather than by luck.
- **Density regions.** Different room spacing per area of the same floor.
- **Three ways to run it.** Import it as a library, run it as a gRPC subprocess that announces itself on stdout, or open the local HTTP debug interface — the map above is a screenshot of it.
- **Bounded.** Grids up to 256×256, 65,536 cells, 256 rooms. A request over a limit is rejected before any allocation, never truncated.

## Input

The `Config` that produced the map above:

```go
config := daedalus.Config{
	Width: 96, Height: 96, CellSize: 1, Seed: 20260927,
	MinDistance: 6, MaxAttempts: 30, MaxRooms: 128,
	CorridorOrder:  daedalus.CorridorOrderXThenY,
	ExtraEdgeCount: 6,
	RoomRoleRequests: []daedalus.RoomRoleRequest{
		{Role: daedalus.RoomRoleStart, Count: 1},
		{Role: daedalus.RoomRoleBoss, Count: 1},
		{Role: daedalus.RoomRoleTreasure, Count: 3},
	},
	RoomGeometry: &daedalus.RoomGeometry{
		MinWidth: 3, MaxWidth: 9, MinHeight: 3, MaxHeight: 9,
		MaxFootprintCells: 81, MinRoomGap: 1,
		Shapes: []daedalus.RoomShapeWeight{
			{Shape: daedalus.RoomShapeRectangle, Weight: 4},
			{Shape: daedalus.RoomShapeL, Weight: 2},
			{Shape: daedalus.RoomShapeT, Weight: 2},
			{Shape: daedalus.RoomShapeCross, Weight: 1},
			{Shape: daedalus.RoomShapeCircle, Weight: 2},
		},
	},
}

layout, err := daedalus.Generator{}.Generate(config)
```

Every field has a default except `Width` and `Height`. A nil `RoomGeometry` is the dynamic profile, not one-cell rooms.

Over gRPC and HTTP the request is the same thing as ProtoJSON. Field names are the proto names, and `seed` is a decimal string because it is a `uint64`:

```json
{"config": {"width": 96, "height": 96, "seed": "20260927", "extra_edge_count": 6}}
```

## Output

```go
fmt.Printf("seed=%d rooms=%d corridors=%d doors=%d cells=%d\n",
	layout.Seed, len(layout.Rooms), len(layout.Corridors), len(layout.Doors), len(layout.Grid.Cells))

cell := layout.Grid.Cells[66*96+88] // row-major: (x, y) is Cells[y*Width+x]
fmt.Printf("cell %v: room=%v corridors=%v\n", cell.At, cell.RoomID, cell.CorridorIDs)

door := layout.Doors[138]
fmt.Printf("door %d: room=%d at=%v corridors=%v\n", door.ID, door.RoomID, door.At, door.CorridorIDs)
```

```
seed=20260927 rooms=125 corridors=130 doors=257 cells=9216
cell {88 66}: room=<nil> corridors=[119 120]
door 138: room=113 at={24 75} corridors=[69 126]
```

`RoomID` is nil on anything but a room cell. `CorridorIDs` is ascending, and two ids mean two corridors share the cell. A door belongs to one room and can serve several corridors.

Over HTTP and gRPC the same `Layout` comes back as ProtoJSON. The first cell of each kind, in row-major order — note that the first room cell belongs to room 116, because scan order is not `RoomID` order:

```json
{"at": {"x": 0, "y": 0}, "kind": "CELL_KIND_EMPTY", "corridor_ids": []}
{"at": {"x": 47, "y": 0}, "kind": "CELL_KIND_ROOM", "room_id": 116, "corridor_ids": []}
{"at": {"x": 85, "y": 1}, "kind": "CELL_KIND_CORRIDOR", "corridor_ids": [111]}
{"at": {"x": 88, "y": 66}, "kind": "CELL_KIND_CORRIDOR", "corridor_ids": [119, 120]}
```

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
