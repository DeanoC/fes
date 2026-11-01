# DE25 MENU lifecycle software validation — 2026-11-01

This record qualifies software lifecycle changes only. It does not enable the
DE25 production factory, implement its MENU presenter, install a kernel or UI
on a board, or extend the earlier physical DMA display acceptance.

`NativeHardware` now consumes `MenuDisplay` and `MenuMemory` interfaces;
the existing DE10 mailbox/window adapters implement them. Failed revocation
is retained independently by the memory adapter and lifecycle, so closing a
descriptor or attempting another idle/replacement load cannot erase the error.
The native fault test verifies that neither idle nor contained replacement
programming reaches the FPGA after that failure.

The DE25 owner's additive `OWNER_IDLE` query reports whether the ready profile
still has an active allocation or storage lease, without allocating, reaping,
reading MMIO or relaxing overlay exclusion. Actual kernel file/VMA tests verify
that REAP and closing every descriptor still leave a revoked mapping busy;
only the final unmap releases its lease. This is an observation under existing
maintenance serialization, not a reservation, configuration identity or DMA
drain proof. An older installed kernel returns `ENOTTY`, which is not idle.

The UI now drains its outstanding frame before releasing its grant mapping on
Pause or PauseForLaunch. Cancellation leaves an in-flight mapping intact;
release errors refuse launch; a superseded pause cannot release a resumed
mapping. Resume after a rejected launch can map the same generation again
without clearing its pixels. This uses the existing runtime socket and client,
without moving programming authority into the UI.

| Check | Actual producer | Result |
| --- | --- | --- |
| Native routine suite and archive audit | `739669eb46d67ebb6c8c62c0835598c36fbc7c3c` | PASS, 287.6 seconds; [job](native-tests.json) |
| Standard dynamic AArch64 runtime/archive and agent recipe | `739669eb46d67ebb6c8c62c0835598c36fbc7c3c` | PASS, 226.0 seconds; [receipt](aarch64-runtime.json) |
| Native and QEMU AArch64 clients against combined ZX81/PR RTL | `739669eb46d67ebb6c8c62c0835598c36fbc7c3c` | PASS, all 14 cases in 1,661.5 seconds; [actual report](combined-proof.json) |
| AArch64 QEMU kernel ownership suite | `9de85374d28a73b499673006e0aefacc15403d68` | PASS, all 43 cases in 155.7 seconds; [receipt](kernel-checks.json), [results](kernel-results.json), [raw kernel log](kernel-raw.log) |
| Full gfx, menu client and tenfoot Go packages | `da40738f75ec1fafdcac074a356b18aed76df32f` | PASS, 88.0 seconds; [job](ui-tests.json) |
| New pause/release tests, 20 repetitions with race detection | `9964f2aa9f26dee6c99a518e86135126ccc72e3b` | PASS, 4.8 seconds; [job](ui-race.json) |
| Static AArch64 UI build and five actual menu tests under QEMU Cortex-A53 | `9964f2aa9f26dee6c99a518e86135126ccc72e3b` | PASS; [receipt](ui-aarch64.json), [pause tests](ui-aarch64-gfx.log), [mapping test](ui-aarch64-mapping.log) |

The combined test includes BASIC load/run, ROM SAVE, reload removing an edit,
and interrupted delivery. Memory and persona selection remain modeled; no
physical PR or new board run is claimed. The actual report refreshes the
branch-local fixture. All 16 strict recipe/source gates pass after ordinary
merges, and [subtree comparisons](merged-source-check.json) verify the tested
runtime, kernel driver and UI bytes are unchanged. The UI's later commit only
makes blocked test workers unblock during failed-assertion cleanup.

Failed controls remain separate: the first kernel run rejects the newer user
SDK because this kernel recipe locks an older SDK archive; the first 43-case
guest run passes but its host verifier still expects 42 named cases. The
checked rerun adds the specific required case and preserves missing, duplicate,
skipped, failed and kernel-warning rejection. The UI build's initial wrapper
mistakenly expects launcher `-h` to return zero. Its existing parser returns 2
with `flag: help requested` before startup; the corrected receipt records that
negative control. It does not claim launcher startup or a rendered screen.

Normal DE25 MENU still needs its allocated-memory/presentation adapter,
observed hardware build identity, described package/profile and production
construction. Managed catalog launch/stop/relaunch, safe daemon restart,
physical tape SAVE, broader security/reset/failure bounds and timing #1284
remain open. The broader affected shared-video/core campaign is separate and
has no aggregate pass yet. The bottom board and its installed candidate card
are unchanged by this software work.
