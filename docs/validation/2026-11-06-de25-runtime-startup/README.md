# Bounded cold BL31 runtime startup diagnostics

Implementation `4146f5345` adds 31 ordered progress checkpoints after the
[consumed set/way trial](../2026-11-06-de25-setway-trial/README.md). That trial
returned from cache maintenance, logging restoration and all 15 vendor entry
stages, then timed out after `FES_NANO_SD_PLATFORM setup=1 ready=1`. Its exact
later failing/stalled instruction remains unknown. This milestone builds a
new diagnostic input; it does not replay or qualify the stopped input.

The new markers cover cold main entry/return, owned platform setup, GIC driver,
distributor, redistributor and CPU-interface setup, mailbox initialization and
notification, libraries, runtime services, next-image preparation, runtime
setup, console completion, the original data/BSS clean calls and cold exit.
Mailbox markers encompass the optional V3 initialization in both configurations.
All original calls, arguments, conditionals and warm-entry bytes are preserved.
The generator checks exact vendor/FES-transformed source pins and verifies an
insertion-only inverse. No cache operation, reset, interrupt policy, service
permission or secondary-CPU release policy is changed.

The linked C-call UART leaf is 136 bytes. Instruction checks prove no stack
access, RAM store, nested call, cache operation or vector write, and preserved
callee-saved registers. Its index/table/message bytes and fixed UART addresses
are checked against the actual ELF. Each message retains the existing 384-byte
and 100000-poll bounds. Inline assembly markers preserve cold call arguments.
C callers retain ordinary ABI stack handling after setup has established it.

The decoder requires all 31 phases after the guard/vendor/cache/logging proofs,
with provider/SD observations inside the owned-setup bracket and the complete
sequence before U-Boot. Missing, duplicate, reordered, failed and misplaced
records remain terminal. A before marker identifies an interval; it does not
prove the next call executed or exclude a failure in the diagnostic's final
drain/return. The early private fault vectors are restored before vendor C
setup, so silence still does not exclude a later vendor-vector fault.

Validation on the corrected source:

- 89 focused tests, including isolated generator loading, complete boot parsing,
  damaged progress and retained shared-Tenfoot launch/return models.
- Actual pinned vendor main/platform functions execute with the original call
  sequence for both mailbox configurations. All 83 injected failure prefixes
  stop without later calls or progress markers.
- Seven ARM helper cases on QEMU cover A53/max success and register/SP/LR
  preservation, invalid/wide indices, all-ones/stuck UART status and an
  unterminated 384-byte string. UART status is fixture memory; these are not
  physical UART, GIC or mailbox tests.
- All ten affected steps pass, including 1554 parent tests/two environment
  skips and 1581 FPGA tooling tests/75 tool or fixture skips. The final run
  completed in 813.3 seconds under its original 900-second cap.
- Actual final BL31, SPL/U-Boot and shared-lock file composition pass. The
  linked 1428-byte early guard / 19 wire assertions, 15 vendor markers, 43 fixed
  and 20 RAS observations, logging writes/restoration, set/way helper and
  uncached SPL controls pass their current-artifact audits.
- All 20 existing firmware-driver objects and the cache/decode/entry/fault
  assembly inputs remain byte-identical. Prior source-identical cache/logging
  ARM evidence is reused explicitly. The native application/entry are reused
  from `31d716957`, with all 2098 selected source hashes checked; the sealed
  FPGA producer remains `505993c08155d71794bacd1260155ace736fe19a`.

The final matched chain is
`b47d8054763a5543daf6f40f5b5d6832c2db88989edebaed52171c5955aafaea`;
the FIT is 8,187,904 bytes. Only `native-bl31-final`, `native-uboot-final` and
`native-composition-final` beneath `out/nano-runtime-startup-20261106` are
selected. The initial implementation `42f252091` passed its firmware build
and linked checks but failed isolated Python loading in the parent suite and
file composer. A file-relative import and independent extraction fixed that;
all final firmware/bootloader/files and affected checks were rebuilt from the
corrected source. Initial artifacts and failure logs are retained separately.

Other offline preparation failures remain retained: a fixture counted letters
inside its terminal label, an inherited audit expected 13 installed files rather
than the reviewed 17, a reuse check consulted a changed-files-only ledger, and
the initial evidence collector included its own open wrapper log. The final
checks correct those scopes without changing a hardware limit or firmware
behavior. They do not qualify the earlier outputs.

This source has zero physical attempts. TRWU8CBL remains claimed after the
preceding stopped boot, with the spare card retained. No board programming,
upload, SD write or factory restoration occurred in this milestone; factory
card and bottom kit are untouched. CPU/FPGA halt is not established. Ordinary
core loading, native ROM/Run/return and DE10-equivalent behavior remain open.

Next freeze and test a runtime-aware operator, requiring these 31 receipts in
its service gate and retaining partial runtime progress in its receive-only
failure tail. Reuse/verify the seven catalog files and bind this FIT as the
eighth upload. Fresh claim/device/UART/card/recovery admission must precede
one new bounded trial. Prior 17 campaign stops / ten pre-native stops remain
separate. Preserve prompt50/startup100/launch100/config20/control5/native450/
child480/work740/host850, passive2-second/1MiB and ROM16-byte/400M-tick/64-poll
bounds. Stop first failure, no retry or fallback; restore 15M/exact USB/private
JTAG/UART afterwards, retain claim/spare, and leave factory card/bottom kit alone.

Exact identities, results and evidence hashes are in [result.json](result.json).
Source-pack import and complete archive readback are verified at
`/home/deano/fes-retained/de25-runtime-startup-20261106-r1`, with prerequisite
`de25-setway-stop-20261106-r1`. Build/model/audit success is distinct from a
new physical boot and ordinary runtime admission.
