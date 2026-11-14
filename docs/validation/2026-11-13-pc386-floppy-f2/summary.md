# pc386 removable media F2: the floppy in the machine, 13 November 2026

This record covers slice F2 of the
[removable-media design](../../design/pc386-removable-media.md) (tracker
[#1694](https://github.com/DeanoC/fes/issues/1694)), with the drive-select fix
[#1796](https://github.com/DeanoC/fes/issues/1796).

**What is built:**

- **#1796, DOR's drive select.** `pc386_fdc` keeps the two drive numbers
  apart. The cable is the drive DOR selects: the medium and its geometry,
  ready and spin-up, write protect, the head its step pulses move, TRK0, the
  disk-change line and the step that clears it, and the image a sector is
  read from or stored to; ST3 reports pin states, so Sense Drive Status
  answers for that drive. The controller's own registers stay under the drive
  the command names: the PCN its seeks work from (Sense Interrupt Status's
  cylinder and Dumpreg's PCN bytes), the head, sector and EOT it works
  through, the C its result phase returns, and ST0's drive select bits.
- **Three cylinder numbers, kept apart** (the shape the review rounds
  settled, below). The track a drive's head stands on, which only step
  pulses move; each unit's PCN, which Seek, Recalibrate and a transfer's
  implied seek write; and the C of the command in hand, which the result
  phase returns and an EOT rollover advances without moving anything. A read
  or write performs the implied seek, stepping the selected drive by the
  named unit's distance to C, and those pulses clear that drive's change
  line. Under a DOR mismatch the head and its PCN can come apart, and a
  transfer that finds them apart ends with ST0 IC=01 and ST1 ND rather than
  reading another cylinder's sector.
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

## What the review rounds settled

Two review rounds ran against this branch after its first seal, and the
model they arrived at is not the one either round proposed. The record keeps
the route, because the second round's expected test is one this machine
cannot satisfy.

- **Round 1** asked for a physical head position: under a DOR mismatch the
  command unit's PCN was being used as the image cylinder, so a mismatched
  command could read a cylinder the selected drive's head was not on. The
  fix taken at `ee64414e6` added that head and then required a read or
  write's C field to equal it.
- **That broke the machine.** The locked BIOS sends no Seek before a
  transfer: `floppy_prepare_controller` turns the motor on and waits for
  RQM, and the AH=02h/03h paths go straight to the E6h/C5h command byte. So
  every transfer above cylinder 0 ended ND. On `ee64414e6` the `floppy`
  suite stops at `write-a` with a BIOS panic (port 401h), 14 of 24 checks
  in, two of the four swaps unmade and nothing of the pattern in the
  written-back image ([log](r1-head-floppy-failure.log)). `--suite fdc`
  passed on that head because the replay seeks of its own accord before
  every read; the suites that drive the real BIOS were not run.
- **Round 2** found, correctly, that an EOT rollover was advancing that head
  with no step pulse behind it, and asked that a read of the next cylinder
  without a seek then fail as a wrong-cylinder read. It cannot: with implied
  seek on, which it must be for this BIOS, an 82077AA seeks there and reads
  it.
- **What the datasheet gives instead** is three numbers rather than two, and
  that is what is built. Table 5-6 is headed "ID Information at Result
  Phase" and gives C+1 for a final sector equal to EOT, so the C that
  advances past EOT is a result byte, not a position (290166-007, 5.1.1).
  The PCN is "the current head position" a SEEK compares against NCN and is
  written by SEEK and RECALIBRATE (5.2.2, 5.2.3). And a transfer moves the
  head only through the implied seek: "if implied seek is not enabled, the
  read and write commands should be preceded by: 1) SEEK command" (5.2.3),
  with CONFIGURE's EIS "a seek operation will be performed before executing
  any read or write command that requires the C parameter in the command
  phase". Implied seek is therefore always on here, which is also what the
  design records for 40-track media: the cylinder the guest asks for is the
  cylinder read.
- **Both findings are met by that split.** No step pulse is issued at a
  sector boundary, so neither the head, the PCN nor the change line moves
  with the result C (round 2); and under a DOR mismatch the steps a command
  asks for are the named unit's while the head they move is the selected
  drive's, so a transfer that finds them apart ends ND instead of reading
  another cylinder (round 1).
- **Bochs keeps one cylinder per drive**, advances it past EOT and calls
  `reset_changeline()` there, and that one register is also its PCN and its
  ST3 TRK0 source. So the PCN, TRK0 and the change line after an EOT
  rollover all differ from it: the `eot-step-pulse` divergence names the new
  `eot_no_step_pulse` case, with `implied-seek` recording the EIS question
  beside it.

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

Every suite ran on this branch, on the ROM the branch ends with
(`pc386-bios.bin` `23b9df91`).

| Suite | Result |
| --- | --- |
| `floppy` (new) | PASS: 24 checks, all ok, all four swaps made, and the pattern FLOPTEST.COM wrote to B: is in sector 2399 of the image the bench wrote back ([log](floppy-suite.log)) |
| `floppy-boot` (new) | PASS: FreeDOS boots from the 1.44M floppy in A: with CMOS 2Dh, 38h and 3Dh set to an A: launch, reaches the `A:\>` prompt 5.90 simulated s in, and DIR shows the boot floppy's label FDBOOT |
| `fdc` | PASS: 37 cases, 20 matching Bochs, 14 listed divergences (the new `dor_mismatch` and `eot_no_step_pulse` among them), 3 RTL-only ([log](fdc-suite.log)) |
| `devices` | PASS, with three new CMOS cases: no drive, two drives with an A: launch, one 360K drive with a CD launch |
| `boot` (oracle) | PASS, with the VGA replay clean (84,949 events, no mismatch) |
| `a20` | PASS on the boot and games images |
| `system` | PASS: FreeDOS to the prompt on `pc386_system`, the record round trip, holds cutting port 2 bursts, and the status ring |
| `tests.test_pc386_machine`, `tests.test_pc386_rtl_stops`, `tests.test_build_fes_pc386_quartus` | PASS, 124 tests |

**The RTL CPU**, which is the slice's lockstep check: the same scripts with
`fes_i386_cpu` in the oracle's place, so the DMA-terminated INT 13h reads,
DMA channel 2 and IRQ 6 are its.

- **`floppy --cpu rtl`: PASS** ([log](floppy-rtl-suite.log)). The oracle boots
  and the RTL takes over at the prompt, as the `ems` suite does. All 24
  checks pass on it, the pattern reaches B:'s image, and the CPU retired
  235,870,314 instructions with **no oracle step, no stop and no
  protected-mode stretch**; the run ended at the prompt at 6.0 simulated s.
  So the RTL CPU drove four disk swaps, a format and a write-back through the
  controller — and every implied seek under them — without the oracle taking
  an instruction.
- **`floppy-boot --cpu rtl`: PASS** ([log](floppy-boot-rtl-suite.log)). Here
  the RTL CPU runs from reset, so POST, the BIOS's floppy boot and DOS are
  all its: it retired 396,740,311 instructions with **no oracle step, no stop
  and no protected-mode stretch**, reached the `A:\>` prompt and ended there
  at 6.5 simulated s, with DIR showing the floppy's label.

**The kit** (Kit B, run `f1`, 13 November 2026). The sealed package
`0445da315c50211f` launched as a library game with the repository's FreeDOS
fixture image: the new ROM posts ("Bochs 3.1 BIOS - build: 11/03/26"), the
machine boots from the hard disk under the new boot-order bytes, and DIR at
the `C:\>` prompt reports 33,292,288 bytes free ([grab](kitB-f1-dir.png)).
The kit fits no floppy drive, so this checks the ROM and the CMOS bytes, not
the drives. The baseline was unchanged throughout (software `7ddb0bfb` on
image `179affbb`, the same snapshot keys before and after, agent idle and
lease free), and the run's four FogCast paths were removed afterwards.

**The kit again, after the controller changed** (Kit A, run `a1`, 14 November
2026). The round-3 seal `91a5f76453272d59` launched the same way on Kit A's
baseline (software `a2756499` on image `179affbb`), core-only: no install, no
image change, no reboot. FreeDOS boots to `C:\>` and DIR reports **the same
33,292,288 bytes free** as the first run ([grab](kitA-a1-dir.png)), so the
head, PCN and C split costs the booted machine nothing. Claimed only after
the status file logged the kit FREE on that baseline with its agent idle and
its lease free; released FREE after the post-run snapshot matched the probe,
and the three FogCast trees the run created (its game record, the ATA disk
delta and the staged fixture image) were removed in a second claim.

## Seal

Quartus 17.0.2, seed 1, the CUDA slot on `chip-30x0-ai-dev`, from a clean
worktree of the exact commit, with the lockstep `--netlist` report (#1782) in
front of it (`lockstep exit=0`). Every analysis passes. The branch was sealed
twice: once at its first head, and again at `41ecd7ef7` after the review
rounds moved the controller. The branch's own merge base `53681975f` is
sealed the same way on the same host, so the slice can be measured against
the tree it is a change to; the two mains of the two days are kept in the
table as well, with what they do and do not show.

| Build | ALMs | RAM blocks | DSP | Registers | Setup | Hold |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| **the branch's base `53681975f`** ([summary](base-r3-build-summary.json)) | 36,399 | 274 | 40 | 32,728 | +0.147 | +0.080 |
| **F2 seal `41ecd7ef7`, 14 Nov** ([summary](seal-r3-build-summary.json)) | **36,594** | 274 | 40 | 32,716 | **+0.810** | **+0.082** |
| **the slice** | **+195** | **0** | **0** | **−12** | | |
| The doc's F2 estimate | +150 | 0 | 0 | | | |
| main `1b4601831`, 14 Nov ([summary](main-r3-build-summary.json)) | 36,189 | 274 | 40 | 32,226 | +0.468 | +0.090 |
| main `8ab34380e`, 13 Nov ([summary](main-build-summary.json)) | 36,333 | 274 | 40 | 32,915 | +0.822 | +0.092 |
| F2 seal `92ef982d6`, 13 Nov ([summary](seal-build-summary.json)) | 36,399 | 274 | 40 | 32,869 | +0.694 | +0.068 |
| An earlier F2 head `18ccf4884` ([summary](earlier-seal-build-summary.json)) | 36,471 | 274 | 40 | 32,965 | +0.330 | +0.076 |

- **+195 ALMs, +0 M10K, +0 DSP and 12 registers fewer**, measured against
  the branch's own merge base `53681975f` sealed the same way on the same
  host: the same tree with and without this slice, which is the only
  comparison that isolates it. That is the figure to quote. At 87.3% of the
  5CSEBA6 the device keeps its room, and the slice comes in close to the
  doc's +150 estimate, inside the ±200 ALM fit noise the area audit
  measures.
- **The main-of-the-day comparisons are noisier, and the record keeps them
  so nobody re-derives them.** The branch carries none of main's commits
  since `8ab34380e`, and main itself shed 144 ALMs and 689 registers over
  the nine commits from there to `1b4601831`. So against today's main the
  same build reads +405 ALMs and +490 registers, and against yesterday's
  +261 and −199. Both mix this slice with main's own movement; the merge
  base does not.
- **What the area buys, and whether it is avoidable.** What the final model
  added over the first seal is the second and third cylinder number — a PCN
  and a head position per drive, 32 flip-flops — with the implied seek's
  adder and the comparator that catches a head away from its PCN. **That is
  not avoidable while the fix stands**: the head position is the whole of
  the first review round's finding, since without it a command naming a
  drive DOR does not select reads a cylinder that drive's head is not on,
  and the comparator is what turns that into ST1 ND instead of the wrong
  sector's bytes. It is paid for in logic, not memory: no M10K and no DSP
  moved, and the register count went down.
- **It is still a floor.** `pc386_system` ties the drive types to none and
  the boot device to the hard disk until slice F3, so Quartus prunes the
  CMOS arithmetic and the drive-select paths only a fitted drive reaches. F3
  pays the rest when its endpoint drives those inputs, as slice F1's record
  says of the controller itself.
- **Timing is met with room**, and more of it than the base had: setup
  +0.810 ns against the merge base's +0.147, hold +0.082 ns, minimum pulse width
  +1.122 ns, recovery +7.000 ns, removal +0.270 ns, on the 61.44 MHz system
  clock with the 74.27 MHz pixel and 12.29 MHz audio clocks. Nothing was
  relaxed.
- **The sealed commit is not the branch head**: everything committed after
  `41ecd7ef7` is this record and the PR body. No RTL, firmware or other seal
  input changed, and all 119 of the seal's recorded input digests match the
  head's files.
- Package `91a5f76453272d59…`, `BUILD_ID` `dd40e9922315367c5e3c421c952888bb`,
  backend CUDA, device 5CSEBA6U23I7, status `pass`.

## Reproduce

```bash
python3 scripts/sim_fes_pc386.py --suite fdc --download
python3 scripts/sim_fes_pc386.py --suite floppy --download
python3 scripts/sim_fes_pc386.py --suite floppy-boot --download
python3 scripts/sim_fes_pc386.py --suite floppy --cpu rtl
python3 scripts/sim_fes_pc386.py --suite floppy-boot --cpu rtl --limit-seconds 20
python3 -m unittest tests.test_pc386_machine tests.test_pc386_rtl_stops tests.test_build_fes_pc386_quartus
```

And the seal, on a host with the CUDA toolchain slot, from a clean worktree
of the commit being sealed:

```bash
export FES_TOOLCHAIN_CACHE_ROOT=/data/fes-toolchain-cache
export PATH=/usr/local/cuda/bin:$PATH
export QUARTUS_ROOTDIR=$HOME/intelFPGA_lite/17.0/quartus
python3 scripts/test_fes_i386_lockstep.py --download --netlist --gpu-router CUDA \
    --cache-root "$FES_TOOLCHAIN_CACHE_ROOT"
python3 scripts/build_pc386_firmware.py --download
python3 scripts/build_fes_pc386_quartus.py --root "$PWD" --seed 1 --gpu-router CUDA \
    --cache-root "$FES_TOOLCHAIN_CACHE_ROOT" \
    --lockstep-report "$PWD/build/sim/fes-i386-lockstep/report.json"
```

Run them all from `sources/misteross`. The sealer takes no `--download` of
its own, so the firmware is built first; the shared toolchain cache refuses
an `LD_LIBRARY_PATH` override, so CUDA goes on `PATH` alone.

## Not covered

- **DISKCOMP**, which the design's `floppy` row sketched, is not in the
  FreeDOS packages the lock pins, so the write check compares the image the
  bench writes back instead. The design's row now says so.
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
- **Double-stepping 40-track media** still is not modelled: a 360K disk in a
  1.2 MB drive reads image cylinder `c` at cylinder `c`, as Bochs does and
  as the design records. The drive's head position now exists, so what a
  later slice needs is a drive that steps two tracks for one pulse and a
  BIOS that asks for it.
- **A cylinder mismatch reports ND without ST2's WC bit.** A real controller
  that finds an ID field whose C differs from the command's sets wrong
  cylinder as well. The replay checks ST0 and ST1 on that path; no guest
  reads ST2 there.
