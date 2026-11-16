# Matched native menu boot files, 2026-11-07

The corrected menu FPGA package, native Tenfoot presenter, EL3 firmware and
SPL/U-Boot now form one checked boot chain. The composition preserves the sealed
FPGA core payload and embeds the exact final SPL. This is regular-file build and
model evidence; no board was programmed or card written for this milestone.
Exact producer revisions, package identities, hashes and limitations are in
[result.json](result.json). The earlier invalid reply-net timing candidate remains
recorded in the separate menu-producer validation history.

The explicit cold menu owns an 8 MiB framebuffer arena with two 4 MiB slots.
Its real Tenfoot renderer submits three frames through the existing MenuSession,
then independently stops the reader and closes the bridges through firmware.
The new serial decoder requires the complete boot/configuration prefix, zeroed
publication, use-point memory gates, acknowledgements in slots 0/1/0 and the
owned secure return. Ninety focused trace regressions pass, including missing,
replayed and corrupted observations, reported underflows and failed closure.
Serial observations cannot establish physical HDMI output or a complete
menu/core/menu application cycle.

Firmware data may now vary in size inside its unchanged reserved interval
`[0x50000,0x60000)`. The stack ends and BSS begins at `0x58000`; the actual linked
menu BSS ends at `0x5afc0`. Build and early-entry checks preserve alignment,
ordering, load-segment and reservation bounds. They no longer require the ROM
variant's exact BSS end. Seventy-two ARM exit-cache and 45 ARM entry cases pass
with modeled device state; these do not qualify physical cache behavior.
Compiled final SPL instructions also confirm that the allocation guard runs
after the last handoff trace and before BL31, with its rejection branch and
private RAM cookie present. The final FIT is 8,051,232 bytes, within the unchanged
8 MiB limit. The audit rechecked 4,454 recorded input hashes.

The corrected FPGA compilation removes the invalid direct-net delay assignment
finding and has positive constrained setup, hold, recovery, removal and pulse
slacks. Four unwaived I/O-delay/reset rule classes remain; full timing signoff is
false. Existing RTL checks cover the actual package/build ID, three Tenfoot
frames, 640 crossing packets and failure controls. Generated HPS AXI simulation,
board electrical constraints, physical DDR/drain and display remain open.

The native memory change passed the affected integration checks, 1,626 parent
tests (3 skips) and 1,595 misteross tests (79 skips). Those broad checks selected
the memory-bound revision; the later SPL guard is covered by focused compiler
tests and the actual matched firmware/boot builds. The serial-observer delta
passed all six affected commands and 1,632 parent tests (3 skips) on its base
revision. A subsequent mode-type guard has the 90 focused regressions separately
recorded; the broad suite was not repeated for that small delta. Neither snapshot inherits earlier
physical computer-core acceptance.

The file-only delivery plan selects the 1,715,200-byte package at
`FES/PKGTEST.BIN` and the matched FIT at `FES/UPLOAD.BIN`, using the existing
recovery uploader. It validates the actual artifacts and prepares no device
operation or automatic activation. The recovery boot FIT stays separate.

Next is a frozen bounded operator and fresh top-kit/card validation for this
exact menu chain. General package admission, live menu input, storage resumption
and menu-to-core-to-menu remain required for DE10-style use. TRWU8CBL's factory
card and the bottom kit remain untouched by this work.
