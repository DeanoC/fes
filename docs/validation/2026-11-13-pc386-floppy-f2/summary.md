# pc386 removable media F2: the floppy in the machine, 13 November 2026

This record covers slice F2 of the
[removable-media design](../../design/pc386-removable-media.md) (tracker
[#1694](https://github.com/DeanoC/fes/issues/1694)), with the drive-select fix
[#1796](https://github.com/DeanoC/fes/issues/1796).

**What is built:**

- **#1796, DOR's drive select.** `pc386_fdc` keeps the two drive numbers
  apart. The cable is the drive DOR selects: the medium and its geometry,
  ready and spin-up, write protect, TRK0, the disk-change line and the step
  that clears it, and the image a sector is read from or stored to; ST3
  reports pin states, so Sense Drive Status answers for that drive. The
  controller's own registers stay under the drive the command names: the PCN
  (Sense Interrupt Status's cylinder and Dumpreg's PCN bytes), the head,
  sector and EOT it works through, and ST0's drive select bits.
- **CMOS from configuration.** `pc386_rtc` writes 10h (the drive types, A: in
  the high nibble), 14h's floppy fields (bit 0 a drive exists, bits 7–6 the
  count less one: 45h for two drives), 2Dh bit 5, and 38h and 3Dh from the
  launch's boot device, all from new `pc386_machine` inputs.
- **The BIOS change line.** INT 13h AH=15h reports a change line (AH=02h) for
  a drive whose CMOS type is 2–5; AH=16h reads DIR bit 7 of the drive, with
  its motor on, and on a change clears the drive's recalibration bit and its
  "media type established" bit so the next access senses the medium;
  `floppy_drive_recal` steps the head in one cylinder before the Recalibrate
  when the line is active.
- **`machine_tb` floppy units.** `--floppy`, `--floppy-type`, `--floppy-out`,
  `--floppy-read-only`, `--swap` (at a time or at a debug-port marker, with a
  write-protected insert) and `--boot-device`. A disk's format is its exact
  size, and a drive that cannot read that format is an error. No drive is
  fitted unless a run names one, so every other suite still simulates the
  machine the seal builds.
- **The `floppy` and `floppy-boot` suites**, with FLOPTEST.COM
  (`cores/fes-pc386/sim/floppy_test.asm`) and FreeDOS floppy fixtures built by
  the repository's own tooling (`build_pc386_freedos_image.py --profile
  floppy-boot|floppy-a|floppy-a2|floppy-b|floppy-b360`).

**Out, as the slice says.** The storage endpoint is still not connected:
`pc386_system` ties the drive types to none and the boot device to the hard
disk until slice F3 reads them from the `fes.pc386.machine` record. So the
sealed core fits no floppy drive, and what a kit sees that is new is the ROM
and the boot-order bytes: CMOS 3Dh is 12h and 38h 30h, a hard-disk launch
that now names the floppy and the CD behind the disk instead of stopping at
"No bootable device", where before they were 02h and 00h.

## What settled the behaviour

- **The drive select bits in a command byte** "are used internally only. They
  are not reflected in the Drive Select pins. It is the users responsibility
  to maintain correspondence between these bits and the Drive Select pins
  (DOR)" — the note under the command set table, 82077SL datasheet
  290410-001 page 26, which is the 82077AA's superset (8.0). The DS0–3 pins
  "are decoded from the select bits in the Digital Output Register" (pin
  description and 2.1.3), and ST3's bits report the WP, TRK0, HDSEL and
  DS1,0 pins (7.4). Bochs and QEMU run such a command on the drive it names;
  that is the `dor-mismatch` divergence, which replaces the shared deviation
  of the same name that slice F1 recorded, with the `dor_mismatch` replay
  case.
- **The change line** is reset by a step pulse with a disk in the drive, and
  RECALIBRATE issues step pulses only "as long as the TRKO pin is low"
  (290410-001, RECALIBRATE), so a head already at cylinder 0 would leave the
  line set for ever. The IBM PC/AT BIOS's own diskette listing (5170 BIOS
  source, `DSKETTE.ASM` of 1985-06-10, read in the pcjs mirror of the IBM
  listings) settles each answer: `DISK_TYPE`
  returns CHGLN for an 80-track drive and NOCHGLN for a 40-track one;
  `DISK_CHANGE` calls `READ_DSKCHNG`, which turns the motor on and reads
  3F7h, and reports a timeout when there is no drive; and `MED_CHANGE`
  resets the change signal with "THIS SEQUENCE OF SEEKS", a seek to cylinder
  1 and back to 0, after clearing the drive's media state.
- **One difference from the AT BIOS,** stated in
  `cores/fes-pc386/firmware/README.md`: the AT BIOS also calls `MED_CHANGE`
  at the start of every read, write and format and fails that operation once
  with 06h, where this BIOS leaves the change to AH=16h and lets the next
  access read the new disk. That is what the design asks for ("DOS, through
  the patched BIOS, then rereads the FAT and the boot sector").

## Suites

(filled in from the runs)

## Seal

(filled in from the build summaries)

## Not covered

- **DISKCOMP**, which the design's `floppy` row sketches, is not in the
  FreeDOS packages the lock pins, so the write check compares the image the
  bench writes back instead.
- **`verify-a` and `verify-b` prove the BIOS path, not the controller.** This
  BIOS returns success for INT 13h AH=04h before any transfer, as upstream
  does.
- **The kit still fits no floppy drive**, so the kit check is a boot check of
  the new ROM and CMOS bytes, not of the drives.
- **AH=16h's no-drive branch** (a timeout, as `DISK_CHANGE` answers) is
  reached by no suite: both drives are fitted in the `floppy` suite, and DOS
  does not query a drive the CMOS does not have.
- **An eject-only `--swap`**, which takes no file, is not exercised by a
  suite; the replay's `fence_and_swap` case covers an eject at the
  controller.
