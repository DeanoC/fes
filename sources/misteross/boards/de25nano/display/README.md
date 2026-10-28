# DE25 idle and menu scanout diagnostics

These separate Quartus Pro 26.3 lanes build FPGA-only `A5EB013BB23BE4SCS`
SOFs for the DE25-Nano revision-B HDMI connector. `idle` renders the existing
FES splash logo and autonomous motion. `menu-pattern` exercises the existing
menu reader, synchronous FIFO, sequence submission and 720p scanout against
its local pattern responder, alternating slots every 60 frames.

Neither is the described `fes.menu` package or an appliance boot image.
They instantiate no HPS, LPDDR4 controller, GP mailbox, framebuffer-upload
interface or blitter. DE10 producers, packages, profiles and image inputs
remain unchanged.

From this directory:

```sh
make test
make sim
make project MODE=idle VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
make idle QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
make menu-pattern QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
```

Each command prepares a fresh private output; `OUTPUT=/new/directory`
selects an explicit one and existing outputs are refused. Builds require
clean committed FES source, copy their functional closure from that Git
revision, generate IP with the explicitly selected Quartus installation and
compile with two CPU workers. Run the full compiler jobs serially.
`inputs.json` records source/vendor hashes, prepared project hashes, mode,
device and the reviewed IP metadata corrections. `build.json` records the
SOF digest, exact source revision/tool commands and report/SDC hashes.
The runner never programs a device, creates a JIC or writes flash.

The complete vendor ZIP is SHA-256 locked; extraction uses the exact bytes
that were hashed. Only source inputs from Terasic's
`Demonstration/FPGA/HDMI_ASx4/` are selected: its 50 to 74.25 MHz PLL,
Reset Release IP and FPGA-side ADV7513 I2C initializer. Generated IP HDL and
vendor SOFs are excluded. The reference's device metadata targets
`A5EB013BB23BE4SR1`; preparation guards and records correction of the PLL's
two device fields and Reset Release's one device field to SCS. The 74.25 MHz
output, zero phase and 50 MHz reference are unchanged. Original PLL/reset
module parameters must survive generation/compile; changes fail with a
retained review. The vendor I2C source and 33-entry register table are unchanged.

The generic menu reader's Cyclone V `M10K` hint is illegal on Agilex. The
DE25 preparation retains that original Git input and derives a separate
`fes_menu_reader_de25.v` by replacing only the guarded FIFO RAM-style hint
with `M20K`. Both hashes and the exact replacement are recorded; reader
logic, dimensions and synchronous read behavior are unchanged. Simulation
uses the same derived source. The default DE10 reader is untouched.

The selected pin locations and IO standards must match the SCS
`Demonstration/FPGA/Golden_top/golden_top.qsf` exactly. No DE10 pin names,
Cyclone V PLL/HPS primitives, HPS I2C BEL or DDR layout is reused.
Reset release is synchronized separately in the reference and pixel domains;
PLL loss/configuration reset blanks RGB and sync without another pixel edge.
SDC exceptions cover only four dedicated reset synchronizer clear pins,
leaving stage data and reset fanout timed. Generated PLL SDC owns its clocks.
The preserved initializer's internal clock is explicitly derived from its
50 MHz counter: it toggles after counting 0 through 2500, so a full period
is 5002 reference cycles. Preparation checks that exact divider contract.

`make sim` checks the actual wrapper, video cores and reset RTL with
controllable digital models for the PLL, configuration endpoint and I2C
initializer. It checks full 1650 by 750 frame periods, 1280 by 720 active
pixels, sync widths, idle logo/motion, 120-frame pattern switching without
underflow, silent audio pins and immediate blanking/relock. These models do
not validate PLL frequency/lock, serial I2C transfers, transmitter register
acceptance or the electrical link. Compiles use actual vendor I2C source.
Audio pins are tied low; no audio stream is implemented.

The vendor reference supplies no complete HDMI flight-time/setup/hold
budget. RGB, sync and DE terminate at the clocked ADV7513 receiver, so they
receive no asynchronous-output exception or invented output delay. Retained
reports expose missing external IO/I2C timing coverage; internal timing alone
does not establish HDMI acceptance. An unresolved electrical-document
discrepancy also remains: vendor QSFs assign HDMI clock DJ24 `1.1-V`, while
revision-B manual table 3-13 says 3.3 V. The recipe preserves the SCS QSF.
Board rail/translation evidence and a link test are required for acceptance.

These SOFs do not preserve a running HPS/LPDDR4 boot design and are not
paired with the BSP's factory QSPI image, bridge-enable boot sequence, DTB
or Linux framebuffer. Do not install them as `/idle.rbf` or infer firmware
compatibility from compilation.

Runtime-presentable menu firmware next needs an explicit Agilex HPS/LPDDR4
and framebuffer-allocation contract, then integration of the real DDR reader,
menu display/control and blitter with DE25 transport and runtime admission.
The local responder supplies none of these. Hardware awaits the physical board.
