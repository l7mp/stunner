# Troubleshooting

## Profiling the Gateway Operator

Use the integration benchmark in `stunner-gateway-operator` to get a quick baseline for operator bootstrap and reconciliation cost.

### Run the bootstrap benchmark

```bash
cd ../stunner-gateway-operator
make bench BENCH=BenchmarkBootstrap BENCHTIME=25x COUNT=1
```

Notes:
- `BENCHTIME=25x` means 25 iterations, and each iteration inserts one full Gateway/GatewayClass/UDPRoute/StaticService combo.
- The benchmark prints per-iteration timings (`iteration=NN duration=...`) so you can see growth in cost as state size increases.

### Capture a CPU profile

```bash
cd ../stunner-gateway-operator
make bench-cpu BENCH=BenchmarkBootstrap BENCHTIME=25x COUNT=1 CPUPROFILE=bench.cpu.pprof
```

Inspect profile hotspots:

```bash
go tool pprof -top ./test.test bench.cpu.pprof
```

Open interactive flamegraph UI:

```bash
go tool pprof -http=:8080 ./test.test bench.cpu.pprof
```

### What to look for

- Rising per-iteration durations usually indicate state-of-the-world work scaling with object count.
- High CPU in status update paths can indicate too many no-op status writes.
- Compare before/after runs with identical `BENCHTIME` and `COUNT` when validating optimizations.
