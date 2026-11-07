# Native launch comparison with per-walk FAT metadata reuse

This fresh bounded operator uses the [fully rebuilt matched chain](../2026-11-07-de25-fat-native-chain/README.md)
and the physically qualified 100-ms recovery image. The preceding
[three-cycle baseline](../2026-11-07-de25-timer-launch-trial/README.md) passed
all eight uploads and ZX81/alternate-ROM/ZX81 launches/returns in 408.5 seconds;
launches took about 72.5 seconds each.

The fresh SDK has one changed compiled source, `fat.go`. It reuses the existing
metadata-sector buffer within one chain walk, with fresh reads on every later
walk. On the actual package in a private image, FAT reads fell from 1,623 to 15
and data reads stayed unchanged. The source change and host read-count result
do not establish a physical speed improvement. No verification, ownership,
reset, clock, IRQ, DMA or timeout policy is weakened.

The test repeats the same eight fixed uploads, 272 ordered return markers,
eight write/readbacks and eight independent fresh reads before native
programming. It requests the same three serial Tenfoot launch/return cycles,
retained ROM selection sequence, shared USB pause/resume, master-stop and owned
Quit. Initial/native/recovery maps, sealed FPGA payload, ROM catalogue and
seven qualified safety helpers remain exact. Fourteen fixture groups pass,
including 43 source/provenance rejections and seven actual stale/damaged input
plan rejections. Normal native source/ELF/FIT/SPL/resource checks pass.

Source admission is rooted in the fresh SDK/entry/firmware commit; later
changes may only add the three named validation-document groups. All 4,419
source checks and 2,205 unique inputs remain exact. The old attempt is consumed;
this is a new single attempt with first-failure stop. All original prompt,
startup, launch, configuration, control, native/process, outer/host and console
bounds remain 50/100/100/20/5/450/480/740/850 seconds and one MiB per phase.
The shared JTAG lock, continuing top-kit claim, exact USB/device admission,
6 MHz programming, 15 MHz restoration, exact USB permissions and owned
UART/network/private-JTAG cleanup remain mandatory.

This freeze is preparation. The board was last verified after native Quit at
ready generation 6; fresh recovery/card/ownership checks are required before
writes. Factory restoration remains waived, its original card stays safe aside,
and the bottom kit remains untouched. Full general-package, core/video,
HDMI/menu/input/media/native-network and four FPGA signoff gates remain open.
This comparison cannot establish full DE10-style readiness on its own.

Raw operator/evidence: `out/nano-fat-launch-board-20261107`.
