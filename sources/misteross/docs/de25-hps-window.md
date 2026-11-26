# DE25 HPS window (`fes.fabric.de25-hps-window` 1.0)

A computer's window in HPS DDR on the DE25-Nano. pc386 is the first user
(#2029; plan decision P1, accepted 20 November 2026: all of pc386's memory
in HPS DDR). The constants are generated from
`sources/mister-packages/packages/fabric/fes_fabric_de25_hps_window.yaml`.

## The grant

The kernel owner allocates one contiguous span of `WindowSpan` bytes
(256 MiB), aligned to `GrantAlignment`, below 2^`DmaAddressBits`. The span
is fixed rather than sized per launch: pc386's ATA region starts at window
offset `0x08000000` and may run to `0x0fffffff`. The kernel owner, not the
runtime, publishes the grant through the control block while the machine is
held, because no CPU or DMA address reaches userspace (as for
`fes.fabric.de25-scanout`). It writes:

- base, through `GrantLo` and `GrantHi`;
- span;
- generation, through `GenerationLo` and `GenerationHi`, nonzero.

It then writes `CommandGrant`. The block accepts a grant only with the
exact span, an aligned base whose whole span lies below 2^`DmaAddressBits`, a
nonzero generation and every port drained.
`CommandClear` withdraws it, again only while drained.

## The ports

The fabric presents `fes.memory.hps-ddr` 1.0's three ports unchanged, so a
DE10 machine connects as it does there:

- port 0 is 128-bit;
- ports 1 and 2 are 64-bit;
- all use word addresses, with `hold`, `pN_reset` and `pN_drained`.

Each command's address is rebased: the window offset is the byte address
minus `FES_APPLICATION_HPS_DDR_WINDOW_BASE`, and the issued address is the
grant base plus that offset. A command must lie inside the span with its
whole burst. A command outside it, or any command without a grant, is
never issued. It latches a sticky fault, which names the port and the
reason (`FaultNoGrant`, `FaultOutside`), and the port stays waiting. No
burst crosses a `BoundaryBytes` boundary. All three ports reach HPS memory
through one SMMU stream, for reads and writes.

When the window and scanout share F2SDRAM, the adapter issues reads with
one AXI ID so the hard bridge returns them in issue order. Its 16-entry
FIFO restores each Avalon request's ID through the last read beat.
The width adapters can then select the correct 128-bit half of each
256-bit reply, even while scanout and guest reads are both pending.

## The control block

The block sits at `PeripheralOffset` on the lightweight bridge,
`ControlSpan` bytes, in the board's 100 MHz peripheral clock.

| Offset | Register | Access |
| --- | --- | --- |
| `IdOffset` | `Identity` | read |
| `VersionOffset` | `Version` | read |
| `StatusOffset` | `Granted`, `Drained`, `Held`, `Fault` | read |
| `CommandOffset` | `CommandGrant`, `CommandClear` | write |
| `GrantLoOffset`, `GrantHiOffset` | grant base | read/write |
| `GrantSpanOffset` | grant span | read/write |
| `GenerationLoOffset`, `GenerationHiOffset` | grant generation | read/write |
| `FaultOffset` | first fault: port bits, reason | read; cleared by `CommandClear` |

A fault is fatal in 1.0. The faulting command stays presented, so its port
never drains and `CommandClear` (which needs every port drained) can't be
accepted: after a fault the window cannot be cleared or re-granted until the
window logic is reset.

This contract assigns no Linux device, firewall, boot or runtime admission.
The kernel owner's allocation and the runtime's use of it are slice 4 of
#2029.

## HPS reset

On HPS-window machine builds the GHRD's active-high `h2f_reset` output
asserts the common fabric reset. This resets the grant registers, the machine
and both sides of the F2SDRAM clock crossing. The port guards discard old
commands and outstanding replies, and both sides of the fault handshake
clear their state. A normal hold still drains outstanding operations.

The DE25 pc386 board wrapper also latches an execution hold when
`machine_reset_n` asserts. Its mailbox survives that reset, so the hold stays
active after reset release even if the mailbox still reports execution
released. The wrapper hands off only when a fresh grant crosses into the
machine clock domain and the mailbox and normal port hold both report held.
This lets the machine verify the ATA image through port 2 while execution is
still held. A retained Run state cannot resume by acquiring a grant alone.

The reset manager owns fencing and draining on the HPS side. Validate its
behavior across HPS cold, warm and watchdog resets with the exact boot chain
and bitstream before qualifying live-window reboot.
