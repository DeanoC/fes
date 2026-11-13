# Bounded AFh-only flash identity read: PASS

TRWU8CBL at source `f22af5ec98505f81d968afaae8123a052fbba5f8` passed the separate
read-only experiment. Exact live CONFIG_STATUS, OPEN, chip-zero SELECT,
AFh three-byte ID read, two status reads, CLOSE and final CONFIG_STATUS
completed through the documented CLI with full response framing checked.
The AFh response was raw word `0018bb20`, yielding bytes `20 bb 18`; both status
reads returned zero. Configuration status matched before and after. No fallback
opcode or mode change was attempted. The prior 9Fh-zero attempt remains failed.

Independent replay/cleanup confirms a successful CLOSE, restored 15 MHz and
auto-adjust, absent private service, exact USB permissions and free UART. The
UART was never opened. No image was programmed, no array was read, no flash or SD
write was issued and no reset or reboot occurred. The native final Park remains
the prior observation, not a new CPU-state proof. Historical programming failures
remain three. Both factory media and the other kit were preserved.

The immutable [Terasic U-Boot ID table](https://raw.githubusercontent.com/terasic/u-boot-socfpga/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/drivers/mtd/spi/spi-nor-ids.c)
contains two entries for this ID: n25q128a11 and mt25qu128ab. Both specify
256 sectors of 64 KiB, giving a 16 MiB main array and a 4 KiB subsector flag.
The complete fetched source is hash-bound in `capacity-proof.json` and retained
with the raw trace. This supports the bounded main-array backup range; the ID
does not distinguish the exact die, package, revision or security/OTP areas.
The live identity receipt itself deliberately retains its narrower, unqualified
part/geometry flags; this separate source inference does not rewrite it.

The [manufacturer protocol reference](https://onlinedocs.microchip.com/oxy/GUID-5474CCCD-F385-4AD7-9759-539BB1019357-en-US-10/GUID-4F13BD82-4678-46E3-A8DB-759A4A056317.html)
describes AFh for Micron Quad I/O ID reads. The separate AFh result resolves the
practical identification path without resetting the flash protocol; it does not
independently expose every internal controller or device-mode setting.

A complete factory backup is still absent. Next is bounded main-array readback:
verify actual data framing and byte order, read every byte twice with identical
full-array hashes, and prepare a usable rollback artifact before any request to
change factory flash. A persistent or efficiently batched read-only transport
must first be qualified; this CLI identification took about fifteen seconds,
so starting a new process for all 8192 verification reads would be inefficient.
The SDM read command's success header alone cannot prove complete correct data.
Standalone power-on boot, native Ethernet and broader appliance acceptance
remain open. See `result.json` for exact raw evidence and source/tool closure.
