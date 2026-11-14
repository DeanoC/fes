# One bounded cold-runtime diagnostic on TRWU8CBL

This freezes one fresh operator for firmware `4146f5345`, selecting the final
matched outputs from the [runtime diagnostic milestone](../2026-11-06-de25-runtime-startup/README.md).
The native application and entry remain byte-identical to `31d716957` and
the sealed FPGA payload is unchanged. The chain identity is
`b47d8054763a5543daf6f40f5b5d6832c2db88989edebaed52171c5955aafaea`;
the new FIT is 8,187,904 bytes. Pre-import-fix outputs are unselected.

The candidate proof binds the linked 136-byte UART leaf, 31 runtime phases,
17 installed source files, guard/cache/logging/fault audits, 20 unchanged
firmware-driver objects and exact native inputs. Service acceptance requires
all runtime phases alongside the existing entry/vendor/cache/logging, boot,
ownership, ROM-receipt, input and return proofs. The actual App result gate
passes the modeled three-session cycle and rejects 82 damaged records.

The actual receive-only collector and current boot decoder retain 41 runtime
cases using ARM-emitted messages in modeled boot context, including every
partial boundary, truncated, duplicate, reordered, missing, unknown and flooded
progress. Even all 31 completed runtime markers cannot qualify a loader without
the remaining boot proof. Logging restoration remains a distinct observation;
partial runtime progress never grants startup or a hardware capability. The
existing 21 fragmented logging/fault-tail cases and the active-failure/console
bound checks also pass. No outgoing command is sent by passive collection.

All source/recovery, catalog, fixed-upload/readback, private JTAG, clock and
exact-device fixture groups pass. Seven catalog files are verified identical
to the retained catalog; the eighth upload binds the final new FIT. The
source freeze and selected successful receipts are in
[operator-source.json](operator-source.json). No firmware change or new full
parent campaign was needed for these operator-only checks.

The prior 17 campaign stops remain separate: one programmer failure, two host
setup stops, four ROM deadlines and ten pre-native stops. This firmware input
has zero physical attempts at freeze. The previous `0cab5df0c` input consumed
its attempt; its later startup timeout is not replayable. Current boot/card/
CPU/FPGA state must be established afresh after that stop.

Read-only preflight requires the continuing top-board claim, frozen sources,
exact USB identity, free UART, disk and network route. Under the normal-user
shared Quartus lock, a private JTAG session must admit exactly TRWU8CBL and
chip4362C0DD at 6M. The unchanged pinned recovery then establishes its checked
loader and spare-card/protected-file state before eight fixed uploads and
eight fresh readbacks. No card swap is assumed; the factory card stays aside.

One attempt; stop first failure without retry or fallback. Keep prompt50/
startup100/launch100/config20/control5/native450/child480/work740/host850-second
limits, the passive two-second/1MiB tail and ROM16-byte/400M-tick/64-poll guards.
A before marker locates an interval; it does not prove the next call executed.
Vendor-vector fault visibility after early setup remains incomplete. Missing
progress, silence or a timeout cannot qualify boot, RAM lifetime or recovery.

Only a fully healthy loader may receive `fesboot` and attempt all three
ZX81/JP-loop/ZX81 Tenfoot launch-return-Quit cycles with activation receipts,
USB reports and pause/resume checks. No physical control action is required
or claimed. Afterwards restore/read back 15M, exact USB permissions, stop the
owned private JTAG server and independently verify free UART. Retain the claim
and spare card. Factory restoration is user-waived; the bottom kit is untouched.
Host cleanup does not prove CPU/FPGA halt.

This freeze precedes live admission and physical evidence. Ordinary core loads,
native ROM completion/Run/return and DE10-equivalent behavior remain open.

## Physical result

One attempt on operator `7918a4a31` / firmware `4146f5345` passed fresh recovery,
exact 6M chip admission/programming, eight fixed uploads, eight fresh SD
readbacks and SPL FIT verification. The final selected BL31 hash matches the
physical trace. No retry or second attempt occurred.

Entry/pass, both cache markers, normal logging restoration and all 15 vendor
markers completed. The new runtime trace captured 29 ordered checkpoints:
owned setup, GIC driver/distributor/redistributor/CPU interface, mailbox,
platform return, libraries, services, next-image preparation, runtime setup,
console completion, cold-main return and the DDR data-clean return. It ends:

```text
FES_NANO_RAM RUNTIME phase=data-clean-before
FES_NANO_RAM RUNTIME phase=data-clean-after
FES_NANO_RAM RUNTIME phase=bss-clean-before
```

No BSS-clean return, cold exit or loader prompt arrived before the unchanged
50-second prompt limit. Active commands stopped, the two-second passive tail
preserved the same partial runtime state, and `fesboot` was never sent. The
decoder retains a valid incomplete startup with no reported exception; the
host timeout is terminal. Independent raw replay exactly matches the saved
receipt. These observations locate the stop after the final marker, without
proving its UART drain returned, the next call executed or a particular
instruction faulted. Later vendor-vector fault absence remains unproved.

The [linked interval audit](interval-audit.json) places the next call at the
end of that marker's bounded UART expansion. Its original
`clean_dcache_range` target contains `DC CVAC,x0` and the exact compiled BSS
range is `58000..5dec0`. No PC/register snapshot from inside that call was
captured. Source inspection also confirms the actual vendor
`bl31_plat_enable_mmu` hook is a stub and its architecture setup does not
enable the MMU. Those source facts do not replace fresh live control checks.

There are now 18 campaign stops, including eleven pre-native stops. This
firmware input has consumed its single attempt. No native startup, ROM
completion, Run, return or ordinary core-load acceptance is established.
Independent cleanup confirms 15M readback, exact USB permissions, termination
of the owned private JTAG server and free UART. Claim/spare remain retained;
factory card and bottom kit were untouched. CPU/FPGA halt is unproved.

The [next prepared plan](next-plan.json) qualifies a private cold-exit BSS
clean by set/way, after proving control history, cache/alias assumptions and
fresh exclusive CPU/master ownership. It preserves original DDR/warm paths
and the operation's writeback intent. It must review the actual `DCCSW`
preprocessor selection, including the vendor erratum alias to clean/invalidate,
rather than assume an operation number. This is not an implemented change or
a justification for skipping cache maintenance. Its fresh image and operator
must retain every existing bound and fail without retry/fallback.

Exact [physical result](physical-result.json),
[independent replay](independent-boot-replay.json),
[cleanup](cleanup-evidence.json), operator helpers, fixture receipts and raw
trace are retained with verified source-pack import and archive readback at
`/home/deano/fes-retained/de25-runtime-startup-stop-20261106-r1`, with matched
build prerequisite `de25-runtime-startup-20261106-r1`.
