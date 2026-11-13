# Retirement queue-progress observations

The format-9 input profile replaces four separately guarded completion-poll
reads with a single bounded read-only observation. Its 24 registers cover
fixed capabilities, error/event state, queue bases, abort, both control values,
the published producer and a freshly sampled consumer. Ownership/time checks
bracket both the batch and caller. Callback substitution, invalid or backward
consumer progress and the original command/poll bounds still retire the attempt.
Slot stores, the producer write, final queue snapshots and all five table scans
keep their existing gates. Arm-only and fault profiles leave this callback
unselected and retain their individual queue reads.

New mutation tests expose a late phase change during the final ownership
observation that could previously reach the next disable write. Command ownership
now validates phase and binding after its register batch as well as before it.
The new regression stops before that fifth write. The initially failed model
runs are retained separately; their failures are not relabelled as passes.

All 23 focused retirement/profile/observation tests pass. The C models run
under ASan/UBSan. Callback tests cover failed suppliers, owner loss, changed callback
or context, expired/backward clocks, stalled and invalid/wrapped/backward
consumer pointers, and failure after returning a completion-looking value.
Portable retirement mutations cover every read position after publication,
including the new progress batch and final ownership views. Failed attempts
retain memory, forbid retry and grant neither reuse nor ordinary activation.
The explicit profile rejects actual format-7 and format-8 contract records.

The [joined cost model](model-result.json) compares against a separately captured
format-8 operation baseline. Command-phase supplier reads fall from 5,514 to
3,006 (45.48%), counter calls from 293 to 149. Counts in the other six phases
remain identical, including all 25,600 table-word checks and the same five
control writes. Injected 32 us read cost completes where format 8 failed; 64 us
reads and a slow counter still fail. The 100 ms command, one-second retirement
and 20-second source bounds and their origins are unchanged. These injected
costs do not predict physical Nano latency or establish consumer completion.

The [AArch64 compilation](portable-arm64.json) checks six portable units,
including both setup profiles, with the pinned compiler and strict firmware
flags. The largest individual frame is 384 bytes; this is not a linked BL31,
whole EL3 stack proof, matched boot image or hardware acceptance.

The prior [format-8 physical trial and restoration](../2026-11-13-de25-command-guard/README.md)
remain separate: full source transfer passes, command retirement fails after
publication and recovery programming reports a broken JTAG chain. A later
physical reset permits exact original-FIT rollback and an independently verified
51-command baseline cycle/final Park. Format 9 has not touched the board. A new
complete matched chain and frozen one-use procedure are required before testing
it physically; no ordinary activation or source-memory reuse is enabled.

The [affected checks](checks.json) pass the full selected software campaign:
2,651 parent tests (15 reported skips), the native runtime/firmware and producer
integration checks, and 2,181 FPGA software tests (88 reported skips). Generated
consistency verifies all 69 consumers and 45 fixture copies; documentation links
also pass. These remain software and model checks, not a physical retirement
qualification.
