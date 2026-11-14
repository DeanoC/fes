# DE25 initial retirement passes; next warm preparation stops

One fresh diagnostic on TRWU8CBL used unchanged firmware
`8394acfa52b1cbd898010e33d8e70aa380dd632d`, boot `c5556b2812fa937c`,
FIT `3a70eb273cbdd3d2f01882f03e8b12c64ef2851d5ba56b4a4d7136e21d259a0c`
and the [private-server tracer](../2026-11-14-de25-jtag-server-trace/README.md).
Procedure `e2bae5dd7ffd03fbfe5c67c9c6a5198e318a25f03c4678e461bde6a5086c9319`
is consumed once. It stops after 481.287 seconds without a host timeout.
QSPI, factory card and bottom kit are untouched.

Recovery boot, temporary FIT write/readback, all seventeen protected-file
before/after checks, native phase-1 programming, sealed native boot and initial
MENU Ready pass. Independent saved-wire/program joins and reconstruction of the
initial client/source sequence confirm generation 4's exact 1,708,032-byte source,
two posts/two ACKs and SHA-384. Transfer is 11.614778 seconds. Policy-3 retirement
passes in 1.074802 seconds, and continuation is verified at 1.288791 seconds from
the retirement origin. This exercises the new initial retirement/continuation
path physically; it does not qualify general or repeated loading.

The first source release (4 → 10) reports success at 925.185 ms. The next
1,654,784-byte source has only zero-completed/zero-pending arm/start observations.
Its warm preparation then stops at stage 2, error `-110`, under the unchanged
one-second preparation origin: last accepted 131,666,137 us from start
130,668,276 us; last operation kind 4, offset `0x3400`, 1,792 stores, 5,120 loads,
48 reads and zero register writes. The nested command queue has not been
attempted/submitted. No further DATA/engine observation exists after the release.
The final failed INPUT_RESULT retains the preceding generation-4 engine's full
byte counters; these are not a second transfer. No cold recovery request marker
is observed. Full ZX81/ZX80 launch/return and final Park do not pass.

Both recovery and native JTAG programming succeed with complete owned tracer
attachment/detachment receipts. The traces bind the same live server epochs,
pinned executable, exact top USB node and raw transcript digests. They capture
USB ioctl calls, but this strace build prints URB pointers rather than completed
URB contents. Syscall returns do not qualify USB transfer health, identify the
older intermittent JTAG failure, or establish production timing. Instrumented
programming remains diagnostic evidence.

Independent cleanup verifies all twelve recorded processes stopped, the UART
unheld, automatic adjustment/15 MHz restored, and exact USB inode/permissions
restored. The actual physical archive is
`/home/deano/fes-retained/de25-server-traced-warm-prepare-stop-physical-20261114-r1/physical-trial.tar.gz`,
SHA-256 `f367d5b6e0aedaabef0a43ccaa1fd81f13f001741d5963229fe83b80df034f29`.
All 177 entries are independently read back for bytes, hashes and modes.
[physical-result.json](physical-result.json) contains the source-bound observations;
[retention-proof.json](retention-proof.json) records retention. Initial retention
checker failures (UTF-8 UI text and an overbroad arm/start assertion) are preserved;
only the host checker was corrected, with no hardware retry.

**The candidate FIT remains on the spare card; the original FIT is not restored.**
All our hardware handles are closed. A fresh top-only human power cycle is
requested before restoration-only original-FIT/baseline verification. The failed
diagnostic will not be retried. Continuing top claim `7a124fae52bf7062` remains held.

Next investigate warm table-preparation cost while keeping the independent
preparation deadline and all ownership/table/readback checks. Current code
regenerates each expected table word through a complete binding validation;
the existing `nano_sdm_input_words` API can generate up to 32 words per validation.
This is an optimization candidate, not a measured attribution or implemented fix.
No new firmware or readiness acceptance is implied.
