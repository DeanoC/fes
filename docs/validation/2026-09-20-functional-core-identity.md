# Functional FPGA identity and documentation-only reuse

Status: real compiler build, unchanged-artifact reuse and exact-package hardware
diagnostic passed. Full-image qualification is separate.

The development-ease migration separates embedded functional build identity from
repository revision provenance. This record describes the first real build and
reuse rehearsal and bounded package diagnostic, not full-image acceptance.

## Exact artifacts

- Producer source: misteross `5be29401614864e249f4f9c418c52385d52ccbd2`.
- Record format: 2; package manifest format remains 2.
- Core: `fes.coleco`, HIP route, GPU device 0.
- Functional build ID: `5cded8799805806326eed589d5c3fef5`.
- Full functional digest: `5cded8799805806326eed589d5c3fef542f43d70940ebff8f74cb0b37004b84b`.
- Package: `d1031c0ce98d443ae4b3b3830ae10bd3712969ffc8d4ea761f031e5bb3980e93`.
- RBF SHA256: `141bff3eca261a0adf0d8804fe0375f9602cb4adaac3e94f6f49fa9120b685ee`.
- Manifest SHA256: `47a0fc79640e0d205466c20d234fa8b0f9ce28935e0296368e92cf519bd52e7f`.
- Original build-record SHA256: `632c60c670b1f8a80b6702eaf1533931846a9a76ea5cf0c35c963c43669e05f9`.

Authenticated cached tools include nextpnr `0fad53a7`, Yosys `e2d425de` and
Mistral `b28e30a3`. Controlled execution fingerprints include executable bytes,
Yosys ABC/support files, dynamic dependencies and KFD/kernel identity. The
before/after execution checks passed. The first configured placement (seed 4,
weight 300) passed system 54.5137/52 MHz and pixel 90.6618/74.25 MHz.

## Reuse rehearsal

A separate local worktree added only `docs/functional-identity-rehearsal.md` and
committed `3ea95076f5d8045b08f5ee8ecbe09db1c7f3add9`. The parent resolver derived
its canonical record using the real authenticated tools, verified the original
record against historical Git objects, then selected the original sealed package
from the shared cache. The producer entrypoint was replaced with a failing guard
for this resolution; it was not invoked.

The original manifest, record and RBF bytes remained unchanged. The selection
receipt records original revision `5be2940` and selected revision `3ea9507`.
The selected record's digest is
`d43432105c250894da7ca8e80d74b9b4d515e8bc2d2e20135b85853c600ef8ee`.
This demonstrates unrelated documentation commits no longer force FPGA builds.
The current closure conservatively includes each owning core directory, so a
core-local README edit can still invalidate that core; narrowing it requires
input-access evidence.

Local evidence is under `/home/deano/fes/out/dev/development-ease/`:
`coleco-functional-build.log`, `coleco-v2-selection.toml`,
`coleco-docs-selection.toml`, their `.provenance.json` sidecars and
`coleco-docs-reuse.json`. Producer reports and the original archive are under
`misteross/build/fes-coleco-oss` and `misteross/build/packages` there.

## Imported repository reuse

Import commit `0ff5652fab64f855d44249140a6102da21c38245` retains FES plus
all four component histories as parents. All imported module trees match their
original selected trees. `make check` and `make host` passed from the imported
checkout. Resolving Coleco there reused the package above without invoking the
producer. The new sidecar records the actual FES repository, commit and
`sources/misteross` path, preserving the original manifest and build record.
Local evidence: `coleco-import-reuse.json` and `coleco-import-selection.toml`.

## Exact-package hardware diagnostic

Bob confirmed the disruptive maintenance window in the
[coordination thread](https://deanoshome.slack.com/archives/C0C3BKMU67J/p1789890519995599).
Under the target lease, temporary runtime `079b4548` and FogCast agent/host
`e69e92db` exercised the exact package above through an isolated host library.
The sequence covered 32768-byte, 24576-byte, 32767-byte and legacy 989-byte
media, Stop, and a 32768-byte relaunch. All five captures matched 664/664 HDMI
samples; active package, attached input and stream capacity were checked.
The original runtime, agent and Kit binaries were restored and their running
hashes verified. The kit remained on the same boot, ready/idle and lease free.

Evidence and full identities:
`/home/deano/fes/out/hardware/coleco-functional-20260920/README.md`.
This is a bounded package diagnostic; same-generation stale-tail behavior is
simulation coverage, and no imported full image was deployed or qualified.
Later shared-source reorganization changes functional inputs and needs new
artifact evidence. Existing immutable artifacts and this evidence stay intact.
