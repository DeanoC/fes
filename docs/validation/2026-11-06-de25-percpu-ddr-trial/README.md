# One bounded initialized-per-CPU DDR trial

The compiled firmware is `718b5c1427cecce2dc64868de8a9df7b599abf3c`.
Its initialized per-CPU array is DDR `0x80036000..0x80036100`, with the first
affinity state at `0x80036018`. The original `DC CIVAC` and all original PSCI
operations are retained. The format-4 private BSS ends at `0x5ddc0`; the native
application and entry remain the exact retained `31d7169577` artifacts. Both
loader/FIT DTBs reserve `0x80000000..0x80200000` as no-map; the compiled loader
requires one exact matching LMB reservation before initial CPU queries.

This is a new input after the consumed `23db3f0d5` trial stopped at
`cache-before`. That stop, its incomplete-call uncertainty and independent
cleanup remain retained. No retry of that input is authorized by this record.
The campaign contains 21 prior procedure stops, eleven before the loader and
three in the post-loader/application-start interval. This new firmware has
zero attempts before admission. One attempt is selected, first failure terminal.

The separate operator checks actual initialized ELF placement, immutable
bytes, both DTB hashes and exact reservation evidence. Its 25 DDR rejection
cases execute the actual admission helper. The existing upload, service,
source, transport and receive-only failure-tail fixtures remain required.
The copied call-tail fixture's first run used the old ARM output directory;
its corrected run consumes the new source-bound ARM console. Both receipts
are retained. Missing/invalid/reordered/fragmented diagnostics grant no boot
permission. Every healthy-service result needs all 48 CALL, 26 LOADER,
31 RUNTIME, both EXIT_CACHE and existing cache/vendor/guard receipts.

Fresh read-only source/claim/UART/exact USB admission precedes any operation.
The user-reported power interruption invalidates prior live state. Use one
qualified recovery, seven byte-identical catalog payloads plus this new FIT,
eight checked Ethernet uploads and eight fresh SD readbacks. Then select the
retained ZX81 ROM, JP-loop ROM and ZX81 ROM through native serial Tenfoot,
requiring matching activation receipts, USB report progress, pause/resume and
owned return/quit. This does not require or claim a physical button press.

All bounds remain: prompt 50s, startup/launch 100s, configuration 20s,
control 5s, native 450s, child 480s, work 740s, host 850s; failure tail 2s/1MiB;
ROM observer 16 bytes/400M ticks/64 polls. The top kit alone is selected by
TRWU8CBL and JTAG `4362C0DD`. The normal-user shared Quartus lock spans the
attempt. Use the private top-only JTAG server, temporarily set 6M, then restore
15M, exact USB permissions, no owned JTAG process and free UART. No fallback,
reset-bit bypass, deadline expansion, factory-card write or bottom-kit access.

The spare remains in the top board and its continuing claim stays held.
Factory restoration is waived. Host cleanup does not prove CPU/FPGA halt.
This trial confers no ordinary profile admission, physical HDMI/native-network
acceptance or DE10-equivalent loading status, even if all three cycles pass.
The full active goal remains open until the normal package-loading path and
its required device/lifecycle behavior are verified.

## Observed result

The one attempt on operator `8c7d3e22e046952d7b0dbffa67f8f414ec3372aa`
completed recovery, all eight verified uploads/fresh SD readbacks, matched
programming and firmware boot. All 31 runtime, both exit-cache, 26 loader and
48 call receipts completed. All eight initial/late affinity cache flushes and
SMC calls returned. The loader passed its new firmware-reservation gate. This
observes the software reservation gate; it does not independently qualify
physical protection of the whole DDR region.

Native Tenfoot became ready in 5.10 seconds. The first ZX81 ROM launch completed
in 73.16 seconds under the unchanged 100-second bound, with the matching retained
ROM activation identity. Its bounded fetch observer held cycles at zero, then
saw 19,150 cycles advance to address 975 in 122.64ms, one poll under the existing
one-second/64-poll guard. This is an observed native ROM launch and execution
counter, without physical video or button acceptance.

Stop returned firmware master-stop generation 5 with reader drain, disabled
probe, complete=1 and held data. Native ownership advanced to parked generation
2. USB polling drain then timed out; its containment wait could not observe
CHENA clear at register `0x500` (value `0xe05e8808`) within the existing 500ms
bound. The software ring shows seven arm writes followed by the containment
write on packet 53. It does not establish the controller cause or prove USB
channel halt. The application panicked; no next launch, retry or Quit was sent.
The requested three-cycle sequence failed, and no complete owned return or
ordinary core-loading acceptance is claimed.

The first observation script supplied the master-stop result generation to a
validator that expects the preceding configuration generation; the corrected
replay passes with generation 4 and checks the actual generation-5 receipt.
Both off-board attempts remain retained. Independent boot replay matches the
raw console and original decoder. Independent cleanup verifies restored 15M,
exact USB permissions, the owned private JTAG process gone and free UART.
No hardware retry occurred. Campaign procedure stops are now 22; the eleven
pre-loader and three application-start stops remain distinct from this first
USB-return stop. The consumed matched chain will not be replayed.

Next audit USB polling drain, periodic/idle packet completion and return
ordering against the source and injected controller. Preserve the original
500ms drain/halt bounds, terminal containment and ownership retention. Do not
release ambiguous USB state or substitute a new timeout for a completion proof.
The FPGA Park receipt does not prove USB halt; claim and spare remain retained,
factory restoration waived, factory/bottom untouched, CPU/FPGA halt unproved.

Exact observations and source/evidence hashes are in
[physical-result.json](physical-result.json) and
[usb-stop-interval.json](usb-stop-interval.json). Source pack and trace retention
use `/home/deano/fes-retained/de25-percpu-ddr-stop-20261106-r1`, with prerequisite
`de25-percpu-ddr-20261106-r1`. The full active goal remains incomplete.
