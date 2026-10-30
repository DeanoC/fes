# DE25 ZX81 BASIC load, run and reload, 2026-10-30

Issue #1274 concerns a test fixture: the previous proof uploaded a raw live NEW
RAM snapshot. Its CDFLAG at $403B was $C0, retaining the active SLOW display bit.
The ROM's actual SAVE path enters FAST mode and emits $40. The live snapshot
was therefore unsuitable for proving post-LOAD editor return.

Owner: misteross. The task started at
`0e81447dc19f8f1167ce67d110b5c14e054f2d77`. Normal merge
`773568c215f3663f299520c2219f1d0a1462c408` incorporates parent
`54c9b6ce6716dcde188c08cae43de5f6943e53b7`, including its separate completed-HID
and frame-boundary video-gating fixes. This PR changes only simulation clients,
observation helpers, proof validation and documentation relative to that parent.

## Cause and controlled comparison

The [previous failing control](../2026-10-29-de25-zx81-pr-computer/postload-baseline-control.json)
used the unchanged internal-16-KiB machine model and actual native client. A
160,000,000-system-cycle editor-return observation failed after loading NEW.
It already showed that the failure did not establish a PR RAM-bus regression.

The new [CDFLAG control](cdflag-control.json) reuses the same frozen RTL model
archive. It clears only bit 7 of the copied upload's CDFLAG and adjusts the
observer's expected byte to match. Both client and server now exit successfully
after the same post-LOAD editor check. No RTL or guest RAM is patched. This
one-byte experiment is diagnostic evidence; the final round-trip proof uses
unmodified bytes from actual ROM SAVE, rather than the patched control image.

The [annotated original ROM disassembly](https://k1.spdns.de/Vintage/Sinclair/80/Sinclair%20ZX81/ROMs/zx81%20version%202%20'improved'%20rom%20disassembly%20(Logan,%20O'Hara).html)
provides the SLOW/FAST and newline-handler entry points. The test follows actual
opcode fetch at $060C before releasing ENTER, then observes command completion.
A keyboard-scan acknowledgement alone was too early. Token observation also
stops at the edit line's newline, excluding stale buffer bytes.

## Required functional sequence

Both actual C++ client architectures drive actual endpoint, CPU, ROM, keyboard,
tape-loader and renderer RTL. The internal-16-KiB model and combined
empty/16-KiB-persona model each perform this sequence:

1. Type `1 PRINT 7` through complete HID snapshots and observe the ROM store it.
2. Type `SAVE "A"` and retain the real SAVE emitter's bytes, including CDFLAG $40.
3. Upload those bytes through the actual CRC/chunked client with PIX stopped;
   require CPU progress and then restart PIX.
4. Type `LOAD ""`, observe every loader RAM write and editor return; type RUN,
   require output 7 in the display file and return to the editor.
5. Type `2 REM` after RUN and observe that stored edit.
6. Upload and LOAD the same SAVE image again without reset, HOLD or persona
   selection. Require line 2 removed, then RUN output 7 and editor return again.

A listing begins with line number 1 and cannot satisfy the output-7 predicate.
The proof requires two loads, two runs, 24 neutral editor returns and removal
of the intervening edit. The retained native/AArch64 images and all semantic
results must match within each model. AArch64 is static ELF64 executed under
QEMU Cortex-A76, using the recorded pinned SDK; no physical AArch64 board ran.

The combined session additionally retains the independent mailbox/PR execution
gates, empty/RAM/empty selections, an interrupted third LOAD with subsequent
freeze, and actual final 1-KiB BASIC boot. The fresh-RAM workaround is removed.
Existing identity, uncertainty, reset and tape-bounds cases remain enabled.
The internal session's exact write count includes every HID snapshot and both
uploads. Missing load/run/editor/output/reload observations fail schema checks.

The private SAVE-RAM observer only supplies a fixture. It adds no production
SAVE-export API, protocol capability, shared schema or runtime implementation.
No guest instructions or system variables are injected by the final proof.
The original 160M editor and 500M loader phase bounds are unchanged. Program
storage is checked after the same bounded editor-return event, replacing an
arbitrary new 40M storage poll that stopped during expanded-display clearing.
Storage and editor return must now both finish within one 160M phase, instead
of separate 40M storage and 160M return waits.
A diagnostic trace observes storage at cycle 388,179,944, about 55M cycles after
ENTER acceptance. The internal host-only session watchdog increases from 900
to 1800 seconds for the expanded SAVE/two-LOAD/two-RUN/edit workload, matching
the combined suite. It cannot extend a failing guest phase. Other scenario
watchdogs and physical timing constraints are unchanged.

## Evidence and limits

[Internal RAM: 20 cases](internal-proof.json) and
[PR RAM pack: 14 cases](combined-proof.json) pass on committed producer
`a05bd205c6929a72d5c070d353c8ea37203fd665`. Both architectures match every
semantic result. All four retained SAVE files are identical 923-byte images:
SHA-256 `b4e6e04e3063149a0e7232f23e4aa24b96d0631d557920e97c65d1e01dfb3c71`.
Each internal session performs exactly 1,475 client register writes and observes
637 complete output frames. Each combined session makes three persona selections,
freezes the third LOAD after 9 of 923 bytes, observes 720 output frames and
returns to actual 1-KiB BASIC.

The [source audit](source-match.json) matches every selected production, test
and runtime byte to the receipts. It confirms no production-source changes
relative to the integrated parent. The [manifest](manifest.json) lists hashes
for receipts, per-architecture logs and retained SAVE images. Rejected fixture candidates
and the controlled CDFLAG comparison are retained separately from passing runs.
The [line-completion control](line-completion-control.json) records the actual
store/editor events. Its private early-return client deliberately omits the
full picture sequence and later fails that normal client gate; it is no full
session pass.

`make test-changed` against parent `54c9b6ce6716dcde188c08cae43de5f6943e53b7`
passes all selected commands, including 971 parent Python cases (one skip),
1,511 FPGA cases (35 skips), whitespace and generated-consumer consistency.
That local aggregate began at `773568c215` while the fixture was still being
refined, so it is not a frozen final-head claim. The four focused schema cases
and complete native/AArch64 matrices select committed producer `a05bd205c6`.
`make check` reports 37 consumers/37 fixtures; documentation links pass.

No FPGA production source, timing constraint, toolchain pin or shared contract
changes relative to the merged parent. This task does not run Quartus. The
older C7 compilation remains evidence for its own captured inputs only; the
parent's later video changes prevent attributing that artifact to this tree.
The [input comparison](compiler-input-comparison.json) records the three changed
parent inputs and 55 unchanged inputs out of the previous 58-source closure.
The selected build ID identifies the simulation endpoint, not a newly compiled
SOF or RBF.

All evidence here is off-board client/RTL simulation with behavioral memory.
The combined persona selector models selection, not SDM programming. Physical
HPS bridge access, target timing, PR loading, reset/CDC/IO signoff and exact
artifact acceptance remain pending an identified DE25 board. No kit, image,
boot media, flash or harness was touched. The next physical step is serial/
Ethernet/SSH boot followed by runtime-owned loading and this program round-trip.
