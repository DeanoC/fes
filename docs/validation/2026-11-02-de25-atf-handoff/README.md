# Final SPL handoff observation: uploads pass, native boot remains stopped

TRWU8CBL verifies the new firmware and sealed package on its spare SD card,
then stops after `FES_NANO_SPL HANDOFF` at the unchanged 50-second prompt
bound. Neither the new final ATF observation nor either EL3 capture marker
appears. No `fesboot`, native SD write, FPGA DDR request, drain test or runtime
core launch occurs. The [hardware experiment](experiment-result.json) remains
failed; the [independent audit](hardware-evidence.json) verifies the saved
observations and cleanup.

Tested checkout: `82c410cc8a5af4fa2c77d535b3bd22eb7a19fa19`.
Native producer: `05006009e5a27deedd804e8711aa9bdecd3546b9`.
Native boot ID: `48e13760760a4451`.
The producer and tested checkout differ only in an export test fixture.
[Reuse verification](native-build-reuse.json) checks every selected production
input against the compiled chain; it does not describe a new build at the
later checkout.

## Loaded firmware guard

The matched SPL checks fixed loaded BL31 bytes against its compiled size and
SHA-256 before branching. It also checks target and argument ranges, completed
cold containment, EL3, disabled MMU/data cache, masked exceptions and primary
CPU identity. Raw SHA calls avoid the watchdog/device callbacks used by the
earlier pre-containment image checks. No cache, reset or bridge policy changes.

The checked `atf_handoff` descriptor supplies these expectations to the serial
decoder. Current composition requires `spl_atf_trace=true` and the ordered
`FES_NANO_SPL ATF` observation, then matching capture pointer/cookie. Missing
observations cannot select a weaker format or make the loader ready.
[Pipeline checks](atf-pipeline-verification.json) exercise the actual emitted
C and recursive complete configuration/DDR decoding with that descriptor.

[Linked selection](selection-proof.json) verifies the strong firmware symbols,
raw SHA calls and generated seal. Six linked BL31 startup/setup/console/capture
functions retain the preceding candidate's instruction bytes and addresses.
The sealed FPGA package and DDR contract are unchanged. BL31 is 90,173 bytes;
this run does not verify its loaded-memory digest because the final guard
observation is absent.

## Software checks

All ten selected [software scopes](software-result.json) pass, including native
launch/return and failure integrations, firmware/RTL and producer integrations,
and 1,577 FPGA tests with 75 skips. The selected parent suite covers all 1,271
test IDs with two skips in three intact-module partitions, each capped at
300 seconds. [Coverage](parent-coverage.json) checks exact selection, uniqueness,
module boundaries, child exits and log hashes. That partitioned scope uses an
adapter to the planned aggregate command; the other scopes retain their
planned commands. [Generated checks](make-check.log) cover 35 consumers and
37 fixture copies.

The original 300-second aggregate run and its nine stale export-fixture errors
remain in the retained build directory. The repair supplies the new exact-size
argument and trace stub to those fixtures. The corrected partitions pass with
their original per-job bound. No interrupted aggregate is promoted to a pass.

The continuation adds [44 orchestration cases](experiment-fixtures.json),
[16 readback cases](readback-fixtures.json), six private transport/clock/coverage
checks and compilation/global-name screening. They exercise failed or stale
receipts, ownership loss, short reads, hash mismatch, missing prompts, native
failure and cleanup failure without opening devices.

## Transport stop and bounded continuation

The [first run](original-experiment-result.json), 10:39:18–10:40:42 UTC,
uploads and independently reads back the exact 6,499,492-byte native FIT.
Its next recovery query reports [a broken JTAG chain](broken-chain-query.log)
despite exit status zero. Correct-ID validation stops before second recovery
programming, package upload or native execution. The [stop audit](transport-stop-evidence.json)
verifies that boundary and cleanup.

Two bounded top-only [chain observations](jtag-observation.json) subsequently
return `4362C0DD` at 6 MHz, without programming, HPS reset or SD writes. They
restore the original 15 MHz clock and stop their owned private service.

The separately fused [continuation](experiment-result.json), 10:56:10–10:59:06
UTC, performs one fresh qualified recovery boot and uploads/reads back the
1,645,568-byte package. A fresh [read-only FIT check](live-fit.json) verifies
the existing firmware again after that upload, without rewriting it. Both
protected boot files and recovery manifest remain unchanged. The original
qualified recovery producer is retained as a pinned, hash-checked dependency;
its binaries are unchanged.

Native programming completes. The [1,602-byte trace](native-console.log)
passes initial CPU/DDR/sealed-FIT/containment checks and ends at the handoff
line. The new guard gives no final observation or rejection, and secure
startup supplies no marker. The loader never becomes ready.

The [linked interval check](handoff-interval-evidence.json) identifies the
handoff output call at `0x1c10`, followed by `DSB SY` at `0x1c14`, return, the
guard call at `0x398c` and eventual BL31 branch at `0x39a8`. These are linked
instruction addresses, not an observed CPU program counter. The remaining
interval includes output completion, the barrier, return, argument/state
checks and loaded-memory hashing. The trace proves no precise fault location
or BL31 entry. The next diagnostic needs observations before/after that
barrier and at guard entry, preserving the barrier and admission checks.

The original clock is restored and [read back](clock-restored-readback.log)
while the private service is live. That service then stops, exact USB
permissions are restored, and the independent audit verifies a free serial
port and continuing claim. This is the second failed hardware run on this
checkout; no further HIL is attempted. The diagnostic and spare card remain
on the exclusive top kit under the user's retention policy. No factory
restore, recovered prompt, claim release or physical core acceptance is
claimed. The full runtime/core-load objective remains open.
