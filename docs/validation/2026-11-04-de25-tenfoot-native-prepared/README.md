# Matched native Tenfoot serial candidate prepared

Native source: `e225ea89b002fcc53a90c743f66c592d1f8d35ff`.
FPGA producer source: `7808f74c2fda87cc56a0fe92e5a17454fd31746a`.
Board revision: `79d616e85e19d65f0d645b67a85ffe7e142dcdfc`.
This is an off-board candidate. No kit programming, SD write, flash change or
native hardware trial occurred in this stage.

Both fresh ZX81 and ZX80 diagnostic builds passed on the 3090 under
`/tmp/lander-quartus-3090.lock`, with the existing 120-second per-phase and
840-second overall limits. Their full exports were verified file by file.
Reported timing-summary slacks are nonnegative with zero reported TNS; this
is not constraint-completeness or whole-board timing qualification.

The native SDK, entry, BL31, U-Boot and HPS-first composition passed. The FIT
is 7,958,520 bytes, below the existing 8 MiB limit. Exact verification covers
366 committed native files and 1,681 previously pinned external source files,
actual ELF App-to-lifecycle call paths and CPU translation instructions, strong
owned firmware callbacks, and unchanged compiled vendor startup assembly.
Tenfoot serial selection matches across the complete chain; an unselected
control SDK excludes RunPlatform and the native serial App marker.

Both actual sealed packages passed native retained-package preflight against
one boot anchor, in the order ZX81, ZX80, ZX81. The generated UI catalog also
passed the native Go readers. These checks establish package/metadata binding
and source compatibility, without firmware or physical activation evidence.
[upload-plan.json](upload-plan.json) lists seven exact files and fixed upload
destinations. Every file requires fresh verified SD readback before a future
native boot. The plan executes nothing.

The retained candidate and standalone source bundle are under
`/home/deano/fes-retained/de25-tenfoot-native-boot-20261104-r3/`.
All 43,651 regular archive members were independently read back; 59 internal
build links were normalized to relative targets and checked. A standalone
bundle restore and strict fsck verified native, FPGA and board revisions.
The first host-only retention check caught an omitted, separately rooted board
history. The second caught an internal absolute U-Boot build link. Both failed
staging records remain; corrected retention passed without rebuilding or
altering compiled images. External toolchain/module bytes remain pinned by
hashes; this is not an offline toolchain export.

The next gate is one bounded serial App launch/return procedure on TRWU8CBL,
with recovery, seven owned uploads/readbacks, the exact matched native boot,
and checked lifecycle/ownership traces. A dedicated observer must check the
App state stream; the older ProgramConsole observer cannot qualify this mode.
Stop on the first failed physical proof and retain the capture.

Serial output explicitly declares serial-state-only; preferences stay in RAM
and UART supplies launcher navigation, without core keyboard forwarding. Owned
HDMI, USB hub input, ordinary ROM/parts/media admission, native Ethernet and
physical App/core-cycle acceptance remain required for DE10-style readiness.
Shared schema/protocol and FPGA RTL were unchanged by the native App feature.
