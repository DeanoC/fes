# Embedded identity and read-drain contract

`fes.fabric.embedded.control` 1.0 is experimental internal fabric, with generated
C++/Verilog constants. It adds no GP capability, playable-package ABI, physical
base address or registered Nano programming profile. Its constants and frozen
oracle accompany the firmware and RTL consumers in the same FES change.

The [definition](../packages/fabric/fes_fabric_embedded_control.yaml) contains
the exact byte offsets. The aperture is 32-bit, word-aligned, full-strobe only.
Requests and responses use ready/valid handshakes; the response stays stable
under backpressure. Readonly/unmapped writes and invalid commands return error
without changing the reader's ownership. The board supplies the actual bus
adapter and address, clock and reset mapping.

| Offset | Contents |
| --- | --- |
| 0x00 / 0x04 | Magic / control-interface version |
| 0x08 | Immutable producer-selected ABI personality value |
| 0x0c–0x18 | Immutable 128-bit build identity, low word first |
| 0x1c / 0x20 | Current display generation, low/high word |
| 0x24 / 0x28 | Staged request generation |
| 0x2c / 0x30 | Staged disable cookie |
| 0x34 | Command: 1 enable, 2 disable |
| 0x38 | Status bits: enabled, draining, drained, sticky fault |
| 0x3c | Outstanding bursts in low bits; held AR at bit 16 |
| 0x40 / 0x44 | Acknowledged generation |
| 0x48 / 0x4c | Acknowledged cookie |

Build identity is a producer's immutable `BUILD_ID` parameter, matching the
retained descriptor and its recorded build inputs. Package/payload identity
remains a separate runtime binding; embedding the package hash inside its own
payload would be circular. No producer or Nano ABI mapping is admitted merely
by defining this endpoint. Expected identity comes from the admitted target.

Enable requires a fresh nonzero generation, no fault, no outstanding/held read
and no active drain. Disable must match that generation and use a nonzero cookie
greater than the preceding cookie in this image. It blocks new reader requests
in the command's acceptance cycle. An already offered AXI AR remains valid and
stable until accepted. At most four bursts are outstanding, all on ID zero;
every expected beat and RLAST is checked. The disabled reader's remaining data
is consumed/discarded even if that reader applies backpressure.

Drained is acknowledged only after the held address and all accepted bursts
finish, with no current RVALID or sticky fault. The acknowledgement echoes the
exact generation/cookie. Unexpected ID, response error, incorrect RLAST or
unsolicited data sets a sticky fault and invalidates drain. Reset is disabled
with generation zero and no acknowledgement, so it cannot satisfy an old drain.
CPU copies, barriers, disabling a plane and elapsed time are not drain evidence.
This covers this read port only; global containment is a platform responsibility.

The pinned FES TF-A prototype uses local experimental SiP query `0xc2000300`,
unused in the inspected v2.12 service. It is **not** an assigned Intel API.
It returns x0=0, x1=`0x46455343`, x2=`0x00010000`, x3=15. Feature bits mean:
no automatic bridge release (1), containment before configuration (2), full-only
configuration (4), and null-safe remapper initialization (8). Version 1 retains
the inspected Agilex 5 SDM-relative completion address convention.
This marker distinguishes behaviour; it is not an authenticated firmware
measurement. Boot artifact identity and board qualification are still required.

The [FES firmware integration fixture](../../../platform/embedded/firmware/README.md) checks
that tuple before disabling the old display. The required runtime sequence keeps data bridges contained
while opening only the control aperture to verify the newly configured image.
The existing explicit bridge-enable service is still available to a trusted
runtime; exclusive control and correct mask/readback are required. Actual SoC
glue, complete boot image and physical acceptance remain unfinished work.
