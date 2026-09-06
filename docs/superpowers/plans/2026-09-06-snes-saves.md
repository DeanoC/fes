# SNES Save Persistence Implementation Plan

**Goal:** Preserve ordinary SNES battery RAM across clean Stop and reboot.
**Architecture:** FogCast selects persistent per-game paths; the runtime restores
and snapshots SRAM through the existing core backup protocol before idle.
**Tech Stack:** Go, C++14, existing MiSTer SPI and filesystem primitives.
**Spec:** ../specs/2026-09-06-snes-saves-design.md

## Constraints

Preserve selected sources and prior evidence. Workers use `out/dev/snes-saves/`.
No FPGA changes, host save synchronization or power-loss autosaving. Root owns
kit leases and parent pins. No worker deploys or publishes. Use existing installed
tools and incremental/derived diagnostic image workflow.

## Tasks

- [x] Runtime: add optional `save_path` admission, derived battery RAM metadata,
  restore-before-input and atomic Stop snapshot with retryable `save_failed`.
  Extend `runtime.h`, daemon protocol, artifacts/core loader/native hardware and
  focused unit/integration tests. New transport/file units stay under native.
  Test missing/exact/wrong size, type/exponent bounds, 2/128 KiB sector byte order,
  mount-before-download-end, bad SD requests/timeouts, file replacement failure,
  failed launch not publishing, Stop failure retaining session and successful retry.
- [x] FogCast: pass game identity in PreparedLaunch; derive save path in native
  adapter under configurable save root; serialize SNES-only optional `save_path`.
  Test stable identity across cache paths/restarts, game/ROM separation, direct
  and cached launch, unwritable root before mutation, other systems unchanged,
  save error mapping, Stop retry and lease retention. Update native architecture.
- [x] Root review shared contract and tests before selecting local commits.
  Keep generated package definitions unchanged unless source tracing demonstrates
  a shared-definition requirement. Run consistency and host build.
- [x] Build changed target binaries using retained tools, derive diagnostic image
  from preserved verified image, claim free kit and test real save/relaunch,
  Pong switching and reboot. Retain original save files before hardware use.
- [x] Record exact tested revisions, image hashes and limitations in guides;
  finish with selected integration validation and worktree handoff.

Each component writes meaningful failing tests before implementation, runs the
focused suite after changes, and hands the integrator a reviewed local commit
when needed to build selected inputs. External publication remains a separate
user action.

## Software validation

Runtime `93b369f7bf56757697cc5e59332545f5b4ee62f3` passed full host tests and
archive audit, including independent-review regressions for late input faults
and clearing historical idle errors. FogCast `5fc0b4ad8cac63be8fc6e9d0b9e82c8aede8baac`
selects that runtime; full Go race tests, focused save/lease tests and updated
runtime-lock fixtures passed. Parent consistency, 29 tests and host build passed.
The ARM diagnostic reuses the preserved three-system image and existing compiler.

## Integration result

Diagnostic hardware checks passed for Zelda save/Stop/Pong/reboot/restore,
failed-write lease retention and same-owner retry, separate SMW saves, a
non-battery HiROM cartridge, and Mega Drive gameplay. Final Stop and lease
release succeeded. See `docs/snes-saves.md` for identities and limitations.
The selected normal image passed a fresh two-pass build, structural verification
and QEMU boot. Subsequent exact-image hardware acceptance passed: a new Zelda
slot survived Stop/Pong/reboot, write-failure retry retained ownership, and
2 KiB SRAM plus cross-system regressions passed. The kit was left on the normal
image, idle and released. See the save guide for scope and evidence. Publication
is authorized: runtime PR #15 and FogCast PR #149 are open,
with the parent integration submitted after those dependencies. Preserve the
accepted component revisions when merging.
