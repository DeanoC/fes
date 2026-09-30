# Tenfoot CPU-backend diagnostic — 2026-09-30

Scope: FogCast software rasterization, the tenfoot framebuffer scene loop, and
menu-display submission. Base: `17929b2bd5636655586e47f121d44242e5519446`.
Issue: [FES #318](https://github.com/DeanoC/fes/issues/318).
This is a working-tree CPU diagnostic, not installed-launcher or image acceptance.
No runtime, FPGA, shared schema or wire-contract changes are included.

## Method

Go 1.24.4, `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`, designated MiSTer Pi
`192.168.10.84`, dual Cortex-A9 ARMv7. The existing kit lease was held by
`fes-318-cpu`. Only private test binaries under `/tmp` were executed. Installed
services, FPGA and image stayed unchanged. The installed grid remained running,
so these elapsed times include competing CPU load and should not be read as
isolated microarchitecture measurements. The target clock is unset; profile
wall-clock dates show 1970 and do not identify the capture date.

Final scene fixtures use 48 generated opaque covers, real App Tick/Snapshot,
texture synchronization and rendering at 1280×720. Three warm frames precede
measurement. Settings is already hydrated; network requests are excluded.
Navigation cycles visible focus. Room is a synthetic display list, without Lua;
Attract is a still, without video decode. Neither includes physical presentation.
The same fixture helpers were added to the baseline archive without changing its
production rasterizer. Two runs use `-test.benchtime=1s -test.benchmem`.

CPU profiles of the original scene benchmark found `Software.Draw` at 53.55%
cumulative CPU, `sampleCoord` at 32.43%, and `blendOver` at 10.73% flat. Final
mixed-scene profiling shifts cost toward memory copies (33.74% flat) and fills
(17.77% cumulative). These profiles include fixture setup/calibration and several
scenes; they are not live idle-launcher percentages.

## ARM results

Elapsed time per operation, ranges across two runs. The optimized immediate
column isolates raster changes; cached is the production scene-cache path.

| Scene | Original immediate | Optimized immediate | Optimized cached |
| --- | ---: | ---: | ---: |
| Static browse | 131.46–134.25 ms | 19.77–20.65 ms | 0.616–0.665 ms |
| Navigation | 120.80–125.93 ms | 19.52–23.29 ms | 20.16–21.93 ms |
| Static settings | 266.44–266.84 ms | 100.69–108.30 ms | 1.365–1.467 ms |
| Static room | 6.44–7.13 ms | 6.65–6.67 ms | 0.471–0.497 ms |
| Static attract still | 112.30–116.69 ms | 10.07–10.43 ms | 0.263–0.311 ms |

Separate menu-display benchmarks use a fake presenter. Original byte comparison
cost 13.43–14.04 ms for unchanged frames. Revision detection costs
**1.335–1.501 microseconds**, zero allocations. Changed-frame revision submission
still costs 16.45–18.53 ms and one small 48-byte allocation; original copy plus
comparison cost 34.92–36.32 ms and approximately 3.69 MB allocated. These timings
exclude socket transfer, runtime work and the physical frame boundary.

Sprite microbenchmarks: opaque 1:1 draws fell from 5.15–5.86 ms to
0.913–0.957 ms; warm opaque scaling from 53.19–54.96 ms to 1.923–2.194 ms.
Full-screen constant alpha fill fell from 142.6–143.9 ms to 72.68–75.95 ms.
Streaming texture update plus draw remains on the general path (7.75–8.96 ms,
262208 bytes and two allocations); no resized bitmap is allocated per video
frame. Texture opacity scanning adds upload work, amortized by repeated draws.

The installed grid process was separately sampled at **1.267 cores** over
10.25 seconds (1299 CPU ticks, USER_HZ=100), with no benchmark running. It was not
replaced. These results do not claim a reduction of the installed grid or the
actual tenfoot process's idle CPU usage.

## Implementation and validation

Complete unchanged command lists skip rasterization while input, scripts and
snapshots continue. Texture mutations invalidate reuse and preserve ordering.
Rendered revisions avoid byte scans while servicing generation probes, retries,
and pause/resume. Submission buffers remain immutable while queued/in flight and
are recycled through a bounded pool. Opaque row copies, cached exact fractional
nearest-neighbour resizes (8 MiB / 64 entries), exact alpha lookup tables and
borrowed synchronous text pixels reduce active raster cost.

Validation passed:

- Focused FogCast gfx, tenfoot and launcher tests with the race detector.
- Random scalar pixel equivalence, exhaustive alpha arithmetic, clipped/fractional
  geometry, texture update/destruction, cache eviction, streaming/moving sprites.
- Scene pixel equivalence through focus, settings, status, artwork, layout,
  attract updates, room animation and room exit.
- Revision generation/retry/pause tests and queued/in-flight buffer immutability.
- `make test-changed TEST_CHANGED_ARGS='--base 17929b2bd5636655586e47f121d44242e5519446'`:
  affected host/race suites, appliance tests, generated consumers and parent
  consistency checks passed. Parent suite: 588 tests, 39 documented skips;
  browser suite skipped two Chrome cases because Chrome is unavailable.
- ARMv7 test executables ran successfully; launcher cross-compiled. No cold image
  build or exact-artifact HDMI acceptance was performed.

## Artifact identities

SHA-256 of the binaries used for the selected measurements and launcher build:

- `gfx-before.test`: `df13b03c856d3223f767bdf57f6bfec065abe7e8969a5bd999c03eb53192154d`
- `gfx-selected.test`: `8a401c3ce12b2c7b347622b65d6d7582d13747a19f49202451250d23dd146aeb`
- `tenfoot-baseline-warm.test`: `ee50dd172dd29fc078d3695eac9a94d4f7e527d48a417f6a34824d9c1887434c`
- `tenfoot-warm.test`: `a786f43a06c0cd42f1c40276c21a3eb541c66030e08ecd95b5c0d73a9aa70061`
- `fogcast-tenfoot`: `fcc780bfc775cbef29ff9a837684f3a80e8dcd2ec9995dd7ff8f221d898f7e4b`

Local ignored receipts live in `out/cpu-backend/`: `manifest.json`,
`arm-final-scenes.txt`, `arm-selected.txt`, baseline/expanded benchmark logs,
`final-scene-arm.pprof`, original CPU profiles, `grid-idle.json`, and
`test-changed.log`. Final binary measurements precede a formatting-only change
in FrameCache; the production behavior is identical. The manifest records
production source hashes and the base commit.

## Remaining acceptance and next pass

Unchanged detection meets the issue's 5 ms bar in this diagnostic. Changed
settings redraws still miss a 30 Hz budget; changed frames still copy and send
all 3.69 MB. Cached overlay layers or bounded damage regions are candidates for
another measured pass. Streaming video, Lua animation and actual runtime
presentation need actual-process profiles before making further claims.

Next integration step: select the committed FogCast bytes, run the launcher under
the existing kit lease with real artwork/input/video, verify displayed pixels,
generation recovery and pause/resume, and measure idle/navigation process CPU.
Only that qualifies the selected artifact for hardware use or a default change.
The grid default is unchanged.
