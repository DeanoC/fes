# Firmware-owned USB lease

[`fes.fabric.embedded.usb`](../packages/fabric/fes_fabric_embedded_usb.yaml)
provides generated C/Go constants for the exploratory Nano USB-only PIO lease.
It registers no production programming profile or physical readiness.

Calls use X0 `0xc2000303`, X1 operation, X2 owner, X3 cookie `0x46455355`,
and X4 USB generation. Query (0) requires zero owner and generation. Acquire (1),
Confirm (2) and Stop (3) require the original trusted boot owner and current USB
generation. USB generation is independent of bridge and SD generations.

Replies use X0 status, X1 tag `0x46555331` in the high word and flags in the low
word, X2 owner and X3 USB generation. Errors contain only tag and status; owner,
generation and flags are zero. Statuses are OK (0), Busy (1), Rejected (2),
Error (4) and NotReady (5). An unbound provider returns NotReady.

Every successful reply has Supported (1), DataHeld (16) and ClosedOnly (32),
plus exactly one of Contained (8), Active (2), or Active|Confirmed (6). Unknown
flags are rejected by the native client. Owner is nonzero. Generation starts
at zero with the controller contained; acquire advances it to a prepared active
lease, confirmation advances it after host/PHY checks, and active stop advances
it after drain/reset/readback. Query and an already-contained stop do not advance
it. Generation never wraps. Duplicate acquire/confirm requests are rejected.

Trusted firmware alone initializes the service and callbacks. SD and FPGA
bridges stay held. While USB owns the shared device lease, other SD, bridge and
FPGA configuration requests return Busy without touching those providers.
Uncertain acquisition, confirmation or stop retires ownership and retains any
reservation; callers must not retry an ambiguous mutation. Confirmation precedes
report delivery. Stop revokes polling before returning to another device owner.
No mixed USB/SD or USB/FPGA permission is granted by these flags.

The implementation is an unbound service plus an injectable native client; a
selecting secure boot profile and physical qualification remain required.
