# Configuration START call boundaries

Physical `6c9e1aaecb129336` restored successful FPGA_CONFIG_COMP communication
through the secure mailbox mapping, then stopped at the unchanged initial-menu
deadline before the CANCEL return checkpoint. The interval still included
request-state updates, remapper setup, response clearing and CANCEL. No precise
stalled call, FPGA data WRITE, menu Ready, launch, tape or normal return was
established. That image and its one-use operations remain retired.

Fresh matched image `8bc1ed9a9478e26d` adds three constant UART records only:
phase 12 before the original remapper call, phase 13 after it, and phase 14 after
the original response-clear call. Existing phase 5 remains the CANCEL return.
The transformation reverses exactly to the physically tested service bytes.
Mailbox transport/header, every other producer input, complete memory layout
and all descriptors remain identical. Commands, arguments, device operations,
ownership/CPU/master guards, retries, resets, response rules and deadlines do
not change. The observer binds the explicit order
`0,1,2,3,4,12,13,14,5,6,7,8,9,10,11` and remains observational.

Thirteen actual vendor START cases compare original and instrumented call order,
results and retained failure state, including rejection paths. Tests verify that
the boundaries bracket the actual calls. Full modeled seven-configuration,
three-launch/four-menu cycles pass; missing, repeated, malformed, reordered or
nonzero boundary records cannot complete the configuration proof. All selected
parent/native/producer/firmware integration checks pass. Unchanged FPGA-only
checks are reused with current tracked membership/hashes verified.

The current linked image verifies the actual phase/call sequence and rejects
three removed boundary calls. Its generated secure-base selector/header and
mandatory driver calls verify, along with the canonical MMU transition and its
negative controls. Complete table bytes, layout and protected producer inputs
match the physical predecessor. The existing MMU execution evidence applies to
those unchanged sources; no new emulated device or physical result is claimed.
BL31 remains 311744 bytes and FIT remains 8383144 bytes within the existing
upload bounds. The exact qualified recovery artifacts remain unchanged.

A fresh top-only bounded trial still requires admitted inputs, one-use
recovery/native scans, all 13 uploads/fresh SD reads, complete original boot
proof, the original full launch/return/tape exercise and independent cleanup.
Preserve native 450 s, outer 800 s, host 890 s, initial Ready 100 s, tape 1 s,
first-failure stop and two-second passive tail. Successful software checks do
not establish ordinary core-load or full appliance readiness.
