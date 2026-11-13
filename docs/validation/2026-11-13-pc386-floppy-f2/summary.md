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

Every suite ran on this branch; the ROM column is the `pc386-bios.bin` digest
the run used, since the BIOS changed once during the slice.

| Suite | Result |
| --- | --- |
| `floppy` (new) | PASS at `f79bc8f56`: 24 checks, all ok, and the pattern FLOPTEST.COM wrote to B: is in sector 2399 of the image the bench wrote back ([log](floppy-suite.log)) |
| `floppy-boot` (new) | PASS at `f79bc8f56`: FreeDOS boots from the 1.44M floppy in A: with CMOS 2Dh, 38h and 3Dh set to an A: launch, reaches the `A:\>` prompt in 7.25 simulated s and DIR shows the boot floppy's label FDBOOT |
| `fdc` | PASS: 36 cases, 19 matching Bochs, 14 listed divergences (the new `dor_mismatch` among them), 3 RTL-only |
| `devices` | PASS, with three new CMOS cases: no drive, two drives with an A: launch, one 360K drive with a CD launch |
| `boot` (oracle) | PASS, with the VGA replay clean (84,949 events, no mismatch) |
| `a20` | PASS on the boot and games images |
| `system` | PASS at the earlier ROM `b5beb4cd`: FreeDOS to the prompt on `pc386_system`, the record round trip, holds cutting port 2 bursts in 4 of 186 trials, 2,783 status records |
| `tests.test_pc386_machine`, `tests.test_pc386_rtl_stops`, `tests.test_build_fes_pc386_quartus` | PASS, 124 tests |

**The RTL CPU.** `floppy --cpu rtl` and `floppy-boot --cpu rtl` run the same
scripts with `fes_i386_cpu`, so the INT 13h transfers, DMA channel 2 and
IRQ 6 are its. (Results below once the runs finish.)

**The kit** (Kit B, run `f1`, 13 November 2026). The sealed package
`0445da315c50211f` launched as a library game with the repository's FreeDOS
fixture image: the new ROM posts ("Bochs 3.1 BIOS - build: 11/03/26"), the
machine boots from the hard disk under the new boot-order bytes, and DIR at
the `C:\>` prompt reports 33,292,288 bytes free ([grab](kitB-f1-dir.png)).
The kit fits no floppy drive, so this checks the ROM and the CMOS bytes, not
the drives. The baseline was unchanged throughout (software `7ddb0bfb` on
image `179affbb`, the same snapshot keys before and after, agent idle and
lease free), and the run's four FogCast paths were removed afterwards.

## Seal

Quartus 17.0.2, seed 1, the CUDA slot on `chip-30x0-ai-dev`, from a clean
checkout of `92ef982d6`, with the lockstep `--netlist` report (#1782) in
front of it. Every analysis passes.

| Build | ALMs | RAM blocks | DSP | Registers | Setup | Hold |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| main `8ab34380e` ([summary](main-build-summary.json)) | 36,333 | 274 | 40 | 32,915 | +0.822 | +0.092 |
| F2 seal `92ef982d6` ([summary](seal-build-summary.json)) | 36,399 | 274 | 40 | 32,869 | +0.694 | +0.068 |
| An earlier F2 head `18ccf4884` ([summary](earlier-seal-build-summary.json)) | 36,471 | 274 | 40 | 32,965 | +0.330 | +0.076 |
| The doc's F2 estimate | +150 | 0 | 0 | | | |

- **+66 ALMs, no M10K and no DSP** against main sealed the same way, at 86.9%
  of the device. The earlier head measured +138; the two differ only in the
  ROM contents and the fit, and the area audit puts fit noise at about ±200
  ALMs, so the figure is "at or under the estimate", not a measurement of the
  logic.
- **It is a floor.** `pc386_system` still ties the drive types to none and the
  boot device to the hard disk, so Quartus prunes the CMOS arithmetic and the
  drive-select paths that only a fitted drive reaches. F3 pays the rest when
  its endpoint drives those inputs, as slice F1's record says of the
  controller itself.
- **The sealed commit is not the branch head.** Everything after `92ef982d6`
  is `floppy_test.asm`, `sim_fes_pc386.py`, this record and the core README;
  all 119 of the seal's recorded input digests match the head's files.
- Timing at the seal: setup +0.694 ns, hold +0.068 ns, minimum pulse width
  +1.122 ns, recovery +7.092 ns, removal +0.428 ns, on the 61.44 MHz system
  clock with the 74.25 MHz pixel and 12.29 MHz audio clocks.

## Reproduce

```bash
python3 scripts/sim_fes_pc386.py --suite floppy --download
python3 scripts/sim_fes_pc386.py --suite floppy-boot --download
python3 scripts/sim_fes_pc386.py --suite floppy --cpu rtl
python3 scripts/test_fes_i386_lockstep.py --download --netlist --gpu-router CUDA
python3 scripts/build_fes_pc386_quartus.py --root "$PWD" --seed 1 --gpu-router CUDA \
    --lockstep-report "$PWD/build/sim/fes-i386-lockstep/report.json"
```

Run them from `sources/misteross`; the seal needs the pc386 lock's CUDA
toolchain slot.

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
