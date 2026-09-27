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

Daedalus does not:

- render, instantiate scenes or prefabs, load assets, or know Godot, Unity, or Bevy;
- compute pixel positions, build a navmesh, or hold game state;
- read a configuration file.

`CellSize` is an opaque unit defined by the caller and is only copied onto the `Layout`.

The root package imports only the Go standard library. A test parses that package's AST and rejects any import outside the standard library, so the generator embeds without pulling in gRPC, protobuf, or the process dependencies. Those live in `cmd/daedalus` and `internal/`. The long form of this contract — vocabulary, masks, pipeline, determinism, errors, concurrency — is the package comment in `doc.go`; `go doc .` prints it.

## Install

```
go get github.com/Otoru/daedalus
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

## Input

`Config` is the whole request. There is no configuration file. The zero `Config` is not valid: `Width` and `Height` have no default.

A compact request, with the fields a first-time caller actually sets:

```go
daedalus.Config{
	Width: 20, Height: 16, Seed: 7, // required; sides 1..256, product ≤ 65,536
	CellSize: 1.0, MinDistance: 6, MaxAttempts: 30, // 0 becomes 1.0, 6.0, 30
	MaxRooms: 128, ExtraEdgeCount: 6, // 0 becomes 256; 0 keeps the graph a tree
	CorridorOrder:    daedalus.CorridorOrderXThenY,
	RoomGeometry:     nil, // nil selects the dynamic profile
	RoomRoleRequests: nil, // nil or empty disables thematic rooms
	DensityRegions:   nil, // nil or empty uses one MinDistance for the whole grid
	PlantCatalog:     nil, // nil leaves every PlantID and Tags empty
}
```

Things callers get wrong, each verified against a generated `Layout`:

- a zero that becomes a default is not the same as a zero that is an error. `CellSize`, `MinDistance`, `MaxAttempts`, and `MaxRooms` normalize; `Width` and `Height` do not — zero there is `ErrInvalidConfig`, and a side above 256 is `ErrLimitExceeded`;
- a nil `RoomGeometry` is the dynamic profile — width and height from `min(3, side)` to `min(9, side)`, `MaxFootprintCells` 81, `MinRoomGap` 1, weights Rectangle 4, L 2, T 2, Cross 1, Circle 2 — **not** one-cell rooms. A present `RoomGeometry` is taken as sent; its zeros are not replaced;
- `seed` is a decimal **string** in ProtoJSON, because it is a `uint64`;
- the enum offset: `RoomShapeRectangle` is 0 in Go and `ROOM_SHAPE_RECTANGLE` is 1 on the wire, because proto reserves zero for `UNSPECIFIED`. The same applies to `RoomRole`, `Direction`, `CorridorOrder`, and `CellKind`;
- HTTP rejects any spelling but the proto name, so callers send `max_footprint_cells`, not `MaxFootprintCells` or `maxFootprintCells`;
- validation runs before any random draw. `ErrInvalidConfig` is a value outside its range; `ErrLimitExceeded` is a v1 product limit (`MaxCells` 65,536, `MaxRooms` 256, `MaxFootprintCells` 4,096). On gRPC those are `InvalidArgument` and `ResourceExhausted`.

For the field-by-field reference — every `Config`, `RoomGeometry`, `RoomShapeWeight`, `RoomRoleRequest`, `DensityRegion`, `PlantCatalog`, `RoomPlant`, `CorridorPlant`, `Layout`, `Grid`, `CellState`, `Room`, `Corridor`, `Door` field and every enum — run `go doc github.com/Otoru/daedalus`.

## Response

`Layout` is the successful result. It carries the seed and the grid, not the rest of the request. Slices are allocated per call, so changing one `Layout` does not change another.

- `Layout` holds `Seed`, `Grid`, `Rooms`, `Corridors`, `Doors`;
- the grid is row-major, Y then X: the state of `(x, y)` is `Grid.Cells[y*Width+x]`;
- a cell knows its `Kind` (empty, room, corridor) and, when it is a room cell, its `RoomID`. `CorridorIDs` is non-empty only on corridor cells, ascending, and one cell can belong to more than one corridor;
- a door belongs to one room and can serve several corridors;
- `Rooms` is in canonical `RoomID` order, which is creation order starting at 0. The corridor and door slices follow their own ids the same way. Row-major order is **not** `RoomID` order;
- with one room there are no corridors. With n rooms, n > 1, there are at least n−1 corridors, and the graph is connected. `ExtraEdgeCount` 0 keeps it a tree.

The picture's layout is one witness: cell `(88, 66)` is a corridor cell with no `RoomID` and corridor ids 119 and 120, and door 138 belongs to room 113 at `(24, 75)`, faces east, and serves corridors 69 and 126.

One short fragment of a real response, from `Generator.Generate` with `Width` 20, `Height` 16, `Seed` 7, serialized the way `POST /api/v1/generate` writes ProtoJSON (`UseProtoNames`, `EmitUnpopulated`). Index 0 is empty; index 7 is the first room cell in scan order and belongs to room 1, not room 0; index 49 is a corridor cell:

```json
{"at": {"x": 0, "y": 0}, "kind": "CELL_KIND_EMPTY", "corridor_ids": []}
{"at": {"x": 7, "y": 0}, "kind": "CELL_KIND_ROOM", "room_id": 1, "corridor_ids": []}
{"at": {"x": 9, "y": 2}, "kind": "CELL_KIND_CORRIDOR", "corridor_ids": [1]}
```

For the field-by-field reference, run `go doc github.com/Otoru/daedalus`.

## Three ways to run it

The same built-in generator is reachable as a library, a gRPC subprocess, and an opt-in local HTTP debug interface. gRPC and the debug server run `poisson_disk_rooms_v1` and `prim_rooms_v1`; injected `Placer` and `Connector` exist only in the SDK.

### SDK

- Call `Generator.Generate` or `GenerateContext` as in the example. A nil `Placer` selects `poisson_disk_rooms_v1`; a nil `Connector` selects `prim_rooms_v1`.
- The call returns one `Layout`, or an error and the zero `Layout`. Nothing is kept for a later request. The zero `Generator` is safe for simultaneous calls.

### Subprocess

- `cmd/daedalus` is a gRPC server. It writes one JSON object to stdout, then a newline, and nothing else. Logs go to stderr. The client reads that line, then connects. A plain `go build` stamps `version` as `dev`; `make build` stamps `main.Version` from `git describe`.

```
go build -o bin/daedalus ./cmd/daedalus
./bin/daedalus
```

One run looked like this. The port and pid are assigned at startup; the default listen address is `127.0.0.1:0`.

```json
{"transport":"tcp","addr":"127.0.0.1:61207","pid":64602,"version":"dev"}
```

- The service is `daedalus.v1.DaedalusService.Generate` (`proto/daedalus/v1/daedalus.proto`). TCP listens on a literal loopback address. `--transport` is `tcp` (the default) or `uds`. `--addr` defaults to `127.0.0.1:0`. `--max-concurrent-generations` defaults to `runtime.NumCPU()`.

### HTTP debug

- The debug server is off unless requested. It binds a literal loopback address, has no authentication and no CORS, and is not meant to be exposed. A wildcard, a hostname, a non-loopback address, or port 0 is rejected. The default address is `127.0.0.1:8090`.

```
./bin/daedalus --http-debug-enabled --http-debug-addr=127.0.0.1:8090
```

- `GET /` redirects to `/debug/`, an embedded map viewer. `GET /healthz` returns JSON. `POST /api/v1/generate` runs the same generator as gRPC. The handshake line is unchanged, and the gRPC listener stays on its own address.

## Determinism

For the built-in algorithms, the same effective `Config` and `Seed` reproduce a `Layout` bit for bit for the whole v1 major, in the SDK and over gRPC, on amd64 and arm64. Effective config includes the normalized `RoomGeometry`, so an omitted geometry and an explicit copy of the dynamic profile are the same request. `CellSize` is copied onto the `Layout` and does not move a room.

- Five independent SplitMix64 streams are derived from the seed: `PlacementSeed`, `ConnectorSeed`, `RoomPlantSeed`, `RoomGeometrySeed`, and `CorridorPlantSeed`. Consuming one does not advance the others.
- Products and sums that affect a candidate, a distance, a weight, a priority, or a tie-break stay in separate statements, so a fused multiply-add cannot move the result between architectures. The path does not call sine or cosine.
- A change to that frozen output is a new major version, or a new algorithm id. `poisson_disk_rooms_v1` stays where it is. Pinning the module version is what keeps a replay stable.
- `testdata/golden` holds the fixtures. `TestFrozenGoldenLayouts` compares each `Layout` to its fixture field by field.
- The CI job `determinism` in `.github/workflows/ci.yml` is written to run that suite on native amd64 (`ubuntu-latest`) and native arm64 (`ubuntu-24.04-arm`). This repository has no remote, so that job has never been scheduled. Locally, `TestFrozenGoldenLayouts` passed on a native arm64 binary and on an amd64 binary running under Rosetta, both against these fixtures. That is not a run of the CI job. A pass on one architecture does not replace the native runner of the other.

## Limits

- Each grid side stops at 256. `MaxCells` is 65,536, the largest `Width` × `Height`.
- `MaxRooms` is 256 accepted rooms, not 256 occupied cells.
- `MaxFootprintCells` is 4,096 cells in one room when the caller sends a `RoomGeometry`; the default profile uses 81.
- Exceeding a product limit returns `ErrLimitExceeded` before allocation or any random draw. The request is not truncated.

Three release budgets describe the loads the built-in path is sized for. They are budgets, not timings measured here. The CI workflow records benchmark output and is not the authority for these absolute numbers. That workflow has not run: there is no remote.

- **Small** — 64×64, `MinDistance` 6, `MaxAttempts` 30, `MaxRooms` 128: SDK p95 ≤ 20 ms.
- **Typical** — 128×128, same distance and attempts, `MaxRooms` 256: p95 ≤ 50 ms, p99 ≤ 100 ms.
- **Maximum v1** — 256×256, `MinDistance` 1, `MaxAttempts` 1024, `MaxRooms` 256, dynamic geometry, footprints, gap, cycles, roles, and density regions: p95 ≤ 500 ms, p99 ≤ 1 s.

## Extension points

- `Placer` and `Connector` are the only strategy interfaces in v1. Each has a function adapter, `PlacerFunc` and `ConnectorFunc`, so a plain function or a closure is injectable without declaring a type.
- `Placer` returns room placements and does not assign plants, doors, or ids. `Connector` returns edges and does not route cells. The built-ins are `poisson_disk_rooms_v1` and `prim_rooms_v1`.
- The generator checks every placement and rejects a connection that is duplicated, a self-loop, unknown, or disconnected.
- A plugin owns its determinism and its safety under concurrent calls. This package does not serialize a shared plugin. A plugin that is not deterministic makes the `Layout` not deterministic; that is the plugin's contract.

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

Without `-update` the same test checks the fixtures in place.

## Licence

MIT. See [LICENSE](LICENSE).
