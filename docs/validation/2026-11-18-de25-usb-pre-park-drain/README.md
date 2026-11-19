# USB drain before final MENU Park

The shutdown ordering repair passes off-board tests. **The fresh TRWU8CBL
trial stops earlier, during the first core's return, so physical USB shutdown
and final native Park remain unqualified.** Exact original-FIT restoration
and independent cleanup/readback pass without power cycling. The failed
procedure is consumed and was not retried.

## Change and ownership boundary

The previous [25-second trial](../2026-11-18-de25-source-budget25/README.md)
reached final Quit after both core returns, then timed out on the first poll of
a pending USB packet. Its application parked MENU before calling USB Close.
The synchronous firmware/menu work could therefore age a packet past its
unchanged 500 ms deadline before the closing drain first serviced it.

`ProgramBackend.ParkMenu` now holds the backend operation lock, validates its
Ready/input state, and drains the retained input source before invoking the
actual MENU Park callback. It leaves polling paused through the existing USB
Close and firmware Stop receipt. Drain failure prevents MENU closure; an
attempted shutdown cannot resume polling or admit a later launch. The native
application uses this backend callback for final paired shutdown.

The [policy audit](policy-proof.json) verifies unchanged production DWC2,
USB-control and secure firmware sources against delivery `b1da19d2`. No packet,
drain, command, phase, 25-second source-operation or host limit changes. No
ownership, freshness, integrity or stop check is removed. The shared paired
observer requires the additional same-owner Ready pause before final MENU
closure, unchanged report/poll counters after that pause, and the existing
inactive Stop and Parked exit. Non-paired operation retains its own lifecycle.

## Source and software evidence

Implementation, compiled native artifacts and host decoders use
`64684abda2f4cbf9c81365514f8bf13ed1b987e3`; host runner remains
`7119ffa96b153149499f67e797a7ae18fe2413db`. Documentation is delivered in a
later docs-only commit. There is no artifact relabelling.

- The actual DWC2/owned-source regression reproduces the old first-poll timeout
  after a long simulated MENU closure. Pre-draining succeeds and retains the
  required inactive Stop receipt. The old ordering still fails closed.
- Native/USB-control/DWC2 Go race suites pass in 174.895 s. Actual Tenfoot App
  launch/return/final shutdown callbacks and drain, MENU failure, cancellation,
  concurrency, overflow and replay tests pass. Existing driver fault and
  firmware ownership rejection suites remain active.
- All 47 focused shared/paired/installer observer tests pass, including missing,
  late, resumed, foreign-owner and inconsistent-counter final-pause rejection.
- All eight affected commands pass on this source: 2,904 parent tests
  (21 skips), command inventory/consumer checks and actual native firmware/
  FPGA-launch software integration. The outer runner completes normally in
  806.849 s. Broader FPGA synthesis, normal HIL and whole-appliance coverage
  are outside this check.

[Exact software results](software-results.json),
[software summary](software-summary.json) and [build stages](build-stages.json).
Complete SDK/entry/BL31/U-Boot/full-SOF matching and fresh source-bound native
admission pass. Boot `476b5370ab8565bd`; FIT 8,575,696 bytes,
SHA-256 `d9dbb6240670c374bebf9b1f55b1efe1aed806f3f161fe99fa7f369f0540955a`; full SOF
`34ca3aca3a08ab4d3ac15529b8465825b5931e1ad250cc60ae1f2cd32d2752a7`. Both FPGA core payloads are unchanged.
See the [matched chain](matched-chain.json).

Fresh recovery packaging initially encounters Powerboat's shared Pro lock;
the unsuccessful/abandoned build preparations perform no kit operation.
Only our idle waiter is stopped. Both nonce-bound recovery images are then
packaged on the preferred 3090 builder under `/tmp/lander-quartus-3090.lock`.
The remote Pro 26.3 wrapper and binary hashes equal Powerboat's; the original
local package verifier independently checks the returned SOF's exact vendor
payload and embedded nonce-bound SPL. The [transport/tool/lock receipts](recovery-packaging.json)
and its private source are retained. No other build or shared lock is disturbed.

## Fresh physical stop

One-use procedure `b16f8ba8005d2442ddd10d75b98ef8e6d555690a4a5306d9bec7ff1d44c86d9f` uses fresh forward/rollback
nonces, complete native admission, a stable 60-second USB window and retained
source/artifacts. The candidate write and preservation checks pass.

Initial MENU and ZX81 configuration, proof, retirement, continuation, the
917-byte tape and running fetch pass. Source releases 4→10 and 10→15 pass at
899,192 and 886,279 us. The next MENU generation15 passes its full DATA/input
proof at 20,138,337 us, retirement at 1,589,309 us and continuation at
1,863,611 us. These are separate observations and must not be added to infer
an unobserved native clock duration. No firmware source-clock failure is printed.

**Native return then fails with `configuration already completed`.** No
MENU control/data opening follows generation15 continuation; ZX80, final
USB drain/Stop and final native Park are not reached. This trial does not
confirm or refute the shutdown repair. Its status remains
`diagnostic-failed-original-restored`.

The message originates in `ConfigurationTransfer.Cancel` when the transfer is
already Done. The enclosing native configuration loop calls it after a failed
clock/context check. Off-board overlays of the unchanged production source
reproduce the same retained error shape for both expiry just after completion
and a backwards clock, without opening computer control. The original clock
cause is masked by the cancellation error, so **the physical trace does not
establish which check failed or its measured duration**. A cancelled-context
exploratory case instead returns `context canceled`; it is retained separately
and excluded from the two-case reproduction pass. No production timing/error
policy is changed based on that hypothesis.

[Native boundary](native-boundary.json), [terminal failure](native-terminal.json)
and [trial result](trial-result.json). Next work must retain the original
post-completion rejection reason and profile that native validation boundary
before another full-cycle trial. No deadline or ownership check may be waived
to reach final shutdown.

## Restoration and retention

The fresh independent original restoration verifies all 17 before/after file
readbacks in each role, 16 preserved files, 824+824 command receipts, 19 NS reset
words per recovery, distinct reset/session/capture identities and exactly two
FAT writes (candidate then exact original). Candidate/native/original roles
last 307.675/194.294/315.298 s; total 818.361 s, inside unchanged limits.
All three programmer/server traces and cleanup pass. Exact USB permissions and
JTAG 15 MHz/auto1 are restored; all owned UART/JTAG/process handles are closed.
See the [independent physical audit](physical-audit.json).

Original FIT `d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
is restored and read back; current verified state is the recovery prompt,
parked, with spare test card installed and continuing claim held. No old
native baseline is booted. No physical power cycle, QSPI/raw-SD/factory/bottom/
J8G operation occurs. Reset/old-DMA guarantees, physical display/input and
ordinary Kit D25 HIL remain open. This is an experimental CBL serial diagnostic.

Prepared retention verifies 84,625 entries and exact Git archive blobs for both
source heads. Complete boot retention verifies 46,669 entries. The final physical
archive `0990edd156b9430969afade54bb1c0a38eff868bc812101a78c4772f738b2e33` verifies 296 entries.
[Preparation](prepared-retention.json), [boot retention](boot-retention.json),
[physical retention](physical-retention.json). Raw binaries, ROMs, wire and
trace files remain outside Git at `/home/deano/fes-retained/de25-usb-shutdown-trial-20261118-r1` and `/home/deano/fes-retained/de25-usb-shutdown-source-20261118-r1`.

PR #1952 remains draft/`lane:deano`; [#1867](https://github.com/DeanoC/fes/issues/1867)
stays open. No exact-head review, normal HIL waiver or Lander handoff is implied.
