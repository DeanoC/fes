# Contained Nano DDR prerequisites — 2026-11-02

The sealed DDR diagnostic and matched native boot chain pass their bounded
TRWU8CBL observation at source `b05c00f0a6f26df0acfc03e762e85e71e86d6d81`,
using frozen board source `79d616e85e19d65f0d645b67a85ffe7e142dcdfc`.
This is a contained hardware diagnostic. FPGA DDR reads, active traffic drain,
runtime admission and playable-core launch/return remain unqualified.

The DDR recipe removes the unused coherent F2H interface, its JTAG manager and
translator. Otherwise reset containment prunes their connection and leaves an
enabled HPS primitive illegally unconnected. It requires exactly one original
HPS parameter change (`f2s_data_width=256` to `0`) and five known Quartus 26.3
defaults, before and after compile. Original EMIF/adapter parameters, vendor
boot data, generated routing, reset inventory and PLL checks remain enforced.
Four failed FPGA candidates and the cancelled intermediate software run are
retained with their original source identities.

The board loaded the canonical package from FAT32, contained SD and configured
the exact 1,642,496-byte core payload. Native Go pinned the 8 KiB test pattern
at `0x95000000` inside its separate 2 MiB reservation. Firmware observed 26
SMMU reads and 66 firewall reads (two matching snapshots of each): region 0 enables
`0x80100000..0xbfffffff`, covering the requested window; all eight regions have
the expected register shape. Those readings grant no FPGA memory permission.

The live FPGA matched ABI `0x10203041` and its sealed build ID. The real secure
master-stop callback checked the idle reader (88 reads, one poll, no disable)
and inherited probe (two polls, no cancel), then reclosed control at generation
6. All data bridges stayed reset. The final native marker explicitly reports
DDR access blocked, translation/drain unqualified and runtime programming
unavailable. No FPGA DDR request or active drain was exercised.

Both Ethernet uploads passed SD readback and protected-file checks. The first
native helper stopped on an obsolete receipt variable before opening UART or
starting native JTAG. Its original source, failed result and verified uploads
are retained. One corrected native-only continuation reused those uploads
without another SD write and passed independent saved-trace decoding. The
original 15 MHz cable clock and USB permissions are restored; UART is free.
The spare card, parked diagnostic and exclusive claim remain under the user's
retained-diagnostic policy. No factory restore was requested.

All 11 affected software commands passed on the tested source: 1,265 parent
tests (two skips), 1,577 FPGA tests (75 skips), firmware/runtime/producer
integrations and generated-consumer consistency. The read-only firewall helper
passes 619 C cases. The exact package passes 23 generated-handoff rejection
cases and the DDR RTL model's 23 traffic cases plus three identity/window
rejections. The matched AArch64 ELF contains the real firewall and master-stop
callbacks. Quartus completed every standard task without relaxed constraints;
all 46 reported summary slacks are nonnegative (100 MHz setup: 1.688 ns).
This does not establish complete timing coverage or production acceptance.

The next hardware step is a separately guarded bounded FPGA read of the pinned
pattern, with fresh translation/firewall checks at release and a real active
disable/drain before reclosure. The current observer does not enable that path.
See [manifest.json](manifest.json), [artifacts.json](artifacts.json),
[hardware.json](hardware.json) and [retained-failures.json](retained-failures.json).
Raw consoles, exact compiled project and matched boot files are retained in the
session archive named by the manifest; private claim data is excluded.
