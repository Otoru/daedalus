# Benchmark reference

This measurement records a local reference for the three loads in section 11 of
the specification. This computer is **not** the reference hardware of the
release SLA. The numbers are regression evidence, not a guarantee.

## Environment

- Date: 2026-09-27
- CPU: Apple M4 Pro
- System: Darwin arm64
- Go: `go1.26.4`
- Command: `go test -run '^$' -bench '^BenchmarkGenerate$' -benchmem .`

That command is what `make bench` runs. It was executed three times in a row.
The paste below is the middle run by Maximum v1 time (83.947 ms, 85.964 ms,
88.202 ms), not the fastest.

## Budgets

Section 11 sets these p95 budgets on the declared reference hardware:

| Load | Sub-benchmark | p95 budget |
| --- | --- | --- |
| Small | `Small_seed_3` | 20 ms |
| Typical | `Typical_seed_11` | 50 ms |
| Maximum v1 | `Maximum_v1_seed_17` | 500 ms |

## Result

```text
go test -run '^$' -bench '^BenchmarkGenerate$' -benchmem .
goos: darwin
goarch: arm64
pkg: github.com/Otoru/daedalus
cpu: Apple M4 Pro
BenchmarkGenerate/Small_seed_3-14         	     831	   1372232 ns/op	 1132452 B/op	    2389 allocs/op
BenchmarkGenerate/Typical_seed_11-14      	     159	   7515675 ns/op	 4953022 B/op	    9754 allocs/op
BenchmarkGenerate/Maximum_v1_seed_17-14   	      13	  85964276 ns/op	12791910 B/op	   12338 allocs/op
PASS
ok  	github.com/Otoru/daedalus	4.696s
```

## Headroom

`ns/op` is the **mean** time per operation from that run, while the section 11
budgets are **p95**. Those are different statistics, so the table below places
them side by side rather than claiming the budget is met: a mean under the
budget does not by itself prove the 95th percentile is. The headroom happens to
be wide enough — the heaviest load uses 17% of its budget — that the distinction
does not change the conclusion here, but it would on a slower machine.

Section 11 also asks for a distribution derived from repeated samples isolated
per process, which `go test -bench` does not produce. Establishing the real p95
and p99 belongs to the release measurement on the declared reference hardware.

| Load | Measured | Budget | Headroom | Share of budget |
| --- | --- | --- | --- | --- |
| Small | 1.372 ms (1,372,232 ns) | 20 ms | 18.628 ms | 6.9% |
| Typical | 7.516 ms (7,515,675 ns) | 50 ms | 42.484 ms | 15.0% |
| Maximum v1 | 85.964 ms (85,964,276 ns) | 500 ms | 414.036 ms | 17.2% |
