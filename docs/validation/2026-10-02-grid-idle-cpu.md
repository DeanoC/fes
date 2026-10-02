# Grid idle CPU diagnostic — 2026-10-02

Scope: FogCast `cmd/fogcast-kit`, completing the grid follow-up to
[FES #318](https://github.com/DeanoC/fes/issues/318). Base:
`4e0524ca931538f65c7b58071f630e3f85a99d2d`; result is the uncommitted task
worktree diff. Tenfoot already has the revision and complete-scene cache
described in the [September diagnostic](2026-09-30-tenfoot-cpu-backend.md).
No shared contract, runtime, FPGA, image selection or default changes.

## Change

The grid's unchanged render key now skips drawing for up to one second rather
than 100 ms. Known scene changes, artwork generations and animation ticks still
draw immediately. The first settled frame after animation draws as well. The
bounded refresh captures fields outside the render key, such as title changes.
Input, model updates and asynchronous fetches retain their existing cadence.

Grid menu-display explicitly enables change-driven submission. Each rendered
frame advances one revision; skipped ticks call `PresentRevision` with the
retained revision to service generation probes, failure retries and Resume
without scanning/copying pixels. Linuxfb skipped ticks do not present.

## ARM diagnostic

Designated MiSTer Pi `192.168.10.84`, Cortex-A9 ARMv7, Go module-selected
toolchain, `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`. Existing target lease
owner: `fes-318-grid-cpu`. Only private binaries under
`/tmp/fes-318-grid-17a49c0d` were executed; those files were removed afterward.
The lease was released and confirmed free. No installed executable or service was replaced. The installed tenfoot service
remained running and competed with the benchmarks. This is CPU diagnostic
evidence, not deployed-grid or exact-image acceptance.

Two runs, `-test.benchtime=2s -test.benchmem -test.count=2`:

| Benchmark | Elapsed per operation | Allocation per operation |
| --- | ---: | ---: |
| Grid, prior 100 ms refresh gate | 9.456–9.499 ms | 18.6–18.8 kB, 91–92 allocations |
| Grid, one-second idle refresh | 1.228–1.233 ms | 2.4 kB, 11 allocations |
| Unchanged menu revision | 1.191–1.213 microseconds | zero |

`BenchmarkKitGridIdle` paints a 1280×720 placeholder grid through the real
`fbgrid.Paint` and identity-chrome path. It includes periodic refreshes over
simulated 30 Hz ticks. Both gates run the current rasterizer; this isolates the
refresh policy change. The fixture excludes host/model work, real artwork,
socket transfer and physical presentation. It is not a process CPU measurement.

A separate, benchmark-free ten-second sample of the installed tenfoot PID 664
advanced user/system CPU from 79458/12657 to 79580/12688 ticks over 10.02 seconds
of target uptime. With USER_HZ=100 this is **0.153 cores**. The installed binary
SHA-256 was `2fa158c84dee25dae7275ba2262937fa0a88d25e318835bc7775b29f07adf496`.
Its command line selected `-home rooms`; the visible scene was not captured.
This does not compare scenes or qualify the changed grid. There was no running
grid process to sample; the earlier grid process measurement remains in the
September record.

## Validation and artifacts

Passed focused gfx, tenfoot, kitlauncher, fbgrid and fogcast-kit tests; race tests
for gfx and fogcast-kit; `go vet ./cmd/fogcast-kit`; ARMv7 launcher cross-build;
`make check-generated` (15 generated files and 32 fixture copies); and
`git diff --check`. Final ARM tests passed unchanged scene invalidation,
animation settlement, linuxfb presentation suppression, idle menu generation
recovery and Pause/Resume. Existing gfx ARM tests passed revision retries and
pooled-buffer immutability.

SHA-256:

- Grid benchmark test: `3e4cf07da06b08a761feb6c8823f1967f45bf9fd648206a0d51653f31bceea9a`
- Final grid test (adds generation/Resume coverage): `f50c2b02540e43de6f2a0168ba0269deebcc357e1c53ee428c174834310a69ae`
- Gfx test: `51770ed1eaf3fb5b036b8bc2efc766b97aa9ecffa72c479a078133cce7939312`
- Cross-built grid launcher: `7d4d1c0fb13b31005d511c9368ac57b6fb8bb31e514d33c3c8e6c81a4fbf3e54`

Local ignored logs/binaries are in `out/fes-318/`. No cold image build or physical
HDMI acceptance was performed. Next integration step: select reviewed committed
bytes, coordinate a grid service diagnostic under the existing lease, and
measure its actual idle/navigation CPU with real artwork and HDMI recovery.
