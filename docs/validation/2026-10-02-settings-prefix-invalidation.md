# Settings prefix invalidation diagnostic — 2026-10-02

Scope: FogCast CPU settings navigation, following the
[backdrop checkpoint diagnostic](2026-10-02-settings-backdrop-cache.md).
Base: `4e0524ca931538f65c7b58071f630e3f85a99d2d`; result is the uncommitted
worktree diff. Earlier grid idle, alpha NEON and backdrop changes are preserved.

## Selected change

Settings hint/value labels are textures drawn after the backdrop checkpoint.
Replacing one used to invalidate the saved background unconditionally. The
cache now invalidates that background only when a changed/destroyed texture
is referenced by its saved prefix. Mutations during a frame are recorded until
BeginFrame: a prefix which drew one of those resources cannot be captured as
valid. This preserves old/new texture draw ordering. Overlay-only mutations
keep the saved prefix; whole-scene mutation invalidation remains unchanged.
Command changes still rebuild the backdrop, including layout, artwork, room
animation and panel geometry. No Device/shared/runtime protocol changes.

Two focused tests extend the existing pixel suite: unrelated texture changes
must not increase background clears, and prefix texture updates before/after
the checkpoint must match immediate rendering across subsequent frames. Real
settings navigation and all earlier app-change equivalence cases remain covered.

## Rejected low-level candidates

Private CGO-free NEON copy and opaque-fill kernels were implemented, checked
against GNU ARM instruction encodings, exercised on the kit, then removed
because their isolated measurements did not justify replacing the existing
paths. The retained production NEON kernel is the earlier exact alpha blend.
Benchmark fixtures for Go pixel copies and opaque panel fills remain in
`ui/gfx/pixels_bench_test.go` for future investigation.

Paired same-binary tests, `-test.benchtime=2s -test.count=2 -test.benchmem`:

| Operation | Portable path | Candidate NEON path |
| --- | ---: | ---: |
| 1 KiB disjoint copy | 473–475 ns | 778–781 ns |
| 3,686,400-byte disjoint copy | 8.03–8.57 ms | 8.21–8.67 ms |
| 720×360 opaque panel fill | 0.947–0.965 ms | 0.981–0.982 ms |
| Full 1280×720 opaque fill | 3.35–3.42 ms | 3.34–3.40 ms |

All isolated operations allocated zero bytes. Portable measurements disable
NEON dispatch with `GODEBUG=cpu.neon=off`; x/sys honors that option despite the
Go runtime's unknown-feature notice. Copy candidates passed varied alignments,
all short lengths/tails, unequal slice lengths, guards and protected source/
destination pages; fill candidates passed odd strides, clipped rows, tails and
protected-page stores. Those candidate kernels and their dedicated tests were
removed together. Go's copy and portable opaque fill remain selected.

## Final hardware measurements

Designated MiSTer Pi `192.168.10.84`, Cortex-A9, 800 MHz performance governor.
Go 1.26.5, `CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7`. Lease owner
`fes-318-neon-copy`; private path `/tmp/fes-318-pixels-17a49c0d`. Installed tenfoot
remained running and competed for resources. No executable/service/image was
replaced. Measurements include model update, Snapshot and warmed CPU rendering,
excluding physical presentation, network and image decoding.

Two two-second runs per scene, same benchmark fixtures as the paired baseline:

| Operation | Before this pass | Final selected code |
| --- | ---: | ---: |
| Cached settings navigation | 32.13–32.91 ms | 21.66–21.81 ms |
| Static cached settings | 0.992–0.994 ms | 1.015–1.036 ms |

Navigation improves about 32–34%. It uses 233 allocations per iteration versus
243 in the baseline (22,376 versus 35,250–35,251 bytes). No new low-level copy
or fill kernel is selected in the final artifact. The earlier candidate with
opaque NEON stores measured 19.53–19.81 ms, but isolated full/panel fill tests
showed no gain, so that candidate was removed rather than attributing noisy
whole-scene differences to it. The final result leaves roughly 11.5 ms of a
33.3 ms budget for other work, but physical presentation and input latency
still need measurement; this is not an end-to-end frame-rate guarantee.

The subsequent [overlay restore diagnostic](2026-10-02-settings-overlay-restore.md) checkpoints the fixed panel as well and reduces per-frame restore traffic.

## Validation and handoff

Final host gfx/tenfoot race tests and vet passed; ARM gfx vet, gfx/tenfoot test
cross-builds and launcher cross-build passed. Earlier affected fbgrid/kit race
checks and `make check-generated` (15 outputs, 32 fixtures) also passed during
this investigation. `git diff --check` passed. Final ARM cache tests and real
renderer pixel-equivalence cases passed twice.

Final SHA-256:

- `gfx-prefix-final.test`: `815f9dcee64d34f75afa28bc9bc7e88ba1459b6ab0074d9f2a33dd82c9e5d1d5`
- `tenfoot-prefix-final.test`: `2e515076df0b1be569eea6b5e5a78e69d9254fd42f26f5d8da39fe6ae7e85a73`
- `fogcast-tenfoot-prefix-final`: `41107ab05beb71a03457cbf3b38a28b8641d07f645e73e8118faf010cb86edae`

These are host-only checks plus hardware diagnostics, not exact-artifact
appliance acceptance. Next integration step: select committed module bytes into
the FES native image, then measure actual menu-display submission/input latency
under the existing kit lease.

Private kit files were removed; the existing kit lease was released and confirmed free.
