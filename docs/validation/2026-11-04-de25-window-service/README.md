# Persistent native window service prototype

Source `dc2cf47604ce003744ce2b1cf0e1e53beb1e4738` adds a persistent fixed-package service and a
native UART frontend using the real Tenfoot renderer. Eight affected checks,
twelve service/console test methods with failure subcases, and the matched
SDK/entry/BL31/U-Boot/SOF cross-build passed. This image has not run on hardware.
The earlier [bounded two-load result](../2026-11-04-de25-window-two-loads/README.md)
qualifies its own source and image only.

The explicit selection is `--window-memory --window-read --window-present
--window-storage-return --window-service`; it excludes `--window-reload`.
Composition marks the service persistent and the bounded presentation/SD-return
and reload diagnostics unselected. Firmware uses the existing window/SD hooks
and wire contract. There is no new master, address range or allocation release.

After initial configuration and a complete presentation, `WindowService` takes
exclusive ownership of the live session and original SD store. Its private
handle implements the board display interface. Return completes closure and
resumes the original store before publishing Ready; launch freshly retains the
archive, validates it before containment, configures under the existing 20s
bound and presents the first frame before publishing a new handle. Caller
cancellation after success does not revoke that handle. Once accepted, return
finishes independently of requester cancellation. Stale/foreign tokens and bad
geometry reject before device I/O; uncertain operations retire without retry.
The complete frame arena, plans and reservations remain rooted until reboot.

The frontend accepts bounded status/present/return/launch/park commands. It
keeps display handles private; commands cannot select a path, bus address or
frame bytes. Replies expose cached CRC/hash/byte/reader-drain observations,
last closure and SD generation without granting future access. Failure replies
omit success observations. Service generations are request tokens and differ
from firmware bridge generations. In the modeled three-session sequence,
running service generations are 1, 3 and 5; bridge closures are 7, 13 and 19.
These are expected software observations, not physical evidence for this image.

Race tests cover three request cycles, real Tenfoot-rendered frame changes,
foreign/copied/stale handles, healthy content rejection, request cancellation,
concurrent presentation/return, configuration deadline/backwards clock and
reservation/write/open/frame/closure/SD-return faults. The service tests model
secure calls and CSRs while executing actual archive, configuration and window
layers. The affected integration check separately exercises the existing
patched TF-A C and Verilated RTL with simulated platform operations.

The source audit checks each compiled Git blob and pinned external input,
actual CPU/translation instructions, ARM service-to-storage/configuration/frame
calls and strong EL3 window/SD hooks. The compiler inlines the public window
opening wrapper; the proof checks its actual private callee. The native ELF
strips global symbols, so linker assignment values are tied to the checked
compiler command and exact selected source, rather than claimed as directly
inspected global data. Failed exploratory check logs remain in the archive.

The raw ELF is 14991535 bytes, entry image
7327744 bytes and FIT 7876600 bytes, within the
existing reservations. `retention.json` records the source bundle, independent
restore/fsck and archive verification of all 45188 members.
No board, SD card or flash was accessed, and no expanded outputs were removed.

Next: add the bounded request/reply observer and qualify this image on
TRWU8CBL, then connect the complete Tenfoot App/backend. Ordinary
package/ROM/parts/media admission, physical HDMI, USB hub input and native
Ethernet remain open. The kit remains parked on its previous accepted test
image; its continuing exclusive claim and spare card are unchanged.
