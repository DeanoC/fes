# Incremental native build validation, 2026-09-06

This extends the local integration-entrypoint work described in
[integration validation](integration-validation.md). The selected component
commits are unchanged. The new parent `make dev` invokes existing FogCast
Buildroot recipes and publishes a separate diagnostic image.

## Checks

- All 22 parent tests pass. Cache-key coverage checks application/runtime changes
  retain the base while configuration, source locks, container inputs, scripts,
  Makefile and new Buildroot files invalidate it.
- A regression test reproduced and fixed an unsafe seed case: host-only builds
  can update `inputs.json` while leaving an older image receipt. Seeding now
  requires the stored metadata hash to equal the image receipt fingerprint.
- Receipt tests distinguish development outputs from clean image evidence and
  ensure unchanged development outputs bypass build tools.
- `make check` passes against the real selected package/consumer revisions.
- Independent review found the seed-metadata issue, incorrect host paths passed
  to a container verifier, and a read-only temporary export left by interruption.
  These were fixed and re-reviewed without additional blockers. The initial real
  run reproduced the verifier-path failure after image assembly; it did not
  publish a success receipt.
- A real Docker seed-copy check verified preserved symlinks and file modes.
  A wrong source image hash fell back to a writable fresh development volume.
  Its three disposable test volumes were removed afterward.

Logs: `out/native-dev-tests.log`, `out/native-dev-consistency.log`,
`out/native-dev-seed-check.log`, and the initial failed
`out/native-dev-first.log`. The diagnostic seed/cache check scripts are also
under ignored `out/`.

## Measured build behavior

The final seeded `make dev` passed structural validation in 189.712 seconds.
Its base cache is `fes-native-aafe72abe23ff98a`. Comparison with the original
clean volume found all 62 compiler/base-package `.stamp_built` timestamps
unchanged. Only the runtime package rebuilt, establishing its development
revision marker. An immediately repeated `make dev` reused the complete
hash-checked output in 0.878 seconds.

The published development image SHA-256 is
`670ed8d9707728afcbbab1724aef019f6a137fa2470718c8f1677377f4f7b998`, which happens
to match the previous clean image for these unchanged component inputs. This
observation is not a new two-pass reproducibility check. All original clean
image receipt hashes remain intact.

Logs: `out/native-dev-final.log`, `out/native-dev-reuse.log`,
`out/native-dev-cache-check.log`, and `out/native-dev-package-stamps.json`.
The obsolete volume from the initial failed development run was removed.

A forced warm reassembly (development receipt removed to bypass whole-output
reuse) passed in 152.912 seconds. All 63 package build timestamps, including the
runtime, remained unchanged. A simulated interrupted export left a read-only
selection `.new` file; the run recovered, passed structural checks, and published
a valid development receipt. Its image hash again matched the value above.
Logs: `out/native-dev-warm.log` and `out/native-dev-warm-check.log`.

## Scope

Clean integration outputs and the designated hardware are not modified by
`make dev`. This change adds no component commits, kernel recipe or deployment.
The existing two-pass/QEMU/hardware evidence applies to the earlier exact
artifacts. The incremental path does not establish cold reproducibility or
hardware acceptance. Complete bootable-media assembly remains separate work.
