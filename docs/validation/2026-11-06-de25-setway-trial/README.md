# Bounded cold set/way startup trial on TRWU8CBL

This freezes one fresh operator for firmware `0cab5df0c`, selecting only the
final BL31, U-Boot and composition outputs from the sibling
[set/way validation](../2026-11-06-de25-setway/README.md). The native application
and entry remain byte-identical to `31d716957`; the sealed FPGA is unchanged.
The matched FIT is 8,187,904 bytes and chain identity is
`e86db075cff9809a7679ca227bf1a69b6b1d71e39b4891bb1336c846da04615c`.

The candidate proof binds the actual linked helper, mode0/DC ISW selection,
uncached SPL controls, selector checks, exclusive cold ownership, unchanged
20 firmware driver objects and source-bound RAM contract version2. Existing
126 off-board ARM cases and final affected checks are reused for those exact
inputs. They do not establish physical cache coverage or native boot.

The operator requires both ordered CACHE receipts and complete normal logging
restoration, alongside ENTRY/PASS, all15 vendor checkpoints, empty fault
inventories and existing ownership checks. Its actual App acceptance gate
passes the modeled three-session cycle and rejects74 damaged receipts,
including missing, partial, invalid, reordered and duplicate cache evidence.
The actual receive-only failure collector and real decoder pass21 fragmented
cases using retained ARM exception traces and explicit damaged variants.
A canonical delayed-status failure after verified restoration is recorded as
`restored-with-error`; it still rejects boot. Malformed, duplicate or incomplete
terminal evidence remains unproven. Cache progress is diagnostic only.

Source/recovery, fixed-upload/readback, private JTAG, clock, exact-device and
console-bound fixtures pass. All seven catalog files are verified identical
to the prior catalog; the eighth upload binds this final FIT. Offline fixture
preparation failures are retained: a one-line fault had no remaining fragment,
an unterminated line is deliberately ignored by the decoder while the host
failure gate stops, and a clock fixture retained the old campaign count.
Corrections changed no board state. The selected successful receipts and
exact helpers are in [operator-source.json](operator-source.json).

There are16 retained campaign stops: one programmer failure, two host setup
stops, four ROM deadlines and nine pre-native firmware stops. This new input
has zero physical attempts at freeze; consumed predecessors cannot be replayed.
Deano reported another power interruption during host preparation. USB identity
and the serial link are present, but boot/card/CPU/FPGA state must be freshly
established. No programming, upload or SD write was running in this session.

Read-only preflight checks the continuing top-board claim, frozen source,
exact USB identity, free UART, disk and network route. The normal-user shared
Quartus lock surrounds the private JTAG session. Exact TRWU8CBL/chip4362C0DD
admission at6M precedes recovery; the pinned recovery must establish its
checked loader and spare-card/protected-file state before eight fixed uploads
and eight fresh readbacks. Factory card and bottom kit remain untouched.

One attempt, stop first failure, no retry or fallback. Keep prompt50/startup100/
launch100/config20/control5/native450/child480/work740/host850-second limits,
receive-only2-second/1MiB fault tail, and ROM16-byte/400M-tick/64-poll guards.
Only a fully healthy boot may issue `fesboot` and attempt the three Tenfoot
ZX81/JP-loop/ZX81 launch-return-Quit cycles, activation receipts, USB report
and pause/resume checks. No physical input action is required or claimed.

Afterwards restore/read back15M, exact USB permissions, stop the owned private
JTAG server and independently verify free UART. Retain claim/spare; factory
restoration is user-waived. Host cleanup does not prove CPU/FPGA halt.
This freeze precedes live admission and physical evidence; ordinary core-load
acceptance, HDMI, native Ethernet and DE10-equivalent behavior remain open.

## Physical result

One attempt on frozen operator `7126320b4` / firmware `0cab5df0c` completed
fresh post-interruption recovery, eight uploads and eight fresh SD readbacks,
exact6M chip admission/programming and SPL FIT verification. The trace binds
the final selected BL31 hash. No retry or second attempt occurred.

The new cache path returned with both ordered CACHE markers and verified
normal decode-logging restoration, without a reported status failure. All15
vendor assembly markers completed, including BSS clear, stack setup and early
C setup. BL31 then printed its version, successful USB setup, owned-boot
result0/reset`0000000f`, and finally:

```text
NOTICE:  FES_NANO_SD_PLATFORM setup=1 ready=1
```

This physically crosses the preceding repeated cache-maintenance failure.
These progress receipts do not independently measure cache geometry coverage,
prove every RAM byte or qualify the complete reservation/lifetime contract.
No exception was reported; absent output does not exclude a later fault under
vendor vectors. The exact subsequent stalled or failing instruction is unknown.

No loader prompt arrived within the unchanged50-second prompt bound. The
operator stopped, retained its original two-second receive-only tail and
never sent `fesboot`. No native startup, ROM completion, Run, return or ordinary
core-load acceptance is established. The decoder's error list is empty because
it received an internally valid but incomplete boot; the host timeout is the
terminal failure. Independent replay exactly matches the saved boot receipt.
There are now17 campaign stops, including ten pre-native stops. This input
has consumed its one attempt.

Independent cleanup confirms15M readback, exact USB permissions, owned private
JTAG termination and free UART. Claim/spare remain retained, factory card and
bottom kit untouched. Neither host cleanup nor silence proves CPU/FPGA halt.
The saved cache/logging completion is distinct from the still-open whole-boot
and native ownership evidence.

The next [prepared diagnostic plan](next-plan.json) pins the actual vendor
platform/main/GIC source and calls after the last SD marker: GIC setup,
mailbox notification, runtime services and nonsecure handoff. Their source
order is a search boundary, not attribution of the failure to any one call.
The plan requires bounded call-boundary diagnostics and linked/off-board
proofs before another freshly frozen hardware input; it is not implemented.

Exact evidence: [physical result](physical-result.json),
[independent boot replay](independent-boot-replay.json), and
[cleanup](cleanup-evidence.json). Source, successful and failed preparation
logs, raw traces and receipts are retained with verified source-pack import
and archive readback under
`/home/deano/fes-retained/de25-setway-stop-20261106-r1/`, using the matched
build prerequisite `de25-setway-20261106-r1`.
