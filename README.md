# Daedalus

Daedalus is a Go library that generates a discrete, deterministic 2D dungeon when a game asks for one. The caller passes a `Config` and a `Seed` to `Generator.Generate` and receives one immutable `Layout`: a grid of cells, rooms, corridors, and doors. The module path is `github.com/Otoru/daedalus`. It requires Go 1.25 and is MIT licensed.

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
