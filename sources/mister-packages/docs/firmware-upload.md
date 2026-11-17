# Launch-time firmware upload 1.0

`fes.fabric.firmware-upload` sends a machine's firmware to a core at launch,
over the [fes.computer](computer-io.md) mailbox framing. It is for boards
whose bitstream cannot take linked ROM lanes: DE25 packages cannot seal a ROM
map, so a DE25 core keeps its ROM in block RAM and the host fills it before
the machine first runs. Constants are in
[`fes_fabric_firmware_upload.yaml`](../packages/fabric/fes_fabric_firmware_upload.yaml).

It is not a GP capability bit. A package declares this ID as a required
interface at this version, as a session-display package declares the display
plane, and a host sends these opcodes only to a package that declares it. An
endpoint without it answers them with invalid opcode. The four opcodes are
taken from the fes.computer opcode space and are not reused there.

The endpoint names one exact image size. Which firmware that is (the model's
ROM pages in order, as the DE10 package's ROM requirement lists them) is the
package's contract, not this interface's.

| Opcode | Name | Index | Argument |
| --- | --- | --- | --- |
| 29 | FirmwareInfo | field 0..4 | 0 |
| 30 | FirmwareBegin | word 0..3 | header word |
| 31 | FirmwareData | word ordinal modulo 256 | byte pair, low byte first |
| 32 | FirmwareCommit | 0 | 0 |

Errors are the fes.computer codes. Every rejected request acknowledges the
toggle and changes no state, except where noted. Checks are made in the order
written; the first failing check determines the error.

**FirmwareInfo** (29): fields are 0 image bytes low, 1 image bytes high,
2 state (0 empty, 1 loading, 2 ready), 3 bytes received low, 4 received high.
A field above 4 is an invalid index; a nonzero argument is invalid. Info never
changes state and is valid while held or released.

**FirmwareBegin** (30): words are 0 total low, 1 total high, 2 expected CRC32
low, 3 CRC32 high. A word above 3 is an invalid index. Begin is valid only
while execution is held (invalid state otherwise). The word must be the next
header word, starting at 0 (invalid index). Word 0 makes the image loading at
once: a previous image is gone from the first accepted word. Word 3 checks the
assembled total against the endpoint's image size; a different total is an
invalid argument and abandons the header, so the next Begin starts at word 0.
A matching total arms the transfer with zero bytes received.

**FirmwareData** (31): valid only while armed, held and short of the total
(invalid state). The index must equal the next word ordinal modulo 256
(invalid index). The low byte is stored first. The last word of an odd-length
image carries one byte and its high byte must be zero (invalid argument). A
valid word advances the received count and the CRC exactly once.

**FirmwareCommit** (32): index and argument are 0. Not armed, or received not
equal to the total, is invalid state. A CRC mismatch is an invalid argument
and disarms the transfer with the image still loading: send a new Begin. A
match makes the image ready.

**Execution** (fes.computer opcode 2): a release while the image is not ready
is an invalid state. A hold is unaffected. A ready image survives later holds
and releases, so a relaunch of the same loaded core needs no new upload; a
host reads the state first. Reconfiguring the FPGA empties the image.

CRC32 is CRC-32/IEEE exactly as in [stream media](media-stream.md), over the
image bytes only. It checks delivery; it does not replace the host's SHA-256
identity for the firmware it selected.

One owned GP session serializes each transaction. Do not replay an ambiguous
mutation: send Begin from word 0 again.
