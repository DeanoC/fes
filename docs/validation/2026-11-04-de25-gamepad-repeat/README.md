# Repeated vendor usages: regression fix and early transport failure

Native/firmware and observer source `9cb14393c0463f84e2bd8bd18f786f5e36396b0e`.
Chain `31e4c721789bbf3179557a247cabe827f1c20ad1040577cace211ab2bc5ae42f`.
One actual attempt ran on exclusive TRWU8CBL; it stopped at the first failure
and was not retried. The other kit and original factory card were untouched.

The preceding image captured a 98-byte descriptor and rejected a vendor Input
item whose ten variable fields share one Usage. Regression tests reproduce that
failure before the repair. The generic parser now repeats the last Usage across
remaining variable fields while retaining every field's bit offset. Vendor
controls remain unexposed, duplicate known controls still fail, and missing or
excess usages remain unsupported. There is no VID-specific decoder or mapping.

Tests using the captured descriptor and constructed reports verify eight-byte
input length, independent Output length, X/Y scaling, button bits 44–53, ignored
padding/vendor bits, unchanged state after invalid reports and offsets after
repeated vendor fields. The real DWC2 driver/model and owned menu source also
pass enumeration, delivery and closure with this schema. Focused Go race tests
and `make check` pass. These are software results, not physical button presses.

The fresh matched SDK/entry/BL31/U-Boot/local-PFG image and committed-source
selection audit pass. Operator fixtures cover the actual captured descriptor,
synthetic normalized reports and protocol/startup/terminal failures. Uploader,
receipt and passive failure-tail fixtures pass. The separate host report
replayer accepts known constructed vectors and rejects short reports/wrong
normalized states; there are no physical reports to replay from this trial.

Physical qualified recovery, one experimental boot-file upload/readback and a
fresh SD reread pass. Native boot, all nine CPU checkpoints, source context and
328 HPS pin reads pass. USB enumeration then stops before a complete HID report
descriptor is retained:

```
unsupported RX status 5
irq=0x7000039 peek=0xa0000 pop=0xa0000 after=0x7000029
channel=0x80528008 interrupts=0x400 transfer=0x40080008
port=0x4140f reset=0x80000400
containment: channel=0xc0528008 did not clear enable within 500ms
firmware status 4
```

RX status 5 and HCINT bit 10 identify a data-toggle error; see the pinned
[Linux v6.12 DWC2 register definitions](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hw.h)
and [data-toggle handler](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd_intr.c#L1712).
The trace does not identify which control request/packet failed. It supplies no
physical validation or contradiction of the parser repair, which was not
reached. Accepting this error or blindly retrying the image is not justified.

The independent replay checks exact failure bytes, source/CPU proof, absent
descriptor/Ready/Stop/reports, upload protections and host cleanup. Root read-only
cleanup independently confirms unchanged 15 MHz, exact USB permissions, no owned
private JTAG process and a free UART. The claim/spare remain held; USB containment
and CPU halt are unproven, and no factory restore was performed per standing
instruction. No further native command followed the failure.

Evidence: `out/nano-gamepad-repeat-20261104/` and
`out/nano-gamepad-repeat-trial-20261104/`. The earlier exact descriptor failure is
already retained at `/home/deano/fes-retained/de25-gamepad-hid-20261104-r1/` with
232 regular files and a verified 35,711,409-byte archive plus restorable source.

Next is control-request/packet context and modeled toggle-error/halt handling
before another image is justified. Physical gamepad input, combined device use,
normal package/ROM/media admission and DE10-style core-load acceptance remain
incomplete.
