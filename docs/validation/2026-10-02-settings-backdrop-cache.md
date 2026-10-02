# Settings backdrop diagnostic — 2026-10-02

Scope: FogCast tenfoot CPU settings redraws. Base:
`4e0524ca931538f65c7b58071f630e3f85a99d2d`; result is the uncommitted task
worktree diff, preserving the earlier grid idle and NEON changes. This follows
[NEON alpha-fill diagnostics](2026-10-02-neon-alpha-fill.md).

`FrameCache.CacheBackdrop` marks a reusable command prefix after the settings
backdrop dim pass. Software and linuxfb expose a package-private CPU surface;
menu-display inherits it from Software. The cache retains one framebuffer copy
(3,686,400 bytes / 3.52 MiB at 1280×720) and its prefix commands. A changed panel
restores those pixels, then executes the panel and all later overlays. Prefix
command changes, texture update/destroy and framebuffer geometry changes
invalidate reuse. Texture mutation still flushes commands before modifying
resources, and mutation during a frame prevents capturing a stale prefix.
Identical complete frames bypass even the restore. Devices without the CPU
surface replay normally. Device, FC2D and runtime wire contracts are unchanged.

## Measurements

Designated MiSTer Pi `192.168.10.84`, dual Cortex-A9, 800 MHz performance governor,
Go 1.26.5, `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`. Lease owner
`fes-318-backdrop`; private binaries under `/tmp/fes-318-backdrop-17a49c0d`.
Installed tenfoot stayed running; no service or image was replaced. These are
model/Snapshot/warmed-renderer diagnostics excluding physical presentation,
network and image decoding, not artifact hardware acceptance.

`-test.benchtime=2s -test.count=2 -test.benchmem`:

| Operation | NEON without backdrop checkpoint | With checkpoint |
| --- | ---: | ---: |
| Settings navigation, cached renderer | 37.10–37.36 ms | 31.05–31.62 ms |
| Static settings, cached renderer | earlier 0.988–1.005 ms | 1.004 ms |

The paired baseline is the retained NEON diagnostic binary, using the same
scene fixture and kit. Immediate settings redraw remains 34.36–37.47 ms;
immediate navigation is 35.26–38.06 ms. Cached navigation improves about
15–17%, narrowly within a 33.3 ms renderer budget. This is not an end-to-end
30 Hz guarantee: physical submission and scheduling have additional cost.

A four-second cached-navigation profile measured 31.65 ms; `runtime.memmove`
accounts for 61.01% flat CPU, `blitCopy` 7.73%, `blendOver` 7.73%, and NEON
6.79%. The profile includes benchmark setup/calibration. Full-buffer restore
and remaining opaque fills/copies now dominate; the checkpoint trades repeated
scene rasterization for a conservative pixel restore that also erases prior
overlays correctly.

The subsequent [prefix invalidation diagnostic](2026-10-02-settings-prefix-invalidation.md) preserves this backdrop across panel-only texture changes and records the final selected measurements.

## Validation and handoff

Passed host gfx/tenfoot tests, race tests for gfx/tenfoot/fbgrid/fogcast-kit,
gfx/tenfoot vet, ARM gfx test and tenfoot launcher cross-builds,
`make check-generated` (15 outputs / 32 fixtures), and `git diff --check`.
On ARM, `TestFrameCache*` and the real tenfoot renderer pixel-equivalence test
passed twice. Tests compare moving translucent overlays against immediate
rendering, assert backdrop rasterization is skipped, and verify command and
texture changes invalidate reuse. App-level cases include row navigation,
changed values/status, artwork, layout, panel geometry, animated rooms, and
closing/reopening the settings layer.

Diagnostic SHA-256:

- `tenfoot-backdrop.test`: `478041610cfe9c1b7ccac59930c66163310be5537e544cd72137e3190ae60f4d`
- `gfx-backdrop.test`: `c5b5a5536038896606852c8da03f2cdfc37503729d62c9de27fc34db57708aba`
- `fogcast-tenfoot-backdrop`: `c5330aeba0aa5de8e94e9048b0c69a86f3a1009fb6430515aa38e1c04a856e00`

Private kit files were removed and the lease released. Next integration step:
select committed FogCast bytes into the FES native image and perform
exact-artifact menu-display acceptance under the existing kit lease. This
measurement does not validate an installed appliance image.
