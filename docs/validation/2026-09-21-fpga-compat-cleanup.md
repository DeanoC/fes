# FPGA package-only cleanup — software handoff

Status: implemented as an **uncommitted diff**, independently reviewed with no
remaining blockers. This is host-side validation, not hardware acceptance.

- Base: `2d54817cdd6ee40922f99d4fc4d4383445528bf1`.
- Branch: `refactor/fpga-compat-cleanup`.
- Worktree: `out/dev/fpga-compat-cleanup/fes` in the integration checkout.
- No result commit, push, deployment or PR was created.

## Scope and contracts

FES described packages are the only FPGA product launch path. FogCast no longer
starts Main_MiSTer, sends raw-game launches, seeds the old built-in Pong product,
or accepts raw-core path mappings. The target agent uses runtime protocol 2;
obsolete Main settings fail with safe, actionable configuration guidance.
Existing catalog records and cache/save files are not migrated or deleted.
Host-only execution remains available.

The runtime retains described-package lifecycle, persistent core data, media,
input, composition, splash and explicit contained raw-RBF diagnostics. Removed
code includes protocol 1, raw-game profiles, MiSTer package drivers, legacy
SPI/CoreLoader/framebuffer plumbing and cartridge-save support. Shared
programming definitions and copied protocol fixtures changed with consumers.

Functional FPGA producers require identity 2, including demo and Catch. Retired
raw-core bundle producers, source policies and conventional image lanes were
removed. Native image assembly and kernel toolchain selection use the native
lane. The factory package set and kernel/U-Boot policy were not expanded.

## Validation

- FogCast: full `go test ./...`, `go vet ./...`, ARMv7 agent/kit builds,
  UI tests, six boundary checks, package smoke harness and contained-diagnostic
  contract checks passed.
- Go race checks passed across packages. The catalog's 10,000-row fixture
  exceeded the initial 90-second limit; its isolated five-minute-budget rerun
  passed in 180.7 seconds. Changed lifecycle/agent/HTTP/protocol packages also
  passed a final race rerun.
- Runtime: full `make -j2 test`, `make all archive-audit`, incremental/version
  and deterministic active-tree guards passed. Retained lifecycle, persistence,
  artifact, GP, input, video, FPGA manager and daemon tests passed.
- Shared definitions: full test target passed with `jsonschema[format]` in an
  isolated temporary environment. Parent generation check passed: 12 generated
  files and 20 copied fixtures.
- Parent: 562 tests ran successfully, including 37 skips. Image shell tests passed. These include
  simulated assembly/reproducibility and real private media-file checks; they
  do not qualify a new committed-source appliance image.
- FPGA: 121 producer tests and 77 focused identity/provenance/Quartus-helper/JTAG
  tests passed, as did Pong, ZX81 and demo simulations. Full discovery encountered
  unrelated experimental-toolchain failures: unavailable local Yosys and an
  existing oracle `synth_only` policy mismatch. Those lanes remain unqualified.
- Optional Chrome integration tests skipped because Chrome was unavailable.
- Independent review fixes were retested: startup retains the caller's budget,
  each status request is bounded, resumed save-failure ownership is preserved,
  validated package idle recovery retains its single leased Stop, and config
  errors expose setting names without private values. Whitespace checks passed.

## Next integration step

After the changes are committed/selected, assemble and verify a fresh native
image from those committed module bytes. Then claim the designated kit lease
and run exact-artifact package and contained-diagnostic acceptance. Historical
hardware evidence does not qualify this changed runtime/image. This task did
not program hardware, reboot a kit or write a physical card.
