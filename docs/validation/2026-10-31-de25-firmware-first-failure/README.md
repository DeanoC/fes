# First failed Nano firmware handshake: matched diagnostic preparation

Base: `8cae215984968769184e1081ed862fb56180e1a6`. Implementation:
`3baa85a23`, with final helper whitespace/source identity at
`2f47358b438931579673ff85299ec598f34c5354`. This follows the
[physical bridge trial](../2026-10-31-de25-bridge-trial/README.md), which returned
firmware error `4` while the required reset bits matched before/after.
The failing firmware substage remains a physical question.

The contained Terasic reset routine now preserves a snapshot immediately after
its first failed disable poll. It records the stage, signed error, `HDSKACK`,
`HDSKREQ` and `BRGMODRST` before later polls/writes can obscure them. The six
Agilex5 stage names and expected acknowledgment conditions are documented in
the [firmware guide](../../../platform/embedded/firmware/README.md). The snapshot
is a sequence of reads after failure, not an atomic sample of the final poll.

The dedicated probe opts into three extra return registers using a cookie in
the otherwise unused SMC argument. The original return status and ordinary
callers' `SMC_RET1` path remain intact. Each service invocation clears the old
record. The loader logs `FES_NANO_BRIDGE DETAIL`, checks the version/state, and
continues only with a zero service return, matching reset bits and valid empty
failure detail. Missing/inconsistent detail rejects. Stage zero means no
failure snapshot; zero words are not observations that the registers were zero.

Timeouts, poll counts, reset writes, first-error propagation and parking after
rejection are preserved. There is no already-reset shortcut, error waiver or
automatic retry. The extension is local to the contained Terasic probe; the
shared runtime programming/capability contract and upstream profile are unchanged.

## Validation and artifacts

Fifteen focused Python tests pass. Host verification executes the actual
prepared firmware functions, all six Agilex5 disable-poll failure positions,
multiple-failure priority, exact first-snapshot values, three snapshot reads,
stale-state clearing, opt-in service returns and ordinary-caller behavior.
It also retains the original vendor timeout-masking reproduction, enable/disable
error propagation, versioned dispatch and existing actual-RTL lifecycle checks.
Loader tests cover both handoff phases, all diagnostic stage decoding,
missing/inconsistent responses and firmware-dependent loader identity.

The final BL31 and full Nano SPL/U-Boot cross-builds pass. All three new
diagnostic functions and the bridge service/reset functions are present in the
actual AArch64 ELF. The dedicated loader build ID now binds the selected BL31
digest as well as the native entry and handoff sources. Packaging verifies that
binding and rejects stale firmware producer receipts before loader assembly.
The native entry and vendor recovery SOF remain unchanged.

Parent consistency and generated-consumer checks pass. The affected-test wrapper
reached its 600-second outer cap in the final test of its final FPGA command.
All preceding commands completed successfully (the executor stops on failure),
including 1,191 parent tests with 40 skips. No preceding FPGA case reported a
failure. The interrupted AY simulation passed separately in 3.349 seconds.
All planned test coverage was completed this way; the timed-out wrapper is
retained and is not reported as a successful wrapper exit. The plan was
reconstructed after documentation edits, so its producer `--allow-dirty` option
reflects those later edits rather than the original clean invocation.

An initial test rerun used the wrong discovery directory and was corrected;
both rerun receipts are retained. The final firmware was rebuilt/reverified
after removing a trailing blank line from the inserted helper. The early
component is not selected for this candidate.

The matched candidate ID is **`7445df506d2befe3`**. Its BL31 SHA-256 is
`d5bc92fad82d8ee60ebfb0cda1efeea4fbb065dfec1c5af3aa73194e320cf121`.
The 150,994,944-byte SD image SHA-256 is
`0804f88b6b43b26151c80d80a6846d3a993ccf7468cf520c67c273b696c09a6a`.
Raw/FAT readbacks, geometry, source/configuration and component identities pass
the existing package checks. The matched test kit is retained under
`/home/deano/fes-retained/de25-nano-first-failure-20261031/test-kit/`; its files
are read-only. [evidence.json](evidence.json) binds all selected files and logs.

## Physical trial state

The user connected the previously authorized 4 GB spare. It was reidentified
as USB reader `000000000819`, 3,980,394,496 bytes at `/dev/sda`, and unmounted.
The overwritten prefix was backed up privately and the new image's raw
readback matches the digest above. Original/private prefix backups are retained
separately and excluded from published diagnostic evidence.

TRWU8CBL is claimed. Authenticated factory baseline checks matched kernel,
root filesystem, original card CID/capacity and board model, with boot UUID
`fc82bf95-030e-4fb9-beba-4692fba0100f`. Linux has cleanly powered off and the
UART client is closed. The user has been asked to insert the refreshed spare,
preserve the original card and leave power unchanged. No candidate programming
or native launch has happened in this trial yet.

After insertion, use only the exact top cable/FPGA ID and UART interface,
require fresh matching SPL/full U-Boot banners plus the vendor prompt, and
issue `fesboot` once. Preserve the existing 90/45/45-second programming/boot/
launch bounds. Retain both `RESULT` and `DETAIL` records; any rejection or
missing completion ends the attempt. Restore the original factory card and
exact vendor-reference SOF, verify the fresh factory boot and card identity,
close owned clients, restore temporary permissions and release the claim.
Native activation, drain, peripheral operation and tenfoot remain unqualified.
