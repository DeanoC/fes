# Nano final handoff first-failure diagnostic

TRWU8CBL's bounded v12 attempt accepted the initial CPU parking proof, calibrated
and initialized 1 GiB LPDDR4, and verified the matched FIT before parsing it.
All four loadable CRC checks passed. SPL then printed
`FES_NANO_SPL REJECT handoff/PIO drain` and stopped. No BL31 entry, native
`fesboot` command or SD write command followed. The source was
`1a6bde4acc95e9d1edbb3adc3fbea4605e28ec1b`; the full
[console](v12-console.log) and [receipt](evidence.json) preserve the exact artifact
identities. Post-stop exact-cable JTAG identity was observed and USB permissions
were restored. The test card stays inserted; the factory card remains safe.

The combined failure does not identify entry-hash verification, changed CPU
scope, SD idle/clock stop or peripheral reset readback as its cause. No such
cause is inferred from the DDR/FIT milestones. The v12 attempt is fused and is
not repeated unchanged.

The replacement records the first stopped final-handoff stage and only values
already observed by the existing policy. SPL reports saved first/before/last
samples, SD state, entry hashes and each outer gate before its rejection line.
It does not add register probes after failure, alter the sealed version-2 wire
record, relax admission or increase poll limits. Samples interrupted by a read
failure can be incomplete, and unreached values are not hardware observations.

Focused tests exercise the actual C drain implementation, first-failure
retention and unchanged caller output, plus the actual SPL reporting code with
injected EL/FIT/entry/drain failures and successful handoff. The new matched
v13 BL31/SPL/FIT/SOF builds are prepared. Integration checks and its bounded
physical trace remain pending. The card can be updated using the separately
qualified recovery/TFTP/file-write/readback path without a physical card swap.

This remains serial-only diagnostic evidence. Active device drivers, secure
firmware ownership during active traffic, tenfoot on the board and real core
launch/return are still unqualified.
