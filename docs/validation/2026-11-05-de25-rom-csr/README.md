# CPU-fed ROM transfer for the Linux-free DE25 prototype

The freestanding C ROM driver transfers 4 KiB and 8 KiB images through an actual
Verilated CSR endpoint into the real ZX80/ZX81 machine model. The CPU remains
held during loading. After successful transfer, the model explicitly releases
execution and the synthetic ROM writes its expected RAM marker and halts.
This is software/RTL evidence, not a physical board or ordinary-package result.

Implementation commit: `9bb3350d3` (base `76809a4f6`). All 20 frozen model inputs
match that commit. `08b41a294` corrects the generation-coverage test's expected
file count and validation set; it changes no firmware or RTL. Generated C and
Verilog constants come from `fes_fabric_embedded_rom.yaml` through the existing
registry. The new definition has only private firmware and prototype consumers;
unknown neighboring contracts retain the conservative all-consumer test rule.

The C driver snapshots admitted source bytes, checks the expected CRC before any
MMIO, checks identity before and after the transfer, and checks ownership around
every access. It requires Hold and ordered progress, bounds each wait to 64 polls
and the entire operation to the existing qualified 400 MHz timer's one-second
limit. Completion requires exact length, counters, CRC and final held state.
The endpoint accepts four ordered full-width words per 128-bit beat. It has no
memory-master port. Neither component opens bridges or releases the CPU.

Tests inject changed source, early/late identity mismatch, lost owner or Hold,
read and write failures (including a write that takes effect before reporting an
error), corrupt payload/counters/CRC, unknown status bits, clock rollback,
deadline and poll exhaustion. Failed operations cannot replay MMIO or reinitialize
the same context. Malformed CSR accesses fault the sink. Strict endpoint lint,
host and ARM64 freestanding compilation, and generated consistency pass.

The selected parent aggregate hit its 900-second limit. Its log also exposed an
outdated generation-coverage expectation, corrected by `08b41a294` and rechecked
with all 11 consistency tests. Completed passing/skipped cases are retained;
only outstanding cases completed in 11 bounded groups. The exact selected set
accounts for 1,516 passes and 42 skips. The FPGA software suite completed 1,577
cases with 75 skips. `result.json` records the
exact coverage and final status rather than calling the original aggregate a pass.

The implementation source bundle is self-contained and was restored and checked
with strict Git fsck. Logs, model binaries, compiler result and coverage are retained
at `/home/deano/fes-retained/de25-rom-csr-20261105-r1/` with hashes and archive
readback evidence. A separate publication bundle adds the test correction and
this record to that source snapshot.

No FPGA image or hardware action is produced by this change. TRWU8CBL remains
parked after the earlier physical controller/core-cycle run. The new C component
is not linked into the board boot chain, bound to a secure firmware operation,
or admitted by the native package loader. Its caller must retain admitted ROM
bytes, hold the existing lifecycle lock, validate the secure source span, current
owner/epoch, bridge scope, parked masters and CPU Hold, and own containment.

The next integration needs the matched physical endpoint/conduits and sealed
identity, secure bulk-transfer binding, and retained-ROM launch receipt. The
existing native rejection of unsupported ROM-linked packages remains in place.
Native video, Ethernet, ordinary media and broader playable acceptance remain open.
