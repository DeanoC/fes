# DE25 validation after the main mailbox update

The normal merge of main `16afcb7cc` changes the shared computer mailbox, so
prior source-bound proofs were refreshed rather than relabelled. Parent producer
`348baa734` passes all 14 native/AArch64 combined RTL cases in 1,463.2 seconds;
platform producer `d4c6544e3` passes all 14 in 1,502.8 seconds. Each branch retains
its own actual report as its fixture, and each passes all 16 recipe checks.
The parent publication is `60f0e891d`; the platform merge is `638abab2b`.

The rebuilt platform diagnostic passes Quartus Pro 26.3 in 533.8 seconds.
All 19 report hashes were checked and all 46 summary timing rows are
nonnegative. The [readiness audit](readiness-paths.tsv) retains 16 setup/hold
paths across four corners, with minimum setup 9.566 ns and hold 0.109 ns.
Only first-stage asynchronous ingress is excluded. Five other signoff rules
remain open; #1284 and whole-board timing qualification remain open.

On bottom board TRWU8J8G, the actual shared NativeHardware path loads the
[new described package](package.json) twice, gates CPU start on FPGA video
readiness and delivers live tape and HID LOAD/RUN. Both runs visibly print 7.
The [first picture](first-seven.png), [second picture](second-seven.png),
[blank state](blank.png) and [intermediate state](between-loads.png) come from
110 frames captured through the shared HDMI alias; other viewers stayed open.
The 7 glyph region matches between both runs. Some tool previews rendered the
white background incorrectly; retained PNG bytes and independent ffmpeg gray
decoding establish the white background and changed scenes.

The runtime binary remains producer `16838e82c`, with its source bytes unchanged
through this platform merge. The [run log](d4-two-load-basic.log) records both
cycles and final healthy HOLD. The [kernel log](d4-log.log) retains four vendor
OF overlay-property lifetime warnings. [Known-success cleanup](d4-success-cleanup.log)
removes the owned overlay, firmware copy, configfs mount and temporary test
files under the exclusive lease. Boot, kernel notes, root receipt and owner
permissions remain unchanged. No reboot or power cycle was used.

This is a diagnostic harness, not normal factory, MENU or catalog acceptance.
No DMA grant/allocation, physical PR, failed-transfer injection, SD/kernel/QSPI
write or userspace bundle installation was performed. The candidate claim is
retained; no baseline promotion, restoration/release or FPGA SRAM erasure is
asserted. Full artifacts are retained at
`/home/deano/fes/out/artifacts/de25-main-sync/d4c6544e3`.

[Results](results.json) and [board plan](board-plan.json) bind the producer,
package, build identity, payload and harness hashes. Normal DE10-equivalent
MENU, launch/stop/relaunch, process restart, physical memory/DMA/recovery and
timing acceptance still remain required.
