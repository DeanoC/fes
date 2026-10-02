# Settings raster cost — 2026-10-02

Scope: FogCast settings drawing and the software alpha-fill kernel. Base:
`4e0524ca931538f65c7b58071f630e3f85a99d2d`; result is the uncommitted worktree
diff, alongside the separate [grid idle changes](2026-10-02-grid-idle-cpu.md).
These measurements precede the [NEON follow-up](2026-10-02-neon-alpha-fill.md).
No runtime, FPGA, shared-contract, image or default changes.

## Findings

Profiled the warmed real settings renderer on the designated Cortex-A9 ARMv7
kit at `192.168.10.84`. CPU frequency was 800000 kHz with the performance
governor. Go 1.26.5, `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`.
The existing target lease was held by `fes-318-settings-cpu`. Installed tenfoot
remained running; timings include competing load. Private test binaries and
profiles used `/tmp/fes-318-settings-17a49c0d`. No services were replaced.

The baseline settings profile measured 83.63 ms per redraw. `FillRect` consumed
63.51% flat / 76.38% cumulative CPU; `runtime.memmove` 20.51% flat, and
`rasterizeLabel` about 1.27% cumulative. Most of the cost was the full-content
translucent backdrop, not typography or allocation. The renderer also shaded
pixels that its opaque settings panel immediately overwrote. ARM disassembly
showed separate bounds branches for each channel in the large-fill byte loop.

## Changes and paired measurements

Settings now shades four disjoint rectangles outside the opaque panel and its
border. A randomized pixel test compares this against full dimming followed by
the panel overwrite, including clipping, overscan, varied destination alpha,
tiny viewports and panels outside or covering the viewport.

The ARM alpha-fill loop operates on a complete pixel each iteration, with
lookup-table pointers outside the loop. The compiler eliminates per-channel
bounds branches. Channel arithmetic and `/255` truncation remain exact.
The existing indexed loop remains on other architectures: the slice-advance
variant regressed the measured amd64 large-fill microbenchmark. An attempted
packed 32-bit lookup variant also failed to improve ARM and was discarded.
No assembly or unsafe memory access was introduced.

Two runs with `-test.benchtime=2s -test.count=2 -test.benchmem`:

| ARM operation | Before | After |
| --- | ---: | ---: |
| Warm settings immediate redraw | 82.04–82.30 ms | 52.184–52.194 ms |
| Full 1280×720 alpha fill | 52.90–53.07 ms | 41.15–41.41 ms |
| Static settings through scene cache | — | 0.989–1.017 ms |
| Settings navigation, immediate | — | 50.00–53.19 ms |
| Settings navigation through scene cache | — | 52.43–53.00 ms |

The scene fixture includes App Tick/Snapshot, texture synchronization, 48
generated opaque covers, and actual rendering. It excludes network, decode,
socket transfer and physical presentation. `SettingsNavigation` changes the
settings row each iteration, so the production cache must redraw. Static cache
timing is not the cost of changing a setting.

The settings redraw improves about 37%, and the alpha kernel alone about 22%.
On the amd64 workstation, the final large alpha fill remained about 0.50 ms
(baseline 0.51 ms); settings immediate redraw fell from 0.915 to 0.685 ms.
These host figures are individual diagnostic runs, not kit results.

The intermediate optimized settings profile was still dominated by alpha
dimming (42.08% flat CPU) and memory copies (32.04%). Changing settings focus
still exceeds the 33.3 ms budget. Next useful pass: cache the already dimmed
background while the settings panel is open, invalidate it when underlying
scene/resources change, and redraw the opaque panel for focus/value updates.
The current implementation still redraws the entire scene on a changed row.

## Validation and handoff

Passed gfx, tenfoot and fogcast-kit race suites, gfx/tenfoot vet, ARM tests for
backdrop pixel equivalence and cached-renderer visible changes, all ARM
`TestSoftware*` cases (including exhaustive alpha arithmetic), ARM launcher
cross-build, generated-consumer checks (15 outputs and 32 fixture copies), and
`git diff --check`. Hardware classification: CPU diagnostic, without installed
launcher or exact-image HDMI acceptance. Select reviewed committed bytes and
coordinate an actual-launcher diagnostic before deployment qualification.

SHA-256:

- Baseline scene test: `44eb47547f3ca56d8af301a365fce65b307591a4b9da5fb33106e75c17067d4b`
- Final scene test: `c8d63f285e017c3c0a5537119019f2d809396157977a7a39248ded2ff3f41cdc`
- Baseline gfx test: `51770ed1eaf3fb5b036b8bc2efc766b97aa9ecffa72c479a078133cce7939312`
- Final gfx test: `9178ccc045141836f3781a92768c4074531dcadbdaa51cb5481efbba16173c1d`
- Cross-built launcher: `b293cd3bf6fe14de8efbf862a1e9a2223b4477582e226c603472cf6ecbc5d3b9`

Ignored logs and profiles live in `out/fes-318/settings-*`; test binaries there
retain their measured identities. Target temporary files were removed and the
lease was released and confirmed free.
