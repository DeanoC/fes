# Independent USB periodic timing model

Base: `0955ccf80183de694ec597076f9e129a5b9f94e6`.
Implementation: `ee8c6a0b0ae684c4e522746d34f10021f079ddd6`.

This test-only milestone adds an independent frame clock and scheduled wire
and halt events to the existing DWC2 device/FIFO model. The production USB
driver, native program, firmware, FPGA inputs and ownership rules are unchanged.
No native chain was rebuilt and no board operation was performed.

The preceding [bounded physical trial](../2026-11-06-de25-usb-toggle-trial/README.md)
exercised the qualified continuation, then failed on HCINT frame-overrun before
the first launch. Its containment failed. That attempt remains consumed;
these model checks neither repair the controller nor authorize a replay.

The new model interprets the parity actually written to HCCHAR, derives HFNUM
from its own clock, accounts for finite request slots and queued wire time,
and makes reports and channel retirement arrive at separate scheduled events.
It does not read driver completion flags or an intended target frame. An
optional paired toggle/disable notification injects the captured observation;
it makes no claim about that observation's physical cause. Timing values and
channel-write delays are synthetic scenario parameters, not Nano measurements.
Only direct periodic IN is covered; split scheduling and PHY timing are outside
this model's scope.

The seven new test methods pass with the race detector. They check deferred
wire data and halt, frame-budget exhaustion, competing queued requests, a
28-case caller-gap/frame-wrap/toggle matrix, delayed continuation commits with
successful or stuck containment, queue pressure/NAK/stalled halt, and a 32-case
phase/write-delay/wire-time sweep. The full DWC2, USB ownership and native runtime
race suites also pass. All six selected affected-check steps pass; exact run
receipts and parent test counts are bound in [result.json](result.json).

The [sweep](sweep.json) delivers 28 reports only after real modeled data and
retirement; four scenarios stop on frame-overrun without input. Caller gaps
alone do not cause overrun in the tested matrix. A 200 ms caller gap after a
toggle instead exhausts the original packet lifetime. Delaying the channel
write can produce an overrun when the selected frame lacks sufficient time.
These distinguish possible mechanisms without identifying the physical cause.
The existing deadline, fatal error, PID and terminal ownership rules remain
required in every scenario.

During this software work Deano reported an accidental power interruption to
TRWU8CBL, with power restored. Read-only host checks again saw its exact USB
identity and serial alias. No programming, upload or serial trial was running.
The continuing claim is retained. Boot state and card contents have not been
reverified since the interruption; the next physical operation needs a fresh
preflight. The factory card and bottom kit remain untouched.

Next: obtain source-bound observations of frame selection and channel-write
timing in a fresh matched bounded diagnostic, then choose a scheduler repair
from that evidence. Do not mask frame-overrun, manufacture completion, extend
the packet deadline or release ownership without an observed halt. Ordinary
core admission and complete physical launch/return acceptance remain open.

Reproducible logs, check scripts and a source update bundle are retained at
`/home/deano/fes-retained/de25-usb-frame-model-20261106-r1`, with the preceding
`de25-usb-toggle-stop-20261106-r1` archive as prerequisite. The archive is read
back and every member hash and imported source pack are checked independently.
