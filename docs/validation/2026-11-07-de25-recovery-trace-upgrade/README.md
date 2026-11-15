# Trace recovery packaging and UART upgrade path

The [traced recovery build](../2026-11-07-de25-recovery-return-trace/README.md)
now has a separate package validator, regular-file card/manifest output,
one-shot UART update controller and trace-aware download console. These are
off-board results. No device was opened or card written in this milestone.
The complete physical operator still needs freezing and live qualification.

Implementation: `d0abab607ee1db69fdbe88c63789894adede288b`.
Compiled trace firmware: `dfd4863c02f571c0875cd5c30b5163d9f58a6eef`.

## Explicit package boundary

`recovery_return_package.py` checks the trace mode, exact helper/profile source,
source inverse and linked helpers. It runs the unchanged base build validator
on a private source view that removes only the independently inverted trace
statements. Actual ELF/FIT/SPL/configuration bytes remain unchanged in that
view. Native handoff and automatic writes remain excluded.

It accepts only the exact qualified baseline FIT/SOF and identical SPL/BL31.
The same SOF is reused; no FPGA or PFG build occurs. The new 150,994,944-byte
regular-file image has the fixed FAT geometry and verified FIT/manifest
readbacks. The protected prefix differs solely in the fixed raw FIT mirror;
its new hash is `371cd116055bc7f458dc8254071f62768dcf7bd3c2498e70e8c3e6e04f62a956`.

The distinct `prepared-return-trace` status requires the explicit trace loader.
The ordinary uploader rejects it. Tool command names remain intact for mtools
dispatch, with their resolved binary paths and hashes bound separately. All
generated images, firmware and metadata must be bounded regular files.

## UART update and trace gates

The controller accepts only the known PHY-prepare baseline to this trace mode.
It snapshots caller-owned bytes/maps, consumes one attempt, checks claim
ownership before and after commands, and verifies the spare's version,
geometry, prefix, root files, current package and experimental FIT before any
write. A verified FAT load owns the full 8,198,704-byte UART scratch window.
The three planned transfers are a 1,024-byte probe, 954,368-byte padded FIT
and 1,464-byte manifest. The per-transfer/control/upgrade bounds remain
150/5/330 seconds. The upgrade uses no Ethernet and never activates its result.

Five fixed writes preserve rollback and update the root FIT/mirror/manifest:

| Target | Bytes |
| --- | ---: |
| `FES/R1107A.ITB` | 953,032 |
| `FES/R1107A.JSN` | 1,315 |
| `u-boot.itb` | 954,288 |
| raw FIT mirror, sector 512 | 954,368 |
| `FES/RECOVERY.JSON` | 1,464 |

Each write requires its pre-write ownership/memory/digest proof and independent
readback. Fresh rollback destinations must be absent; the existing earlier
rollback files remain intact. The current package and experimental FIT remain
unchanged and are checked again at completion. An uncertain write stops the
attempt; this is not an atomic updater. All wire entry points retire after
failure or success, including ownership/journal/UART failures. No implicit
retry, boot, reset or rollback activation is supplied.

After a separately qualified boot of the traced loader, `TraceConsole` requires
all 34 ordered return markers within the fresh bounded TFTP response. The base
console must first accept its fresh result nonce and prompt. Missing, partial,
duplicate, reordered or foreign markers reject before `UploadSession` can send
its SD write. Trace completion cannot replace command success. Failure retires
the console; it sends no cleanup control afterward, and close retires it before
its file descriptor can be reused.

## Evidence and next step

All 26 focused recovery tests pass, including 32 modeled upgrade scenarios and
40 TFTP trace scenarios. Tests execute the actual controller and upload session
over the existing stateful card/memory model. Separate package tests execute
the base/trace gates with explicitly mocked ELF symbol inspection and real
regular-file image/mtools checks. They reject altered artifacts, profiles,
source bindings, manifest fields, tool dispatch and status.

The actual compiled trace build and the actual planned FIT, manifest, current
package and experimental FIT pass the file-only validators. The real controller
then completes the same five writes/readbacks using these exact bytes over the
card model. This checks sizes, hashes, transfer reservation and preservation;
it does not exercise UART transport or a physical SD card. The full affected
run passes 1,600 parent tests (three skips), planner checks and consistency.

The first host packaging check rejected symlinked mtools command names. The
corrected package binds the resolved binary while preserving dispatch names.
An initial input-plan generator typo also stopped locally. Both failed logs
and partial file-only outputs remain separate from the successful package/plan.

Next: freeze the complete one-attempt physical operator and its ownership/
failure fixtures, requalify TRWU8CBL through the existing recovery path, perform
the UART update with fresh live card/file proofs, then run one bounded traced
network test. The consumed earlier attempt cannot be replayed. No physical
controller action or card swap is required by this plan, but the physical
path remains unproven.

The continuing claim is retained. No new device-state observation was made;
the last recorded recovery prompt remains unconfirmed. The original factory
card and bottom kit are untouched. Native core-load acceptance, HDMI/menu,
input/media/native networking and timing remain open.

Raw evidence: `out/nano-recovery-trace-upgrade-20261107/`.
Retention: `/home/deano/fes-retained/de25-recovery-trace-upgrade-20261107-r1`,
layered on the traced-build and prior physical-stop archives.
