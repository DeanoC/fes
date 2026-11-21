# DE25 HPS window (`fes.fabric.de25-hps-window` 1.0)

A computer's window in HPS DDR on the DE25-Nano. pc386 is the first user
(#2029; plan decision P1, accepted 20 November 2026: all of pc386's memory
in HPS DDR). The constants are generated from
`sources/mister-packages/packages/fabric/fes_fabric_de25_hps_window.yaml`.

## The grant

The kernel owner allocates one contiguous span of `WindowSpan` bytes
(256 MiB), aligned to `GrantAlignment`, below 2^`DmaAddressBits`. The span
is fixed rather than sized per launch: pc386's ATA region starts at window
offset `0x08000000` and may run to `0x0fffffff`. The runtime publishes the
grant through the control block while the machine is held:

- base, through `GrantLo` and `GrantHi`;
- span;
- generation, through `GenerationLo` and `GenerationHi`, nonzero.

It then writes `CommandGrant`. The block accepts a grant only with the
exact span, an aligned base, a nonzero generation and every port drained.
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
| `FaultOffset` | fault record: port bits, reason, as of the last hold | read; cleared by `CommandClear` |

`Fault` in the status word shows a machine fault as it happens. The
record in `FaultOffset` is copied when the machine is held, so read it
after holding the machine. `CommandClear` zeros it.

This contract assigns no Linux device, firewall, boot or runtime admission.
The kernel owner's allocation and the runtime's use of it are slice 4 of
#2029.
