# Retained source continuation: physical proof and later readiness failure

The format-12 trial passes full source transfer, source translation retirement
and one retained continuation on TRWU8CBL. It then fails the normal FPGA
configuration-status query before the initial MENU opens. This is a scoped
continuation proof and a failed native-menu trial. No core launch, source reuse
or general runtime acceptance follows from it.

The candidate uses production source
`09904343a78ff42b93a6d0bc6d34e3c47f224dd0`, published with documentation only at
`3ad86522589a87ca77579406d8aaea589c4a688b`. Its [matched software/build evidence](../2026-11-13-de25-retained-continuation/README.md)
binds chain `081d55a8cdded8739bc2b9913f01eff45836b07cf207e055b3d0ce60987cf87b`,
boot `95d1b9c9b79ed255`, FIT
`1f248739ef1154f49fbd68fd02ccd40b3195073d53a8a4edec649c7e236b9cb9`.
No FPGA synthesis, fitting, timing or seal changes are involved. A
[current source/artifact file check](publication-files.json) confirms unchanged
native membership and both matched/sealed-project archive hashes after these
documentation edits. That file-only check grants no hardware acceptance.

The [one-use trial](trial-procedure.json) consumes procedure
`13fd4da8431cf9e05085482e3d5df26e9192f81918dd81104ace3b65a2b402aa`
once under standing human approval. The 30,703-file freeze, 61 controller tests,
real current candidate binding, injected joined startup/Ready-Quit/Park paths,
active/unknown-worker recovery rejection, eight dual-root checks and real
user/root file-only admissions pass. The latest successful format-11 four-role
restoration is independently replayed from its immutable archive first.
Injected positive menu traces are model evidence; the actual candidate never
reaches that positive path on this board trial.

The [saved raw-wire analysis](failed-trial.json) verifies all 1,708,032 bytes,
two posts/two acknowledgements, matching SHA-384 and 11,581,395 us source time.
Retirement completes phase 6/status 0/retired 1, five writes, producer/consumer
12, control 0 and abort `0x100000`, in 401,247 us. Owned command consumption
passes at 96,819 us under its original 100 ms origin/bound. Mandatory final
snapshots remain under the existing one-second retirement scope. The fresh
live-engine continuation passes at 592,705 us from that unchanged origin.
All source, table, queue and control storage remains retained; reuse stays off.

After returning to the native configuration controller, firmware emits
`FES_NANO_FABRIC_ACTIVATION ... complete=1 data_held=1` and transfer completion.
The next `CONFIG_STATUS` command 4 returns `status=-1 words=0 ready=0`; native
startup panics with bridge firmware status 4. A configuration-completion record
does not substitute for the failed independent readiness/endpoint/MENU gate.
No App commands are sent. The trial stops at 381.489 s.

The active-native failure boundary prevents recovery programming after this
unknown native state, despite successful host cleanup. The candidate FIT stays
on the test card until a fresh human-confirmed top-only power cycle. All owned
workers and JTAG handles stop; UART is independently verified free by a root
process-FD scan. JTAG auto/15 MHz and exact USB identity/inode/permissions are
restored. The [failed archive](failed-retention.json) is retained and read back
without changing its verdict.

An [ASan/UBSan software reproduction](status-reproduction.json) accepts the same
six-word synchronous status reply before transfer preparation and rejects it
after successful transfer/retirement/continuation. The transfer service remains
configured; its `active()` guard sets sticky error -13. The vendor parser returns
-1/zero words before dequeuing the header or reading its payload. The three
[reproduced vendor functions](parser-membership.json) match current production
bytes exactly. This demonstrates a software handoff defect consistent with the
trace; it does not independently establish every physical cause. A scoped
post-completion control-reply handoff is still required; weakening the transfer
header guard or granting arbitrary mailbox use is not justified. The
[reproduction archive](reproduction-retention.json) retains its source/binary and
references the complete frozen source closure.

After the actual reset confirmation, [restoration-only procedure](restoration-procedure.json)
`25394079d39f1a3543ab25b5f09089e6a10f1c87d83b20eb0f858775cc580151`
permits only recovery-rollback, exact original-FIT rollback and native-baseline.
The candidate/forward roles reject before worker or directory creation. A mistyped
host digest is rejected before attempt reservation/device access; the actual
restoration consumes its frozen procedure once using the verified digest.

[Independent restoration](baseline-restoration.json) verifies exact original FIT
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`, protected-file readbacks, all 51 fresh
ZX81/ZX80 launch/return exchanges, USB STOP generation 3 and final Park generation
5. Owned processes stop, UART is free and JTAG/USB cleanup passes.
The [restoration archive](restoration-retention.json) is independently read back.
The failed candidate is never retried. Latest verified board state: original
native baseline final Park, test card installed, all owned host handles closed,
top claim held. QSPI, factory card and bottom kit remain untouched.

Failed-trial archive: `237cc5effca33b7d40c4ec00b9b631794e18e45f24d59c4eebc2a7aacc8fbe1c`. Restoration archive:
`49579d709f13e29c60728e94e2a6eb2557472177c30bb09d23446aa84def148c`. Software reproduction archive:
`4e2d21d007f27c7c8530cc2ff7b4b2befd5f49c4b859383e7a4e0b16a02a9a9b`. Each retention record identifies its exact preparation
and source dependencies; all referenced archives are required.

The earlier warm USB ownership panic remains unresolved by a cold restoration
pass. Repeatable retirement-path core loading, safe memory reuse, independent
interconnect drain, broader devices/packages and DE10-equivalent appliance
acceptance remain open. The active goal is incomplete.
