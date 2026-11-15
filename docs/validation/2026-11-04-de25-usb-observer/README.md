# Selected native USB observation gate

This is host-only validation of the USB-only operator trace decoder, based on
`e09d81590`. It changes no native image, firmware, FPGA producer, shared wire
contract or admission policy. The existing matched USB candidate remains the
`aaa45acfc` build recorded in the USB hub validation. No USB hardware execution
or new board programming is claimed here.

`platform/embedded/nano/usb_trace.py` requires successful secure USB setup before
the normal owned provider, the complete source-bound HPS context and 328 pin
samples, all entry/BSP startup checkpoints through `go_vectors`, followed by
ordered Ready, optional raw boot-keyboard reports, Stop and Pass. Stop must
match the cold owner and generation 3. The selected program emits no Go-main,
SD or FAT startup checkpoints, so the decoder does not require those phases.
The generic boot decoder rejects USB setup unless its separate USB mode and
HPS context/pin expectations are explicitly selected.

Nine synthetic tests cover idle success, key/modifier press and release,
missing/duplicate/reordered records, setup failure, wrong owner/generation,
malformed/rollover reports, truncated serial records, invalid CPU startup,
missing HPS proof, the bare loader prompt and foreign/late evidence. The operator startup gate calls
the existing CPU decoder itself. Idle success is distinct from valid key-press
evidence; neither implies combined USB/storage/core acceptance.

The source/context/pin and generic native-boot regression run passes 70 tests;
`make check` passes. The wider affected-test run passes 31 CLI, 39 policy and 1,440 parent tests.
That run started before the final prompt/capture-format repair; the final focused
70-test run covers those final bytes. Both receipts are recorded in `result.json`. An initial test import failure was corrected and its log retained;
no hardware attempt occurred.

After the user reported the motherboard USB recovery, a root read-only process
inventory verified the existing TRWU8CBL claim, exact top-board USB identity and
free serial endpoint. It opened no device, programmed no FPGA and wrote no SD
file. Both DE25 adapters were visible. The other kit was untouched. A separate audit verifies that the existing
matched USB candidate still has its exact selected native inputs and chain hash. This is a
host connectivity observation, not a JTAG chain or native USB result.

Logs and read-only transport evidence are retained under
`out/nano-usb-observer-20261104/`. The next physical step remains a single bounded
USB-only run after preparing its boot-upload and native observation procedure.
The original factory card remains outside the board; the spare and exclusive
claim remain in place under the user's authorization. No factory restoration
was requested or performed.
