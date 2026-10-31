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

Layout `fes.atari-st-floppy.image` 1.1 retains the 1.0 envelope and namespace,
using minor 1 for the eleven nonlegacy lengths in [computer I/O](computer-io.md).
Record length is exactly payload length plus 173 bytes; maximum payload is
839680. 737280-byte records always use minor 0 with byte-identical encoding.
Decoders reject unknown minors, invalid lengths, mismatched minor/length,
identity or checksum. Endpoint admission still requires the extension and
restores reject saved/base length mismatch before hardware transfer. Saved BPB
edits do not change physical geometry. `atari-st-geometry.json` independently
fixes the 839680-byte patterned header, checksum, revision and namespace.

## ZX81 tape capture

`fes.zx81-tape.image` 1.0 is the `.p` captured by `fes.media.zx81-tape-write`.
The envelope matches the ST record field order with a different magic and
layout id, so an ST decoder rejects it. Payload length is 1..16384. Layout
minor is 0. The namespace uses the same `fes-media-data-v1` binding. Record
length is the payload length plus 167 bytes (116-byte prefix, 19-byte layout
id `fes.zx81-tape.image`, 32-byte checksum).

| Offset | Size | Bytes |
| --- | --- | --- |
| 0 | 8 | ASCII `FESTAPE1` |
| 8 | 96 | Core, game and base-image SHA256, as in the ST record |
| 104 | 2 | Little-endian unit (0) |
| 106 | 2 | Little-endian layout major (1) |
| 108 | 2 | Little-endian layout minor (0) |
| 110 | 2 | Little-endian layout ID byte length (19) |
| 112 | 4 | Little-endian payload byte length |
| 116 | 19 | UTF-8 `fes.zx81-tape.image` |
| 135 | N | Raw `.p` image |
| 135+N | 32 | SHA256 of every preceding record byte |

The interim host library stores that raw `.p` as core media so the Workshop
cassette picker can select it. The kit-as-host store is a separate change.

## ATA disk delta

`fes.ata-disk.delta` 1.0 is the durable record of a `fes.media.ata-disk`
image ([computer I/O](computer-io.md#ata-hard-disk)). The imported base image
stays immutable; the record names the 64 KiB chunks the guest changed. Its
namespace is the `fes-media-data-v1` namespace above, with unit 0 and the
base image's SHA-256, so two games or two base images never share a record.

`<namespace>/record.bin` is a manifest:

| Offset | Size | Bytes |
| --- | --- | --- |
| 0 | 8 | ASCII `FESDELT1` |
| 8 | 96 | Core, game and base-image SHA-256, as in the ST record |
| 104 | 2 | Little-endian unit (0) |
| 106 | 2 | Little-endian layout major (1) |
| 108 | 2 | Little-endian layout minor (0) |
| 110 | 2 | Little-endian layout ID byte length (18) |
| 112 | 4 | Little-endian base size in bytes |
| 116 | 4 | Little-endian chunk size in bytes (65536) |
| 120 | 4 | Little-endian chunk count N |
| 124 | 18 | UTF-8 `fes.ata-disk.delta` |
| 142 | 36N | Per chunk: little-endian u32 chunk index, then SHA-256 of its data |
| 142+36N | 32 | SHA-256 of every preceding record byte |

The size is exactly 174 + 36N bytes. The base size is a whole number of
512-byte sectors from 1 MiB to 128 MiB and equals the bound base image's
length. Indices are strictly ascending and below ceil(base size / 65536).
Chunk `i` covers base bytes `65536 × i` up to the next chunk or the end of the
image, so the last chunk of an image that is not a whole number of chunks is
short. An `FESDISK1` or `FESTAPE1` decoder rejects the record by its magic.

Each chunk's data is the file `<namespace>/chunk-<lowercase SHA-256>`,
holding exactly the chunk's bytes. Equal chunks share a file. Revision is
lowercase SHA-256 of `record.bin`; `absent` means no record and restores the
base image unchanged.

Restoring writes the base image and then every named chunk over it. A chunk
file that is missing or whose bytes do not match its hash is corruption: it
blocks the launch like a corrupt manifest and is never replaced silently.

Saving at Stop, and before an eject, reads the core's dirty map after
Freeze, reads those chunks from the window, writes and syncs each new chunk
file under a private temporary name, then merges the new indices into the
existing manifest, keeping entries for chunks the guest did not touch.
The merged manifest is published with the temporary-file, rename and
directory-sync rule above, under the namespace lock and against the
expected revision. Only then does the core receive Saved. Last, still under
the lock, chunk files the published manifest does not name are deleted.
Opening the namespace also removes leftover private temporaries named
`.chunk-<pid>-<sequence>`. A whole-image record would rewrite up to 128 MiB
at every Stop after a game install; the delta writes only changed chunks.

[`ata-disk-delta.json`](../testdata/media-data-v1/ata-disk-delta.json) is the
golden vector: a 2,098,688-byte patterned base, chunks 0, 5, 31 and the
1,536-byte tail chunk 32, the complete manifest, its revision and namespace,
the restored image's SHA-256 and CRC-32, and rejected manifests.
