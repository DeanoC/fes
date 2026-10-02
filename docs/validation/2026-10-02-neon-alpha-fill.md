# NEON alpha-fill diagnostic — 2026-10-02

Scope: FogCast software rasterizer's large constant-colour alpha fills on
32-bit ARM. Base: `4e0524ca931538f65c7b58071f630e3f85a99d2d`; result is the
uncommitted task worktree diff. This extends the earlier
[settings raster investigation](2026-10-02-settings-raster-cost.md) and preserves
the separate grid idle changes. Graphics API, runtime, FPGA, shared contracts,
external build dependencies and image/default selection are unchanged.

## Kernel and dispatch

`ui/gfx/alpha_fill_arm.go` checks the OS-observed `cpu.ARM.HasNEON` from the
already-selected `golang.org/x/sys/cpu` module. Large alpha fills use NEON only
when eight pixels fit in a row. Small fills, narrow rows, other architectures
and unavailable/disabled NEON retain the scalar path. Each clipped row passes
complete 32-byte blocks to assembly; up to seven remaining pixels use the
existing scalar blend. Calls are bounded to one row and allocate no memory.

The ARM assembly kernel handles eight RGBA pixels per iteration with
unaligned-safe vector loads/stores. Widening unsigned-byte multiplies add the
constant source contributions in unsigned 16-bit lanes. For colour channels,
`x = source*A + destination*(255-A)`; for destination alpha,
`x = A*255 + destination_alpha*(255-A)`. Every numerator is at most 65025.
Biasing the source contributions by one gives `y=x+1`, then
`floor(x/255) = (y + (y >> 8)) >> 8`, exactly. The shifted sum fits 16 bits.
The kernel preserves the existing truncation and destination-alpha semantics;
there is no `/256` approximation or FPSCR modification.

Go's ARM assembler does not expose these NEON mnemonics. Instruction WORDs
carry their mnemonic comments and were checked with GNU binutils 2.44
(`arm-linux-gnueabihf-as -march=armv7-a` and objdump). This development tool is
not needed to build the checked-in Go assembly. Production remains CGO-free.

## ARM measurements

Designated MiSTer Pi `192.168.10.84`, dual Cortex-A9 at 800 MHz with performance
governor. `/proc/cpuinfo` advertises `neon vfpv3 vfpd32`. Go 1.26.5,
`CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`. The existing lease owner was
`fes-318-neon`. Private binaries/profiles used `/tmp/fes-318-neon-17a49c0d`.
Installed tenfoot remained running and competed for CPU/memory bandwidth.
No installed executable, service or image was replaced.

Two benchmark runs, `-test.benchtime=2s -test.count=2 -test.benchmem`:

| Operation | Scalar | NEON |
| --- | ---: | ---: |
| Full 1280×720 alpha fill | 39.94–40.44 ms | 11.27–11.80 ms |
| Warm immediate settings redraw | 52.04–52.88 ms | 34.22–37.13 ms |
| Settings navigation through production scene cache | earlier 52.43–53.00 ms | 37.37–37.77 ms |
| Static cached settings | earlier 0.989–1.017 ms | 0.988–1.005 ms |

Scalar and NEON fill timings use the same final binary, with dispatch disabled
by `GODEBUG=cpu.neon=off` for the scalar run. Go's runtime prints an unknown
feature-name notice on ARM, but x/sys recognizes this option: direct NEON tests
skip and exhaustive renderer tests pass through the scalar fallback. The
paired scalar settings runs use that same switch; a separate four-second NEON
profile measured 37.18 ms. Scene fixture and physical-presentation exclusions
are unchanged from the settings investigation.

Large fills improve about 3.5× with zero allocations. The final amd64 fill
benchmark remains around 0.524 ms (previous diagnostic 0.499 ms); it retains
the indexed scalar kernel. This is an individual host diagnostic, not a claim
of a statistically significant change.

The settings profile now has `runtime.memmove` at 48.71% flat CPU, NEON at
16.59%, scalar `blendOver` at 9.48%, and `blitCopy` at 9.38% flat / 32.33%
cumulative. Dimming is no longer the largest flat cost. Settings navigation
still exceeds a 33.3 ms frame budget. The next measured pass should cache the
unchanged dimmed backdrop and avoid unnecessary background copies on row/value
changes. The subsequent [settings backdrop diagnostic](2026-10-02-settings-backdrop-cache.md) implements and measures that cache.

## Validation and handoff

Passed:

- All ARM `TestSoftware*` cases twice, including exhaustive source-channel,
  source-alpha and destination-channel arithmetic.
- Direct NEON/scalar equivalence for widths 1–129, all row tails, offsets 0–15,
  odd strides, varied destination alpha and untouched padding/guard bytes.
- Protected pages at both ends of rows, verifying vector access bounds.
- Exhaustive renderer arithmetic with NEON disabled, twice.
- ARM settings backdrop equivalence and cached-renderer visible-change tests.
- Host race suites for gfx, tenfoot, fbgrid and fogcast-kit; host and ARM gfx
  vet; tenfoot vet; ARMv7 launcher build and ARMv5 gfx test cross-build.
- Generated-consumer checks (15 outputs and 32 fixture copies) and whitespace.

Hardware classification: CPU diagnostic, not deployed-launcher or exact-image
HDMI acceptance. Target temporary files were removed; the lease was released
and confirmed free. Next integration step: select reviewed committed bytes and
coordinate actual-launcher/artwork/HDMI validation under the existing lease.

SHA-256 of measured binaries and the cross-built launcher:

- Final gfx test: `f44d2ed4636f3a49de42ec0f6a788e2c70d5b4d5c823142b207302a24558ace2`
- Final scene test: `4480a86f87b739f7ae0862cf260f26123ea0ef856268bb7a68249c3371acfc03`
- Launcher: `8527e6520053742afe01a4d375f55846454725132e6fd3d56403c48e2eb06a3b`

Local ignored logs, profiles and binaries remain under `out/fes-318/`:
`neon-final-arm.txt`, `neon-settings-paired.txt`, `settings-neon.pprof`,
`neon-consistency.log` and the named test/launcher binaries.
