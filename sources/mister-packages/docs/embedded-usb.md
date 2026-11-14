# Firmware-owned USB lease

[`fes.fabric.embedded.usb`](../packages/fabric/fes_fabric_embedded_usb.yaml)
provides generated C/Go constants for the exploratory Nano USB PIO lease.
It registers no production programming profile or physical readiness.

Calls use X0 `0xc2000303`, X1 operation, X2 owner, X3 cookie `0x46455355`,
and X4 USB generation. Query (0) requires zero owner and generation. Acquire (1),
Confirm (2) and Stop (3) require the original trusted boot owner and current USB
generation. USB generation is independent of bridge and SD generations.

Replies use X0 status, X1 tag `0x46555331` in the high word and flags in the low
word, X2 owner and X3 USB generation. Errors contain only tag and status; owner,
generation and flags are zero. Statuses are OK (0), Busy (1), Rejected (2),
Error (4) and NotReady (5). An unbound provider returns NotReady.

Every successful reply has Supported (1), exactly one scope selector
ClosedOnly (32) or SharedPio (64), and exactly one of Contained (8), Active (2),
or Active|Confirmed (6). ClosedOnly always includes DataHeld (16). SharedPio
includes DataHeld only when all FPGA data bridge reset bits remain asserted;
control access alone does not clear it. Owner is nonzero. Generation starts
at zero with the controller contained; acquire advances it to a prepared active
lease, confirmation advances it after host/PHY checks, and active stop advances
it after drain/reset/readback. Query and an already-contained stop do not advance
it. Generation never wraps. Duplicate acquire/confirm requests are rejected.

Trusted firmware alone initializes the service and callbacks. In ClosedOnly
mode, SD and FPGA bridges stay held. While USB owns the exclusive device lease,
other SD, bridge and FPGA configuration requests return Busy without entering
those providers.
Uncertain acquisition, confirmation or stop retires ownership and retains any
reservation; callers must not retry an ambiguous mutation. Confirmation precedes
report delivery. Stop revokes polling before returning to another device owner.
ClosedOnly grants no mixed USB/SD or USB/FPGA permission.

Version 1.1 adds SharedPio. It requires an explicit secure initializer, the exact
cold USB/SD composition and accepted boot/master binding, and the compiled USB
platform callback identities. A live USB observation precedes other owned calls.
An acquired but unconfirmed controller blocks them; confirmed USB may remain
active across SD use and owned FPGA transitions. SD's separate lease still
blocks bridge/configuration access, and existing producer-stop/drain and return
proofs still apply. USB uses an independent retained reservation, not SD's lease
bit or bridge generation. Stopping USB can preserve an active SD lease.

Shared USB mutations require all bridges closed and either original cold history
or a qualified held/window return. A running core returns Busy for USB Stop.
USB mutations return Busy while configuration is in flight. Shared queries
remain independent and may observe a running core or active configuration with
changing DataHeld, without changing USB identity/generation.
DataHeld is an observation, never a grant to open a bridge. Errors retain zero
state fields; uncertain ownership retires the shared authority and keeps the
controller reservation. A fresh trusted boot is required after such failure.

The native default `New` accepts only ClosedOnly. `NewSharedPIO` explicitly
requires SharedPio, generation zero, a contained controller and held data paths.
Both reject unknown flags, mixed scope selectors, mode changes and adoption of
active or returned leases. Typed report sources retain their USB identity when
only DataHeld changes. Neither constructor initializes secure ownership.

The service and native clients have injected dispatcher/platform tests. Existing
board selection remains USB-only; a composed boot selector and physical
qualification remain required. This capability registers no production
programming profile or normal core admission.
