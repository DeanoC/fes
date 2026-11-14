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

## Physical comparison result

The single bounded attempt passed in 358.3 seconds. All eight
recovery uploads completed their 34-marker return traces, RAM digests and SD
write/readbacks; all eight independent fresh reads passed before native
programming. The console has exactly 272 return markers. This run did not
repeat the predecessor's fourth-upload stall. It does not prove idle DMA or
general native networking.

The refreshed native image booted with the cold CPU/secure handoff and full
firmware/loader traces accepted. The ordinary computer constructor completed
three physical serial Tenfoot launch/return cycles: ZX81, alternate retained
ROM, ZX81. The accepted records bind the exact package, build, activation
receipt, selected ROM, owner and generations 1/2, 3/4, 5/6. Fetch activity was
observed. All three master-stop records completed, shared USB delivered
42 reports with pause/resume, and owned Quit ended
at ready generation 6 with the USB service stopped. No new physical control
press or HDMI/video behavior is established by this run.

Launches took 55.236, 55.179
and 55.222 seconds, within the unchanged 100-second
launch limit. The retained baseline mean was 72.525 seconds;
the new mean is 55.212 seconds, a
23.9% reduction for this three-cycle
comparison. The remaining launch time is a prototype limitation; this does
not establish broad DE10-style loading performance.

Independent cleanup confirms recorded 15 MHz readback, exact USB ownership/
permissions, the owned private JTAG server gone and UART free. TRWU8CBL remains
claimed after the native service's verified exit, rather than at a recovery
prompt. Factory restoration remains waived; the factory card and bottom kit
were untouched. The operator is consumed and cannot be replayed.

This is exact-artifact physical serial launch/return evidence. The retained
diagnostic admission, physical DDR-drain qualification, full core conformance,
general package acceptance, HDMI/menu/input/media/native-network behavior and
four FPGA signoff gates remain open. Full DE10-style readiness is not complete.
