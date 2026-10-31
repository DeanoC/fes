# First failed Nano firmware handshake: FPGA acknowledgment timeout

Base: `8cae215984968769184e1081ed862fb56180e1a6`. Implementation:
`3baa85a23`, with final helper whitespace/source identity at
`2f47358b438931579673ff85299ec598f34c5354`. This follows the
[physical bridge trial](../2026-10-31-de25-bridge-trial/README.md), which returned
firmware error `4` while the required reset bits matched before/after.
The matched physical trace now identifies the first failing substage as
`f2s_fpga_ack_set`: the initial FPGA acknowledgment wait during FPGA-to-HPS
bridge disable. The pinned firmware returned `-ETIMEDOUT` (`-60`) and the
loader rejected before native entry. Factory Linux was subsequently verified
restored and the top kit released. Why the acknowledgment is absent remains
unresolved.

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

## Physical trial and first failure

The user connected the previously authorized 4 GB spare. It was reidentified
as USB reader `000000000819`, 3,980,394,496 bytes at `/dev/sda`, and unmounted.
The overwritten prefix was backed up privately and the new image's raw
readback matches the digest above. Original/private prefix backups are retained
separately and excluded from published diagnostic evidence.

TRWU8CBL was claimed. Authenticated factory baseline checks matched kernel,
root filesystem, original card CID/capacity and board model, with boot UUID
`fc82bf95-030e-4fb9-beba-4692fba0100f`. Linux cleanly powered off and the user
confirmed insertion of the refreshed spare while preserving the original.

Only the exact top cable `DE25-Nano [3-11.1-iface0]`, FPGA ID `4362C0DD` and
UART interface `if02` at 115200 were used. Volatile programming and fresh
matching SPL/full U-Boot banners passed. At the vendor prompt, `fesboot` was
issued exactly once within the existing 90/45/45-second programming/boot/launch
bounds. The diagnostic produced:

```text
FES_NANO_BRIDGE RESULT phase=before-load smc=4 smc_hex=00000004 mask=0000000f before=0000000f after=0000000f smc_error=1 reset_mismatch=0
FES_NANO_BRIDGE DETAIL phase=before-load tag=46424431 valid=1 captured=1 stage=3 name=f2s_fpga_ack_set error=-60 failure_ack=00000000 failure_req=00000004 failure_reset=0000000f
FES_NANO_HANDOFF REJECT bridge phase=before-load smc_error=1 reset_mismatch=0
### ERROR ### Please RESET the board ###
```

The new versioned detail was physically returned through the EL3 SMC path.
Stage 3 polls `HDSKACK` mask `0x4` for value `0x4` after setting
`HDSKREQ.FPGAHSREQ`. The pinned poll has a 1,000-iteration budget with a 1 ms
delay per unsuccessful read and returns `-ETIMEDOUT`; that firmware's errno
header defines `ETIMEDOUT` as `60`. At the first failure snapshot, ACK was
`0`, request was `4` and bridge reset was `0x0f`. Reset bits also matched
before and after the full service, so this remains a handshake timeout rather
than a reset mismatch.

This identifies the first failed wait, not the cause of the missing
acknowledgment or every later poll outcome. Native entry, the `after-remove`
gate and all four handoff/entry/Go success markers were absent. The loader
parked before entry-file read and peripheral-reset sequencing. There was no
retry, reset-state shortcut or service-error waiver. Native activation, drain,
peripheral operation and tenfoot remain unqualified.

## Verified recovery and capture limitation

The user confirmed restoration of the original factory card. Programming the
exact vendor reference SOF succeeded. During recovery, the top USB device
re-enumerated from `003/037` to `003/044`, and UART `if02` became `ttyUSB1`.
The one-off recovery helper failed cleanup on the vanished old USB node before
saving its raw console and receipt. The programmer logs survived; the missing
raw recovery boot capture is explicitly recorded rather than reconstructed.

A separate fresh UART authentication on the stable serial-ID path verified
kernel `6.12.11-gd7d192a9ddd9`, root `/dev/mmcblk0p2 ext3`, the original card
CID/capacity and Nano model, with new boot UUID
`7d003c64-ad2a-4557-9b39-cd9c630ffa03`. The qualified recovery receipt supersedes
the generic helper's original no-power-cycle wording. No physical power cycle
was requested, and a physical power event was not independently confirmed.
Autonomous QSPI boot is not claimed.

The fresh session logged out and closed. The new verified top USB node has
its baseline `root:root 0664` permissions; the old node no longer exists.
Owned programmer clients finished, the shared JTAG broker was left running,
and the top claim was released. Flash and the factory card were not programmed
or reimaged; normal Linux login/shutdown may update accounting. No other kit
programming or harness operation was performed.

The next investigation is the reference FPGA's acknowledgment path and the
vendor sequence for an already-reset bridge. Cold/closed and active/draining
states need separate evidence before changing admission. Matching reset bits
alone do not justify accepting the missing acknowledgment.
