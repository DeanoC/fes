# Scanout presenters and owned menu display

`scanout` provides the bounded CPU pixel copier (RGBA to little-endian
XRGB8888, 4-row cooperative yield chunks) shared by QEMU `ramfb` and the
DE25-Nano display paths, plus the owned double-buffered menu presenter and
legacy diagnostic window reader.

## Boot-owned surface (`Surface`)

Copies private Go RGBA pixels into a dedicated Normal noncacheable XRGB8888
reservation outside Go's heap and orders stores in `Publish`. Writes serialize;
a write or publication failure poisons the surface and retains the reservation
until reboot.

### Nano firmware contract

Schema-8 profile `display` + `handoff.display_platform_ready`:
- `base`, `size`: 2 MiB-aligned **4 MiB** HPS DDR reservation disjoint from Go
  RAM, firmware reservations and Ethernet DMA (DE10 `0x30000000` rejected).
- `width: 1280`, `height: 720`, `stride: 5120`, `format: "xrgb8888"`,
  `lifetime: "boot"`, plus `firmware_artifact_sha256` provenance.

## Owned menu video prototype (`NewMenuPresenter`)

Drives the double-buffered 8 MiB (two 4 MiB XRGB slots) FPGA menu pipeline
([`fes_embedded_menu_scanout`](../../../../sources/misteross/prototypes/embedded_control/fes_embedded_menu_scanout.sv)
and `fes_embedded_menu_control.sv`, private `FMS1` register block, ABI
`0x10203043`):

- Checks compiled base, 8 MiB extent, geometry and live build/generation before
  copying into the spare slot.
- Submits one sequence per frame and waits (bounded polls and monotonic
  deadlines: up to 4 s CPU publication + 1 s endpoint I/O) for full-frame
  acknowledgement before reusing the prior slot.
- `Stop(ctx, cookie)` issues local Quiesce once, waits for reader stop, and
  returns the endpoint's generation/cookie `ReaderDrain`; the platform still
  runs independent EL3 all-master stop and bridge closure.
- `nativefabric.OpenMenuSession` binds this presenter to the paired Tenfoot
  launcher lifecycle and retains the 8 MiB arena until reboot.

Verify the Go presenter and C firmware stop helper against actual Verilator RTL:

```sh
python3 sources/misteross/prototypes/embedded_control/verify_menu.py
```

## Probes and evidence

- `NewWindowReader` controls the legacy single-slot 4 MiB CRC-sink window
  diagnostic (`ReadFrame`, `Drain`, `verify_window.py`).
- `run.py display --board qemu` exercises the shared `Surface` copier on QEMU
  `ramfb` without storage, networking or FPGA dependencies.
