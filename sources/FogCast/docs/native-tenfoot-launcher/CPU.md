# CPU rendering

Tenfoot's direct `linuxfb` and `menu-display` shells keep the 30 Hz input and
application update loop. `gfx.FrameCache` records complete draw commands and
compares them with the last scene. Identical commands and unchanged textures skip
rasterization; incremental scenes without an initial clear always render. Texture
updates and destruction preserve draw order and invalidate reuse, including the
frame after a mutation that follows an earlier draw. Script updates and snapshots
continue even when their visual output is identical.

Menu-display uses rendered-frame revisions to skip full-frame byte comparisons.
An unchanged revision still services the existing generation/status probes and
retry schedule. Callers that do not supply revisions keep byte-based detection.
Queued, in-flight, retained and probe frames share immutable buffers; references
release storage into a bounded free pool. A changed frame still copies and sends
all 1280×720 RGBA pixels. There is no runtime protocol or FPGA change.

The software rasterizer classifies opaque textures on upload/update. Opaque 1:1
sprites copy clipped rows. Scaling computes source columns once with the original
float32 sampling expression, preserving nearest-neighbour boundaries. Repeated
resizes are cached within 8 MiB and 64 entries; updates/destruction invalidate the
texture's entries. Resized bitmaps cover the visible integer raster bounds and
use the original sampling expression even for fractional source/destination
rectangles. First-use and continuously updated textures use the general sampler
to avoid allocating a resize bitmap for every video frame. Large
constant-color alpha fills use exact channel lookup tables. Text blits borrow
pixels during the synchronous draw rather than cloning them.

## Profiling

From `sources/FogCast`, run the actual launcher with:

```sh
fogcast-tenfoot -gfx menu-display \
  -cpu-profile /tmp/tenfoot-cpu.pprof \
  -heap-profile /tmp/tenfoot-heap.pprof
```

Exit normally or with SIGTERM, then inspect with matching binary bytes:

```sh
go tool pprof -top fogcast-tenfoot /tmp/tenfoot-cpu.pprof
go tool pprof -alloc_space fogcast-tenfoot /tmp/tenfoot-heap.pprof
```

CPU sampling stops and its file closes before the forced GC and heap capture,
so shutdown heap collection is excluded from the CPU profile.

Profiles are opt-in files, not HTTP endpoints. For comparable captures, record the
scene, input activity, duration, selected binary hash, kit CPU/load and competing
processes. Separate idle browse, navigation, overlays, room animation and video;
keep decoding and physical presentation visible in actual-process profiles.

## Benchmarks

```sh
go test ./ui/gfx -run '^$' -bench 'Benchmark(Software|MenuDisplay|FrameCache)' -benchmem
go test ./ui/tenfoot -run '^$' -bench BenchmarkCPUBackend -benchmem
```

`BenchmarkCPUBackend` includes App Tick, Snapshot, texture synchronization and
real scene drawing with warmed generated covers. It excludes network operations,
asset decode and physical presentation. Its Room case replays a synthetic room
display list; it does not measure Lua execution. Static Attract measures a still,
not video decode. Navigation changes visible focus each iteration.

Cross-build the same tests for ARMv7:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go test -c ./ui/gfx -o /tmp/gfx-arm.test
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go test -c ./ui/tenfoot -o /tmp/tenfoot-arm.test
```

Claim the designated kit using the existing FES kit lease before executing them.
Copy to private `/tmp` paths and run the test binaries with the corresponding
`-test.run`, `-test.bench`, `-test.benchmem` and `-test.cpuprofile` flags. These CPU
benchmarks do not program an FPGA, replace services or qualify a factory image.

## Diagnostic measurements

ARMv7 measurements and qualification limits are recorded in the FES
[CPU-backend diagnostic](../../../../docs/validation/2026-09-30-tenfoot-cpu-backend.md).
Kit-wide CPU, memory, and storage budgets for the local host and shell are in
[kit resource limits](../kit-resource-limits.md). That collector is read-only
and is separate from these opt-in profiles.
