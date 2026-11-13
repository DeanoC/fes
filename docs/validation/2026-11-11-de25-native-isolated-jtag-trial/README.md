# Native installer with one private JTAG service per image

The preceding [native programming](../2026-11-11-de25-native-installer-prefix-trial/hardware-result.json)
stopped at scan error87 after all ten staged files and fresh readbacks passed.
Separate [API enumeration](../2026-11-11-de25-jtag-enumeration/hardware-result.json)
and [vendor device-list](../2026-11-11-de25-jtag-programmer-enumeration/hardware-result.json)
diagnostics passed on fresh private services at3MHz/auto0. They leave the
physical cause unresolved and do not qualify image programming.

This candidate changes the programming service lifetime and its read-only
preflight. Recovery gets one fresh private service. After its verified boot,
the host restores both original clock properties, stops that service, restores
USB permissions and archives its exact files. Uploads run with no private JTAG
service active. Native programming then gets a new service with a distinct
PID/start-time identity, fresh clock checks and fresh grants. Each programming
reservation requires the actual vendor `-a` device list, exact private cable
and sole expected FPGA, followed by the existing forced scan and unchanged
durable consumption policy. No failed phase is replayed. A failed recovery or
cleanup cannot open the native service. Old records and all three prior
programming failures remain retained; this is a new bounded qualification.

The same candidate `c491ac67743d3526`, compiled source `49752d8b2`, remains
selected. FIT, SPL, entry, native ELF and sealed menu/computer payloads are
unchanged. The current recovery package is reused only after its original
source/input checks pass; it is not relabelled or regenerated. The upload
sequence, empty bootstrap, actual Tenfoot Install/Library controls, one ZX81
launch and final owned Quit retain their previous observer and phase bounds.
The separate fresh-discovery test remains required to establish persistence.

All23 focused operator model cases pass, including both programming roles,
malformed/failed vendor device lists, no scan/program after a rejected list,
durable refusal to retry, partial starts, reused service identity, failed
operations and cleanup failures. The latest six selected software gates at
`0e67e4f93` cover unchanged production source; new runner behavior has its
separate focused models. Full file admission covers5642 files/eight trees.
A prepared procedure is not a hardware pass.

The continuing top-only claim, shared Quartus lock, fresh stable USB/free UART,
source/tool checks and single-use image attempts remain mandatory. Existing
native450/outer800/host890 and per-operation bounds remain unchanged. The first
failure stops with its original two-second receive-only tail. Only TRWU8CBL
and its installed spare are selected. No physical keyboard action is required.
Native Ethernet remains disabled; visible HDMI remains separate. Factory
restoration is waived; its card and the bottom kit stay protected. Old-output
cleanup remains paused.


The [physical result](hardware-result.json) at source `a65a2a130` passed both
actual image programmings, recovery boot, ten uploads/fresh readbacks, the
matched native loader and complete firmware RAM/MMU/affinity/loader trace.
Both distinct private services closed with15MHz/auto1 readback and exact USB
permission restoration; their original files are separately archived. No new
programming failure occurred. This is one successful bounded image-loading
observation for the mitigation, not a demonstrated cause of the earlier87.

Application startup stopped at `openProgramUSB` before the SD-open checkpoint
or initial menu. HPRT offset0x440 was0x1000: the controller's power bit was set,
but the connection bit remained clear at its unchanged500ms bound. No descriptor
enumeration, Install or core launch was reached. The compiled main source and
complete saved firmware trace were checked independently. The native client
retains its partial USB resources and refuses retry; the first-failure capture
and two-second passive tail remain unchanged. Host cleanup does not prove a
native Park or final containment.

The operator has been asked whether the keyboard/hub is still attached and
powered. A connection failure is not converted into a successful input source,
a dummy driver or a longer timeout. The current USB prototype requires an
attached accepted device and does not implement hotplug recovery; normal
no-device/late-attachment behavior remains a separate parity gap. The next
physical run requires its physical preconditions and fresh admission; it cannot
resume this retired USB instance.

Independent root inspection verifies both owned services gone, both archived
service-file hashes, exact USB baseline permissions and complete UART release.
The [retention proof](retention-proof.json) covers183 files, archive readback
and modes, independently restored source, strict fsck and pack verification,
with the retained native candidate capsule declared as a dependency. The kit
remains at the observed native panic with no final Park claimed. Factory
restoration is waived; its card and the bottom kit remain untouched.
