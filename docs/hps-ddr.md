# HPS DDR for cores

The DE10-Nano has two external memories an FPGA core can use: the MiSTer
GPIO SDRAM addon, wired to fabric pins, and the SoC's 1 GiB DDR3, owned by
the HPS SDRAM controller. A core reaches the DDR3 through the hard
FPGA-to-SDRAM ports (`cyclonev_hps_interface_fpga2sdram`). FES describes that
use as the `fes.application` interface `fes.memory.hps-ddr` 1.0.

## What each part owns

| Part | Owns |
| --- | --- |
| mister-packages | The interface, its capability bit 8, the port layout constants, the window and the SDR mirror registers ([application I/O](../sources/mister-packages/docs/application-io.md#hps-ddr)) |
| misteross | `cores/fes-common/rtl/fes_hps_ddr.v`, the splash that carries the layout at boot, and the `fes.ramtest` tester |
| libmister-runtime | Admission of the interface, and releasing the SDR FPGA ports after the layout check |
| FES image | Pinning the splash as `/menu.rbf`, which U-Boot loads before it enables the bridges |

FogCast admits the interface from the registry the runtime advertises. It has
no DDR-specific code.

## Boot requirement

The SDR controller adopts an FPGA port layout only when `staticcfg.applycfg`
is written. Linux cannot do that safely, because every DDR access has to stop.
U-Boot does it once in `bridge enable`, with `/media/fat/menu.rbf` loaded. The
FES splash carries the `fes.memory.hps-ddr` layout: a 128-bit port and two
64-bit Avalon-MM ports, the MiSTer sysmem layout. A card whose `menu.rbf` has
no fpga2sdram cell latches unusable values, and every core's DDR command is
then refused. The RAM tester shows that as `NACK`.

The SDR registers `CPORTWIDTH` through `PORTCFG` show the loaded core's
configuration inputs, not the latched layout. The runtime compares them with
the constants before it writes `FPGAPORTRST`. A core without the interface
never gets its ports out of reset.

## The window

A core owns DDR3 bytes `0x30000000` to `0x3fffffff` (256 MiB), the MiSTer core
DDRAM window. The kernel boots with `mem=511M memmap=513M$511M`, so Linux
never allocates `0x1ff00000` and above. The lower half of the reservation is
not free, though. The MiSTer kernel's `MiSTer_fb` node claims `0x22000000`
(8 MiB) and fbcon draws its console there. On the kit, its blinking cursor
overwrote RAM tester data at `0x22001000`. The ports can reach all of DDR, so
an address outside the window is a core defect.

## Using it in a core

1. Instantiate `fes_hps_ddr` and connect the Avalon-MM ports you need. Tie an
   unused port's read and write low. Addresses are port words: byte address
   / 16 on port 0, and / 8 on ports 1 and 2.
2. Pass the execution reset as `hold` and reset each port's master from its
   `pN_reset`. The guard finishes a started write burst and hides stale reads,
   because the controller cannot recover a burst stopped midway.
3. Set `ENABLE_HPS_DDR(1)` on `fes_application_gp`, and declare
   `fes.memory.hps-ddr` 1.0 `required = true` in the package manifest.
4. OSS builds need the Yosys in `sources/misteross/toolchains/ramtest.lock` or
   later, whose blackbox declares every fpga2sdram port. Check the netlist
   with `hps_ddr_layout_evidence` in `scripts/fes_de10nano_evidence.py`, as
   the RAM tester and splash recipes do.

## Testing it

`fes.ramtest` scans the whole window on all three ports with seven patterns,
from single beats to 128-beat bursts, and shows errors, failing bits and MB/s
per port. Its [README](../sources/misteross/cores/fes-ramtest/README.md) has
the OSS and Quartus builds and the Linux signature check. Load the package
through a `fes-gp-v1` path (a library launch or a development core load). A
raw development RBF keeps the ports in reset.
