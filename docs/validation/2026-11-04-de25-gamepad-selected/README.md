# Matched gamepad diagnostic candidate

Native/entry/firmware source: `19a59c4f0a9877669e9cfcd0336d23c84c2a3211`.
FPGA producer remains the earlier sealed ZX81 project; no full FPGA recompile
or hardware operation was performed for this candidate.

`tamago/run.py usbcheck --usb-input gamepad` selects descriptor-driven HID input.
The SDK records and links the exact input protocol. Entry rejects absent,
duplicate, malformed or contradictory assignments and requires the driver path
in the real ELF. The cold identity carries the protocol through BL31 and U-Boot.
The unchanged probe profile keeps USB closed at entry and grants no static
device readiness; SD, FPGA data access and ordinary runtime admission remain held.

After acquiring and confirming the USB lease, the bounded native program records
the exact report descriptor, raw reports and normalized axes/buttons/hat. It
then requires the owned stop receipt. Unsupported schemas retain the bounded
descriptor for diagnosis without reporting Ready. Gamepad mode does not send
keyboard boot requests, fabricate keyboard reports or guess button mappings.

The observer requires the protocol supplied independently from the checked
candidate. Gamepad Ready requires one matching-length/hash descriptor; keyboard
and gamepad records cannot substitute for one another. Stop owner/generation and
terminal Pass remain mandatory. Idle enumeration is distinct from reported state
changes. Normalized states are the selected native parser's reported observations,
not an independent off-board re-decoding or physical control mapping proof.

Software checks in `out/nano-gamepad-parser-20261104/` all pass:

- 22 USB selection/firmware/trace tests, including both protocols, malformed and
  fragmented records, descriptor retention/hash/order and terminal failures.
- 24 HPS source/I/O tests, including cold input selection.
- 24 entry tests and 9 cold firmware tests.
- Focused Go race tests for DWC2, USB ownership, input and HID parsing.
- Native diagnostic cross-build and `make check`.

Matched build in `out/nano-gamepad-selected-20261104/` passes native SDK,
entry, BL31, U-Boot, local PFG composition and source/selection audit. The audit
compares every selected source against the committed version, checks actual ELF
paths, equal USB/gamepad selection through all stages, equal firmware/bootloader
cold profiles and the composed FIT hash/size.

Chain: `586ca73ab290ad7afa0b4f053d11ad20143977e23211c301e448e74d4abfaa0d`.
FIT: 2,695,056 bytes, below the unchanged 8 MiB upload bound.

No new kit, JTAG, UART, SD-write, physical input or core-load result is supplied.
The next step is to adapt and replay the bounded operator procedure with this
explicit protocol, then run one test on claimed TRWU8CBL. The factory card remains
outside the board and the continuing claim/spare remain held. Combined device
ownership, physical input routing, ordinary package/ROM/media admission and
normal DE10-style core-load acceptance are still incomplete.
