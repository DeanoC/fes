# Native DE25 computer control (`computer/`)

`computer` is the EL1 Go driver for the `fes.computer` GP mailbox over the DE25
Avalon adapter. It executes core lifecycle commands (`Hold`, `Run`, `Keyboard`,
`LoadInitialFirmware`, `LoadTape`, `EjectTape`) with zero hardcoded physical
addresses or firmware grants.

- **Called by:** [nativefabric](../nativefabric/README.md) (`ComputerSession`
  and `ProgramService`)
- **Delegates to:** injected bounded `Read32`/`Write32` MMIO hooks (supplied by
  `nativefabric.CPUHooks` while the control bridge is open), monotonic clock,
  and cooperative yield.
- **Wire contract:**
  [`fes.fabric.embedded-computer`](../../../../sources/mister-packages/docs/embedded-computer.md)
  (generated into `computer/generated/` via `make generate`).

## Protocol and lifecycle

1. **Strict identity check (`Open`):** Takes the exact 128-bit build ID, GP ABI
   minor (`1.0` reads 16 identity words; `1.1` reads 17), and capability mask
   from the verified package and platform contract. Requires Avalon adapter
   register version `1.1` (rejects `1.0` before issuing commands) and checks
   both magic words, versions, capabilities, and all 128 build bits (low-byte
   first for GP build words). No fallback discovery.
2. **Toggled Avalon writes:** Each command performs one full-width toggled
   Avalon GPO write followed by bounded GP ACK snapshot polls verifying exact
   GPO, signature, reserved bits, status, fault, and ACK parity.
3. **Core operations:**
   - `Hold` neutralizes input while preserving GP epoch and loaded tape.
   - `Run` releases execution reset.
   - `Keyboard` delivers all nine HID rows (including batched fast-path writes
     during active gameplay).
   - `LoadInitialFirmware` verifies an empty endpoint of exact expected size
     while held, streams header + byte pairs, and checks `Commit` readiness.
   - `LoadTape` / `EjectTape` stream unit-0 chunks with CRC while preserving
     execution.
4. **Fail-closed retirement:** Any bus error, uncertain write, deadline,
   backwards clock, rejection, or partial multi-command failure permanently
   retires the handle without replay (the caller invokes EL3 containment; a
   hardware reset poisons the adapter until FPGA reconfiguration). Limit errors
   match `errors.Is(err, ErrLimit)` and record `deadline` / `backwards-clock` /
   `ack-polls` diagnostics without extra I/O.

## Shared core shell support

The same mailbox transport and generated opcodes drive all native DE25 cores:

| Core | Firmware upload | Media & notes |
| --- | --- | --- |
| ZX81 / ZX80 | Held ROM via `cpu-csr-v1` / mailbox | Unit-0 tape via `LoadTape` / `EjectTape` |
| Spectrum 48K (`--shared-core-shell`) | 16 KiB `gp-firmware-upload-v1` (`LoadInitialFirmware`, 8,192 word writes while held) | Spectrum tape (`format: "spectrum-tape"`, converted to PZX via shared FogCast converter while SD is owned, capped at 64 KiB) |
| C64 (`--shared-core-shell`) | 36 KiB firmware upload while held | Optional read-only 35-track D64 disk image |

Under `--shared-core-shell`, the FPGA scanout reader pins display grants to the
two native DDR slots and routes normal Tenfoot Stop through scanout drain +
`Hold` + EL3 bridge closure before SD resumes.

## Verification

```sh
# Go host tests under -race
GOWORK=off go -C platform/embedded/tamago test -race ./computer

# Verilator RTL + Go Avalon mailbox test for Spectrum 48K
python3 sources/misteross/boards/de25nano/spectrum/sim.py --model 48 --native-mailbox
```
