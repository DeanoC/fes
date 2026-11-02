# Native Z80 fetch observation with owned recovery uploads

The bounded TRWU8CBL trial passes: one qualified recovery session uploads and
independently reads back both matched files, freshly rereads the firmware,
boots the Linux-free native program, configures the sealed FPGA package and
observes actual Z80 instruction-fetch bus activity across Run/Hold/relaunch.
All three held pairs remain identical; both Runs advance. Complete master stop
and owned reclosure also pass. The [independent hardware audit](hardware-evidence.json)
and [cleanup postcheck](hardware-postcheck.json) verify the saved raw output
and final board state.

This is an internal-ROM ZX81/Z80 computer diagnostic. It counts fetch bus cycles,
not retired instructions, emulation fidelity or video. It does not grant ordinary
runtime package admission, warm core reconfiguration, Tenfoot launch/return,
USB input or video acceptance. The full DE10-equivalent core-load goal remains
active; `runtime_admission` and `core_load_acceptance` stay false.

## Source, artifacts and bounded execution

The [experiment](experiment-result.json) runs from
2026-11-02 17:39:54 to 17:43:28 UTC on the exclusive top TRWU8CBL. The actual
host implementation/check source is `fa27960da8e52b6ef754bfc830b20544d1058826`.
It fixes upload-session memory ownership after the
[two preserved upload stops](../2026-11-02-de25-native-fetch-trial/README.md).
An actual code fix and its current checks enable this new sequence; a
documentation-only commit does not reset the earlier two-failure limit.

The FPGA producer remains `c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`, native
producer remains `726a16f4e2d979e68fb67e85e97c916e3e94ee4f`, and captured board
source remains `79d616e85e19d65f0d645b67a85ffe7e142dcdfc`. No FPGA or firmware
rebuild occurs in this trial. The
[matched candidate](../2026-11-02-de25-native-fetch-observer/README.md) retains
its original producer, source selection, sealed model, loaded-BL31/startup proof
and timing coverage limits. Host-only changes are not attributed to that build.

| Item | Observed selection |
| --- | --- |
| Package | `b2353f540cc81b9896db86867248880aceffce8c81314042b02317f76f947f37` |
| FPGA build | `dda236646bb868e63c72bd0e777f3dd6` |
| Native boot | `829865a45111af8d` |
| Configured core | 1,646,592 bytes; SHA256 `e9255d3dc149e7db17f012c3032f8c1da245d913ded3947a2342034eb1927ac0` |
| SD package | 1,649,664 bytes; SHA256 `75a2cdfb38f9e61f41ebe2554ca30de6e8e367246d294d3d8f00c514dbf47b73` |
| Firmware FIT | 6,606,660 bytes; SHA256 `412ded5471e741cbb43d2a7ccc86a0dfde2ba158364e0ba15bf7b5951faff264` |
| Raw native UART | 10,130 bytes; SHA256 `765231e37510f3f0519a37d63123cfbae23999c6fd674567cfefa1e725bcc98c` |

The [preflight](preflight.json) verifies exact selected inputs, current host
checks, unchanged compiled-native/FPGA evidence, prior stopped-state audits,
current claim, USB identity/permissions and free UART before opening a device.
The original prompt, native storage, observer and actual configuration limits
remain 50, 100, 45 and 20 seconds. The
[native result](native-result.json) observes package retention in 58.701575
seconds within the 100-second limit. Configuration passes the existing
20-second gate; deadlines, clocks and timing/seal requirements are unchanged.

## Upload ownership repair and current software evidence

`UploadSession` binds one exclusive console and a volatile boot marker. Its
initial scratch window must contain no reservations; it never adopts an old
session or a `none` reservation merely because it is visible. Successful bounded
FAT/TFTP loads extend an in-memory allocation ledger. Fresh complete DRAM,
relocation and LMB observations must equal the original map plus those owned
loads before every upload, before its SD write, and before success. Smaller
loads preserve the previous maximum; adjacent owned ranges coalesce. Changed
maps or markers reject. Failure or close retires access without replay.

The public caller and tests use this single session API. `session.verify`
freshly reads/hashes only a fixed file already verified with the current
identity in that session, and accounts for that load in the same ledger.
No memory reservation is freed or silently ignored, and no environment is
saved. This addresses the actual LMB side effect that stopped the earlier
single-session attempt.

The [current source plan](software-plan.json) and [result](software-result.json)
pass all six selected groups. [Parent coverage](parent-coverage.json) contains
all 1,303 selected IDs in three complete-module partitions capped at
300 seconds, with no omitted selected test or widened failed scope. The
30 focused uploader checks include the exact captured before/after map,
repeated writes and rereads, maximum-sized/shorter payloads, unknown and
protected reservations, changed boot marker, target reset, mutable receipt
isolation, concurrent calls, close and failure retirement. The first development
checks exposed numeric-padding and test expectation/receipt-shape mistakes;
their failed logs remain retained separately. Corrected checks pass on the
selected implementation. Generated consumers remain 39 with 37 managed fixture
copies; no shared FPGA contract changes in this host fix.

The [combined runner checks](upload-both-fixtures.json) exercise 18 success and
failure cases using the actual public session over a stateful LMB/FAT model.
They reject malformed inputs, recovery failure, either transfer's failure,
lost claim/marker, foreign allocation, corrupt reread and cleanup failure.
The [native controller checks](controller-fixtures.json) cover 79 cases,
including missing/mismatched receipts, session identity and memory phases,
unclosed uploads, incomplete native proof and cleanup failure. Only the
complete positive case is accepted. The [helper checks](helper-verification.json)
also pass. None of these off-board cases is substituted for the physical result.

The unchanged native candidate separately retains its original 11-scope checks
on its native producer. The [host input selection](selected-inputs.json) and
preflight preserve those source boundaries instead of claiming a new target build.

## Physical upload and fetch observations

The [qualified recovery](recovery-result.json) passes with exact top-cable
identity at 6 MHz. The same open console/session performs the
[FIT upload](boot-upload.json) and [package upload](package-upload.json),
each with a complete transfer, RAM hash, one fixed file write, independent SD
readback/byte comparison and protected prefix/root recovery FIT/manifest checks.
Their live memory observations match the owned ledger. The
[fresh firmware read](fresh-fit.json) follows the package write and binds both
upload receipts, the same session, and the exact carried protection receipt.
The [complete upload result](uploads-result.json) requires both the session
and console to close. The [raw upload output](uploads-console.log) is retained.
The separate earlier PHY autonegotiation timeout's cause remains unproved.

Native boot passes CPU/reset/SPL checks, actual loaded-BL31 verification, paired
EL3 capture, secure ownership and EL1 entry. It retains the expected package
from SD, configures the sealed core, checks complete FPGA/GP identities and
uses the owned native aperture at `0x20010300`. The
[raw native output](native-console.log) reports:

| Phase | Fetch count | Last sampled address |
| --- | ---: | --- |
| Initial Hold | 0 | `0000` |
| Initial held check | 0 | `0000` |
| Run | 38,494 | `03cf` |
| Return Hold | 260,112 | `03d8` |
| Return held check | 260,112 | `03d8` |
| Relaunch | 298,606 | `03cf` |
| Final Hold | 327,001 | `03d2` |
| Final held check | 327,001 | `03d2` |

The count is not reset on Hold/return. Both Runs advance, while each held
count/address pair stays identical. The native command cycle also passes
neutral keyboard delivery, final held acknowledgement and complete reader/probe
master stop: status 0, reset `d`, generation 5. Verified owned reclosure ends
at owner `23140eb3fc88c68f`, generation 6, `held_ack=1`, `data_held=1`.
`execution_observed=1` means the explicitly decoded fetch-bus observation only;
the final marker still says `runtime_admission=unavailable`.

## Cleanup, coordination and retained state

The independent audit rederives the selected maps and decodes the saved UART
again, matching the native receipt exactly and requiring all eight activity
samples and complete stop/reclosure. It also rechecks both upload files,
fresh-read provenance and closed upload group. The
[bounded audit receipt](hardware-audit-result.json) and postcheck pass.
Original 15 MHz is read back before only the owned private JTAG service stops.
USB ownership/mode are restored exactly, the UART is free and the continuing
claim is owned. The exclusive top retains its spare and completed diagnostic
under the user's retention policy; the factory card stays safe. There is no
factory restore, bottom-kit operation, native SD write or active FPGA DDR request.

The user also requires the existing shared 3090 Quartus lock for every future
compile. The [coordination record](quartus-coordination.json) locates
`/tmp/lander-quartus-3090.lock` on `fes-build-3090` read-only. Future jobs must
hold that existing exclusive lock for their full run; it must never be replaced
with a private lock or unlinked. No Quartus job is started in this work.

Raw trials, current checks, helpers and source are retained under
`out/nano-upload-session-20261102`. [SHA256SUMS](SHA256SUMS) covers the copied
records. The earlier failed sequences and complete matched candidate remain
separately retained with their original source identities. The next integration
step is ordinary native runtime admission and repeated core load/return;
this cold one-shot diagnostic does not qualify those paths.
