# Referência de benchmark

Esta medição registra uma referência local para as três cargas da seção 11 da
especificação. Ela serve como evidência de regressão e **não** declara este
computador como o hardware de referência do SLA de release.

## Ambiente

- Data: 2026-09-27
- CPU: Apple M4 Pro
- Sistema: Darwin arm64
- Go: `go1.26.4`
- Comando: `make bench`

## Resultado

```text
go test -run '^$' -bench '^BenchmarkGenerate$' -benchmem .
goos: darwin
goarch: arm64
pkg: github.com/Otoru/daedalus
cpu: Apple M4 Pro
BenchmarkGenerate/Pequena_seed_3-14         	     921	   1273267 ns/op	 1131573 B/op	    2386 allocs/op
BenchmarkGenerate/Tipica_seed_11-14         	     162	   7315577 ns/op	 4952200 B/op	    9751 allocs/op
BenchmarkGenerate/Maxima_v1_seed_17-14      	      14	  81047458 ns/op	12791012 B/op	   12335 allocs/op
PASS
ok  	github.com/Otoru/daedalus	4.778s
```
