# Bounded private ROM FIFO protocol steps

The preceding physical trace spent 97.8% of its unchanged one-second budget in
ownership callbacks and reached a last written-byte observation of 1,792/8,192.
This implementation frames one natural 16-byte FIFO step with complete fresh
CPU/cold/USB/master ownership proofs. Every register access still calls the
ownership supplier and retains before/after timer checks. Inside the step it
checks private owner/generation/bindings and samples live CPU, bridge and master
reset safety. No non-secure execution occurs between these checks under the
existing secure lock and parked-secondary-CPU contract. USB/clock/producer drift
inside a step is rejected by the complete end proof before step credit or any
load completion. Data bridges remain held throughout.

A step permits at most 64 status polls, the exact received/written counters and
four ordered data writes. It cannot issue another command or touch an arbitrary
register. Offset/phase/owner/generation checks prevent nested, skipped, oversized
or replayed steps. End consumes permission before validating it. Failure and
abort never fall back; abort performs bookkeeping only, without endpoint, HPS
or timer reads. Only a qualified newer successful configuration can construct
a fresh platform object. The driver copies its trusted hooks once per load.

The legacy unbound API retains its full proof around each access and its
pre-I/O CRC rejection behavior. Native control opening remains one-shot and
retires failed activations even when no ROM register was touched. Neither the
400 MHz/400,000,000-tick limit nor the per-poll bound changes. Final byte counts,
CRC, held state, build identity and private source snapshot checks remain. No
new SMC, external capability, CPU release or package admission is introduced.

Seventeen targeted tests pass. The real shared boot/board/master and strong
callback fixture covers 47 original ownership cases and 82 complete step-driver
cases across both ROM sizes: mid-step DMA/IRQ/clock/CPU/reset and binding drift,
late generation changes, source mutation, counter/CRC errors, poll and clock
limits, callback replacement, missing hooks, malformed protocol and abort with
no further I/O. It verifies failed transfers cannot replay. Its latency model
charges 4,500 ticks per actual cold-register read, 5,000 per fast guard, 3,000
per endpoint access and explicit smaller reset/timer costs. Both sizes complete
within the fixed budget under these assumptions; the full-per-access control
exhausts it. This is a synthetic feasibility model, not physical timing.

Two separate 23-phase C/RTL runs pass for 4 KiB and 8 KiB. One uses the original
unbound provider path; the other selects the driver's step API through a wrapper
around the actual provider/control fixture. They execute actual CSR/RAM/CPU RTL
and reject source, identity, epoch, I/O, ownership, clock/poll and CRC faults.
The RTL step wrapper retains full ownership callbacks; the complete shared
platform step guards are exercised against the register model separately. This
distinction is intentional and does not establish whole-board acceptance.

Evidence is under `out/nano-rom-steps-20261105`. No hardware operation has yet
been performed for this source. A fresh matched chain and bounded TRWU8CBL trial
are required. Ordinary package admission and DE10-equivalent readiness remain
open.

## Bounded boot stopped before ROM execution

One matched attempt on TRWU8CBL passed recovery, all eight fixed uploads and
fresh SD hashes, matched native startup and serial launcher readiness. First
launch then stopped in the native USB packet path before the input-pause receipt
and before any FPGA configuration or ROM transfer marker. The driver observed
an IN data-toggle notification (RX `0xb0000`, channel IRQ `0x400`, channel
`0xe05e8808`, transfer `0x80008`), no data/completion and a continuation blocked
by channel state. Packet timeout was followed by the existing 500 ms channel
halt wait exhausting. There was no retry or subsequent native command.

The native application ELF is byte-identical to the preceding shared-ownership
trial; the firmware changed, but its new ROM path was not reached. This does
not by itself establish the cause of the USB failure. It provides no physical
ROM timing or step-implementation acceptance. Cached native state was still
Ready/generation 0 and cannot establish live driver health.

Independent cleanup verified exact USB permissions, recorded 15 MHz JTAG
readback, the owned private server's exit and a free UART. Claim and test card
remain retained; factory card and other kit were untouched. CPU and USB halt
were not independently proved. The next work is to reproduce and investigate
the USB toggle/channel-halt path without relaxing its packet, deadline or
ownership checks, then prepare a fresh trial. The failed image is retired.
