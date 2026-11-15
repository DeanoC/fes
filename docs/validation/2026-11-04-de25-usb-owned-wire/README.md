# Nano firmware-owned USB wire and native report source

Base `ecc90090c`; USB wire `c0cf35bac`; input-closure/test repair
`faa14c06b`. This is off-board evidence. No board was programmed or reset.

The USB service reserves a private prepared/confirmed lease before exposing
reports. It blocks SD and FPGA controls during that lease, validates callback
post-state, retains ownership after uncertainty, and supports one checked stop.
The native client rejects stale sources, lost replies and repeated failed-driver
access. The actual DWC2 driver is exercised with a modeled USB device; separate
C RPC tests exercise the actual dispatcher, board and cold policy.

The first parent run executed 1,511 tests with three fixture failures. The
generated-file expectation and two C link lists were corrected. All 241 Nano
tests and 11 consistency tests then passed, including those failures and a new
installer-to-chain source-inventory check. The entire parent suite was not
rerun. Seven remaining affected checks passed, including 1,577 FPGA tests
(75 skipped), native integration, producer identity, SD lifecycle and schemas.
`make check`, focused Go race tests, five freestanding AArch64 objects, Nano
SDK, native entry, linked BL31, SPL/loader and final composition passed.

The matched profile is identical across BL31 and SPL; all producer input hashes
match the current tree. The composed FIT is 7,958,520 bytes, below the existing
8 MiB upload bound. Receipts, logs and build outputs are retained under
`out/nano-usb-wire-20261104/`; the JSON record pins their hashes.

USB remains disabled in the selected boot profile. A secure physical binding,
hub/controller support and integration with storage/core ownership remain open.
The new chain needs frozen catalogue/operator inputs before the bounded full
Tenfoot App launch/return procedure. No physical USB, full App launch, ordinary
admission, HDMI or native Ethernet acceptance is established here.
