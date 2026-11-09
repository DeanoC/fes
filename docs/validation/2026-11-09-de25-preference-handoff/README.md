# Native Tenfoot preference handoffs (software only)

The native launcher reads optional `FES/PREFS.JSON` before menu construction and
serves settings from private RAM. Its current Nano build keeps changes volatile
and explicitly selects read-only SD access. Physical tests are deferred until
the operator returns; this work performs no kit access.

Explicit persistent mode binds once to the service's exact store before frontend
ownership and requires the paired menu lifecycle. Both Ready-menu and Running
states keep SD contained. Launch and return checkpoint pending changes in their
existing active-SD windows, before retaining the next package. Final menu exit
seals further edits, writes a pending record and quiesces without a programming
handoff or another FPGA configuration. A changed edit during a write remains
pending. Known capacity/read-only refusals remain visible; uncertain IO or lost
ownership retires the runtime. Failed writes are not automatically replayed.

Tenfoot renders pending or failed save status from cached state, without storage
access from the UI. The filesystem uses a fixed, bounded JSON path and existing
preallocated-file writes/flushes. Its boundary is clean-restart persistence;
file allocation, general native package installation and power-cut atomicity
are outside this change.

Focused race tests cover real App settings, lease exclusion, repeated
launch/return checkpoints, final shutdown, concurrent edits, stale/foreign
binding and safe/uncertain failures. An independent mtools read and a FAT remount
verify the saved file. The first broad focused run exposed a test using the
forbidden cold-resume path after shutdown; the corrected restart test creates a
new boot lease. The original failed log remains in the local evidence.

All nine selected checks complete successfully across split runs, including
actual C/RTL native launch/return failures, SD/storage race checks, generated
consistency, host Go tests and Chrome/CDP UI integration. The aggregate runner
reached its 900-second cap after eight checks while host checks remained; only
the unfinished host check was rerun separately under its own unchanged
900-second cap, passing in about 138 seconds. The original timeout and focused
failure logs are retained. No test timeout or coverage gate was relaxed.

Local evidence: `out/nano-preference-handoff-20261109/validation.json` and
`out/native-fabric-hooks-2ff9dfdf20174084963fa31a019faf28/result.json`.
Native ARM64 cross-build verification follows from this committed source.
