# Shared keyboard polling and matched initial-tape candidate

Compiled native source `d250310a1de67d1a975c585d4e37da9e13f11da4` counts successful
raw USB polling calls separately from received reports. The explicit keyboard
path can remain idle without a report before every transition. This matches
[HID 1.11 section 7.2.4 SET_IDLE(0)](https://www.usb.org/sites/default/files/hid1_11.pdf).
The observer still requires fresh successful polls, the same owner/lease,
pause-before-mutation and resume-after-opening, unchanged poll/report counts
during pause and exact Stop. Reports stay monotonic and cannot exceed polls.
Gamepad report-progress requirements remain. The counters grant no device or
core authority, and zero reports do not prove physical input.

Forty-two focused observer tests pass, including three full idle keyboard
launch/return cycles, four menu sessions, missing/stale/overflow/paused-counter
failures and retirement without retry. A complete 1,724-test parent run passes
with three existing skips. The initial full run found two historical captures
without the new counter; it is retained. The original captured UART/contract
bytes stay unchanged. Current acceptance rejects them; new positive ordering
and mutation cases use clearly synthetic current-format traces. A test-only
repair adds no native/producer source changes; compiled and validated commits
are distinct in result.json. Six affected checks are satisfied, reusing the
unchanged selector/consistency passes and rerunning committed whitespace and
the complete parent suite after that repair.

The fresh matched SDK/entry/BL31/U-Boot/FIT/SPL/SOF chain has boot identity
`76cd915c046abe45`, shared boot-keyboard input, paired menu/computer programs and
secure initial tape. All 2,260 source/artifact joins pass, including embedded
SPL and unchanged FPGA core bytes. Independent actual firmware ELF/bin checks
match the receipts and source-bound load segments; OCRAM has 7,680 bytes free
within the unchanged 0x60000 limit. Composition held the shared Quartus lock.
The earlier uninstrumented build was deliberately stopped for this native
source change and is preserved; it was not programmed.

Thirteen immutable upload files include the retained 917-byte `10 REM A` tape.
Actual package/ROM/tape preflight, raw collector/receipt joins with zero keyboard
reports, upload/readback fault fixtures, sixteen master-lifecycle cases and six
stale/forged prefix rejections pass. Full default native-file admission checks
5,525 files in eight source/project trees, including standalone firmware ELF/bin
and validators. Fresh worker-prefix hashing takes about one second and repeats
no HPS derivation. It opens no device and grants no live authority.

This milestone does not program a board or write a card. The separate forced
JTAG scan passed with complete cleanup, but the preceding keyboard programming
failure remains recorded and its image consumed. The new image has no board
attempt. The physical operator/live preflight still needs final binding to the
new poll protocol and prior transport-failure accounting; a second transport
programming failure must stop hardware work pending a fix. Then run the matched
keyboard/tape/core launch-return cycle. Physical input/video, native networking,
ordinary package admission and the full DE10-style appliance goal remain open.
