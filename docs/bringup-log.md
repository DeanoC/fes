# Host survey — 2026-08-26

Initial non-mutating survey on the development host:

| Capability | Result |
| --- | --- |
| CMake | present |
| Ninja | present |
| Python | present |
| Git | present |
| FPGA tools (Quartus, Yosys, nextpnr-mistral, Mistral, openFPGALoader) | not detected |
| USB-Blaster | not detected |

This log records exact tool identities, command syntax, device support, cable
visibility, artifact hashes, and hardware observations as they are established.
The initial host survey does not install packages or alter the machine.

## Evidence policy

Bootstrap and diagnostics are idempotent and non-mutating unless an explicit
build action is requested. Before wrappers rely on command syntax, the pinned
tools' version/help output will be captured here. A hardware result records the
experiment, tool versions, artifact hash, and observed LED cadence.

## CLI and device evidence — 2026-08-26

The following commands were run after `source scripts/env.sh` against the
repository-local install. Complete raw stdout/stderr is retained only under the
ignored `build/toolchain/evidence/` directory.

### Pinned tool identities and help behavior

| Exact command | Observed result |
| --- | --- |
| `nextpnr-mistral --version` | `"nextpnr-mistral" -- Next Generation Place and Route (Version nextpnr-0.11.1-14-g7d4f72c0)` |
| `openFPGALoader --version` | `openFPGALoader v1.1.1` |
| `mistral-cv --help` | Exit 1: `Unknown command --help`; the current CLI prints its command list and uses `models` for the device database. |
| `mistral-cv models` | Lists `5CSEBA6U23I7` as die `sx120f`, package `u23`/672, speed grade 7. |

The exact device acceptance probe was:

```text
nextpnr-mistral --device 5CSEBA6U23I7 --test
```

It exited 0 with `Info: Program finished normally.` after checking the
architecture database. This is a parse/database probe only; it does not ingest
a design, write a netlist, generate a bitstream, or access hardware.

### Confirmed build and programming flags

The pinned `nextpnr-mistral --help` accepts these exact inputs and outputs:

```text
--json <synth.json>                 JSON design file to ingest
--device 5CSEBA6U23I7               target Cyclone V device
--qsf <pins.qsf>                    QSF constraints input
--sdc <clocks.sdc>                  SDC timing input
--rbf <top.rbf>                     RBF bitstream output
--write <routed.json>               routed JSON design output
--report <timing.json>              timing/utilization report output
--detailed-timing-report            include detailed net timing data
```

The pinned `openFPGALoader --list-boards` contains `de10nano` with cable
`usb-blasterII`; `--list-cables` contains the `usb-blasterII` entry at VID/PID
`0x09fb:6810`. Its read-only discovery command is:

```text
openFPGALoader --detect
```

Programming syntax is `openFPGALoader --board de10nano --write-sram <top.rbf>`
(or `--cable usb-blasterII --write-sram <top.rbf>` after an explicit cable is
identified). The help output states that `--write-sram` is volatile/default,
while `--write-flash` is a separate option defaulting to false; no flash,
HPS-storage, SD-card, or network deployment operation is part of this task.

### Current hardware result

`openFPGALoader --detect` exited 1 with `JTAG init failed` because no cable was
visible. The host `lsusb` listing contained only hubs and ordinary peripherals,
with no Altera/Intel USB-Blaster VID `09fb` device. The available MiSTer Pi and
SuperStation One devices have no onboard USB-Blaster, and no external
USB-Blaster is connected, so general diagnostics report hardware as **NOT
READY** while OSS tool/device readiness remains independently reportable.

`QUARTUS_ROOTDIR` was unset for this evidence capture. Quartus was therefore
not inspected; it remains an optional oracle dependency and is only inspected
when that variable is explicitly provided.
