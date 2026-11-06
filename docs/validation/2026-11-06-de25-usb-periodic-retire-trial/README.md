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
packet stops before launch; this compiled input has zero prior physical attempts.

Preparation performed no hardware operations. Subsequent physical result,
raw replay and cleanup evidence must be recorded separately. Nano halt
acknowledgement and complete core-load/return acceptance remain unproved.
