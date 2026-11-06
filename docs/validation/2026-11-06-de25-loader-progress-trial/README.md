# Bounded initial loader progress trial

One physical attempt captured six loader receipts through `cpu0-before`, then
stopped at the unchanged 100-second application startup deadline. No core was
launched. The [physical result](physical-result.json) records this separately
from off-board fixtures and build evidence.

The selected firmware is `7f3824490acf0f9adedd1aeb16b3c3ea2bb8b358`.
Its [matched build and off-board validation](../2026-11-06-de25-loader-progress/README.md)
bind chain `66e99a26fe7bbfbc56d939423b1dd361026cdbf2bef103f3a51110726b5c73cf`.
The native application and entry remain byte-identical to source `31d716957`.
This compiled input has zero attempts before this frozen procedure. Previous
19 stops remain retained: eleven stopped before the loader and the latest
stopped after its memory report, before native application entry.

Exact checked helpers live under `out/nano-loader-progress-board-20261106`;
[operator-source.json](operator-source.json) pins their bytes and fourteen
completed proof/fixture groups. The outer healthy-service gate now requires
all 26 loader markers, both EXIT_CACHE records, full 31-phase runtime startup
and every existing boot, ownership and launch/return proof. The actual model
service exchange passes and 100 damaged outer results reject. The actual
receive-only collector passes 37 new loader-tail cases with linked ELF strings
within modeled boot contexts, including partial and uncertain firmware-call
progress. These checks do not execute the new markers on ARM or hardware and
grant no board permission.

The continuing claim covers TRWU8CBL only. Fresh preflight checks current
source/artifact hashes, claim, USB identity, UART owners and host route.
One volatile qualified recovery freshly admits the spare card and protected
boot files before any SD write. Verify/reuse seven retained catalog payloads
and upload the new 8,193,712-byte FIT as the eighth fixed file, then freshly
read back all eight. The native attempt may send `fesboot` once. Only a
complete native App Ready permits the existing ZX81 / JP-loop ROM / ZX81
launch-return-quit sequence, matched activation receipts and fetch/ownership
proofs. Shared USB idle reports cannot qualify physical buttons. No physical
controller action is requested.

Keep every existing bound: prompt 50s, startup/launch 100s, configuration 20s,
controls 5s, native 450s, child 480s, work 740s and host 850s. ROM transfer stays
16 bytes per step, 400M ticks and 64 polls. Failure collection is receive-only
for two seconds with a 1MiB console bound. It retains complete/partial loader
state and firmware-call uncertainty but cannot restore health after a timeout.
The first failure consumes the input; no replay, fallback or extra command.

The normal-user shared Quartus lock encloses the private top-only transport.
Programming uses 6M and cleanup restores original 15M, exact USB permissions,
private-server termination and a free UART. Host cleanup cannot prove CPU/FPGA
halt. Retain claim and spare after a stop. Factory restoration is waived;
factory card stays safe aside and TRWU8J8G receives no operations.

At source freeze this input has no hardware evidence. Next run fresh preflight
and one bounded attempt. Ordinary core loading and DE10-equivalent acceptance
remain open; record any physical result separately from model/build evidence.

The frozen operator was `5bafe64c26cc4533d18aeecdacf973f781653653`.
Fresh recovery, eight fixed uploads and eight fresh SD readbacks passed.
Exact chip/programming, SPL FIT/hash, both private exit-cache receipts,
full 31-phase runtime progression and the matched U-Boot prompt passed.
The operator sent `fesboot` once. The loader emitted `memory-before`,
`memory-after`, `snapshot-before`, `registers-before`, `registers-after` and
`cpu0-before`. No `cpu0-after`, owned receipt, native entry marker or App Ready
arrived within 100 seconds. No launch/control command followed.

The memory/EL validation and initial MPIDR/counter reads returned. The last
before receipt does not prove that the actual SMC instruction ran. The static
interval includes final UART drain/diagnostic park, `invoke_smc` argument
preparation/memset/memcpy, `smc_call`, EL3 entry/context-save/dispatch, PSCI
handling and return before the missing after receipt. Exact stopped/faulting
PC, EL3 runtime entry and later fault absence remain unobserved.

Actual selected BL31 symbols put `percpu_data` at `0x58000`, 256 bytes, inside
private BSS. Its PSCI affinity handler passes the four-byte affinity-state
member at offset `0x18` to `flush_dcache_range`; the actual helper uses
`DC CIVAC`. CPU zero's candidate member address is therefore `0x58018`.
These are source/ELF facts, not evidence that this operation executed or caused
the stop. No cache policy was changed. The interval auditor initially expected
the old uninstrumented snapshot signature; correcting it to the selected
traced call makes the source/ELF audit pass. Its failed off-board run is retained.

Independent raw replay exactly matches the stored decoder result: loader-ready,
incomplete native boot, six valid incomplete loader receipts and firmware-call
uncertainty. The two-second passive tail retains that state and restored logging;
there is no partial final loader line. Independent cleanup confirms original
15M, exact USB permissions, private-server termination and UART free. This
host cleanup cannot prove CPU/FPGA halt. Claim and spare are retained; factory
card and bottom kit remain untouched.

This is campaign stop 20, including eleven earlier pre-loader stops and now
two post-loader application-start stops. The `7f3824490` compiled input is
consumed and will not be replayed. Native startup, ROM/Run/return and ordinary
package admission remain unqualified.

Next instrument actual SMC issue/return and runtime EL3/PSCI entry, including
per-CPU cache-maintenance boundaries. Preserve existing calls, arguments,
cache behavior, ownership gates and all time/byte/poll bounds; bind and test
assembly effects off board before building and freezing a new single attempt.

Full trial evidence and incremental source pack are verified by archive
readback/import at `/home/deano/fes-retained/de25-loader-progress-stop-20261106-r1`,
with prerequisite `de25-loader-progress-20261106-r1`.
