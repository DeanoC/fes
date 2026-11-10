# Native installer observation and bounded hardware procedure

Implementation `8fdd4949e476c3a6a1d9daf3c782ebeea9db599f` adds an explicit
first-install observer and a fresh-recovery read-only file reader. The ordinary
paired two-program verifier keeps its existing default contract. Installation
retains actual native counters: empty Ready 0, completed Install 1, launch 2,
return 3 and final Park 4. Navigation records observe UI controls; they grant
neither storage ownership nor firmware authority.

The observer requires independently presented Install and Library controls,
old-menu closure, the complete firmware configuration queue and master-stop
receipts, independent menu restoration, USB pause/resume, a nonbusy completed
setup view and a fresh menu frame. Each failure retires the observer without
retry. Its 220-second collection bound composes existing dispatch, pause,
100-second work, 100-second menu restore, resume and presentation limits. It
extends no native operation deadline.

`UploadSession.read_existing` starts with fresh recovery ownership and empty
volatile marker/scratch reservations. It performs no download, SD write or
activation; it verifies the fixed target and rechecks protected recovery files.
It cannot adopt an old session or establish CID identity. The compiled native
writable mount separately requires the admitted card CID and layout.

The [software result](software-results.json) records six selected host gates,
including 1,908 parent regression cases (three skipped), planner checks,
whitespace and generated consistency. Focused tests cover first install,
fresh discovery, repeated launch, missing ownership/USB/menu evidence,
phase deadlines, replaced captures and uncertain writes. Their model wire
uses captured firmware/menu fixtures and supplies no new hardware acceptance.

The operator under `out/nano-installer-hardware-20261111` stages the unchanged
native candidate `c491ac67743d3526`, compiled from `49752d8b2`. It requires ten
fixed verified uploads with the install manifest last and an empty bootstrap
library. It invokes ordinary Tenfoot Settings, Systems, Install and Library
controls, then one ZX81 launch and final owned Quit. A separate fresh boot and
read-only source/bootstrap verification must establish persistence. This first
experiment must never report persistence by itself.

Operator models execute the actual nested UART exchange with split packets,
queued frames, lost claim, incomplete input and short/uncertain writes. Saved
bytes and original polling times are replayed through the same strict observer;
mutated receipts and extra output are rejected. Qualified JTAG, serial receive,
first-failure tail and cleanup helpers retain their established implementations.

File admission initially rejected the retained recovery package because its
producer receipt pinned the preceding `upload.py`. The package was regenerated
in a new private directory through the existing producer. FIT, SPL, SOF,
protected `RECOVERY.JSON` and pre-partition prefix remain byte-identical. The
original retained package and failed admission log remain unchanged. This
refresh changes host bindings, not the firmware or the installed boot files.

No installer hardware acceptance is claimed here. The kit-wide claim,
source/inputs, fresh USB stability, complete root UART-owner inventory, shared
Quartus lock and new one-use programming grants remain required. Only top
TRWU8CBL and its installed spare card are selected. Native Ethernet remains
disabled; physical HDMI confirmation remains separate. Factory restoration is
waived by the operator; original factory media remains protected. Output cleanup
remains paused.
