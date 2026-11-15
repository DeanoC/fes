# One bounded halt-first periodic USB trial

Compilation source: `0d7556334156bc70450a90c4e6434d2c7ff80695`.
Driver implementation: `33d40069011175f265d08e668c61db8c8cf3edc6`.
The [matched candidate](../2026-11-06-de25-usb-periodic-retire-build/README.md)
has a fresh native app/entry, BL31, U-Boot and file composition. Six changed/added
compiled sources and 2,094 unchanged combined input hashes are bound, with all
forty entry-producer records and memory/FPGA/ROM contracts unchanged. The prior
`3149fb25e` failed input is consumed and cannot be replayed.

This input replaces active periodic continuation with one request per service.
A paired toggle can request a halt only after channel/HCTSIZ, PIO/link, RX/status
and queue admission. Actual CHHLTD and both command bits clear are required
before normal same-request retry or explicit Pause cancellation. PID, buffer,
public packet and its original deadline remain retained. HCTSIZ is programmed
only on the observed idle channel for a new attempt after its advertised interval.
Pause cancellation fabricates no data, completion or NAK and advances no PID;
resume preserves the next permitted polling time. Fatal errors remain terminal.
Issued-command admission is frozen alongside the existing sequential frame/clock
observations. They add overhead and do not establish a physical wire frame.

A separate frozen operator admits one physical attempt on TRWU8CBL after exact
claim/source/USB/UART checks and fresh qualified recovery/live-card admission.
It requires eight bounded Ethernet/RAM uploads, eight SD writes and eight fresh
readbacks before native programming. The planned native sequence is ZX81 ROM,
JP-loop ROM, ZX81 ROM, with full launch/return, activation receipts and shared
USB pause/resume observations for each, followed by owned Quit. No physical
controller action is required or claimed; idle reports cannot qualify controls.
Ordinary package admission remains closed.

Sixteen off-board fixture/proof groups pass, including actual source/recovery
admission with missing halt-first/halt-proof/cancellation/deadline rejection,
changed-device rejection, upload/receipt failures, private JTAG/clock cleanup,
all boot-proof/tail decoders, per-CPU DDR/no-map reservation and fixed catalog
binding. [operator-source.json](operator-source.json) freezes their source bytes
and completed receipts. Publishing these checks is not a physical result.

Retain every original bound: prompt 50 s, startup 100 s, launch 100 s,
configuration 20 s, control 5 s, native 450 s, child 480 s, work 740 s,
host 850 s; passive tail 2 s, console 1 MiB and unchanged ROM limits;
USB drain 500 ms/4,096 steps. There is one fused physical attempt, first failure
terminal, with no replay/fallback or relaxation of errors, deadlines or ownership.
A protocol attempt inside an observed retired request cannot authorize a second
native trial or release an unhalted resource.

Hold the shared Quartus/JTAG lock as normal user and use only the exact private
top-kit server. Temporarily use 6M, then restore and independently verify 15M,
exact USB identity/permissions, private server termination and free UART. Host
cleanup does not prove a CPU/USB/FPGA halt. Keep the continuing claim and spare
card. Factory restoration remains waived; factory/bottom kit remain untouched.
There are 25 prior procedure stops, including one USB return stop and three USB
packet stops before launch; this compiled input had zero physical attempts before
its one-attempt fuse was consumed.

Preparation performed no hardware operations. The physical result below is
recorded separately from those off-board checks. General core-load/return
acceptance remains open.


## Physical result

The single attempt on operator `ec69ae251` passed in 360.44 s. Fresh recovery,
eight bounded Ethernet/RAM uploads, eight SD writes and eight fresh SD readbacks
passed before matched native programming. Tenfoot Ready appeared in 5.08 s.
Independent raw replay verifies all 31 runtime, two exit-cache, 26 loader and
48 firmware-call records, with all eight SMC/cache-flush returns and no failure
or exception marker.

All three planned serial Tenfoot cycles pass, including final owned Quit:

| Selection | Native launch/return generations | Outer launch | Configuration interval | Live fetch |
| --- | --- | --- | --- | --- |
| ZX81 ROM | 1 → 2 | 73.18 s | 12.00 s | 20,404 cycles; address 979 |
| JP-loop ROM | 3 → 4 | 73.11 s | 12.00 s | 15,318 cycles; address 0 |
| ZX81 ROM | 5 → 6 | 73.25 s | 12.00 s | 19,130 cycles; address 978 |

Each fetch observation is bound to the selected package/build identity,
activation receipt, exact owner and native generation. The held and before
cycle counters are zero; release produces positive fetch activity. The loop
selection reaches its required address zero. This proves the stated bounded
fetch observations, not emulator fidelity, video output or physical controls.
The same FPGA core image is reconfigured for these two ROM selections; these
are three repeated selected-program cycles, not three distinct emulator cores.

Three independent firmware master-stop receipts return status0/complete1 at
firmware generations5,10,15 with the same owner, reset `0xd` and data held.
The runtime returns to native Ready generations2,4,6. The final Quit is verified.
These fixed-scope receipts do not qualify general external DDR DMA/drain or
physical cache-geometry coverage.

Shared USB records exactly twelve ordered pause/resume transitions across menu,
loading, play and return, then one Stop. The controller retains USB generation2
through every transition; report counts advance 7,14,22,28,36,44 between them,
and final Stop advances the USB lease to generation3 with44 reports. No physical
button press was requested or claimed. The recorded interface receipts and exact
compiled driver establish this pause/resume integration path. No raw DTE/HCHLTD
register receipt was emitted, so a particular toggle-error recovery branch or
physical wire-frame cause is not inferred from this success.

The [lifecycle audit](lifecycle-result.json) revalidates all three activation/
fetch/return cycles, every USB event and final Quit against the raw console and
frozen inputs. [physical-result.json](physical-result.json) binds the complete
raw boot replay and eight fresh SD readbacks. All launch/control/configuration
and outer time limits are unchanged and pass. Each complete launch is about
73 s; the measured configuration interval is about12 s. The remaining launch
time is not attributed to a particular stage by this capture. Performance work
remains separate from correctness; no timeout was increased.

Independent host cleanup verifies original15M readback, exact USB identity and
permissions, the owned private JTAG server gone and UART free. The continuing
claim and spare card remain held, with factory/bottom kit untouched. No factory
restore was performed. A software Quit and host cleanup do not prove physical
CPU halt. This input is consumed after its one successful attempt, without
retry/fallback. Prior procedure-stop count remains25; no new stop is recorded.

This is an exact-artifact **hardware diagnostic pass** for the contained native
serial selected-program scope. Ordinary package admission remains closed;
general core loading, native Ethernet, physical input/video acceptance and
launch performance remain open. U-Boot upload Ethernet is not native OS network
qualification. Do not promote this result to a normal-kit or all-core pass.

Next: connect the verified native path to ordinary sealed-package admission,
reviewing its fixed catalog/boot-context restrictions rather than only changing
an acceptance flag. Retain the now-working USB lifecycle, real firmware stop,
source/identity and memory guards. Subsequent native/firmware changes require
fresh matched artifacts and their relevant physical test.

Source, frozen operator, raw trace and independent checks are retained in
`/home/deano/fes-retained/de25-usb-periodic-retire-pass-20261106-r1`, with the
matched build archive as prerequisite. Every member is read back and checked,
and the incremental source pack is independently imported and verified.
