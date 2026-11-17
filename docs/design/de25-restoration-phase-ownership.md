# DE25 restoration phase ownership

The retained restoration transaction exhausted its 1,200-second host bound
after the original FIT write and all preservation checks. Its per-command owner
callback rehashed the source/tool closure and queried the programmer clock.
That failed verdict remains failed; this change does not extend any deadline.

`ResetRecoveryAttempt` now requires two observers. The programming observer
keeps the complete existing private server/config/client/probe/clock identity,
fresh locked scan, durable one-use consumption and exact SOF digest. The serial
observer starts only after the program-bound fresh SPL witness and new console
creation. It checks live claim, source, private server incarnation, configuration,
cable, USB node and open UART identity, bound to the selected reset nonce. The
common fields must match the admitted programming identity; changed observations
retire the attempt. The caller must validate actual live resources, not return
cached observations. The serial guard never claims to observe the JTAG clock.

The existing uploader still checks its fresh volatile boot marker, scratch map,
card layout and protected recovery files. It retains one session across all 17
before/after readbacks and the selected single FAT write. Each command retains
its random response nonce and live owner checks. Reboot, scratch/session loss,
partial results or changed files remain failures; no old session is adopted.
After successful serial work, the programming boundary is audited again once.
Clock and USB permission restoration and independent handle checks remain the
caller's mandatory cleanup. The helper never grants ordinary native resume,
SDM reset, old-source reuse or raw SD/flash write authority.

`PinnedFiles` admits immutable source/tool digests at phase boundaries. Between
them it checks filesystem incarnation and nonblocking Linux inotify mutation
events, including edits restored within one timestamp tick. A mutation, watch
loss/overflow, replaced file/symlink/parent, wrong digest or change during audit
retires admission permanently. The caller closes the watch on every exit. This
is a cooperative-host integrity check, not protection against a privileged host
forging observations. Only immutable inputs belong in it; changing outputs and
device permissions use their own live guards.

The complete model restoration performs nine programming observations, independent
of its thousands of live serial observations. Regressions cover replacement of
the claim, server, cable, configuration, reset nonce, USB and UART, plus ownership
loss after the single write. These are injected-device checks. Hardware phase
cost, full restoration completion and the separate native release diagnostic
are recorded in the [bounded physical result](../validation/2026-11-17-de25-restoration-phase-cost/README.md).
Both complete FIT transactions pass within the unchanged bound; the native
diagnostic separately fails returned-state guard 34. The exact original FIT is
restored without a power cycle. This is CBL diagnostic evidence, not normal HIL.
