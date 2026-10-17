# PC/XT ISA transaction fabric 1.0

`packages/fabric/fes_fabric_pcxt_isa.yaml` is the source of constants for
`fes.fabric.pcxt-isa` 1.0. FES `make generate` emits
`cores/fes-common/generated/fes_pcxt_isa.vh` in misteross. The portable RTL
is in `cores/fes-pcxt`. This fabric adapts byte transactions to registered
linkable-card boundaries; it is not an electrical ISA pin interface, GP ABI,
physical placement map or runtime-admitted expansion target.

The shell bridge and every card use the same fabric clock. There is one
request register shell-to-card and one response register card-to-shell.
Card decode completes in one cycle after the endpoint latches the request;
READY may take arbitrarily longer. No host polling participates in a transfer.

## Packed request (48 bits)

| Bits | Meaning |
| --- | --- |
| 19:0 | Physical memory address, or zero-extended 16-bit I/O address |
| 27:20 | Write byte |
| 29:28 | Kind: 0 memory, 1 I/O, 2 interrupt acknowledge, 3 DMA peripheral byte |
| 30 | Write direction (DMA write means memory-to-device) |
| 31 | Valid request |
| 32 | Transaction tag, toggled for each accepted request |
| 33 | Commit permission |
| 34 | Reset, active high |
| 35 | AEN: I/O decoders must not claim while asserted |
| 36 | CPU LOCK indication; upstream arbitration preserves ownership |
| 38:37 | DMA channel 1–3; zero for non-DMA cycles |
| 39 | Terminal count for this DMA byte |
| 40 | Live guest clock-enable marker |
| 47:41 | Reserved, zero |

All request fields except COMMIT, RESET and guest CE remain stable while VALID
is asserted. Guest CE never gates the handshake machinery. The bridge rejects
nonzero high I/O address bits, DMA without AEN/channel, and a nonzero DMA channel
on other transfer kinds. It does not turn INTA into a device-port read. The
motherboard PIC handles actual interrupt acknowledge upstream.

## Packed response (32 bits)

| Bits | Meaning |
| --- | --- |
| 7:0 | Prepared read byte |
| 8 | SEEN: endpoint has decoded this request |
| 9 | Echoed transaction tag |
| 10 | CLAIM: card owns the address or DMA channel |
| 11 | READY: read data is stable and the device can commit |
| 12 | DONE: side effect has committed |
| 13 | PRESENT: this socket has a card endpoint |
| 21:14 | IRQ lines 0–7; shell masks motherboard-reserved IRQ0/1 |
| 24:22 | DRQ lines 1–3; DMA0 remains motherboard refresh |
| 31:25 | Reserved, zero |

A vacant socket returns all zeros. PRESENT remains high on an installed
endpoint, including reset. IRQ/DRQ may change independently of transactions;
combine them with OR. Configuration must prevent conflicting edge-triggered
IRQ assignments and DMA ownership before physical composition. The portable
bridge detects multiple claims at transaction time, not resource conflicts
between idle cards.

## Prepare, commit and drain

1. The upstream master presents `cycle_valid` with its byte request. An edge
   with `cycle_ready` accepts it once. The bridge latches all transaction fields
   and toggles its tag. The master may change inputs immediately after that edge.
2. The card endpoint latches the registered request, exposes it to the device,
   and samples its decode on the next edge. SEEN, tag and CLAIM stay stable
   until VALID drops. Preparing a read must not pop a FIFO or clear a flag.
3. The shell allows six fabric edges for request, decode and response pipelines
   before interpreting vacant slots. A present endpoint with no matching SEEN
   causes a protocol fault. More than one claim causes a conflict fault. Neither
   fault sends COMMIT. No claims returns `FF` with no side effect.
4. For exactly one claim, wait for READY. The endpoint captures the read byte
   when ready, and holds it. It then waits for shell COMMIT. Changes to upstream
   inputs or prolonged waits cannot repeat a read/write side effect.
5. The shell asserts COMMIT. The endpoint emits exactly one `device_commit`
   pulse and records DONE on that edge. The card performs its operation only
   on that pulse. The shell waits for DONE before dropping VALID and COMMIT.
   If the selected owner drops PRESENT or SEEN, echoes a different tag, or
   sets a reserved response bit before DONE, the bridge reports a protocol
   fault and drains. It does not issue another COMMIT. A pulse already
   accepted by the card is not rolled back. A present owner that still
   matches the tag continues to wait for DONE; that wait is the registered
   response path, not a timeout.
6. Four drain edges clear boundary and endpoint state before `cycle_done` pulses
   and `cycle_ready` admits another request. Held master VALID is an ordinary
   ready/valid stream and can request another transfer on a later ready edge.

`cycle_fault` is 0 success, 1 multiple claims, 2 protocol violation or 3 malformed
upstream request; it is valid with `cycle_done`. `cycle_rdata` is also valid then.
A card which never becomes READY stalls until machine reset. The bridge does
not invent an ISA bus-error interrupt or a timeout. The consumer must translate
internal faults into its machine diagnostic policy; no CPU integration exists yet.

Reset dominates commit. The boundary sends RESET to cards and clears responses;
the bridge drains old pipeline state without a phantom completion. Devices
clear their pending operations and IRQ/DRQ. Each card endpoint also receives
the shell reset directly. The boundary register can still hold COMMIT on the
edge reset is asserted, and the registered RESET bit does not update until
the next edge, so the endpoint reset is what suppresses `device_commit` on
that edge. Reset after a completed commit is not a rollback of an external
side effect. The CPU/8237 arbiter must establish its own reset and
persistence semantics when integrated.

DMA here is only the peripheral byte handshake. A future 8237 master must
arbitrate ownership, perform the RAM half of the transfer, and advance address,
count and terminal count once the full transfer completes. This fabric does
not implement a DMA controller, ISA bus mastering, original T-state timing,
video/memory services, audio transport or authenticated resource descriptors.
Those additions must preserve this contract or declare a new version.
