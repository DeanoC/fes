# Bounded private BSS exit-clean hardware trial

The one physical attempt passed the firmware exit-clean and loader-boot gates,
then stopped at the unchanged 100-second native application startup deadline.
No core was launched. The [physical result](physical-result.json) is separate
from the off-board build and fixture evidence.

The selected firmware is `66812745d1e460b9df15ce899bc05b5c80f41edd`.
Its [matched build and off-board validation](../2026-11-06-de25-exit-cache/README.md)
bind chain `43e6315533c2da45c40d4cdc96f5edd41f0099add4c854eaac92292057fa3683`.
The native application/entry remains byte-identical to source `31d716957`.
This is a new compiled input, with zero attempts before this frozen procedure.
The preceding 18 stops, including eleven before native startup, remain retained.

TRWU8CBL remains under the continuing claim. The user reported an accidental
power interruption and restored power; passive enumeration found its exact USB
serial and UART again. That does not prove its CPU/reset or boot state. The
procedure first checks current claim, exact USB identity, UART owners, route,
source and artifact hashes, then starts one fresh qualified volatile recovery.
It freshly admits the spare card and protected recovery before any SD writes.
The factory card stays aside; factory restoration remains waived. TRWU8J8G is
excluded from the private transport and receives no operations.

The frozen helpers are under `out/nano-exit-cache-board-20261106`; exact hashes
and thirteen completed proof/fixture groups are in [operator-source.json](operator-source.json).
The outer service gate requires both ordered EXIT_CACHE receipts, complete
31-phase runtime progression and all previous cache/logging/vendor/boot proofs.
It rejects nine new damaged exit-state variants in addition to the previous
checks: 91 damaged outer results total. Thirteen new receive-only tail cases
exercise fragmented ARM-emitted receipts and model-context damage. Partial
records preserve maintenance uncertainty; complete maintenance grants no boot
permission after a terminal timeout. These are off-board checks.

Seven catalog files are copied and verified byte-for-byte from the previous
retained catalog. The eighth fixed upload is the new 8,192,000-byte FIT. Each
write requires owned card/recovery admission and is followed by fresh readback.
The native service then attempts ZX81, the JP-loop ROM and ZX81 again, with
matching activation receipts, changing fetch observations, launch/return/quit
ownership generations and shared USB report observations. Idle reports do not
qualify physical controller input. No physical input is requested in this trial.

All bounds remain unchanged: prompt 50s, startup/launch 100s, firmware
configuration 20s, controls 5s, native 450s, child 480s, experiment 740s and host
850s. ROM transfer remains 16 bytes per step, 400M ticks and 64 polls. The
failure tail is receive-only for two seconds with a 1MiB console bound. Stop at
the first failure without fallback or replay. A failed input is consumed.
The normal-user shared Quartus lock encloses private top-only JTAG use.
Programming uses 6M; cleanup restores the original 15M, exact USB permissions,
owned private-server termination and a free UART. These host cleanup results
cannot prove CPU/FPGA halt. Keep the claim and spare card after a stop.

At source freeze no board test or card write has occurred on this new input.
The next step is fresh preflight and one bounded attempt. Physical results
must be recorded separately from build/model evidence. Ordinary core loading,
physical cache coverage, native ROM/Run/return and DE10-equivalent acceptance
remain unproven.

The exact frozen operator is `4c7dca87bf314962d8f5617d9b2402dff50d53ca`.
The trial ran once after the power interruption: fresh recovery, eight uploads
and eight fresh SD readbacks passed. Exact chip/programming and SPL FIT/hash
checks passed. Both EXIT_CACHE records, BSS-clean return, all 31 runtime phases
and the matched U-Boot prompt were captured. This is physical evidence that the
new exit operation returned; it does not establish complete cache geometry or
ordinary runtime acceptance.

The operator sent `fesboot` once. Output reached the boot ID, memory layout and
complete LMB reserved-region report, then no native entry marker or App Ready
arrived within 100 seconds. No launch/control command followed. The independent
raw replay exactly matches the stored decoder result: loader-ready, incomplete
native boot, last checkpoint `memory`, no reported exception. The two-second
passive tail preserved completed runtime and exit state plus restored logging.
Later fault absence and exact stopped/faulting instruction remain unproven.

An audit of the actual linked U-Boot locates the uninstrumented interval:
LMB dump return and pure memory validation, the CPU snapshot (including four
PSCI affinity SMCs), reset/watchdog/UART reads, CPU/watchdog checks and the owned
snapshot SMC before its receipt. None has an individual return receipt in this
image. The audit initially looked for SMC directly in `invoke_smc`; the actual
pinned glue calls `smc_call`, which contains it. The corrected source/ELF audit
passes, and both failed off-board audit attempts are retained. No firmware
instruction or hardware safety gate was changed to make this audit pass.

Independent cleanup confirms the 15M readback, exact USB permissions, private
server gone and UART free. The claim and spare are retained; CPU/FPGA halt is
unproved. Factory card and bottom kit remain untouched. This is campaign stop
19; the eleven earlier pre-loader stops remain distinct from this post-loader
application-start timeout. This compiled input is consumed and will not be
replayed.

Next add bounded loader receipts around memory-validation return, each PSCI
call, register-read groups, watchdog stop and owned snapshot. Preserve the
existing calls and policies, test receipt order/failure handling off board,
rebuild the matched chain and freeze a new single attempt. Ordinary core loads
and the requested DE10-equivalent launch/return behavior remain incomplete.

Source pack and full trial evidence are retained and verified by archive
readback/import at `/home/deano/fes-retained/de25-exit-cache-stop-20261106-r1`,
with prerequisite `de25-exit-cache-20261106-r1`.
