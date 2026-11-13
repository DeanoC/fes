# pc386 removable media F2: the floppy in the machine, 13 November 2026

This record covers slice F2 of the
[removable-media design](../../design/pc386-removable-media.md) (tracker
[#1694](https://github.com/DeanoC/fes/issues/1694)), with the drive-select fix
[#1796](https://github.com/DeanoC/fes/issues/1796).

**What is built:**

- **CMOS from configuration.** `pc386_rtc` writes 10h (the drive types, A: in
  the high nibble), 14h's floppy fields (bit 0 a drive exists, bits 7–6 the
  count less one: 45h for two drives), 2Dh bit 5, and 38h and 3Dh from the
  launch's boot device, all from new `pc386_machine` inputs. Until slice F3
  reads them from the `fes.pc386.machine` record, `pc386_system` ties them to
  no drives and a hard-disk launch, and only `machine_tb` sets them.
- **The BIOS change line.** INT 13h AH=15h reports a change line (AH=02h) for
  a drive whose CMOS type is 2–5; AH=16h reads DIR bit 7 of the drive and, on
  a change, clears the drive's recalibration bit and its "media type
  established" bit so the next access senses the medium again;
  `floppy_drive_recal` steps the head in one cylinder before the Recalibrate
  when the line is active, since a drive clears that line only on a step
  pulse and Recalibrate issues none at cylinder 0.
- **`machine_tb` floppy units.** `--floppy`, `--floppy-type`, `--floppy-out`,
  `--floppy-read-only`, `--swap` (at a time or at a debug-port marker, with a
  write-protected insert) and `--boot-device`. A disk's format is its exact
  size, and a drive that cannot read that format is an error.
- **The `floppy` and `floppy-boot` suites**, with FLOPTEST.COM
  (`cores/fes-pc386/sim/floppy_test.asm`) and the FreeDOS floppy fixtures the
  repository's own tooling builds.
- **#1796.** A command that names a drive the DOR does not select now acts on
  the DOR's drive.

## Suites

(filled in from the runs)

## Seal

(filled in from the build summaries)
