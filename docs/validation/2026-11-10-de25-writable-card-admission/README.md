# Writable SD card admission

The owned native SD writer previously accepted a read/write mode without an
independently expected initial card identity. `sdcontrol.Config.WriteCard` now
requires the trusted caller's expected 120-bit CID and sector count. Its zero
value selects read-only access. The Nano board still supplies only its input
clock and therefore remains read-only.

Malformed writable identities fail before firmware or MMIO. A discovered CID
or capacity mismatch returns a retired session with its last invalid receipt
and actual identity retained, without sector writes, an automatic stop or a
retry. Existing same-card resume and controller write protection remain in
force. This is media ownership, not cryptographic authentication or a partition
write boundary; the future platform writer must establish its expected card
and permitted scratch region independently.

Host checks use the actual C firmware dispatcher and Go PIO driver/card model.
They cover matching write/readback, contain/resume, wrong initial CID and
capacity, malformed expectations, replacement on resume, write protection,
uncertain-write retirement, and real FAT32 settings update/remount with
independent mtools readback. The existing native read-only storage diagnostic
also cross-builds with the changed board call and session implementation.

Evidence is under `out/nano-writable-card-admission-20261110/`; `result.json`
records the implementation hashes, commands and outcomes. No new board
operation was performed. The preceding manual visible-menu return and stable
minute are recorded separately in
[the hardware observation](../2026-11-10-de25-menu-visible-hold/README.md).

The next physical write milestone needs a matched diagnostic and boot chain,
an independently identified spare card, a bounded scratch region and original
content hash, native write/readback plus contain/resume, and independent recovery
readback. The existing `storage` diagnostic already prints the CID/capacity;
there is no need to add another identity-only probe. Normal native SD writes,
network upload service and appliance acceptance remain open.

Validation passed: focused race-enabled SD/PIO/FAT checks, 1,851 parent tests
(three existing skips), generated consumer consistency, native package-store
lifecycle checks, the new phase-coverage test, and the Nano storage cross-build.
The initial `make test-changed` completed seven checks but failed its final
integration check at the cumulative 90-second native package deadline. A
baseline Go overlay reproduced that timeout using unchanged firmware/RTL
fixtures. Both failures and an earlier control setup failure are retained.

Commit `39cf2b9e6` separates program/install tests from the control phase and
puts selected-menu cases in the menu phase. Coverage checks every existing
native test belongs to exactly one phase. All phases retain 90 seconds; the
complete replacement firmware/RTL/native/Tenfoot integration run passes.
Control/window/menu/program native packages took 28.3/50.1/57.1/63.4 seconds.
Only this failed check and the added coverage test were rerun; unchanged parent
and storage results were retained.
