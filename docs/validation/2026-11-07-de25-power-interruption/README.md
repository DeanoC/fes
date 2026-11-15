# Serial state after the reported power interruption

Deano reported an accidental power interruption followed by restoration.
Both USB adapters are visible; only TRWU8CBL was opened. Its exclusive serial
observation obtained a fresh nonce and the exact qualified recovery version
at the expected prompt. The console also delivered earlier TFTP retry/timeout
output. This observation does not establish when power was interrupted, that
the HPS restarted, or that the card survived unchanged.

The [consumed upload attempt](../2026-11-07-de25-current-launch-trial/README.md)
stays terminal. Its retained result records three completed writes/readbacks,
a fourth transfer stopped before its RAM digest and SD write, and no native
programming or command. No active attempt was resumed or replayed.

This check sent only a newline and a version/nonce query. It did not program
the FPGA, reset the board, transfer a file, issue any SD command, start a
network server or change USB permissions. Serial settings were restored and
the descriptor closed. The continuing top-kit claim is held; the factory
card and bottom kit remain untouched.

The [100-ms recovery correction](../2026-11-07-de25-recovery-timer-unit-fix/README.md)
remains off-board. Its new exact-image repair must verify the live spare and
all protected/rollback files before a write, using a newly frozen operator
and a fresh bounded boot. No current physical card integrity, native boot,
ordinary core-loading acceptance or timing signoff is claimed.

Raw observation: `out/nano-power-interruption-20261107`.
The built correction and this observation are retained together in
`/home/deano/fes-retained/de25-recovery-timer-unit-fix-20261107-r1`.
