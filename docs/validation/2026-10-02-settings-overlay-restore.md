# Settings overlay restore diagnostic — 2026-10-02

Scope: FogCast CPU settings navigation, following
[prefix resource invalidation](2026-10-02-settings-prefix-invalidation.md).
Base: `4e0524ca931538f65c7b58071f630e3f85a99d2d`; result is the uncommitted
worktree diff. All earlier task changes are preserved.

## Change

Settings checkpoints the dimmed scene plus the fixed panel border, background
and title. Rows, focus, values, status and later overlays follow the checkpoint.
The cache records the bounding rectangle touched by the previous frame's
post-checkpoint commands, and restores only that rectangle before drawing the
new overlay. Pixels outside that rectangle already match the saved backdrop.
Moving/removing overlays restore their old bounds, so old pixels disappear.
Fill/texture destination rectangles use Software's exact clipping. Text,
clears and other commands without certain bounds conservatively cover the full
frame. Frames without a checkpoint invalidate the backdrop: settings reopening
or another rendering path cannot reuse a snapshot of an intervening scene.

Prefix commands/resources retain the earlier invalidation rules. Direct draws
caused by resource mutation still flush once, then the completed frame records
all subsequent overlay bounds. Identical whole frames bypass restoration. One
full RGBA snapshot remains retained (3.52 MiB at 1280×720); this change reduces
per-frame copy traffic rather than snapshot capacity. No Device, FC2D, runtime
or shared contract changes; portable copy/fill and the exact NEON alpha kernel
remain selected.

## ARM measurements

Designated MiSTer Pi `192.168.10.84`, Cortex-A9 at 800 MHz/performance governor,
Go 1.26.5, `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`. Lease owner
`fes-318-overlay-damage`, private path `/tmp/fes-318-damage-17a49c0d`. Installed
tenfoot remained running and competed for resources; no service or image was
replaced. These diagnostics include App Tick, Snapshot and warmed rasterization,
excluding physical presentation, network and image decoding.

Two runs, `-test.benchtime=2s -test.count=2 -test.benchmem`:

| Operation | Previous selected build | Overlay restore |
| --- | ---: | ---: |
| Cached settings navigation | 21.71–27.15 ms | 10.57–12.80 ms |
| Static cached settings | 1.014 ms | 1.066–1.070 ms |

The paired baseline is the retained `tenfoot-prefix-final.test` binary from the
previous diagnostic; benchmark scene and loop are identical. The wider baseline
range illustrates kit contention. The previous session measured 21.66–21.81 ms.
Navigation is roughly twice as fast, leaving more renderer margin at 30 Hz;
physical submission/input timing still needs measurement. It does not establish
an end-to-end frame rate.

A four-second navigation profile measured 10.80 ms. Flat CPU: `memmove` 33.94%,
`blendOver` 20.09%, `blitCopy` 17.50% (37.75% cumulative). It includes benchmark
setup/calibration. Copies are a smaller share than the earlier full-restore
profile (61.01%); textured alpha blits, including labels, are now a substantial
remaining cost.

## Validation and handoff

Host gfx/tenfoot tests and race tests passed, as did host gfx/tenfoot vet,
ARM gfx vet, gfx/tenfoot cross-built test binaries, launcher cross-build,
`make check-generated` (15 outputs / 32 fixtures), and `git diff --check`.
ARM cache tests and the real renderer pixel-equivalence test passed twice.
New checks verify clipped/fractional bounds, unions, empty damage, full-frame
fallbacks and untouched pixels outside restore bounds. Immediate-renderer
comparisons cover moving translucent overlays and removed text/debug glyphs.
The real app test also opens/removes OSK and debug overlays over settings,
then exercises artwork, layout, status, panel geometry and animated rooms.
Earlier texture-mutation ordering and checkpoint-close/reopen cases still pass.

SHA-256:

- `gfx-damage.test`: `be1a9f0488df94c786493612c5b5fe5e09c17ed80b477b646e9a35e5e3ff4277`
- `tenfoot-damage.test`: `35f174f4fbd3d092db6342d0353ea717ba6817d1b33b19506d05b4168f0a4956`
- `fogcast-tenfoot-damage`: `c40847d9bdfb007b7666cf61bf4021fd6f0106216ccd1cef697f3de61d09f824`

Hardware classification: bounded diagnostics, not exact-artifact appliance
acceptance. Next integration step: commit/select these FogCast module bytes
into the FES native image, then validate physical menu submission and input
latency under the existing kit lease. Private kit files were removed and the
lease released and confirmed free.
