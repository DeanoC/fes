# SNES cartridge save persistence

The approved milestone is ordinary SNES battery RAM surviving clean Stop,
system switching and kit reboot. The existing pinned FPGA backup transport is
used; FPGA rebuilds, enhancement chips, save states, cloud/host synchronization
and sudden-power-loss autosaving are outside scope.

FogCast owns persistent per-game naming beneath `/media/fat/fogcast/saves/snes`.
The path includes SHA-256 of the game ID and raw ROM bytes, independent of cache
location. Different copier-header variants intentionally have distinct saves.
The runtime owns cartridge admission, SRAM transfer and atomic file replacement.

The local launch request gains optional top-level `save_path`, only for SNES.
It remains protocol 1, with cartridge media and settings unchanged. Omission
preserves volatile behavior. Battery cartridge type 2 and RAM exponents 1–7
have 2–128 KiB saves; other admitted ordinary cartridges create no save file.
FogCast passes the game identity through PreparedLaunch and configures the
native save root. Main, Mega Drive and Pong remain on their existing paths.

Existing save bytes must have exactly the cartridge-derived size, and all file
admission happens before FPGA mutation. A missing save mounts zero size while
cartridge download remains asserted, retaining the core's cleared RAM. Existing
save bytes mount at their actual size and are restored through virtual SD slot
0 after download completion, before input starts. Persistence becomes active
only after successful launch.

Ordinary Stop flushes before idle programming. A dedicated hardware flush step
runs under the existing runtime mutation serialization; it stops input before
SPI, freezes SNES and requests the backup snapshot. Full validated sectors are
written through a sibling temporary file, fsync and atomic replacement. A save
failure returns `save_failed` and retains the session for Stop retry. It never
reports successful idle or permits lease release. A captured snapshot can be
retained for a write retry; partial transfers never replace the previous file.
Generic startup, failed-launch and input-fault cleanup must not snapshot an
incomplete game. There is no new public save RPC or second lifecycle owner.

Validation covers cartridge sizing, exact restore/snapshot byte order and
sector sequencing, mount ordering, bounded transport failures, file failures,
retryable Stop and FogCast identity/lease behavior. Hardware acceptance requires
an actual game save, Stop, alternate-system launch, reboot and restored progress.
Evidence distinguishes diagnostic binaries/images from selected exact artifacts.
