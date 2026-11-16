# USB service-gap reproduction

The failed board packet timed out on its first poll, after the second menu frame
acknowledgement. The real DWC2 driver starts an interrupt-IN packet with a
500 ms deadline and returns without polling it immediately. The native input
platform returns on a nil report; menu presentation then runs synchronously.
The next packet poll checks its original deadline before accepting hardware data.

A host-only Go overlay test executes that real driver with the existing register
and clock model: arm a packet, advance 600 ms without servicing it, then poll.
It reproduces the captured first-poll timeout shape, retains the original deadline,
and confirms the failed host performs no further register accesses. No production
code or timeout was changed. The passing test means the failure mechanism was
reproduced; it is not a repair or proof of the exclusive physical cause.

The next implementation step is to keep USB serviced across the blocking menu
path through bounded platform hooks or scheduling, while preserving all existing
ownership, deadlines, transitions and terminal failure behavior. Disabling USB,
relabeling this as a successful launch or extending the packet timeout would not
satisfy the goal. A changed native producer requires a rebuilt matched chain
and a fresh bounded test, rather than replaying this failed candidate.
