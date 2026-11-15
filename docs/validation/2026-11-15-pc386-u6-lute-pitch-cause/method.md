# Why Ultima VI's lute is flat on the kit: the method

fes#1837. Committed before any capture on either machine. Amendments below
this section are dated and say what had already been seen.

## What is already established, and not reopened here

- The kit plays the lute about 2.4 semitones flat at preset 1 (286-12):
  840.72 Hz against the reference's 962.76 Hz for key `6`, 499.79 against
  573.91 for key `1`, from
  [the 14 November record](../2026-11-14-pc386-u6-u7-reference/summary.md)
  (PR #1838). Three kit captures spread 0.006%, five reference captures 0.18%,
  against a ±5.9% band. That failure is not in question here.
- Both keys are flat by the same factor to within 0.3%, so the cause scales
  one timebase.
- A uniform CPU speed-up is excluded: the kit benchmarks *faster* than the
  reference (fes#1840) while its note is *lower*.
- #1453's ISA charges moved the pitch by about two points (#1474); the other
  ten or eleven predate any ISA charge.

This work asks only **why**, and does not fix it.

## What the game's code does

Read out of `GAME.EXE`'s unpacked image (EXEPACK, `dest_len` 12,456
paragraphs), at the two places the
[ISA bus record](../2026-11-03-pc386-isa-bus-time/summary.md) named. No game
code or data is committed; what matters is two loops of three and two generic
instructions and the shape of one routine.

**The calibration**, at 2B30h:005Eh, runs once at start-up. It hooks INT 1Ch
to a one-line handler that increments a flag, waits for a tick to sync, and
then counts passes of

```
    inc  word [m]          ; FF 06 .. ..
    cmp  word [flag], 0    ; 83 3E .. .. 00
    je   back              ; 74 F5
```

until the next tick, that is for exactly one BIOS tick (54.9254 ms). Call that
count **C**. It then stores `floor(18C/750)` at `[2FC6h]`.

**The lute**, at 2B95h:0006h, is the routine the sound-effect switch calls for
object 158. It sets `N = floor([2FC6h]/24)` at `[3090h]` (a byte division,
guarded by a `cmp ax,64h` so a count under 100 gives `N = 0`), loads PIT
channel 2 with 60, and then runs one pass per step of a phase accumulator:
a `cmp`/`ja` on the accumulator against a ramp, an `in`/`and`-or-`or`/`out` of
port 61h that gates the carrier, and a delay of `[bp+0Ch]` (1, for the lute)
times **N** iterations of

```
    dec  word [m]          ; FF 0E .. ..
    jne  back              ; 75 FA
```

The note's frequency is the gate rate, which is this pass rate times a
constant that depends only on the key's arguments. So, with `A` the pass's
other clocks and `t_d` the delay loop's clocks per iteration, both at the
machine's frequency `f`:

**pass rate = f / (A + N·t_d)**, and **N = floor(floor(18C/750)/24)**, and
**C = T·f / t_c** with `T` the tick period and `t_c` the calibration loop's
clocks per pass.

Three things follow, and they rewrite the test #1837 proposes.

1. **The delay is self-calibrating.** `N·t_d ≈ (T/1000)·(t_d/t_c)`, which has
   no `f` in it. A *uniform* error in the cost model cancels exactly, which is
   why fes#1840's faster CPU does not raise the note. Only **t_d/t_c**, the
   *relative* cost of the two loops, and the uncalibrated `A/f` can move the
   pitch.
2. **So the pitch barely follows a frequency override**, and "it did not
   follow" would be the expected result with nothing wrong in the ISA charges.
   #1837's pass rule for that test is therefore not usable, and is replaced
   below by a committed curve. For the same reason #1837's "reference scaled
   to 12.0 MHz" column is not a valid scaling and is not used here.
3. `N` is a small integer — 27 at preset 1 by the charges below — so it steps
   by about 3.7% of the delay. Every prediction here is an integer formula of
   the measured `C`, not a smooth curve.

## What the governor charges

From `cores/fes-common/rtl/i386/governor.toml` and `fes_i386_governor.sv` at
preset 1 (286 table, 1 wait state per memory bus cycle, 16-bit bus, 12.0 MHz).
`unary_m` 7, `test_m` 6, `jcc` taken 8, `nop` 3; one clock per bus cycle of
wait state; neither loop's address has base, index and displacement, so no
`ea3` clock.

| | charged clocks |
| --- | ---: |
| calibration pass: `inc` mem 7+2, `cmp` mem,imm 6+1, `je` taken 8 | **t_c = 24** |
| delay iteration: `dec` mem 7+2, `jne` taken 8 | **t_d = 17** |
| the same delay iteration with five `nop`s | 32 |
| the lute pass's other clocks, port 61h charged 7 ISA clocks each way | **A ≈ 113.5** |

So the governor's **t_d/t_c = 17/24 = 0.7083**, exactly, by construction.

## The probe

`u6cal.asm` in this record, built with the repo's pinned NASM 2.16.03 to
`U6CAL.COM`. It runs on any PC and reports five numbers:

- **C** — the game's calibration, byte for byte, five repeats;
- **CAL** and **N** — the game's own integer arithmetic on the last C;
- **BD** — 1000-iteration blocks of the delay loop completed in 64 ticks;
- **BP** — the same with five `nop`s inside the loop: identical memory
  accesses, five more bytes to fetch;
- **P0**, **PN** — passes of the lute loop, rebuilt from the routine's shape
  with its own argument block, at `N = 0` and at the calibrated `N`, in 64
  ticks, divided by 1000 and 100.

Both loop bodies are byte-identical to the game's and keep its loop-start
address parity (the calibration loop odd, the delay loop even), in case either
machine charges instruction fetch. The probe also waits 256 ticks between two
printed markers, so a video capture gives the machine's tick period.

`t_d/t_c = 64·C/(1000·BD)`: the tick period cancels, so this ratio is
comparable across machines without knowing either one's tick.

## The runs

The same `U6CAL.COM` on both machines, nothing else changed:

1. **Reference** — the 86Box `award286` VM of PR #1838's record
   (`vm/u6ref-286`), Intel 286 at 12.5 MHz, on a copy of that record's drive.
2. **Kit** — the same sealed `fes.pc386` package and preset 1, on a free kit
   under the standing claim rules, at frequency overrides (word 1 of
   `fes.pc386.machine`) of **80, 120 and 160** — 8.0, 12.0 and 16.0 MHz. Those
   three sit inside an `N` step by the charges above (`N` = 18, 27, 36), and
   the probe prints `C` and `N`, so a boundary cannot pass unseen.
   120 is the preset's own frequency and is also run with word 1 at 0.

Three repeats of the probe per point. Every run is verified from
`runs/<run>/result.json` before any figure is taken from it.

## The predictions, committed

From the charges above, at preset 1 with 64 ticks = 3.5152 s:

| word 1 | f | C | N | BD | PN | note from PN |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 80 | 8.0 MHz | ≈18,308 | 18 | ≈1,654 | ≈680 | ≈776 Hz |
| 120 | 12.0 MHz | ≈27,463 | 27 | ≈2,481 | ≈737 | 840.7 Hz |
| 160 | 16.0 MHz | ≈36,607 | 36 | ≈3,308 | ≈769 | ≈877 Hz |

`BP/BD` on the kit is predicted to be **17/32 = 0.531**, the `nop`s' table cost
and nothing else.

The note column uses the one constant this work fits: the key-`6` ratio
between the note and the pass rate, taken from the kit's measured 840.72 Hz at
word 1 = 120. It is the same constant on both machines.

**So the frequency test's real content is the shape of the PN column**: the
pass rate is predicted to rise about **13% for a doubling of the clock**, not
to double. "Follows the clock" would mean PN roughly doubling from 80 to 160.

## The hypotheses, and what separates them

| | prediction |
| --- | --- |
| **H1 — the cost model charges no instruction-fetch time.** The calibration loop fetches 11 bytes a pass, the delay loop 6. A machine that charges fetch makes the longer loop relatively dearer, so its `t_d/t_c` is lower. | kit `t_d/t_c` = 0.708 (the charges exactly); reference lower, and lower by enough to carry the gap. Kit `BP/BD` = 0.531; reference `BP/BD` **above** that, by the fetch time of five bytes. |
| **H2 — the ISA charges on port 61h.** | the gap lives in `A`, so `P0` differs between the machines by much more than `PN` does, and the `PN` gap shrinks as the override rises. |
| **H3 — a wrong table entry, not fetch.** | `t_d/t_c` differs between the machines, but `BP/BD` agrees: neither machine charges per byte. |

## The pass rules

- **Reproduction gate.** The probe's `PN` ratio, reference over kit at word 1
  = 120, must equal the measured note ratio **962.76 / 840.72 = 1.1452 within
  ±3%**. The margin covers the probe's own spread and the fact that its
  argument block is rebuilt rather than the game's. If the probe does not
  reproduce the pitch ratio, it is not measuring what sets the pitch, and the
  outcome of this work is **"not separated"** — no cause is claimed from it.
- **Separation gate.** A hypothesis is the cause only if it accounts for at
  least **9 of the 12.7 points**, and the other two for less than 3 each,
  with the `t_d/t_c` figures and `A` from the probe. Otherwise the record says
  which pair cannot be separated and what would separate them.
- **Quantisation.** Any figure within one `N` step (3.7% of the delay term) of
  a boundary is reported as undecided at that point, not rounded.
- Nothing here is retuned after a capture. A failed gate is reported failed.

## What had already been seen when this was written

PR #1838's pitch and spread figures, #1474's ratios across #1453, the ISA
record's 89 + 475×units model and its account of the routine, fes#1840's
TopBench figures, and the disassembly above. No capture of this probe had been
run on any machine, and `U6CAL.COM` had not been executed anywhere.

## Amendment, 15 November, after the first reference run

Run `c1` on the reference had been taken and read: C = 23,662 (five repeats,
all equal), CAL = 567, N = 23, BD = 2,436, BP = 1,330, P0 = 465, PN = 847. No
kit capture of the probe existed, and the kit's figures in the table above
were still only the charges.

That run showed the `nop` padding does not separate H1 from H3 as written: the
reference charges the five `nop`s 15.0 clocks, the table's own cost and nothing
more, which is what a model with prefetch would also do, because the `nop`s'
own execution time gives the fetch somewhere to hide. **Two variants are
therefore added to the probe** — the same delay loop with one extra
instruction in front of the `dec`, so the `jne` still tests the `dec`:

- **B5** with `cmp word [m], 0` (5 bytes, the calibration loop's own
  instruction);
- **B4** with `cmp ax, [m]` (4 bytes: the same memory read, one byte less and
  no immediate).

`cost = 64T/1000 · (1/B − 1/BD)` gives that one instruction's clocks on each
machine. If the two forms cost the same on the reference, the extra is a table
entry (H3); if the 5-byte form costs materially more, it is per-byte (H1).

**No gate and no prediction is changed.** The reproduction gate, the
separation gate and the committed `PN` curve stand exactly as written above,
and the reference run will be repeated with the amended probe so both sides
run the same binary.

## Amendment, 15 November: the Kit A baseline check

`run-u6ref.sh` was written for Kit B and asks the kit's local-control socket
for `"phase":"idle"`. Kit A's bundle does not serve that socket, so the check
can never pass there and the wrapper refused to touch the kit — correctly.
`run-u6cal.sh` is a copy with that one duplicate idleness check replaced by
the host API's own `"state":"idle"`, which is the same fact from the source
the next check (`games_lease_probe.py`, "idle free") already uses. Every other
baseline, lease and restore check is unchanged.

## Amendment, 15 November, before any kit grab was opened

Run `c3` on the reference had been read (the figures are in the record; they
repeat `c1` exactly and add B5 = 1,625, B4 = 1,755). The kit run `c1cal` was
in flight and **no kit grab had been opened**. Two things are added here.

**1. The kit's predicted B5 and B4, which the first amendment forgot.** Both
loops are charged the same 24 clocks — `cmp` 6 + 1 wait, `dec` 7 + 2 waits,
`jne` taken 8 — so at word 1 = 120 both should read about **1,758**, against
BD ≈ 2,481 and BP ≈ 1,318. The charged B5 = B4 exactly: the cost model's
inputs (`fes_i386_governor`'s ports) carry an instruction's class, its memory
accesses and its I/O, and **no instruction address and no instruction
length**, so by construction it cannot charge one encoding more than another
of the same class and accesses, nor charge for where a loop sits.

**2. One more reference-only variant, `BO`.** The same six-byte delay loop
started at an odd address instead of an even one. The B5 loop (11 bytes, three
instructions, three accesses) costs 27.04 clocks on the reference while the
game's calibration loop (11 bytes, three instructions, three accesses, a
different order and an odd start) costs 29.02. If `BO` differs from `BD` on
the reference, no table entry can explain it, and the missing term is fetch.
If it does not, the B5-against-B4 gap of 2.00 clocks stands alone and the
record will say **"consistent with a missing fetch term, not separated from a
table entry"** — and will not claim that fes#1840 shares this cause.

The kit cannot run `BO` as a comparison, for the structural reason in (1).

## Amendment, 15 November, after `BO` came back null and before `c5`

Seen by now: the reference runs `c1`, `c3` and `c4`, and the kit points at
word 1 = 0, 80 and 120. **`BO` = `BD` = 2,436 exactly** — the six-byte delay
loop costs the same at an odd start as at an even one — so the alignment test
as designed is null, and by the rule committed above the record would say
"consistent with a missing fetch term, not separated from a table entry".

But the reference's four loop costs fit one rule that makes the null
expected, and it is worth one more run rather than leaving the question open.
Write a loop's cost as **max(the execution sum, 3 clocks × bus cycles)**,
where bus cycles are the loop's data accesses plus one word fetch per 16-bit
word its bytes span:

| loop | bytes, start | execution sum | bus cycles × 3 | measured |
| --- | --- | ---: | ---: | ---: |
| `BD` | 6, even | 17 | 5 × 3 = 15 | 18.04 |
| `BO` | 6, odd | 17 | 6 × 3 = 18 | 18.04 |
| `BP` | 11, even | 32 | 8 × 3 = 24 | 33.04 |
| `B4` | 10, even | 24 | 8 × 3 = 24 | 25.04 |
| `B5` | 11, even | 24 | 9 × 3 = 27 | **27.04** |
| the game's calibration loop | 11, **odd** | 24 | 10 × 3 = 30 | **29.02** |

`BD` and `BO` are execution-bound, so alignment is invisible there — which is
why the test was null. `B5` and the calibration loop are bus-bound, and they
differ by exactly their start parity.

**Run `c5`, reference only**, with three loops of identical content —
`cmp word [m],0` / `dec word [m]` / `jne` — differing only in start address
and order:

- **BE**, even start. Predicted **1,625** (27 clocks), the same as `B5`.
- **BQ**, odd start. Predicted about **1,465** (30 clocks) if the rule holds;
  **1,625** if cost depends only on the instructions.
- **BR**, even start, `dec` before `cmp`. Predicted **1,625**: order is not
  part of the rule.

**The rule this settles, committed now.** If `BQ` is near 1,465 and `BE` and
`BR` near 1,625, then the reference charges a cost that depends on where the
loop sits, which no per-class table entry can express, and the record names
the missing term as instruction-fetch bus time — and says that term would
also act on fes#1840. If `BQ` matches `BE`, the record keeps the "consistent
with, not separated" wording already committed, and claims nothing about
fes#1840. Any other pattern is reported as it falls, with no new rule.

## Amendment, 15 November, after `c5` — and the lute under an override

`c5` **falsified** the bus-cycle rule committed just above. `BE` = `BQ` =
**1,625**: the eleven-byte, bus-bound loop costs the same at an odd start as
at an even one, so alignment is free on the reference in both the cases
tested, and the rule that predicted 1,465 for `BQ` is wrong. By the pass rule
committed with it, the record keeps **"consistent with a missing fetch term,
not separated from a table entry"** and claims nothing about fes#1840. The
rule is reported falsified in place; it is not replaced.

`BR` measured nothing and is void by a fault in its design, not a result: with
`dec` before `cmp word [m],0`, the loop's `jne` tests the `cmp`, which is
always equal, so the loop runs once.

**The lute under an override, run `h1`.** The task behind this work asked for
the game's own note under a frequency override, which the probe stands in for
but does not replace. Run `h1` plays the lute at word 1 = **160** on Kit A
through PR #1838's own tested command flow — 160 rather than 80 because the
flow's fixed sleeps were timed at 12 MHz and a faster machine can only reach
each point early. **Committed now, before the run:** key `6` at
840.72 × 769/736 = **875.4 Hz**, from the measured pass rates alone, and the
check is ±3%, the same margin as the reproduction gate. Analysed by
`notes_pitch.py`, the same autocorrelation PR #1838 used.
