# DE25 allocated-buffer scanout

`fes.fabric.de25-scanout` 1.1 is a disabled-by-default two-slot diagnostic
endpoint. It supplies no runtime profile, Linux resource, DMA stream binding,
firewall permission or firmware/hardware acceptance. Its relative lightweight
offset is `0x10200`, span 128 bytes; the architectural CPU candidate
`0x20010200` remains unadmitted. The mailbox and read-only probe are separate.

The allocation is one kernel-owned 8 MiB DMA grant containing two 4 MiB slots,
1280×720 BGRX8888, stride 5120 and 3,686,400 visible bytes. The 64-bit DMA base
must fit the configured 32-bit bus, be 32-byte aligned and end at most `2^32`.
The span must be exactly 8 MiB. These are DMA bus/IOVA bounds, not proof of
physical backing or an identity mapping. Generation is nonzero 64-bit;
sequences are nonzero 32-bit and increase without wrapping inside a generation.

The 128-bit build identity comes from the retained functional source closure
and selected HPS-first/Linux-DMA profile, independently of checkout path or a
documentation-only commit. The builder sets the actual Platform Designer HDL
parameter and verifies its generated value against the retained inputs. The
value is also recorded beside the RBF digest; those are distinct identities.
Unbound HDL defaults to zero, which the Linux owner refuses before allocating
storage. Interface version 1.1 is exact, so the new kernel refuses the previous
1.0 endpoint before accessing these additional registers. This adds live image
identification, not production MENU or hardware/timing admission.

## Commands and slot ownership

SUBMIT (`1`) snapshots the staged descriptor. The first submit requires a
drained, fault-free endpoint, captures the immutable grant/generation and
submits slot 1 before enabling any fetch. Initial displayed slot is 0 with
sequence 0; the allocation is cleared by its kernel owner. Later submissions
must preserve the complete grant/generation, use a sequence greater than the
last accepted sequence, have no existing pending submission, and select the
slot other than the last fully displayed slot.

Accepted sequence acknowledges the command. Current sequence/slot describe the
frame selected for scanout. Displayed sequence/slot advance only after a complete
frame has reached the final active pixel without underflow or memory error.
There is one pending submission. The previous slot becomes reusable only when
accepted, current and displayed identities agree with that pending submission
and Pending is clear. Reading accepted identity alone never releases a slot.
The single writable 8 MiB client mapping does not enforce per-slot write leases;
clients must draw only into the reported back slot and stop writing at publish.

STOP (`2`) stops new fetches, holds presented requests stable through acceptance
and drains all accepted responses. It clears uncompleted pending state but
retains grant/generation and sequence readbacks for ownership proof. Drained
requires no held request, outstanding response or queued reader data and a
quiesced video engine. A missing response cannot time out into Drained.

CLEAR (`4`) is legal only after actual drain. It invalidates the configured
grant/generation and resets local reader/video state; it never resets either
clock-crossing bridge. Successful kernel retirement first checks the old
generation's drained snapshot, then clears and verifies the empty endpoint.
A constructor cannot clear an unknown configured engine to claim ownership.

SNAPSHOT (`8`) freezes all snapshot registers in one pixel-clock observation
and increments SnapshotSerial modulo `2^32`. No other command modifies frozen
registers or the serial. Software reads the old serial, orders the snapshot
write, waits for the exact next serial, reads frozen fields and checks the
serial again. One exclusive controller serializes these operations. Snapshot
can observe the state before a same-edge frame event; every field still belongs
to the same observation. Other command combinations fault and stop new fetches.

DisplayReady reports the board shell's registered HDMI visibility, after its
configuration-complete result crosses through a registered source and the
system/pixel synchronizers. It is observable while scanout is disabled; waiting
for it never resets a live bridge. Loss during a running frame latches Display,
stops new work and prevents a healthy complete-frame acknowledgement. Restoring
readiness alone cannot clear that fault or retire outstanding memory traffic.

## Register layout

Generated C/Verilog constants are authoritative. Offsets are bytes, registers
are 32 bits, staged writes honor byte enables, unused reads return zero and
read-only writes have no effect.

| Offset | Register |
| --- | --- |
| `00`, `04` | Identity `0x44325343`, version `0x00010001` |
| `08`, `0c` | Live status, command |
| `10`, `14`, `18` | Staged DMA base low/high, allocation span |
| `1c`, `20`, `24`, `28` | Staged generation low/high, sequence, slot |
| `2c` | Snapshot serial |
| `30`, `34`, `38` | Frozen status, generation low/high |
| `3c`, `40`, `44` | Frozen accepted, displayed and current sequence |
| `48` | Frozen slots: current bit 0, displayed bit 8, pending bit 16 |
| `4c`, `50`, `54` | Frozen errors, underflows, conservative outstanding beats |
| `58`, `5c`, `60` | Frozen active DMA base low/high and allocation span |
| `64`, `68`, `6c`, `70` | Immutable 128-bit build identity, least-significant word first |

Status bits are Configured `1`, Running `2`, Pending `4`, Drained `8`, Fault
`16`, ClockFault `32`, DisplayReady `64`. Error bits are Descriptor `1`, Command `2`, Response
`4`, Underflow `8`, Clock `16`, Sequence `32`, UnexpectedResponse `64`, Display `128`.
Ordinary read errors and underflow stop and drain without successful display
completion. An unexpected response breaks ownership accounting: it is terminal,
never reports Drained and cannot be cleared locally. The outstanding count
includes a presented but unaccepted burst, so it cannot hide stalled traffic.

## System-clock control guard

The CPU-facing control window is 256 bytes. Its lower 128 bytes forward to the
pixel endpoint ABI 1.1; the upper half implements a distinct guard
identity/version on the 100 MHz peripheral clock. The guard uses the inherited
peripheral reset, so its release does not wait for pixel PLL lock.

| Offset | Guard register |
| --- | --- |
| `80`, `84` | Identity `0x44324744`, version `0x00010000` |
| `88`, `8c` | Live status, sticky faults |
| `90` | Nonzero 32-bit software epoch, write once until peripheral reset |
| `94` | Timeout bound in system cycles (100,000) |

Status bits are Poisoned `1`, Pending downstream transaction `2`, Accepted
by downstream `4`, EpochSet `8`. Fault bits are Timeout `1`, downstream
Response error `2`, UnexpectedResponse `4`, invalid Access `8`. Reads/writes
must be aligned full words; other accesses poison without forwarding. Only the
epoch is writable locally, exactly once with a nonzero value; local fault/status
has no clear command. Epoch is an exclusive kernel owner's identity marker,
not proof of endpoint reset, DMA drain, domain lifetime or allocation ownership.
A new owner must refuse a nonzero preexisting epoch; reuse/wrap is forbidden.

Every accepted endpoint transaction receives a CPU completion within the selected
system-cycle bound. A valid downstream response on the deadline wins over timeout.
Missing acceptance, missing response, transport errors or malformed responses latch
permanent poison. The CPU sees OKAY and zero data on failure rather than an external
bus abort. A driver must check guard identity, epoch and faults before and after
ordered endpoint I/O; zero data must never stand in for endpoint success. Local
reads complete on the next system clock, subject only to a previously accepted
CPU transaction's bounded completion. Bus waitrequest serializes CPU transactions.

Timeout does not cancel a downstream request: address, data, byte enables and
read/write remain stable until acceptance. A late response retires the downstream
transaction without another CPU response, while poison remains. All subsequent
endpoint requests receive bounded dummy responses and cannot reach the crossing.
An unexpected response cannot discharge the pending transaction unless it matches
its accepted command kind. No local operation resets the crossing, manufactures
Drained, frees memory or permits replay. Physical DMA uncertainty still requires
retaining grants and their device/domain resources. The bound assumes a running
system clock and peripheral bus; it does not cover a stopped system oscillator,
asserted system reset or board reconfiguration. Those remain separate lifecycle
gates. The guard is not yet physically qualified or admitted by a kernel backend.

## Clock and reset boundary

Registers, video and reader share the 74.25 MHz pixel clock. Two explicit
Platform Designer bridges cross ordered 32-bit controls from 100 MHz and
128-bit reads/responses to the 100 MHz memory adapter. Memory bursts have at
most sixteen 16-byte beats and split at 4 KiB boundaries. Two-bit response
errors cross with their data. The generated IP owns FIFO crossing constraints;
broad clock-group or false-path exceptions must not bypass them.

Both sides of each bridge use a common cold/configuration reset, with
domain-local release. Startup waits for qualified pixel lock once. Later pixel
PLL loss blanks outputs and latches a fault without resetting bridge FIFOs or
reader accounting. STOP remains incomplete while the pixel clock is stopped;
on recovery the retained requests/responses drain. ClockFault stays sticky
until cold reset and cannot be cleared into a new lease.

Forced reset erases accounting and generation. Empty state after reset does
not prove that traffic from the preceding generation disappeared. The kernel
requires the original external epoch, grant/generation and drained snapshot;
unreadable or changed state retains the allocation. Actual accessors also need
bounded failures when the pixel-clock register endpoint is unavailable. No
physical MMIO accessor or DE25 device binding is admitted by this work.

The global HPS warm-reset gate remains separate: other JTAG masters and
HPS-origin traffic still need fencing, and HPS reset can progress on timeout.
Local scanout drain cannot justify global warm ACK or hardware recovery.

## Explicit Linux SMMU build selection

The generated stream ID is 13 (outside the selected vendor PSS IDs), with
SID in raw F2SDRAM AXI address USER bits 24:9 and nonsecure SECSID bit 8 clear. The lower
8-bit vendor USER attributes remain 0xE0, giving address USER 0x0001AE0.
These additive build constants do not change the endpoint register ABI.
The opt-in FPGA recipe enables HPS SMMU port exposure and verifies the actual
25-bit generated adapter/HPS path. It does not create a kernel device or
assert that firmware translation has been enabled. The Linux-DMA recipe removes
the diagnostic probe and external memory-debug master/window, and verifies
exactly one generated master into the adapter and one adapter route into HPS.
The separate maintenance recipe retains the diagnostic paths. A sole generated
scanout path is not proof of physical isolation, translation, Linux domain or
global reset lifetime. Physical allocation, translation, bounded control access,
drain and reset qualification remain required.

The raw port packing comes from the installed Quartus Pro 26.3 HPS parameter
`f2sdram_SMMU` in `ip/altera/intel_hps/sm/common/parameters_data.tcl`.
The ACE5-Lite translator CSR table uses a different layout and cannot define
this raw port. The [correction and physical diagnostic](../../../docs/validation/2026-11-01-de25-raw-smmu-layout/README.md)
retain the installed primary-source identity, failed old encoding and recovery
boundaries.
