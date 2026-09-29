# Contributing

## Tooling

- Go 1.25 or newer, as declared in `go.mod`.
- [`buf`](https://buf.build/docs/installation) on `PATH` or in `GOBIN`, for the protobuf targets.
- `golangci-lint` 2.14.0 or newer — the version CI pins. An older linter misses findings that fail the build.

## Make targets

- `make generate` runs `buf generate`. The generated bindings under `internal/gen` are committed, and CI fails if regenerating them produces a diff.
- `make lint` runs `buf lint` and `golangci-lint`.
- `make test` runs `go test ./...`.
- `make bench` runs `BenchmarkGenerate`, `BenchmarkBuildGatingPlan`, `BenchmarkComputeSteps`, and the three `BenchmarkComputeVisibility` loads, with `-benchmem`; it also runs the warm `AnswerInto` visibility benchmark.
- `make bench-freeze` runs the release benchmark sweep with `-benchtime 10x` through `rtk proxy`, including every package; record `ns/op`, `B/op`, and `allocs/op` with the machine and Go version.
- `make build` writes `bin/daedalus` and stamps `main.Version` from `git describe`.
- `make build-all` cross-compiles `linux/amd64`, `darwin/arm64` and `windows/amd64`.

## Frozen output

The same effective `Config` and `Seed` must reproduce a `Layout` bit for bit for the whole v1 major, in the SDK and over gRPC, on amd64 and arm64. `testdata/golden` holds the fixtures and `TestFrozenGoldenLayouts` compares each `Layout` to its fixture field by field.

Check the fixtures in place:

```
go test ./ -run '^TestFrozenGoldenLayouts$' -count=1
```

Regenerating them is deliberately **not** a Make target. It rewrites the v1 fixtures, which is a major-version decision, not a way to make a red test go green:

```
go test ./ -run '^TestFrozenGoldenLayouts$' -update -count=1
```

### utils/pathfinding/testdata/golden

`TestFrozenGoldenFields` freezes the navigation choices the distance array does not decide: the North, East, South, West step tie-break, and the flee composition (scale by −12/10, truncation toward zero, then rescan). The distance array is stored in the same files. The dungeon fixtures above and these navigation fixtures move independently.

Check the fixtures in place:

```
go test ./utils/pathfinding -run '^TestFrozenGoldenFields$' -count=1
```

Regenerating them is deliberately **not** a Make target. It records a reviewed result, not a way to make a red test go green:

```
go test ./utils/pathfinding -run '^TestFrozenGoldenFields$' -update -count=1
```

If a change makes the goldens fail, the question is whether the change was meant to alter observable output. If it was not, the change is wrong. If it was, it needs a new major version or a new algorithm id.

### utils/vision/testdata/golden

`TestFrozenVisionGoldens` stores transparency and visibility as readable rows of `.` and `#`. A failure names the first differing `(x,y)` cell and both bits. Check the fixtures with:

```
go test ./utils/vision -run '^TestFrozenVisionGoldens$' -count=1
```

There is no Make target for rewriting them. After an intentional, reviewed visibility algorithm change, regenerate explicitly with `-update-vision`; changing a v1 fixture requires a new major version or an opt-in algorithm ID.

Do not edit existing fixtures while integrating a feature. Run the frozen
golden commands below first; if an output is intentionally changing, add a
reviewed fixture and a new algorithm or major-version decision rather than
using an update flag to make the suite green.

Two traps the code guards against, worth knowing before touching the hot path:

- products and sums that feed a candidate, a distance, a weight, a priority or a tie-break stay in separate statements, because Go may contract `a*b+c` into a fused multiply-add on arm64 and not on amd64;
- the generation path calls no trigonometry.

## Import purity

The root package imports **only** the standard library, and `TestRootPackageImportsOnlyStandardLibrary` parses its AST to enforce it. gRPC, protobuf, fx, zap and the generated bindings belong in `cmd/daedalus` and `internal/`. A dependency added to the root package fails the suite, by design.

`utils/gating` is likewise limited to the root SDK and the standard library. Its
`BenchmarkBuildGatingPlan` loads are small (32 Rooms), typical (128 Rooms), and
maximum v1 (256 Rooms); benchmark output is evidence to record with the CPU and
Go version before treating the proposed budgets as a release gate.

Optional gating consumes thematic capacity: callers requesting optional gates
must request enough `RoomRoleTreasure` Rooms during generation. The request is
still exact. A Layout can have many bridges and fail because Treasure branches
or distinct reachable key Rooms are exhausted; callers should treat
`ErrInsufficientGates` and its diagnostic (`no treasure branches` or `no
distinct reachable key room`) as a generation/retry signal, not as a partial
plan.

The generated-path gating measurement uses fixed 3×3-room layouts, 20 seeds
per cell, and eight or 24 requested Treasure Rooms:

| Load | 1+1 | 2+2 | 4+4 | 8+8 |
| --- | ---: | ---: | ---: | ---: |
| 64×64 / 64 Rooms / 8 Treasure | 20/20 | 20/20 | 16/20 | 5/20 |
| 96×96 / 128 Rooms / 8 Treasure | 20/20 | 20/20 | 20/20 | 9/20 |
| 96×96 / 128 Rooms / 24 Treasure | 20/20 | 20/20 | 20/20 | 8/20 |
| 128×128 / 256 Rooms / 24 Treasure | 20/20 | 20/20 | 20/20 | 16/20 |

The earlier degradation was a bug: optional selection could lock an
unselected bridge on the mandatory target path. Optional candidates now
protect the complete target path. The remaining 8+8 failures are
`no distinct reachable key room`; the measured runs had zero shared-Door,
bridge-capacity, and Treasure-capacity failures.
