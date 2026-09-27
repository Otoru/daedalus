# Daedalus

Daedalus is a Go library that generates a discrete, deterministic 2D dungeon when a game asks for one. The caller passes a `Config` and a `Seed` to `Generator.Generate` and receives one immutable `Layout`: a grid of cells, rooms, corridors, and doors. The module path is `github.com/Otoru/daedalus`. It requires Go 1.25 and is MIT licensed.

![A 96×96 dungeon from seed 20260927: each room has its own hue, corridors are dull ochre, doors are pale marks with a tick, and a faint lattice is the grid.](.github/assets/map.png)

That picture is a 1784×1784 screenshot of this repository's debug canvas, drawing a 96×96 grid. The same seed reproduces it: 125 rooms, 130 corridors, and 257 doors. Each room takes its own hue from its `RoomID`, `(RoomID × 137 + 211) mod 360`, and even and odd ids alternate lightness. Corridor cells are the dull ochre fill, with a darker ochre stroke along the route. Doors are the marked cells on room edges; the tick points the way the door faces. The faint lattice is the grid.

The request body was:

```json
{
  "config": {
    "width": 96, "height": 96, "cell_size": 1, "seed": "20260927",
    "min_distance": 6, "max_attempts": 30, "max_rooms": 128,
    "corridor_order": "CORRIDOR_ORDER_X_THEN_Y", "extra_edge_count": 6,
    "room_role_requests": [
      {"role": "ROOM_ROLE_START", "count": 1},
      {"role": "ROOM_ROLE_BOSS", "count": 1},
      {"role": "ROOM_ROLE_TREASURE", "count": 3}
    ],
    "room_geometry": {
      "min_width": 3, "max_width": 9, "min_height": 3, "max_height": 9,
      "max_footprint_cells": 81, "min_room_gap": 1,
      "shapes": [
        {"shape": "ROOM_SHAPE_RECTANGLE", "weight": 4},
        {"shape": "ROOM_SHAPE_L", "weight": 2},
        {"shape": "ROOM_SHAPE_T", "weight": 2},
        {"shape": "ROOM_SHAPE_CROSS", "weight": 1},
        {"shape": "ROOM_SHAPE_CIRCLE", "weight": 2}
      ]
    }
  }
}
```

The root package imports only the Go standard library. A test parses that package's AST and rejects any import outside the standard library, so the generator can be embedded without pulling in gRPC, protobuf, or the process dependencies. Those live in `cmd/daedalus` and `internal/`.

It does not render, instantiate scenes or prefabs, load assets, or know Godot, Unity, or Bevy. It does not compute pixel positions, build a navmesh, or hold game state. `CellSize` is an opaque unit defined by the caller and is only copied onto the `Layout`. There is no configuration file.

The long form of this contract — vocabulary, masks, pipeline, determinism, errors, and concurrency — is the package comment in `doc.go`. From this module, `go doc .` prints it.

## Install

```
go get github.com/Otoru/daedalus
```

The subprocess is built from a checkout of this module:

```
go build -o bin/daedalus ./cmd/daedalus
```

## Generate a floor

`example_test.go` is part of the test suite. `ExampleGenerator_Generate` is the smallest call: width, height, and seed. The zero `Generator` is ready to use. `shapeName` is in the same file.

```go
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
```

From the module root:

```
go test -count=1 -run '^ExampleGenerator_Generate$'
```

An absent `RoomGeometry` does not mean one-cell rooms. After validation it becomes the dynamic profile: width and height from `min(3, side)` to `min(9, side)`, `MaxFootprintCells` 81, `MinRoomGap` 1, and weights Rectangle 4, L 2, T 2, Cross 1, Circle 2. One-cell rooms are an explicit geometry that asks for them. `ExampleConfig_roomGeometry` in the same file shows the difference.

Rooms use five canonical masks, with no implicit rotation: Rectangle, L, T, Cross, and Circle.

Field names, zero values, and the error for a bad value are in [Input](#input). What comes back is in [Response](#response).

## Input

`Config` is the whole request. There is no configuration file. The zero `Config` is not valid, because `Width` and `Height` have no default. Several other zeros do normalize to a default rather than to zero: `CellSize`, `MinDistance`, `MaxAttempts`, and `MaxRooms`. A nil `RoomGeometry` is the dynamic profile above. A `RoomGeometry` value that is present does not get those defaults; a zero in that struct is the zero you sent.

Validation runs before any random draw. A failure returns the sentinel wrapped with context, tested with `errors.Is`, and the zero `Layout`. `ErrInvalidConfig` is a value outside its range. `ErrLimitExceeded` is a v1 product limit (`MaxCells` 65,536, `MaxRooms` 256, `MaxFootprintCells` 4,096). On gRPC those are `InvalidArgument` and `ResourceExhausted`.

ProtoJSON uses the names in `proto/daedalus/v1/daedalus.proto`. HTTP rejects any other spelling, so callers send `max_footprint_cells`, not `MaxFootprintCells` or `maxFootprintCells`. `seed` is a decimal string because it is a `uint64`. Enums travel as their proto names (`ROOM_SHAPE_RECTANGLE`). Those names are not the Go iota numbers: `RoomShapeRectangle` is 0 in Go, and `ROOM_SHAPE_RECTANGLE` is 1 on the wire.

### Config

| Go | ProtoJSON | Type | Zero value | Accepted | Error |
| --- | --- | --- | --- | --- | --- |
| `Width` | `width` | `uint32` | required; 0 is not a default | 1..256, and `Width` × `Height` ≤ 65,536 | 0 → `ErrInvalidConfig`. A side above 256, or a product above `MaxCells`, → `ErrLimitExceeded` |
| `Height` | `height` | `uint32` | same as `Width` | same as `Width` | same as `Width` |
| `CellSize` | `cell_size` | `float64` | 0 becomes 1.0 | finite and > 0 | any other non-finite or non-positive value → `ErrInvalidConfig` |
| `Seed` | `seed` | `Seed` (`uint64`) | 0 is a real seed, not "random" | every `uint64` | none. In ProtoJSON it is a decimal string |
| `MinDistance` | `min_distance` | `float64` | 0 becomes 6.0 | finite and ≥ 1 | any other non-finite value, or any value below 1, → `ErrInvalidConfig` |
| `MaxAttempts` | `max_attempts` | `uint32` | 0 becomes 30 | 1..1024 | above 1024 → `ErrInvalidConfig` |
| `MaxRooms` | `max_rooms` | `uint32` | 0 becomes 256 | 1..256 | above 256 → `ErrLimitExceeded` |
| `CorridorOrder` | `corridor_order` | `CorridorOrder` | `CorridorOrderXThenY` | the two constants below | any other Go value → `ErrInvalidConfig` |
| `ExtraEdgeCount` | `extra_edge_count` | `uint32` | 0 keeps the graph a tree and adds no cycles | 0 .. N×(N−1)/2, where N is `MaxRooms` after its default is applied | above that → `ErrInvalidConfig` |
| `RoomRoleRequests` | `room_role_requests` | `[]RoomRoleRequest` | nil or empty disables thematic rooms | at most one entry per `RoomRole` | `ErrInvalidConfig` |
| `DensityRegions` | `density_regions` | `[]DensityRegion` | nil or empty uses one `MinDistance` for the whole grid | half-open rectangles inside the grid, with no shared cell | `ErrInvalidConfig` |
| `RoomGeometry` | `room_geometry` | `*RoomGeometry` | nil selects the dynamic profile | a present value is taken as sent | `ErrInvalidConfig`, or `ErrLimitExceeded` when `MaxFootprintCells` is above 4,096 |
| `PlantCatalog` | `plant_catalog` | `*PlantCatalog` | nil leaves every `PlantID` and `Tags` empty | both lists non-empty | `ErrInvalidConfig` |

`CellSize` is copied onto the `Layout` and does not move a room. Placement stops when `MaxRooms` accepted rooms is reached. `ExtraEdgeCount` is the maximum number of discarded short edges put back after the backbone.

### RoomGeometry

When `RoomGeometry` is nil, the profile is width and height from `min(3, side)` to `min(9, side)`, `MaxFootprintCells` 81, `MinRoomGap` 1, and weights Rectangle 4, L 2, T 2, Cross 1, Circle 2. A shape with no legal size on that grid is dropped. Rectangle, including the 1×1 mask, always remains. An omitted profile and an explicit copy of that profile are the same request.

When the pointer is non-nil, the zeros below are not replaced.

| Go | ProtoJSON | Type | Zero value, if the struct is present | Accepted | Error |
| --- | --- | --- | --- | --- | --- |
| `MinWidth` | `min_width` | `uint32` | required | ≥ 1, and `MaxWidth` ≥ `MinWidth`, and `MaxWidth` ≤ `Width` | 0 → `ErrInvalidConfig`. `MaxWidth` < `MinWidth`, or `MaxWidth` > `Width`, → `ErrInvalidConfig` |
| `MaxWidth` | `max_width` | `uint32` | no default of its own | ≥ `MinWidth` and ≤ `Width` | see `MinWidth` |
| `MinHeight` | `min_height` | `uint32` | required | ≥ 1, and `MaxHeight` ≥ `MinHeight`, and `MaxHeight` ≤ `Height` | same rules on the Y axis |
| `MaxHeight` | `max_height` | `uint32` | no default of its own | ≥ `MinHeight` and ≤ `Height` | see `MinHeight` |
| `MaxFootprintCells` | `max_footprint_cells` | `uint32` | required; 0 is not the default 81 | 1..4,096 | 0 → `ErrInvalidConfig`. Above 4,096 → `ErrLimitExceeded` |
| `MinRoomGap` | `min_room_gap` | `uint32` | 0 is legal and does not become 1 | 0..256 | above 256 → `ErrInvalidConfig` |
| `Shapes` | `shapes` | `[]RoomShapeWeight` | empty is invalid | one or more, no duplicate shapes | empty, unknown, duplicate, a zero weight, or a weight sum above `math.MaxUint32` → `ErrInvalidConfig` |

`MinRoomGap` is the minimum number of empty layers between footprints, measured by Chebyshev distance between occupied cells. A shape and size that is not a legal mask, or that occupies more cells than `MaxFootprintCells`, is not a candidate. If no candidate remains, the error is `ErrInvalidConfig`.

### RoomShapeWeight

| Go | ProtoJSON | Type | Zero value | Accepted | Error |
| --- | --- | --- | --- | --- | --- |
| `Shape` | `shape` | `RoomShape` | `RoomShapeRectangle` only when that constant was intended | one of the five shapes | an unknown shape → `ErrInvalidConfig`. The same shape twice → `ErrInvalidConfig` |
| `Weight` | `weight` | `uint32` | 0 is invalid | 1 .. 2^32−1 | 0 → `ErrInvalidConfig`. The sum of weights above `math.MaxUint32` → `ErrInvalidConfig` |

### RoomRoleRequest

| Go | ProtoJSON | Type | Zero value | Accepted | Error |
| --- | --- | --- | --- | --- | --- |
| `Role` | `role` | `RoomRole` | not a default; `ROOM_ROLE_UNSPECIFIED` is rejected | `Start`, `Boss`, or `Treasure`, once each | unknown, or a repeated role, → `ErrInvalidConfig`. `Boss` without `Start` → `ErrInvalidConfig` |
| `Count` | `count` | `uint32` | 0 is an error for `Start` and `Boss`. For `Treasure`, 0 asks for none | `Start` and `Boss` are exactly 1. `Treasure` is 0..normalized `MaxRooms`. The sum of counts is ≤ that `MaxRooms` | otherwise `ErrInvalidConfig` |
| `RequiredTags` | `required_tags` | `[]string` | nil or empty adds no tag constraint | non-empty UTF-8 strings, no duplicates | an empty tag, invalid UTF-8, or a duplicate → `ErrInvalidConfig` |

`Start` is room `RoomID` 0. `Boss` is the unassigned room farthest from start. `Treasure` takes the next farthest rooms, up to `Count`. Tags restrict which catalog plant may be chosen. They do not change which room is chosen. A catalog that is well formed and still has no plant for a room's doors and tags fails later with `ErrNoCompatiblePlant`.

### DensityRegion

The rectangle is half-open: `Min` is inclusive and `Max` is exclusive, so it covers X in `[Min.X, Max.X)` and Y in `[Min.Y, Max.Y)`. A one-cell region is written with `Max = Min + (1, 1)`. `Max` may equal the grid dimension. A region replaces `Config.MinDistance` for room centers inside it. Unlike `Config.MinDistance`, a zero here is not turned into 6.

| Go | ProtoJSON | Type | Zero value | Accepted | Error |
| --- | --- | --- | --- | --- | --- |
| `Min` | `min` | `Cell` | no default | `X` ≥ 0, `Y` ≥ 0, and `Max` strictly greater on both axes | otherwise `ErrInvalidConfig` |
| `Max` | `max` | `Cell` | no default | exclusive corner, with `Max.X` ≤ `Width` and `Max.Y` ≤ `Height` | outside the grid → `ErrInvalidConfig` |
| `MinDistance` | `min_distance` | `float64` | 0 is an error, not 6.0 | finite and ≥ 1 | otherwise `ErrInvalidConfig` |

Two regions that share a cell return `ErrInvalidConfig`.

### PlantCatalog

Nil means the layout carries no plant metadata. Present means both lists exist, each with unique ids inside that list.

| Go | ProtoJSON | Type | Zero value | Accepted | Error |
| --- | --- | --- | --- | --- | --- |
| `Rooms` | `rooms` | `[]RoomPlant` | empty is invalid when the catalog is present | non-empty | `ErrInvalidConfig` |
| `Corridors` | `corridors` | `[]CorridorPlant` | empty is invalid when the catalog is present | non-empty | `ErrInvalidConfig` |

### RoomPlant

| Go | ProtoJSON | Type | Zero value | Accepted | Error |
| --- | --- | --- | --- | --- | --- |
| `ID` | `id` | `PlantID` (`string`) | empty is invalid | non-empty UTF-8, unique among `Rooms` | `ErrInvalidConfig` |
| `Tags` | `tags` | `[]string` | nil or empty is allowed | non-empty UTF-8 strings, no duplicates | `ErrInvalidConfig` |
| `Weight` | `weight` | `uint32` | 0 is invalid | 1 .. 2^32−1 | `ErrInvalidConfig` |
| `DoorDirections` | `door_directions` | `[]Direction` | empty is invalid | non-empty, no duplicates, each a real direction | `ErrInvalidConfig` |

A plant fits a room only when `DoorDirections` is a superset of the directions that room's doors use, and `Tags` contains that room's `RequiredTags`. Daedalus stores the id and the tags. The game resolves the id to a scene, prefab, tile, or mesh.

### CorridorPlant

| Go | ProtoJSON | Type | Zero value | Accepted | Error |
| --- | --- | --- | --- | --- | --- |
| `ID` | `id` | `PlantID` (`string`) | empty is invalid | non-empty UTF-8, unique among `Corridors` | `ErrInvalidConfig` |
| `Tags` | `tags` | `[]string` | nil or empty is allowed | non-empty UTF-8 strings, no duplicates | `ErrInvalidConfig` |
| `Weight` | `weight` | `uint32` | 0 is invalid | 1 .. 2^32−1 | `ErrInvalidConfig` |

A corridor plant has no geometry of its own. The corridor's cells are the geometry.

### RoomShape

No implicit rotation. `ROOM_SHAPE_UNSPECIFIED` is the proto zero and is rejected.

| Go | ProtoJSON | Mask |
| --- | --- | --- |
| `RoomShapeRectangle` | `ROOM_SHAPE_RECTANGLE` | the whole bounding box, including 1×1. Go zero value |
| `RoomShapeL` | `ROOM_SHAPE_L` | top row and left column. Both sides ≥ 2 |
| `RoomShapeT` | `ROOM_SHAPE_T` | top row and center column. Width ≥ 3, height ≥ 2 |
| `RoomShapeCross` | `ROOM_SHAPE_CROSS` | center row and center column. Both sides ≥ 3 |
| `RoomShapeCircle` | `ROOM_SHAPE_CIRCLE` | the disk inside a square. Width = height, odd, and ≥ 5 |

### RoomRole

`ROOM_ROLE_UNSPECIFIED` is the proto zero and is rejected. The Go zero value is `RoomRoleStart`.

| Go | ProtoJSON | Meaning |
| --- | --- | --- |
| `RoomRoleStart` | `ROOM_ROLE_START` | one room, `RoomID` 0. `Count` is 1 |
| `RoomRoleBoss` | `ROOM_ROLE_BOSS` | the unassigned room farthest from start. `Count` is 1, and `Start` must be requested too |
| `RoomRoleTreasure` | `ROOM_ROLE_TREASURE` | the next farthest unassigned rooms, up to `Count` |

### Direction

Diagonals are invalid. `DIRECTION_UNSPECIFIED` is the proto zero and is rejected. The Go zero value is `DirectionNorth`. Canonical order is North, East, South, West.

| Go | ProtoJSON | Vector |
| --- | --- | --- |
| `DirectionNorth` | `DIRECTION_NORTH` | `(0, −1)`, toward decreasing Y |
| `DirectionEast` | `DIRECTION_EAST` | `(1, 0)` |
| `DirectionSouth` | `DIRECTION_SOUTH` | `(0, 1)`, toward increasing Y |
| `DirectionWest` | `DIRECTION_WEST` | `(−1, 0)` |

### CorridorOrder

The Go zero value is `CorridorOrderXThenY`, and it is the default. Proto `CORRIDOR_ORDER_UNSPECIFIED` maps to that same default.

| Go | ProtoJSON | Bend from A to B |
| --- | --- | --- |
| `CorridorOrderXThenY` | `CORRIDOR_ORDER_X_THEN_Y` | X first, then Y. The bend is `(B.X, A.Y)` |
| `CorridorOrderYThenX` | `CORRIDOR_ORDER_Y_THEN_X` | Y first, then X. The bend is `(A.X, B.Y)` |

### Cell

A `Cell` is an integer grid coordinate, never a pixel. Canonical order is Y then X.

| Go | ProtoJSON | Type | Range |
| --- | --- | --- | --- |
| `X` | `x` | `int32` | `0` ≤ `X` < `Width` |
| `Y` | `y` | `int32` | `0` ≤ `Y` < `Height` |

## Response

`Layout` is the successful result. It carries the seed and the grid, not the rest of the request. Slices are allocated for that call, so changing one `Layout` does not change another.

Cells are row-major, Y then X: the state of `(x, y)` is `Grid.Cells[y*Width+x]`. `CellState.RoomID` is nil unless the cell is a room cell. `CorridorIDs` is non-empty only on corridor cells, in ascending id order, and one cell can belong to more than one corridor. A door belongs to one room and can serve several corridors. `Rooms` is in canonical `RoomID` order, and the corridor and door slices follow their own ids the same way. With one room there are no corridors. With n rooms, n > 1, there are at least n−1 corridors, and the graph is connected. `ExtraEdgeCount` 0 keeps it a tree.

The picture's layout is one witness: cell `(88, 66)` is a corridor cell with no `RoomID` and corridor ids 119 and 120, and door 138 belongs to room 113 at `(24, 75)`, faces east, and serves corridors 69 and 126.

### Layout

| Go | ProtoJSON | Type | Meaning |
| --- | --- | --- | --- |
| `Seed` | `seed` | `Seed` (`uint64`) | the seed that produced this layout. A decimal string in ProtoJSON |
| `Grid` | `grid` | `Grid` | the cell space |
| `Rooms` | `rooms` | `[]Room` | canonical `RoomID` order, which is creation order, starting at 0 |
| `Corridors` | `corridors` | `[]Corridor` | canonical `CorridorID` order, starting at 0 |
| `Doors` | `doors` | `[]Door` | canonical `DoorID` order, starting at 0 |

The HTTP debug response body is this message. gRPC returns it inside `GenerateResponse.layout`.

### Grid

| Go | ProtoJSON | Type | Meaning |
| --- | --- | --- | --- |
| `Width` | `width` | `uint32` | cells, 1..256 |
| `Height` | `height` | `uint32` | cells, 1..256 |
| `CellSize` | `cell_size` | `float64` | copied from `Config`. No algorithm converts it |
| `Cells` | `cells` | `[]CellState` | exactly `Width` × `Height` states, row-major |

### CellState

| Go | ProtoJSON | Type | Meaning |
| --- | --- | --- | --- |
| `At` | `at` | `Cell` | this cell's coordinate |
| `Kind` | `kind` | `CellKind` | empty, room, or corridor |
| `RoomID` | `room_id` | `*RoomID` | set if and only if `Kind` is `CellKindRoom`. Nil otherwise, and omitted from ProtoJSON when nil |
| `CorridorIDs` | `corridor_ids` | `[]CorridorID` | non-empty if and only if `Kind` is `CellKindCorridor`. Ascending. More than one id means the cell is shared |

### CellKind

The Go zero value is `CellKindEmpty`. `CELL_KIND_UNSPECIFIED` is the proto zero and is not produced.

| Go | ProtoJSON | Meaning |
| --- | --- | --- |
| `CellKindEmpty` | `CELL_KIND_EMPTY` | no room and no corridor. `RoomID` is nil and `CorridorIDs` is empty |
| `CellKindRoom` | `CELL_KIND_ROOM` | exactly one room. `RoomID` is set and `CorridorIDs` is empty |
| `CellKindCorridor` | `CELL_KIND_CORRIDOR` | an internal cell of one or more corridors. `RoomID` is nil |

### Room

| Go | ProtoJSON | Type | Meaning |
| --- | --- | --- | --- |
| `ID` | `id` | `RoomID` (`uint32`) | creation order, from 0 |
| `At` | `at` | `Cell` | the first occupied footprint cell in Y-then-X order |
| `Shape` | `shape` | `RoomShape` | the canonical mask |
| `Origin` | `origin` | `Cell` | bounding-box top-left. It may be unoccupied, as on a cross or a circle |
| `Width` | `width` | `uint32` | bounding-box width in cells |
| `Height` | `height` | `uint32` | bounding-box height in cells |
| `Cells` | `cells` | `[]Cell` | absolute footprint, Y then X |
| `Role` | `role` | `*RoomRole` | nil when no role was assigned. At most one role. Omitted from ProtoJSON when nil |
| `PlantID` | `plant_id` | `PlantID` | empty string when `PlantCatalog` is absent |
| `Tags` | `tags` | `[]string` | the selected plant's tags, or empty |
| `DoorIDs` | `door_ids` | `[]DoorID` | openings in cell `(Y, X)` order, then direction. Empty when the room has no corridor |

### Corridor

A corridor is a logical edge. Its cells may also appear on another corridor.

| Go | ProtoJSON | Type | Meaning |
| --- | --- | --- | --- |
| `ID` | `id` | `CorridorID` (`uint32`) | creation order, from 0 |
| `FromRoomID` | `from_room_id` | `RoomID` | source room, distinct from `ToRoomID` |
| `ToRoomID` | `to_room_id` | `RoomID` | destination room |
| `FromDoorID` | `from_door_id` | `DoorID` | door on the source room |
| `ToDoorID` | `to_door_id` | `DoorID` | door on the destination room |
| `Cells` | `cells` | `[]Cell` | internal cells from the From side to the To side, excluding both rooms. Empty when the rooms are adjacent. Always 4-connected |
| `PlantID` | `plant_id` | `PlantID` | empty string when there is no catalog |
| `Tags` | `tags` | `[]string` | the selected plant's tags, or empty |

### Door

A door is unique by `(RoomID, At, Direction)` even when several edges use it.

| Go | ProtoJSON | Type | Meaning |
| --- | --- | --- | --- |
| `ID` | `id` | `DoorID` (`uint32`) | creation order, from 0 |
| `RoomID` | `room_id` | `RoomID` | the one room that owns this opening |
| `At` | `at` | `Cell` | a boundary cell of that room's footprint |
| `Direction` | `direction` | `Direction` | the corridor's first step away from the room. If `Cells` is empty, the direction between the endpoints |
| `CorridorIDs` | `corridor_ids` | `[]CorridorID` | one or more edges that use this opening, ascending |

### A trimmed response

This is a real `Layout` from `Generator.Generate` with `Width` 20, `Height` 16, and `Seed` 7, serialized the way `POST /api/v1/generate` writes ProtoJSON (`UseProtoNames`, `EmitUnpopulated`). It has 4 rooms, 3 corridors, 6 doors, and 320 cells: 242 empty, 65 room, 13 corridor. The fragments below are cut from that response. Anything the sentences say was omitted is absent here and is not a value the API returns. `role` and `room_id` are omitted when nil; empty lists and the empty `plant_id` are present because nothing was selected.

Index 0 is empty. The first room cell in scan order is index 7, and it belongs to room 1, not room 0. Index 49 is a corridor cell:

```json
{"at": {"x": 0, "y": 0}, "kind": "CELL_KIND_EMPTY", "corridor_ids": []}
{"at": {"x": 7, "y": 0}, "kind": "CELL_KIND_ROOM", "room_id": 1, "corridor_ids": []}
{"at": {"x": 9, "y": 2}, "kind": "CELL_KIND_CORRIDOR", "corridor_ids": [1]}
```

Room 0 is the rectangle from the example (origin `(9, 7)`, 9×5, 45 cells). Its north door is door 0, at `(14, 7)`. The footprint starts and ends like this; the 41 cells between are omitted:

```json
{
  "id": 0,
  "at": {"x": 9, "y": 7},
  "plant_id": "",
  "tags": [],
  "door_ids": [0],
  "shape": "ROOM_SHAPE_RECTANGLE",
  "origin": {"x": 9, "y": 7},
  "width": 9,
  "height": 5,
  "cells": [{"x": 9, "y": 7}, {"x": 10, "y": 7}, {"x": 11, "y": 7}]
}
```

```json
{"x": 17, "y": 11}
```

Corridor 0 runs from that door toward room 3. Both internal cells are shown. Rooms 1–3, corridors 1–2, and doors 1–5 are omitted. The grid wrapper is `seed` `"7"`, `width` 20, `height` 16, `cell_size` 1.

```json
{
  "id": 0,
  "from_room_id": 0,
  "to_room_id": 3,
  "from_door_id": 0,
  "to_door_id": 1,
  "cells": [{"x": 14, "y": 6}, {"x": 14, "y": 5}],
  "plant_id": "",
  "tags": []
}
```

```json
{
  "id": 0,
  "room_id": 0,
  "at": {"x": 14, "y": 7},
  "direction": "DIRECTION_NORTH",
  "corridor_ids": [0]
}
```

## Three ways to run it

The same built-in generator is reachable as a library, as a gRPC subprocess, and through an opt-in local HTTP debug interface. Injected `Placer` and `Connector` implementations exist only in the SDK. gRPC and the debug server run `poisson_disk_rooms_v1` and `prim_rooms_v1`.

### SDK

Call `Generator.Generate` or `GenerateContext` as in the example above. A nil `Placer` selects `poisson_disk_rooms_v1`. A nil `Connector` selects `prim_rooms_v1`. The call returns one `Layout`, or an error and the zero `Layout`. Nothing is kept for a later request. The zero `Generator` is safe for simultaneous calls.

### Subprocess

`cmd/daedalus` is a gRPC server. It writes one JSON object to stdout, then a newline, and nothing else. Logs go to stderr. The client reads that line, then connects. A plain `go build` stamps `version` as `dev`. `make build` stamps `main.Version` from `git describe`.

```
go build -o bin/daedalus ./cmd/daedalus
./bin/daedalus
```

One run looked like this. The port and pid are assigned at startup; the default listen address is `127.0.0.1:0`.

```json
{"transport":"tcp","addr":"127.0.0.1:61207","pid":64602,"version":"dev"}
```

The service is `daedalus.v1.DaedalusService.Generate` (`proto/daedalus/v1/daedalus.proto`). TCP listens on a literal loopback address. `--transport` is `tcp` (the default) or `uds`. `--addr` defaults to `127.0.0.1:0`. `--max-concurrent-generations` defaults to `runtime.NumCPU()`.

### HTTP debug

The debug server is off unless requested. It binds a literal loopback address, has no authentication and no CORS, and is not meant to be exposed. A wildcard, a hostname, a non-loopback address, or port 0 is rejected. The default address is `127.0.0.1:8090`.

```
./bin/daedalus --http-debug-enabled --http-debug-addr=127.0.0.1:8090
```

`GET /` redirects to `/debug/`, which serves an embedded map viewer. `GET /healthz` returns JSON. `POST /api/v1/generate` runs the same generator as gRPC. The handshake line is unchanged, and the gRPC listener stays on its own address.

## Determinism

For the built-in algorithms, the same effective `Config` and the same `Seed` are specified to reproduce a `Layout` bit for bit for the whole v1 major, in the SDK and over gRPC, on amd64 and arm64. Effective config includes the normalized `RoomGeometry`, so an omitted geometry and an explicit copy of the dynamic profile are the same request. `CellSize` is copied onto the `Layout` and does not move a room.

Five independent SplitMix64 streams are derived from the seed: PlacementSeed, ConnectorSeed, RoomPlantSeed, RoomGeometrySeed, and CorridorPlantSeed. Consuming one does not advance the others. Products and sums that affect a candidate, a distance, a weight, a priority, or a tie-break stay in separate statements, so a fused multiply-add cannot move the result between architectures. The path does not call sine or cosine.

A change to that frozen output is a new major version, or a new algorithm id. `poisson_disk_rooms_v1` stays where it is. Pinning the module version is what keeps a replay stable.

`testdata/golden` holds the fixtures. `TestFrozenGoldenLayouts` compares each `Layout` to its fixture field by field. The CI job `determinism` in `.github/workflows/ci.yml` is written to run that suite on native amd64 (`ubuntu-latest`) and native arm64 (`ubuntu-24.04-arm`). This repository has no remote, so that job has never been scheduled. Locally, `TestFrozenGoldenLayouts` passed on a native arm64 binary and on an amd64 binary running under Rosetta, both against these fixtures. That is not a run of the CI job. A pass on one architecture does not replace the native runner of the other.

## Limits

Each grid side stops at 256. `MaxCells` is 65,536, the largest `Width` × `Height`. `MaxRooms` is 256 accepted rooms, not 256 occupied cells. `MaxFootprintCells` is 4,096 cells in one room when the caller sends a `RoomGeometry`; the default profile uses 81. Exceeding a product limit returns `ErrLimitExceeded` before allocation or any random draw. The request is not truncated.

Three release budgets describe the loads the built-in path is sized for. They are budgets, not timings measured here. The CI workflow records benchmark output and is not the authority for these absolute numbers. That workflow has not run: there is no remote.

| Load | Grid | Other inputs | Budget |
| --- | --- | --- | --- |
| Small | 64×64 | `MinDistance` 6, `MaxAttempts` 30, `MaxRooms` 128 | SDK p95 ≤ 20 ms |
| Typical | 128×128 | same distance and attempts, `MaxRooms` 256 | p95 ≤ 50 ms, p99 ≤ 100 ms |
| Maximum v1 | 256×256 | `MinDistance` 1, `MaxAttempts` 1024, `MaxRooms` 256, dynamic geometry, footprints, gap, cycles, roles, and density regions | p95 ≤ 500 ms, p99 ≤ 1 s |

## Extension points

`Placer` and `Connector` are the only strategy interfaces in v1. Each has a function adapter, `PlacerFunc` and `ConnectorFunc`, so a plain function or a closure is injectable without declaring a type. `Placer` returns room placements and does not assign plants, doors, or ids. `Connector` returns edges and does not route cells. The built-ins are `poisson_disk_rooms_v1` and `prim_rooms_v1`. The generator checks every placement and rejects a connection that is duplicated, a self-loop, unknown, or disconnected.

A plugin owns its determinism and its safety under concurrent calls. This package does not serialize a shared plugin. A plugin that is not deterministic makes the `Layout` not deterministic; that is the plugin's contract.

## Development

The Makefile targets are `generate`, `lint`, `test`, `bench`, `build`, and `build-all`.

- `generate` runs `buf generate`.
- `lint` runs `buf lint` and `golangci-lint`. The linter must be at least 2.14.0, the version CI uses.
- `test` runs `go test ./...`.
- `bench` runs `BenchmarkGenerate` for the three loads above, with `-benchmem`.
- `build` writes `bin/daedalus` and sets `main.Version` from `git describe`.
- `build-all` cross-compiles `linux/amd64`, `darwin/arm64`, and `windows/amd64`.

`generate` and `lint` need `buf` on `PATH` or in `GOBIN`. If the tool is missing, the target fails and prints the install command.

Regenerating `testdata/golden` is not a Make target. It rewrites the v1 fixtures and is reserved for a major-version change:

```
go test ./ -run '^TestFrozenGoldenLayouts$' -update -count=1
```

The same test without `-update` checks the fixtures in place:

```
go test ./ -run '^TestFrozenGoldenLayouts$' -count=1
```

## Licence

MIT. See [LICENSE](LICENSE).
