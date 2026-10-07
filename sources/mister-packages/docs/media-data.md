# Durable removable-media data

The shared layout `fes.atari-st-floppy.image` 1.0 is the complete 737280-byte
raw ST disk image. It uses the writable computer mailbox's whole-image capture;
libmister-runtime owns capture and durable publication, and FogCast supplies
the explicit library binding. Original imported media bytes remain immutable.
Development inserts are volatile, even when their title resembles a game.

The namespace is lowercase SHA256 of these concatenated UTF-8 bytes:
`fes-media-data-v1`, NUL, core ID, NUL, game ID, NUL, decimal unit, NUL,
lowercase base-image SHA256. Unit is 0. Package, firmware and video versions
do not name the record. This lets a compatible new package restore the same
disk, while two games or two base disks remain independent.

The canonical file is `<namespace>/record.bin` under the runtime's trusted
media-data root. A missing record restores the immutable base image. A corrupt
or incompatible record blocks restoration and later publication; it is never
silently replaced with defaults. Revision is lowercase SHA256 of the complete
record; `absent` means no record. Publication compares the expected revision
under the namespace lock.

| Offset | Size | Bytes |
| --- | --- | --- |
| 0 | 8 | ASCII `FESDISK1` |
| 8 | 32 | SHA256 of UTF-8 core ID |
| 40 | 32 | SHA256 of UTF-8 game ID |
| 72 | 32 | Raw immutable base-image SHA256 |
| 104 | 2 | Little-endian unit (0) |
| 106 | 2 | Little-endian layout major (1) |
| 108 | 2 | Little-endian layout minor (0) |
| 110 | 2 | Little-endian layout ID byte length (25) |
| 112 | 4 | Little-endian payload byte length (737280) |
| 116 | 25 | UTF-8 `fes.atari-st-floppy.image` |
| 141 | 737280 | Complete raw disk image |
| 737421 | 32 | SHA256 of every preceding record byte |

There is no padding or trailing data. Total size is 737453 bytes. An envelope
checksum mismatch is corruption; a valid envelope with a different binding or
layout is incompatible. The independent patterned golden vector in
[`testdata/media-data-v1/atari-st.json`](../testdata/media-data-v1/atari-st.json)
pins the header, payload hash, checksum, revision and namespace without storing
a large binary fixture.

Storage retains the existing trusted-root/no-follow directory policy. It
locks the namespace, writes a private same-directory temporary file completely,
syncs it, atomically renames it to `record.bin`, then syncs the directory.
Saved is acknowledged only after this completes. A failure before rename
preserves the prior record. A directory-sync failure after rename may leave
the new canonical bytes visible and still reports save failure; the retained
capture permits reconciliation with the authoritative current revision.

Opening the namespace takes that same exclusive lock before any read. It then
deletes leftover files named `.record-<pid>-<sequence>` or
`.probe-<pid>-<sequence>`. Those are the private save temporary and the write
probe. A crash, or a cleanup that does not finish, can leave them behind. A
save still holding the lock keeps its file. `record.bin` and every other name
stay.

Sector writes stage a complete 512-byte sector before changing the emulated
disk. Once publication to SDRAM starts it drains through warm reset or force
interrupt before capture proceeds. This prevents a torn sector in a captured
image. It makes no claim that several guest filesystem writes form one atomic
transaction or that unflushed guest changes survive physical power loss.
