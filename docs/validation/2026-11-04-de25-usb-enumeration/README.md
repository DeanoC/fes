# Native USB device enumeration after reset and RX fixes

TRWU8CBL now completes device/configuration enumeration and identifies USB
`081f:e401` at address 1, low speed. The selected boot-keyboard diagnostic still
fails because this configuration has no boot-keyboard interface. HID report
interpretation, live input delivery, confirmed USB readiness and a printed
checked-stop receipt remain unproven. Ordinary core-loading parity is incomplete.

Two source repairs led to this result:

* `ecfec51a0` releases USB0's module and packet-RAM/ECC resets together (PER0
  bits 3 and 11). The [Agilex 5 reset sequence](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/taking-the-usb-2.0-otg-controller-out-of-reset?contentId=9Cx0WYhpfh7XsISMD5WQCQ)
  requires both before transactions; the pinned Terasic reset header independently
  names USB0_RESET=3 and USB0_OCP_RESET=11. The old provider released only bit 3.
  Its new scope rejects either partial release, partial stop or reset drift;
  other HPS masters/bridges stay held and DMA/IRQs remain disabled.
* `76f44112d` interprets retained BCNT/PID fields on completion/halted receive
  status entries as metadata. Only status 2 supplies FIFO payload. The primary
  [Linux v6.12 DWC2 receive handler](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd_intr.c)
  reads payload for IN data and ignores data fields for these completion entries.
  Zero/unknown statuses still fail. A real IN-data entry, valid count/PID,
  HCINT completion and retired channel remain required to expose a report.

The paired-reset image executed once on the board at unchanged 15 MHz. It
reached a completion entry `0x70080` with a retained count of 8; HCCHAR was
`00128008` (disabled) and HCINT `21` (completion set). Its old receive check
rejected that metadata. That failed image and its exact original observer were
retained and independently replayed. A preceding 6 MHz procedure stopped at the
live chain check before programming or any SD write. A separate forced read-only
check passed at the original 15 MHz rate; the subsequent procedures kept that
rate unchanged and retained every device-ID and boot gate.

The final image was built from `76f44112d`. Its matched SDK, entry, BL31, loader
and composition passed source/seal checks. Chain SHA256:
`c55ef660cfb2b61c638482c509f8df144741ca2506c039fbf76343c6b5112e42`.
The unchanged FPGA producer was `7808f74c2`; no full FPGA compile was needed.
One qualified recovery boot, one network boot-file upload/readback plus fresh
reread, one native programming and one `fesboot` were performed. The matched
boot/USB setup, nine CPU checkpoints, source context and all 328 HPS pin samples
passed. The final native error was:

```
dwc2: USB configuration has no boot keyboard; discovered addr=1 id=081f:e401 class=00 speed=2
```

This ID also matches the SNES-style gamepad family described by
[Adafruit](https://learn.adafruit.com/pico-8-fruit-jam-reality-console/install-wili8jam-on-fruit-jam).
That is a device-family inference, not a captured HID report descriptor or a
verified button layout. The next implementation step is a descriptor-driven
HID gamepad path with explicit input type and normalized menu/core events;
gamepad reports must not be passed to the boot-keyboard decoder.

The operator now drains a bounded receive-only failure tail while retaining its
active failure gate. Its actual pump was tested with the prior fragmented error;
no command is sent during that drain. The terminal success predicate is the
program's Pass following its owner/generation-3 Stop. A prior host assumption
about runtime halt text was incorrect: the pinned BSP's CPU.Init installs the
silent ARM64 exit hook before runtime.exit's generic print. CPU halt is not
claimed. The generic halt-message acceptance was removed; unknown trailing
output still rejects, and USB Stop remains mandatory.

The first U-Boot build hit a GCC preprocessing ICE in unchanged firmware-uclass.c.
The log, source and generated headers were saved. The exact single-file compile
then passed with unchanged inputs; a fresh full build in `native-uboot-final-r2`
passed without changing source, compiler or time bounds. Only that successful
build was selected. An initial read-only helper also hit a log-name collision
before its forced query; that host stop and its corrected run are retained.
Neither event was a native retry or ignored validation failure.

Validation passed: compiled USB platform/board/selection tests including six
partial-reset/drift cases, native firmware/FPGA integration, controlled USB race
tests, four final driver/input race packages, 71 observer/context/pin tests,
15 actual upload-runner cases, 54 receipt mutations, actual operator/fragmented
tail checks, and affected tests (31 CLI, 39 policy and 1,442 parent tests).
New receive tests cover retained count/PID fields, no additional FIFO read,
channel retirement and completion metadata failing to substitute for real input.

The final run stopped at its first unsupported-input failure. Both real native
images ran once each, with no retries. Independent replay verifies each exact
observer, the snapshots/device identity, upload/readback references and cleanup.
JTAG15M, original USB permissions, private JTAG shutdown and free UART were
independently verified. The claim/spare remain held because a USB Stop receipt
was not observed. The original factory card is safe outside the board; the user
waived factory restoration. The other kit and shared daemon were untouched.

Evidence and procedures: `out/nano-usb-paired-reset-20261104/`,
`out/nano-usb-paired-trial-20261104/`,
`out/nano-usb-paired-15m-trial-20261104/`,
`out/nano-usb-jtag-check-20261104{,-r2}/`,
`out/nano-usb-completion-20261104/`, and
`out/nano-usb-completion-trial-20261104/`.
