# Native controller delivery on TRWU8CBL

One bounded physical test passed: the controller D-pad Left press and release
reached the owned running ZX81 computer mailbox. ZX81 → return, ZX80 → return,
and ZX81 → Quit then completed native generations 1 through 6. This is a
contained diagnostic result; full DE10-equivalent core loading remains open.

The application and matched firmware/boot chain were built from `90e4dbbf5`.
The operator and independent descriptor-specific decoder ran from `f68134edb`;
the three-file difference contains host observation code and its tests.
No native or firmware source changed between that build and the test.

The captured 98-byte descriptor was checked against its fixed SHA-256. A separate
Python decoder interpreted the physical eight-byte reports `007f0080800f0000`
and `7f7f0080800f0000` as Left and neutral. Completed owned writes changed HID
row 5 to bit 0, then returned all nine rows to zero. Both receipts carry the same
boot owner, native generation 1, selected package/build, descriptor and copied
mapping; their sequences are 1 and 2. No UART key report was injected. The input
window took 17.816 seconds under its separate 60-second bound.

Recovery, seven fixed Ethernet uploads and fresh SD readbacks, native startup,
and all three complete FPGA configurations and master stops passed. Each launch
also observed an unchanged held fetch counter/address and forward Run activity
within 245 ms under the unchanged one-second control limit. The shared USB lease
remained generation 2 through transitions and completed its final Stop at 3.
There were 213 delivered reports in this run; idle counts alone prove no control.

Nine focused observer tests, 16 existing serial/shared-input trace tests, selected
parent software checks, the earlier exact-input native C/RTL integration, operator
fixtures and the matched cross-builds passed. The raw hardware trace was replayed
independently. Cleanup independently verified unchanged 15M, exact USB permissions,
private JTAG stopped and UART free. The claim and spare card remain with this
thread; the original factory card and the other kit were untouched. No hardware
retry or factory restore occurred.

Two preparation issues remain in the retained logs: the uploader fixture correctly
refused the initially dirty checkout, and a preflight executable-path comparison
needed normalization. A read-only replay initially compared nested receiver timing
with the enclosing observer timing; replay now uses the enclosing recorded duration
and separately requires the inner duration to fit it. These corrections sent no
additional board commands and widened no hardware deadline.

Matched images, exact software/hardware traces, failed preparation logs, procedure,
source bundle and verified archive are retained at
`/home/deano/fes-retained/de25-gamepad-delivery-20261105-r1/`.
The archive was read back member by member; source restoration with its recorded
prerequisite bundles and strict Git fsck passed. The producer source dependency is
the unchanged retained Tenfoot FPGA candidate named in `result.json`.

This proves physical input delivery through the USB/source/session path and
completed mailbox key writes. It does not prove video, emulated software reaction,
retired instructions or CPU halt after return. Ordinary ROM/package/media admission,
native Ethernet and full native display integration remain incomplete. The next
base-system change needs a real retained-ROM delivery contract and transport;
removing the current package rejection alone would not supply one.
