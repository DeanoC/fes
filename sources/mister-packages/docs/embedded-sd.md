# Experimental firmware-owned SD lease

`fes.fabric.embedded.sd` 1.0 defines a private channel for SD PIO with all data
bridges held in reset. It shares the owned bridge channel's boot owner and
monotonic generation. It adds no production ABI, programming profile or physical
SD qualification. Secure platform code must initialize the service with qualified
ownership, reset, clock/PHY and independent stop/scope callbacks. The default
owned and cold serial producers leave it uninitialized.

The private call `0xc2000302` uses `x3=0x46455353`. That call is unused in the
inspected Terasic TF-A pin; it is an experimental local extension. `x1` is
Query (0), Resume (1), Prepare (2) or Stop (3). Mutations carry expected owner
in `x2` and generation in `x4`. Query requires zero owner and generation. No
non-secure operation supplies policy or callbacks.

Replies carry status in `x0`, tag `0x46534431` in the upper half of `x1`, state
flags in its lower half, owner in `x2` and generation in `x3`. Statuses are
OK (0), Busy (1), Rejected (2), Error (4) and NotReady (5). Every nonzero status
has only the tag, with zero flags, owner and generation. NotReady means trusted
initialization is absent. Invalid/stale/foreign requests reject before callbacks.

| Flag | Meaning |
| --- | --- |
| 0 | Qualified SD lease service supported |
| 1 | SD lease active; all-master containment is unavailable |
| 2 | Controller clock/PHY preparation completed for this active lease |
| 3 | SD stopped and independently contained |
| 4 | All bridge paths remain held in reset |
| 5 | Service supports only the closed-bridge PIO mode |

The only valid successful flag sets are `0x39` (contained), `0x33` (active,
preparation pending) and `0x37` (active, prepared). Resume requires containment;
Prepare requires an unprepared active lease; Stop requires an active lease.
Repeated transitions reject without creating another receipt. Each successful
transition advances the same generation used by bridge control by exactly one.
Query independently checks scope and does not advance it.

While a lease is active, bridge snapshots, bridge mutations and FPGA
configuration return Busy before invoking all-master containment callbacks.
An uncertain scope or transition retires both services and preserves the
unresolved lease. There is no automatic retry, reset-based recovery or
non-secure reinitialization. Stop uses an independent firmware drain/reset
callback even after the native driver reports quiescence. Firmware cannot
synchronize an actively issuing untrusted caller; the qualified native runtime
must fence and quiesce I/O before Stop.

This initial mode permits reading packages into retained CPU RAM before FPGA
transitions. Concurrent SD access during an active emulation core and automatic
adoption by the native fabric actor remain separate integration work.
