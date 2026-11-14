# DWC2 polling keyboard and hub prototype

The Nano provider now opens USB0 (`0x10b00000`) through this polling PIO host.
It uses no DMA buffers, GIC or Linux service. The existing shared USB descriptor,
boot-report and normalized input decoders remain the application boundary.
QEMU retains its separate xHCI driver.

The prototype admits DWC2 2.x/3.x host-capable cores with dynamic FIFOs, at
least 256 FIFO words and an already configured 60 MHz ULPI PHY. Firmware must
release all channels, disable DMA, establish SoC clocks/reset/pinmux and prepare
the board's VBUS source and Type-C role. The driver validates those controller
capabilities, resets the core, partitions/flushes FIFOs, forces host mode, powers
and resets the root port, then enumerates one boot keyboard, directly or through a bounded USB 2 hub
tree. High-speed hubs use transaction translators for low/full-speed children. PHY and VBUS electrical operation remain untested on Nano.

Control transfers perform SET_ADDRESS, descriptor discovery, SET_CONFIGURATION,
SET_PROTOCOL and SET_IDLE. Boot keyboards use a100ms nonzero idle duration
(25 four-millisecond units, report ID0), so the device supplies actual current
snapshots even without a key change. The menu/core release fence can therefore
arm from an idle keyboard without discarding a user's first press. Duplicate
held snapshots produce no new key edge; NAK is still not a release. The endpoint
poll interval and existing transfer/startup bounds remain. This uses the
[HID1.11 Set_Idle contract, section7.2.4](https://www.usb.org/sites/default/files/hid1_11.pdf).
The host uses bounded, single-packet transactions
with endpoint data toggles and explicit channel retirement. Interrupt polling
never waits for device input: a fixed amount of FIFO/status work advances a
pending transfer, NAK retains the data toggle, and the endpoint interval gates
new attempts. All waits have time and poll limits, including frozen-clock cases. Initialization
and the complete hub traversal share a four-second deadline, inside the native
caller’s five-second startup context; per-request limits cannot extend it.
Reports remain private until both transfer completion and channel retirement.
Only IN-data receive entries supply payload byte count and PID. Completion and
channel-halted entries may retain those fields from a preceding packet; the
driver consumes no additional FIFO payload for them. HCINT completion, a real
IN-data entry and channel retirement remain required before reporting input.

Disconnect, STALL, unexpected RX status/channel/PID, overflow, missing or short
reports, hardware errors and ambiguous timeout retire the host. It attempts a
bounded channel halt and refuses further hardware access through that instance.
Invalid receive entries retain a non-destructive GRXSTSR sample, the one popped
GRXSTSP value and the surrounding interrupt/channel/port/reset registers. Status
zero remains an error. This snapshot diagnoses the first physical Nano failure;
it does not establish its cause or make a failed channel safe to reuse.
The board wrapper permits one initialization attempt per boot. Hotplug recovery,
HID output reports, USB 3 hub protocols and hotplug re-enumeration
are not implemented. Hub paths are checked and retired on change. The API has one polling owner; concurrent calls are not
supported.

`PausePolling` drains the pending packet or hub-health operation within 500 ms
and 4096 steps, discarding any report. It requires disabled DMA/interrupts,
an intact link, every admitted channel idle and no unread receive status before
publishing the paused state. Subsequent polls perform no register access.
`ResumePolling` clears only this local pause; it does not reset or enumerate the
device, repair a failed host, or change its endpoint PID. New packets and hub
checks get their ordinary fresh deadlines. The typed gamepad view exposes the
same operations. This supports retaining the controller across long core loads;
model tests cover a 100-second gap, not physical combined-operation acceptance.

If the drain exhausts its existing 500 ms or 4096-step bound, the first error
now retains a pre-containment observation: which bound fired, elapsed time,
pending packet/path-health state, saved continuation/toggle information and
eleven sequential non-destructive controller reads. These include RX-peek,
IRQs, channel, transfer, port/reset, queue and frame state. The observation does
not pop RX or payload, issue a command, change PID, retry or release the source.
The subsequent original halt attempt may still fail. The frozen first error
and terminal ownership remain retained; register samples are neither atomic
nor a USB-completion proof. This supports diagnosing the native Stop failure
that occurs before normal return and causes emergency FPGA Park.

Fatal channel interrupts also retain a read-only register observation before
the existing halt attempt. It keeps the original interrupt bits alongside later
channel, transfer, queue, receive-peek, frame/interval, PIO and link observations.
No receive entry or payload is popped by this capture. In particular `0x80`
is transaction error, distinct from frame overrun `0x200`; the timing trace's
`window_ns` is the measured interval around the software command, not the
remaining frame budget. A failed halt still permanently retires the host.

`cmd/keyboard` is a serial-only bring-up image: initialize the selected backend,
print `FES_KEYBOARD_READY`, then print normalized key events. It needs neither
storage nor a working FPGA display. `run.py keyboard --board qemu` exercises
the same normalized path through an actual QEMU USB keypress. A Nano cross-build
requires the firmware profile's USB readiness and 60 MHz clock declaration.

Registers and transaction behavior were checked against the primary Linux
[v6.12 DWC2 sources](https://github.com/torvalds/linux/tree/v6.12/drivers/usb/dwc2):
`hw.h`, `core.c`, `hcd.c` and `hcd_intr.c`. The hardware-definition header is
Copyright 2004-2013 Synopsys, under GPL-2.0-or-later OR BSD-3-Clause. The Go code
is an independent exploratory implementation; neither a Linux driver nor a
vendor BSP is linked into the native image. The validation receipt records
reference file hashes. Register/device model tests prove software behavior,
not PHY timing, electrical safety or physical USB compatibility.

The controlled Nano entry point wraps this host in `usbcontrol.Source`. `Open`
returns a partial host on initialization or enumeration failure so its owner can
retain it through lease cleanup. Every transfer step rechecks polling PIO mode,
interrupt masking and descriptor-DMA disable. If DMA unexpectedly becomes
enabled, the host retires without rewriting or halting an uncertain channel.

The explicit Nano `usbcheck` build selects the source-bound secure provider and
`OpenKeyboardWithControl`, rather than asserting readiness at entry. It verifies
the cold context/pins before acquiring USB, confirms live readiness after driver
startup and stops after a bounded polling interval. This does not enable USB
for storage/Tenfoot profiles or establish physical hub/input qualification.

Hub discovery admits at most three hub levels, 15 ports per hub, 31 total ports
and 31 addressed devices. It powers ports, requires stable connection, checks
reset completion, validates descriptors and rejects multiple boot keyboards.
Valid non-keyboard devices remain unconfigured. Multi-TT hubs use alternate
zero; no alternate setting is switched. The selected keyboard and translator
route stay private until the complete topology is admitted.

Low/full-speed transfers behind a high-speed hub use start/complete splits
with explicit hub/port routing, ACK/NYET/NAK handling and channel retirement.
OUT payload bytes are sent only with start-split. A complete-split never repeats
them. Transactions start early in a USB frame; missed completion windows retire
the host. High-speed frame intervals use 125 microseconds; keyboard intervals
use the speed-specific USB encoding. Split progress gets at most 2 ms/4096
steps per packet poll. Periodic hub-path health checks advance through bounded
control steps; one public poll spends at most that budget plus one final packet
step. No control wait blocks until user input. Disconnect, reset or overcurrent
on an ancestor path retires the host, without adopting a replacement device.

The model independently implements device addresses, hub power/reset/status,
transaction-translator ownership, split timing, FIFO contents and channel
halts. Tests cover nested high/full-speed hubs, keyboard selection, normalized
input, frame wrap, NAK/NYET, bad descriptors, stalled frames, ambiguous devices,
ancestor changes and firmware-owned source closure. These are model results;
physical split scheduling, hub/keyboard compatibility and Type-C power still
require a bounded Nano test. Linux v6.12 `hcd.c`, `hcd_intr.c`, `hw.h` and
`usb/core/hub.c` are the primary protocol references. Their version/hashes are
retained with validation. No Linux driver is linked into the image.

`OpenGamepad` selects one alternate-zero non-boot HID candidate using the same
bounded topology and PIO transport. It retrieves the exact interface report
descriptor, validates a supported Joystick/Game Pad application and checks that
reports fit the endpoint before readiness. It does not send keyboard boot
protocol requests. Its typed host supplies `PollGamepad` and a copied report
descriptor; it cannot supply `PollKeyboard`. Unsupported schemas retain the
captured descriptor for diagnosis while preventing readiness and input polling.
The explicit Nano `usbcheck --usb-input gamepad` image uses this path under the
secure lease. Model and cross-build results do not qualify physical gamepads.

For a direct nonperiodic IN transaction, RX status5 and HCINT data-toggle notifications are
non-payload events. The core can discard the unexpected PID and continue the
already armed packet. The polling driver clears only that interrupt and keeps
the existing deadline, expected PID and channel. It does not reissue setup or
arm a replacement packet in response to that notification. Actual IN data,
correct size/PID, completion and channel retirement are still required. OUT
and split/translator toggle errors remain fatal. This follows the receive FIFO
and IN-toggle handling in the pinned Linux v6.12 DWC2 reference; no Linux code
is linked. Physical behavior still needs a fresh matched test.

PIO mode queues further IN tokens within an unfinished direct nonperiodic
transfer, at most one per poll and only when its request queue has space.
Continuation preserves setup, HCTSIZ, PID, buffer and deadline. New RX/status,
zero remaining packet count, completion or software halt prevents further
queuing; an unexpected packet count fails. A channel with CHENA clear waits
for status under the existing deadline, including completion races. Direct
periodic single-packet services use the separate retirement path below.
This follows the reference's `dwc2_hc_continue_transfer` slave-mode path. The
model requires a real queued continuation at descriptor offset88; acknowledging
the notification alone cannot supply the remaining descriptor bytes.

Control failures now retain request type/value/index/length, setup/data/status
phase and byte offset. Packet failures add endpoint, expected PID, split state
and notification counts. A later failure preserves the first RX toggle snapshot,
including both transmit queue states and frame number. These observations
cannot authorize reports, bypass closure or release uncertain ownership.

Non-split IN ACK-after-error notifications are cleared without completing the
packet, changing its PID or resetting its deadline. A late ACK must not keep
the continuation helper waiting forever; split ACK retains its translator
state machine and OUT/PING behavior is unchanged. Fatal status still takes
precedence and ACK alone cannot supply a report.

Failures include current done/NAK/halting flags, ACK counts and the last
continuation decision. Its sample bitmap identifies actual queue/global IRQ/
channel IRQ/channel/transfer/frame reads (bits0–5); unsampled fields are not
observations. Interval waits retain their measured wait time. These historical
samples identify which check blocked progress without additional destructive
register reads on the failure path.

A fixed eight-command history now retains every software channel arm,
continuation, retirement and failure-containment write across packet changes.
Each entry carries a sequence, packet number, written value, expected PID and
completion flags. The first toggle snapshot freezes the history at that
observation; a failed containment also records the later halt command. Entries
are software intentions, not register readbacks or proof of a physical halt.
Recording performs no additional register reads or clock samples and changes
no channel command, PID, interval, deadline or ownership decision.

A fresh physical first-toggle trace observed raw CHDIS before any software
disable command for that packet. Following the pinned PIO continuation
reference, the nonperiodic helper clears raw CHDIS only within the same owned,
active, non-split IN packet after both RX and IRQ toggle notifications and a saved first
observation. The software fault/halt/completion/NAK/RX checks still exclude
continuation. The enabled channel's remaining fields must match its original
recipe; fresh PIO/link admission and exact unchanged HCTSIZ size/PID/count are
required. New RX or status sampled during admission blocks the write. Unqualified
disable state waits without being cleared. No setup, PID, HCTSIZ, buffer or
packet deadline is reinitialized; advertised periodic spacing remains required.

Captured-state model tests require actual expected-PID bytes, completion and
channel retirement before input. Missing data, wrong PID/short report, expired
packet, software halt/fault, changed channel/transfer, foreign DMA/interrupts,
disconnect, queue pressure and completion races retain their failure/ownership
rules. The former unqualified-disable case is now separate from this paired
notification case. A model continuation cannot prove the physical disable cause
or Nano hardware readiness; that still requires a fresh matched bounded test.

`periodic_model_test.go` adds a separate clock and event queue for direct
interrupt IN requests. It derives frames and remaining ticks, consumes request
queue slots and configurable wire time, and delays data and channel halt until
their scheduled events. Driver state does not supply its target frame or
completion. Tests cover frame wrap, caller gaps, queue pressure, NAK, delayed
request writes, frame overruns and stalled retirement. The original
deadline, real data and observed halt remain required before input delivery.

The modeled durations are synthetic scenarios, not measured Nano timings.
Caller gaps alone do not produce overruns in these scenarios; a late channel
write or queued wire work can exhaust a selected frame. This distinguishes
possible timing mechanisms without establishing the physical failure's cause.
The latest bounded Nano trial exercised the qualified continuation, then stopped
on HCINT frame-overrun with failed containment. The model does not authorize a
retry, hide that error or change the production scheduler. Split/PHY timing and
the actual frame selected by the physical controller remain outside its scope.

Direct periodic arm/rearm commands retain the full frame sample used
for parity selection and two additional HFNUM observations immediately around
the software write. Three extra clock samples bracket selection and that write
window. The fixed eight-command history stores these observations separately
from the written value; formatting performs no new I/O. Split, nonperiodic,
retirement and containment writes keep their original basic recording path.
Issued continuation, toggle-retire and toggle-rearm records also freeze their
admission samples; later checks cannot replace them with a wait reason.
Packet errors also retain the last and maximum gap between step clock samples,
reusing the existing deadline clock without an additional service clock read.

These samples are sequential and add bounded observational overhead. The write
window includes frame reads and bookkeeping; it is neither an exact bus-commit
timestamp nor proof of the controller's chosen wire frame. A zero first service
gap means no preceding step was sampled. The observations never select another
frame, change a command/PID/buffer/deadline, clear an error or authorize input,
retirement or reuse. Fresh native app/entry and matched firmware are required
before using the new trace in another bounded Nano diagnostic.


Direct periodic IN now queues one request per service, matching the pinned
Linux caller's request/multi-count budget. A paired RX/IRQ toggle notification
cannot clear raw CHDIS and continue an enabled periodic channel. The driver
checks the exact channel recipe and unchanged HCTSIZ, PIO/link ownership, RX/
status and queue capacity, then requests one halt. Both CHENA/CHDIS clear and
an actual CHHLTD observation are required before the attempt is retired.
A new event or changed owner/transfer cannot be cleared away to permit a retry.

During ordinary polling, a retired attempt may be rearmed after the original
advertised interval. HCTSIZ is written only on that observed idle channel;
the public request, packet identity, buffer, expected PID and original deadline
stay intact. A subsequent report still needs actual expected-PID bytes,
completion and final retirement. A real completion or NAK racing the halt uses
the original completion path. Nonperiodic continuation and split behavior are
unchanged; frame-overrun and other fatal errors remain terminal.

During PausePolling, an observed halted periodic toggle attempt can instead be
explicitly discarded. This cancellation supplies no report, completion or NAK
and advances no PID. It preserves the next permitted polling time across resume.
The ordinary final PIO/link/all-channel/RX checks still gate paused state. An
unobserved or stuck halt retains the original 500 ms/4,096-step drain bounds,
packet deadline, containment and permanent failure ownership.

The primary comparisons are Linux v6.12 `dwc2_process_periodic_channels` and
TinyUSB's pinned [slave IN toggle handler](https://github.com/hathach/tinyusb/blob/dffc57135846a4b00aca06b2f588daa6d13b67ef/src/portable/synopsys/dwc2/hcd_dwc2.c#L1031).
TinyUSB requests retirement before periodic retry; its unbounded queue waits
are not adopted. No TinyUSB code is linked into this implementation. Protocol,
model/race and ownership checks do not establish that the Nano will acknowledge
the earlier halt; a fresh matched bounded hardware test remains necessary.

Direct control IN now acknowledges raw NAK as a token notification and continues
the same PIO packet. The pinned Linux v6.12
[slave control-IN interrupt mask](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd.c#L490)
excludes NAK; its continuation path queues further IN tokens and flushes excess
requests on completion. The bounded physical USB probe reached its root hub but
stopped when our former terminal-NAK path raced valid data/completion during
retirement. That trace motivates this mode-specific correction; it does not
establish acceptance of the changed driver.

Only direct control IN uses the notification path. It clears that W1C bit and
counts it for a later failure trace. It does not rewrite HCTSIZ, PID, packet
buffer or deadline, issue SETUP, treat NAK as completion, or release the channel.
Valid expected-PID RX, transfer-complete and observed channel retirement remain
required. Fatal status and ownership/link changes still stop the driver.
Periodic, split and OUT NAK retirement and contradictory-result checks remain.
Tests supply NAK before/with data and during retirement, withhold data/completion
or halt, and exercise malformed PID, lost ownership, disconnect and the original
deadline. Fresh matched image hardware acceptance remains pending.

Discovery failures now include the already captured device address/VID/PID,
class/speed list and the selected candidate interface/endpoint/report length.
The candidate remains unadmitted when its descriptor is rejected. Formatting
performs no extra I/O, retries, target replacement or input-protocol fallback;
the original error remains wrapped for error matching. The retained 53-byte
System/Consumer Control capture stays rejected by the gamepad parser and the
secure source remains unconfirmed. The board test did not print its actual
device IDs, so model IDs in the regression do not identify that physical device.
