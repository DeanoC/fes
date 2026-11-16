# Fixed firmware MAC and DMA-window lease

`fes.fabric.embedded.mac` 1.0 defines exploratory C/Go constants for one
boot-selected MAC and permanently pinned DMA window. The portable engine and
native client implement the protocol. The owned Nano firmware export routes
the channel; its default MAC setup has no trusted physical supplier and returns
NotReady. No production profile or board capability is added.

Requests use X0 `0xc2000305`, X1 operation, X2 boot owner, X3 cookie
`0x4645534d`, X4 MAC generation, X5/X6 zero. Operations are Query (0), Acquire
(1), Confirm (2), Stop (3). Query requires zero owner/generation. All mutations
require the exact accepted owner and current private MAC generation. No request
supplies a MAC index, DMA address, size, readiness or qualification bits.

Replies use X0 status, X1 tag `0x464d4331` in the high word and flags in the low
word, X2 owner, X3 private MAC generation. Status is OK (0), Busy (1), Rejected
(2), Error (4) or NotReady (5). Errors contain the tag alone, with zero flags,
owner and generation. Uninitialized providers return NotReady. Requests during
configuration return Busy without platform calls. Invalid or foreign requests
have no platform effects.

Every success includes Supported (1), PinnedWindow (16), PrivateGeneration (32)
and exactly Contained (8), Active (2), or Active|Confirmed (6). Owner is nonzero.
The initial contained generation is zero. Acquire grants access with DMA stopped
and advances generation; driver descriptor initialization and exact PHY checks
precede Confirm, which independently checks live readiness and advances it.
Only then may packet traffic start. Active Stop advances generation only after
independent drain/containment and final scope checks. Query and already-contained
Stop observe fresh scope without advancing generation or repeating stop writes.
A grant reserves enough generation space for confirmation and stop; no wrap.

Firmware initializes once per trusted boot under the shared platform lock.
Its private binding includes the original owner/epoch, selected MAC/PHY identity
and fixed DMA window. The current prototype requires a two-MiB-aligned two-MiB
window; trusted scope must independently verify memory exclusions, routing,
clock/PHY, SMMU/firewalls, parked CPUs and concurrent master/FPGA policy. These
are required callbacks, not qualification inferred by the portable engine.
The MAC generation does not change bridge, SD or USB generations or reconstruct
a running core's handles. The engine grants no bridge access.

The reservation precedes the first potentially releasing callback. Failed grant,
confirmation, drain or final observation retires the object and retains that
reservation. Uncertain stop never releases memory or claims containment. No
best-effort reset, callback retry, new arena or recovery is supplied. Other
owned mutating transitions must require a successful contained gate. A physical
supplier must also compose the live MAC grant with ordinary core/menu access;
this engine alone does not establish that coexistence or interconnect fencing.

The native client admits only the original contained generation zero. Shared
private state prevents copied handles from forking generations or rebinding.
After exclusive driver binding, raw aliases cannot issue calls. The driver's
restart consumes grant and confirmation separately, then exposes complete
controls to the common packet gate. Unknown/malformed/lost replies retain the
last ownership receipt and retire without retry. No current boot profile selects
this channel.

The owned entry guard rejects a bad cookie or nonzero X5/X6 with an empty tagged
reply before entering the service. Under the shared platform lock, the selected
CPU/MMU guard precedes MAC calls. Setup may publish a provider only for the
already accepted boot owner/epoch, contained generation zero and no reservation.
Missing/foreign/partial setup retires boot without clearing uncertain resources.

Confirmed MAC ownership permits fresh read-only bridge snapshot/context/pinmux
and USB/SD queries through their existing validators. An unconfirmed lease
blocks them. SD/USB mutations and FPGA configuration/bridge transitions require
MAC containment; the dispatcher does not enter those providers while active.
A failed MAC boundary retires shared bridge authority and blocks later hardware
callbacks. This routing is not a physical MAC coexistence or interconnect-fence
qualification; a trusted composed boot/master supplier is still required.
