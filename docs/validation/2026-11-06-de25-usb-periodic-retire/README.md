# Halt-first periodic USB retry and polling pause

Implementation: `33d400690` (full identity in [result.json](result.json)).
Base: `5949ccefeba906b2058ce9aaa65e6d20cf16cc3c`.
This is a hardware-independent driver prototype with source, model/race and
repository validation. No fresh native image, matched boot chain or hardware
attempt is attributed to this source.

The preceding [physical timing trial](../2026-11-06-de25-usb-frame-trace-trial/README.md)
stopped before first launch on frame-overrun. Its continuation had a 2,722 ns
CPU write bracket with no sampled frame-number change. That observation does
not identify the fault cause or reproduce the synthetic delayed-write scenario.
The failed `3149fb25e` input is consumed and cannot be replayed.

Reviewing the primary callers exposed a narrower mismatch than the generic
continuation helper alone showed. Linux v6.12
[`dwc2_process_periodic_channels`](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd.c#L2842)
limits periodic requests by the channel multi-count, which is one for this
single-packet service. The pinned TinyUSB
[slave IN toggle handler](https://github.com/hathach/tinyusb/blob/dffc57135846a4b00aca06b2f588daa6d13b67ef/src/portable/synopsys/dwc2/hcd_dwc2.c#L1031)
requests a halt, then its halt path schedules any periodic retry. These are
protocol comparisons, not evidence that either reference has qualified Nano.
Their exact source/blob/hash identities and license headers are retained;
no Linux or TinyUSB driver is linked. Reference unbounded waits are not adopted.

Direct periodic IN now gets one request per service. A paired RX/IRQ toggle
notification may request one halt after exact channel/HCTSIZ, ownership, link,
RX/status and queue admission. It cannot clear raw CHDIS and continue an enabled
periodic channel. Both CHENA/CHDIS clear and actual CHHLTD are required before
that service attempt is considered retired. Unqualified or missing notifications,
changed state, foreign DMA/interrupt modes, new RX/status and queue pressure
cannot authorize that halt/rearm. An already observed halted channel does not
need another request-queue slot to acknowledge its retirement.

Normal polling may rearm the retained public request on the observed idle
channel after its original advertised interval. Only then may HCTSIZ be written
for a new hardware attempt. The packet identity, buffer, expected PID and
original packet deadline remain unchanged; deadline admission is repeated before
rearm. A report still requires actual expected-PID bytes, completion and final
retirement. Genuine completion or NAK racing a halt uses the existing completion
path. Frame-overrun and other fatal errors remain terminal.

PausePolling can explicitly discard the observed halted toggle attempt instead
of rearming it. Cancellation fabricates no report, completion or NAK, advances
no PID and preserves the next permitted polling time across resume. The original
final PIO/link/all-channel/RX checks still gate paused state. A fresh request
after explicit resume receives its ordinary new packet identity/deadline; it
cannot extend the discarded request. An unobserved/stuck halt retains the
original packet and 500 ms/4,096-step drain limits, containment and failed owner.

The eight-command history now freezes admission samples for issued continuation,
toggle-retire and toggle-rearm commands. Later wait checks cannot replace those
samples. Formatting and copying them perform no extra hardware reads. Original
frame/clock brackets remain separate sequential observations with overhead;
they establish neither peripheral commit nor physical wire frame.

Validation:

- Two new lifecycle requirements fail against exact predecessor Go sources
  through a retained overlay, then pass with the prototype. The overlay restores
  all affected predecessor files, removes the new implementation file, and adds
  only the two requirements. Working source bytes are checked unchanged.
- Nine new methods cover 49 lifecycle scenarios: halt-before-retry, normal and
  stuck Pause, 27 ownership/transfer exclusions, already-halted retirement with
  a full queue, 12 retired-attempt event/ownership/deadline exclusions, two invalid
  report cases, saved admission, interval-preserving Pause/resume, and two races
  with real completion or fatal status. Failed instances perform no further I/O.
- Full DWC2, USB ownership and native runtime packages pass with the race
  detector. All seven independent frame-model methods remain covered; the
  delayed-write case now delays a fresh rearm after actual retirement. The
  32-case synthetic sweep still produces 28 retired reports and four terminal
  overruns. These durations are scenario parameters, not Nano measurements.
- Superseded active-periodic-continuation assertions are replaced with one-
  request/retirement behavior. All 26 remaining nonperiodic paired-toggle guards
  are retained on a bulk-IN fixture; split policy remains unchanged. The basic
  device model now clears both channel command bits on a modeled halt, rather
  than leaving CHDIS set while claiming halt completion. Separate tests reject
  missing/contradictory halt observations and stalled retirement.
- Source audit recovers the complete remaining nonperiodic helper after only
  the periodic route is changed, and the original drain source after removing
  the local draining lifetime. Failure snapshot/containment sources are exact.
  No packet PID/buffer/deadline, NAK or completion is assigned by the new path.
- All six affected-check steps pass in 320.19 s, including 1,568 parent tests,
  three skips and generated-consumer consistency. Original failing development
  logs are retained. The first policy audit confused equality with assignment;
  its corrected check passes without any production change for that correction.

This source changes the periodic driver lifecycle and internal cached state;
it changes no public input API, firmware/FPGA contract, memory reservation or
normal package admission. Model success does not establish that the physical
controller will acknowledge an earlier halt. CPU/USB/FPGA halt and ordinary
core-load/complete launch-return acceptance remain unproved. No kit operation
was performed for this milestone; the continuing top-kit claim and spare remain
held, with factory/bottom kit untouched.

Next: build a fresh native SDK/entry and matched BL31/loader/composition, bind
the compiled halt/rearm/cancellation calls and all selected source bytes, then
freeze a new one-attempt operator with the original board/boot/USB limits.
The preceding compiled input remains consumed. Never reuse its ELF, mask a
frame-overrun, fake completion or release an unhalted owner.

Logs, primary references, overlay, source bundle and evidence are retained in
`/home/deano/fes-retained/de25-usb-periodic-retire-20261106-r1`, with the preceding
frame-stop archive as prerequisite. Every member is read back and checked,
and the imported source pack is verified independently.
