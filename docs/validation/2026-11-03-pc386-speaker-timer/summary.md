# pc386: PIT counter 2 as an AT leaves it, 3 November 2026

[#1457](https://github.com/DeanoC/fes/issues/1457). On Kit B, Ultima VI's
CPU-timed speaker routine played no tone, only stray clicks. This happened on
both [#1453](https://github.com/DeanoC/fes/pull/1453)'s package and main's
(that PR's record, "The CPU-timed routine: run `u6i`").

Nothing ever programs PIT counter 2's mode. An AT BIOS's POST beep leaves the
counter in mode 3 with LSB-then-MSB counts, and the game relies on that. The
pc386 BIOS is Bochs's, and Bochs never programs counter 2. KF8253 resets to
mode 0 with MSB-only counts, so counter 2 stayed that way. The routine's count
write then gave count 65,536 in mode 0, and counter 2's output stayed low. The
PIT itself behaves as an 8254. Only its starting state was wrong.

The fix is a local patch to the vendored BIOS: POST programs counter 2 as an AT
does. It changes no RTL.

**Result.** SPKTEST.COM finds counter 2 in mode 3 at the DOS prompt. It
replays the routine's I/O and gets counter 2's output high after every rising
gate, and a 19.9 kHz square wave while the gate stays high. With main's BIOS,
all three checks fail, and the output never goes high. No kit has run the
fix yet.

## Inputs

- **Branch.** FES `deano/pc386-speaker-timer`, from main `39d79a092`.
- **Firmware.** BIOS `8057fada97565abb…`, built 11/03/26 by
  `scripts/build_pc386_firmware.py` against the updated lock. The VGA BIOS
  (`69401dc8…`) is unchanged. Main's BIOS `47a982a5…` was rebuilt from a clean
  checkout of main for the before run.
- **Test program.** `SPKTEST.COM` `9cd3827fb1d14e9b…`, from
  [`cores/fes-pc386/sim/speaker_test.asm`](../../../sources/misteross/cores/fes-pc386/sim/speaker_test.asm)
  with the test386 runner's NASM 2.16.03, on a copy of the FreeDOS boot image.
- **Tools.** Verilator 5.032 on Powerboat.

## The routine and what it needs

The routine is far procedure 2B95h:0006 in GAME.EXE's unpacked image. The lute
case calls it with 2,000 steps and a delay multiplier of 1. It first writes 3Ch
and then 00h to port 42h, which is count 60 as an LSB and an MSB, with no
control word. Each step then reads port 61h, ORs in 3 or ANDs with FCh, which
sets or clears the gate (bit 0) and the speaker (bit 1) together, writes port
61h back, and runs a calibrated delay. Two 16-bit accumulators choose on or
off for each step. Most steps are on in the first half of a note and most are
off in the second. At the end the routine clears both bits.

No control word for counter 2 appears anywhere on the boot path. The pc386
BIOS programs counters 0 and 1 only. The VGA BIOS's BEL is a `FIXME should
beep`. GAME.EXE's unpacked image and the other Ultima VI executables were
searched for writes to port 43h (`OUT 43h,AL`, `MOV DX,43h`, `MOV AL,B6h`),
and there are none. Their speaker code writes only counts. The game's files
stayed on Powerboat, and none of their bytes are in this record.

On an 8254 in mode 3, a low gate forces OUT high and holds the count. A rising
gate reloads the count on the next clock, and OUT stays high for its first
half. While the gate stays high, OUT is the count's square wave. Count 60 gives
19,886 Hz. So the speaker carries a 19.9 kHz carrier that the routine gates
on and off at the note's pitch, and the mixer's box filter averages it into the
note's envelope.

KF8253's reset state is different. With MSB-only counts, both bytes are taken
as MSBs, so the count is 0000h, which is 65,536. In mode 0, OUT goes low on the
count write, and a low gate holds the count without driving OUT high. OUT rises
only once the gate has been high for 65,536 PIT clocks, which is 54.9 ms. The
routine writes the count again at the start of every note, so in nearly every
note OUT2 never rises, and the speaker never sounds.

## Sim trace: counter 2 alone

[`kf8253_routine_bench.sv`](kf8253_routine_bench.sv) holds KF8253 counter 2
and port 61h bits 0-1, wired as `pc386_at_core.sv` wires them, at the system
clock with `pc386_rate`'s PIT clock. It replays the routine's I/O: 3Ch, then
00h, then 2,000 steps 20 µs apart, chosen on or off with the accumulators of
the game's other 2,000-step call (step 3116, start 20000, −10). One 40 ms note
gives 47,752 PIT clocks, 40,350 of them with the gate high:

| Counter 2 before the note | OUT2 high | Speaker on |
| --- | ---: | ---: |
| KF8253 reset, as pc386 left it (`+mode=boot`) | 0 clocks | 0 clocks, 0.0% of the note |
| After `B6h, 33h, 05h`, an AT POST (`+mode=at`) | 28,132 clocks | 20,731 clocks, 43.4% of the note |

A second note gives the same numbers. [`compare_8254.py`](compare_8254.py)
compares the `at` trace with the 8254 model described above. OUT2 never
differs from the model by more than one PIT clock: KF8253 takes a rising gate
at the next count edge. With the gate high, OUT2's runs are 30 clocks high and
30 low, with a 31-clock first half after each reload (621, 616 and 47 runs),
and OUT2 is high at every clock with the gate low. So KF8253's mode 3 gate
handling is right for this routine.

The kit's clicks are not fully explained. A bench note whose high-gate time
passes 65,536 PIT clocks gives OUT2 high for the rest of the note, so the
speaker follows port 61h bit 1 for that tail. With lute-like accumulators
(start 20000, −20) and steps of 56-58 µs, that is a few milliseconds of short
pulses at the end of the note. The kit's capture shows single pulses of 2-4
samples instead. Their exact source is open. A kit run with this fix will
show whether they turn into the notes.

## The BIOS change

POST's change is in `rombios.c`, right after counter 1, marked `FES 2026-11-03`
([firmware README](../../../sources/misteross/cores/fes-pc386/firmware/README.md#local-changes)):

```
mov al, #0xb6   ; counter 2, LSB then MSB, mode 3, binary
out PORT_PIT_MODE, al
mov al, #0x33   ; count 0533h (896 Hz), the IBM BIOS BEEP routine's
out PORT_PIT_COUNTER2, al
mov al, #0x05
out PORT_PIT_COUNTER2, al
```

The mode is the one an AT BIOS's POST beep leaves, and the count is the IBM
BIOS BEEP routine's. The count does not matter to the game, which writes its
own. The BIOS does not beep:
port 61h bits 0 and 1 stay clear (the machine resets them, and POST never sets
them), so the gate stays low and the speaker stays off. `rombios.h` gains
`PORT_PIT_COUNTER2` (42h). A shutdown-byte resume branches off before this part
of POST, as with counter 1, and the PIT keeps its state through a CPU reset.

## The check: SPKTEST.COM

[`cores/fes-pc386/sim/speaker_test.asm`](../../../sources/misteross/cores/fes-pc386/sim/speaker_test.asm)
runs at the FreeDOS prompt in the new `speaker` suite (`make
sim-fes-pc386-speaker`, in CI's pc386 full tier beside `wait`):

- `post`: counter 2's 8254 read-back status, before the program touches the
  counter. Bits 5-0 must be 36h: LSB then MSB, mode 3, binary.
- `rise`: the routine's write of 3Ch, 00h with no control word, then 64 gate
  falls and rises with the speaker on. Port 61h bit 5, counter 2's output, must
  read 1 straight after each rise.
- `carrier`: with the gate held high, PIT counter 0 times 40 periods of bit 5,
  from one change to the 80th after it. The frequency must be within
  19,400-20,400 Hz; count 60 gives 19,886 Hz.

## Checks

| Check | Result |
| --- | --- |
| `speaker` suite, oracle CPU ([report](speaker-report.json)) | Passes: `SPK post 36h ok`, `SPK rise 64/64 ok`, `SPK carrier 19886Hz ok`, `SPK DONE ok`, and the prompt again at 3.8 s |
| `speaker` suite, oracle CPU, main's BIOS `47a982a5` (same bench and disk; [summary](speaker-before-report.json)) | Fails, as the bench predicts: `SPK post 20h FAIL` (MSB only, mode 0: KF8253's reset state), `SPK rise 0/64 FAIL` and `SPK carrier 0Hz FAIL`. Counter 2's output never read high |
| `speaker` suite, RTL CPU ([report](speaker-rtl-report.json)) | RTL_RESULT |
| `boot`, oracle CPU | Passes: FreeDOS to the prompt at 3.75 s, `dir`, every I/O at its class time, and the VGA trace replay against Bochs (81,109 events, no mismatch) |
| `wait`, oracle CPU | Passes, with the same waits as #1451's record to within 1 µs (10 ms took 10,045 µs) |
| Lockstep | Unaffected: the branch changes no RTL, and the lockstep reads no firmware |
| Unit tests | UNIT_RESULT |

## Seal

None. Under the pc386 reseal exemption, the next sealing PR covers this BIOS.

## Not done here

- **A kit run.** No package with this BIOS has been built or run on a kit. So
  Ultima VI's lute tones on Kit B, and #1453's pitch measurement that needs
  them, are still to do.
- **The `system` and `governor` suites** were not run. Both boot on
  `pc386_system`. This change adds five `OUT`s to POST, and #1451's record
  says the 386DX-25 governor preset hits
  [#1372](https://github.com/DeanoC/fes/issues/1372) when POST timing moves.
- **A second KF8253 difference from the 8254,** found on the way and not
  fixed here. A control word only resets the counter's byte pointers when its
  RW field changes. So B6h, then one LSB, then B6h again, then 33h, 05h gives
  count 333Ch on KF8253, where an 8254 resets the pointer and gives 0533h.
  Ultima VI never writes an odd byte count, so it is not hit here. KF8253 is
  shared with the PC/XT core.
