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
injected EL/FIT/entry/drain failures and successful handoff.

The matched v13 chain passed all ten affected software checks: 1,227 parent
tests (40 skipped), 1,571 FPGA tests (75 skipped), generated consistency and
native/producer/firmware RTL integration. Its actual recovery boot and production
Ethernet upload verified the 4,711,676-byte FIT, independent SD hash/byte
comparison and unchanged protected prefix/root FIT. No physical card swap or
factory restore was needed.

The [v13 console](v13-console.log) identifies the first stopped gate as stage 5,
before any SD state/clock read or reset write. EL3, sealed FIT and entry hash
checks passed, and all saved CPU/power/boot scope checks passed. PER0MODRST was
`ff7fbf3f`, with bit7 clear and bit15 set. The official
[CS/R1 RC28 register map](https://docs.altera.com/v/u/resources/864455/agilex-5-cs-and-r1-suffix-opn-hps-register-map)
identifies bit7 as the controller reset and bit15 as the SDMMC ECC OCP
diagnostics reset. The pinned Cadence driver releases only the named controller
reset; SDMA and ADMA are disabled. This also agrees with the
[Agilex 5 SD reset description](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/resets?contentId=mqCLMO89AedWC3j2hHrc1g).
The v13 gate incorrectly required both resets to be clear. No native command
was sent; the stopped diagnostic and exact chain were retained.

The v14 correction requires the controller to be running before its drain,
while the unused ECC diagnostics reset must stay asserted from the first
sample. Holding that diagnostics reset alone grants no drain proof: DMA-off,
two independent idle observations, clock-stop readback and final reset readback
remain required. Faults in the held diagnostics reset before/during/after drain
retire ownership. The serial decoder checks the same initial mask. Focused
actual-C and decoder checks pass; integration and the fresh matched v14
hardware trace remain pending.

This remains serial-only diagnostic evidence. Active device drivers, secure
firmware ownership during active traffic, tenfoot on the board and real core
launch/return are still unqualified.
