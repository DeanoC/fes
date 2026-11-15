# Why Ultima VI's lute is 2.4 semitones flat on the kit

fes#1837. The method, with every prediction and pass rule, is in
[`method.md`](method.md), committed before any capture and amended and
corrected seven times, each entry dated and saying what had been seen. One
rule committed there is reported **falsified**; it was not replaced.

**The cause.** The game times its own delay loop against the BIOS tick at
start-up and then plays the lute from that calibration. The kit's cost model
charges the *calibration* loop and the *delay* loop at the wrong ratio
relative to the reference machine — 0.709 against 0.622 — so the calibrated
delay unit comes out about 14% too long and every note is flat by the same
factor. That is **10.7 of the 13.1 points**. The other **2.4** are the
per-pass overhead, which is the size of #1453's ISA charge on port 61h and
matches #1474's "about two points", though the reference's own cost for those
two port accesses was not measured, so the record does not attribute the 2.4
to the ISA charge.

**So the fault is in the governor's cost table — what it charges these forms
relative to each other — and not in the ISA charge model, not in the CPU's
instruction timing, and not in any uniform speed error.** The fix is not
applied here: it moves every preset and every title, which is Deano's to
weigh. **The pitch still fails #1368's band.**

**The one number to check.** `cmp word [mem], imm` costs **9.00** clocks on
the reference and **7** on the kit, while `cmp ax, [mem]` costs **7.00** on
both — the same work and the same memory accesses, one byte more of encoding.
The first of those two is in the game's calibration loop and not in its delay
loop. (They are different cost classes, `test_m` and `cmp_load`, which the
286 column happens to give the same number, so a table *can* separate them.)

**The prediction that confirms it.** Before run `h1` the record committed
key `6` at **878.4 Hz** for a word-1 override of 160 — the kit's own 840.72 Hz
at 12 MHz scaled by the probe's two pass rates, 769/736, and nothing else.
The kit then played it at **873.59 Hz**: **−0.55%**, with key `1` at −0.15%,
at a frequency the game had never been played at. The prediction was
committed, and then measured, in that order. (Key `6` is the committed
figure; the key `1` number was worked out the same way after the run.)

**What is not settled.** Whether the reference's extra cost is
instruction-fetch time — which would also act on **fes#1840** — or simply
different table entries. Both tests that would have separated them came back
null, so this record neither claims nor excludes a shared cause with #1840.
See "What this does not settle".

## The test #1837 proposed, and why it was replaced before any capture

#1837 proposed playing the lute under two frequency overrides: if the pitch
follows the clock, the loop is CPU-timed and the clock table is at fault; if
not, the ISA charges are.

Reading the game's code showed that rule cannot discriminate, **before any
capture on either machine**, and `method.md` replaced it then, with the
reasoning and a curve in its place. No kit or reference figure of this work
existed at that point; the first probe run came afterwards. This is the
opposite of changing a rule to admit a result, and the order is the whole of
the distinction.

Why it cannot discriminate: the delay is self-calibrating, so a *uniform*
cost error cancels out of the pitch entirely, and so does the frequency. The
pitch is set only by the two loops' cost *relative to each other* and by the
uncalibrated per-pass overhead. The measured curve bears this out — the pass
rate rises about 8% for a 50% clock rise — so "the pitch did not follow the
override" was always going to be the answer, with or without a fault in the
ISA charges.

That also reconciles #1837 with **#1840**, which has the same preset
benchmarking 22-57% *above* real 286 boards of its clock. A machine that is
uniformly too fast calibrates to a proportionally larger delay unit and plays
the note at the same pitch. The two findings are not in tension. Whether they
share a cause is neither claimed nor excluded here; see "What this does not
settle".

## What the game does

From `GAME.EXE`'s unpacked image (EXEPACK). No game data is reproduced; the
shapes below are what `method.md` describes and `u6cal.asm` measures.

**At start-up** (2B30h:005Eh) the game hooks INT 1Ch, syncs to a tick, and
counts passes of `inc word [m]` / `cmp word [flag],0` / `je` for exactly one
tick — call it **C** — then stores `floor(18C/750)` at `[2FC6h]`.

**The lute** (2B95h:0006h) takes `N = floor([2FC6h]/24)`, loads PIT channel 2
with 60 for a 19.9 kHz carrier, and runs one pass per step of a phase
accumulator: a compare, an `in`/`out` of port 61h that gates the carrier, and
`N` iterations of `dec word [m]` / `jne`. The note is the gate rate, so

> **note ∝ pass rate = f / (A + N·t_d)**, with **N = floor(floor(18C/750)/24)**
> and **C = T·f / t_c**.

`N·t_d ≈ (T/1000)·(t_d/t_c)` has no `f` in it. That is the whole mechanism.

## The probe

`u6cal.asm` → `U6CAL.COM`, 786 bytes, NASM 2.16.03. All three sources
committed here reassemble, with that NASM, to exactly the binaries that ran. It reports `C` (five
repeats), the game's own integer `CAL` and `N`, and block counts over 64
ticks for the delay loop (`BD`), that loop with five `nop`s (`BP`), with
`cmp word [m],0` in front (`B5`), with `cmp ax,[m]` in front (`B4`), and for
the lute pass at `N = 0` (`P0`) and at the calibrated `N` (`PN`). `u6cal2.asm`
adds `BO`, the delay loop at an odd start address. `analyse.py` turns the
counts into clocks; the tick period cancels from every ratio.

Binaries: `U6CAL.COM` `1aa15a91…`, 786 bytes (runs `c3`, `c1cal`; run `c1`
used the earlier `d9dcc85f…`, which lacked `B5`/`B4`), `U6CAL2.COM`
`2fac2445…`, 836 bytes (run `c4`), `U6CAL3.COM` `22b17b0f…`, 1,002 bytes (run `c5`). `u6cal3.asm`
also carries `BE`, `BQ` and `BR`.

## The runs

| run | machine | what |
| --- | --- | --- |
| `c1` | 86Box `award286`, Intel 286 at 12.5 MHz | reference, first probe |
| `c2` | the same | **failed**: `refbox.py` could not read the window geometry at the first grab; no figures taken |
| `c3` | the same | reference, amended probe |
| `c4` | the same | reference, `BO`, the delay loop at an odd start |
| `c5` | the same | reference, `BE`/`BQ`/`BR`, the alignment and order variants |
| `c1cal` | Kit A, preset 1, sealed package `fc01c956…` | the probe at word 1 = 0, 80, 120, 160; three repeats each |
| `h1` | the same kit and package | Ultima VI's lute at word 1 = 160 |

The reference is a copy of PR #1838's own VM (`vm/u6ref-286` → `vm/u6cal-286`),
unchanged but for the drive, which is that record's `drv/u6ref-master.img`
plus `U6CAL.COM`. Run `c1`'s directory name collided with a run of 14
November; what that cost is declared in the scratch README, and `c1`'s
figures are superseded by `c3` in any case. The kit ran
core-only on its baseline software `c9ae79e2` and image `179affbb…`, claimed
and released in `kitA-STATUS.md`, with no install, image change or reboot.
Both kit runs were verified from their `result.json` and `acks.jsonl`:
`c1cal` has five `"state": "active"` loads (the scrub plus the four override
points) and `h1cal` two, neither holds an `error` for any command, and both
ended `HIL rc=0` with the kit released and its baseline re-verified
read-only afterwards. The frame grabs of `runs/c2` to `c5` are all from this
work; `c2`'s directory holds none.

`run-u6cal.sh` is `run-u6ref.sh` with one duplicate idleness check — the
kit-local `"phase":"idle"`, which Kit A's bundle does not serve — replaced by
the host API's own `"state":"idle"`, the source the next check already uses.
Every other baseline, lease and restore check is unchanged.

## What the probe measured

All three repeats of the whole probe were read at every kit point and all
twelve printed identical block counts. The reference's three repeats were
read in `c3` and are identical too. `C` is five repeats within each run: equal in every run except
at word 1 = 80, where it alternates between 18,288 and 18,291 — a spread of
0.016%, against a ±5.9% band.

| | reference, 12.5 MHz | kit, word 1 = 0 | = 80 (8 MHz) | = 120 (12 MHz) | = 160 (16 MHz) |
| --- | ---: | ---: | ---: | ---: | ---: |
| `C` | 23,662 | 27,444 | 18,291 | 27,444 | 36,597 |
| `CAL`, `N` | 567, **23** | 658, **27** | 438, **18** | 658, **27** | 878, **36** |
| `BD` | 2,436 | 2,477 | 1,651 | 2,477 | 3,303 |
| `BP` | 1,330 | 1,317 | 878 | 1,317 | 1,756 |
| `B5` | **1,625** | **1,755** | 1,170 | **1,755** | 2,340 |
| `B4` | **1,755** | **1,755** | 1,170 | **1,755** | 2,340 |
| `P0` | 465 | 433 | 307 | 433 | 543 |
| `PN` | 847 | 736 | 679 | 736 | 769 |
| *predicted* `PN` | — | — | *680* | *737* | *769* |

In clocks (`analyse.py`). The tick period cancels from every *ratio* between
machines, which is what the argument rests on; the absolute clock counts
below also assume each machine's nominal tick (18.2065 Hz) and nominal clock.
The probe's T0/T1 marker, which would have timed the kit's tick against wall
time, was printed but never captured on video, so the kit's tick period is
not independently measured here.

| | reference | kit (all three overrides) | charged |
| --- | ---: | ---: | ---: |
| calibration pass `t_c` | **29.02** | **24.02** | 24 |
| delay iteration `t_d` | **18.04** | **17.03** | 17 |
| **`t_d / t_c`** | **0.6217** | **0.7091** | 0.7083 |
| five `nop`s | 15.00 | 15.00 | 15 |
| `cmp word [m],0` | **9.00** | **7.01** | 7 |
| `cmp ax,[m]` | **7.00** | **7.01** | 7 |

**The kit is paced exactly as it is charged**, at every frequency, to within
0.2% — so the governor is doing what its table says, and the table is what is
being measured. The reference charges the same work more, and unevenly: 5.0
clocks more for the calibration pass, 1.0 more for the delay iteration.

The reference's alignment and order variants (`c4`, `c5`), all at 12.5 MHz:

| | count | clocks | loop |
| --- | ---: | ---: | --- |
| `BD` | 2,436 | 18.04 | `dec [m]; jne`, even start |
| `BO` | 2,436 | 18.04 | the same, **odd start** |
| `B5` | 1,625 | 27.04 | `cmp word [m],0; dec [m]; jne`, even start |
| `BE` | 1,625 | 27.04 | the same, built again in `u6cal3` |
| `BQ` | 1,625 | 27.04 | the same, **odd start** |
| `BR` | 49,560 | — | **void**: with `dec` first, the `jne` tests the `cmp`, which is always equal, so the loop runs once. A fault in the variant's design, not a result. |

Alignment is free on the reference in both cases tested. The game's own
calibration loop nevertheless costs **29.02** clocks against `BQ`'s 27.04 —
and `BQ` is the right comparison, because it shares the calibration loop's
**odd start** as well as its three cost classes, its eleven bytes and its
three accesses. The two differ only in the order of their instructions, and
in 2.0 clocks.

## The gates

**Reproduction gate — passed.** At word 1 = 120 the probe's pass-rate ratio,
reference over kit, is 847/736 = **1.1508**, against the notes' own
962.76/840.72 = **1.1452**: **0.49% apart**, inside the committed ±3%. The
probe measures what sets the pitch.

**The frequency curve — as committed.** The predicted `PN` was 680, 737, 769
at 8, 12 and 16 MHz; the kit gave **679, 736, 769**. The pass rate rises
**13.3% for a doubling** of the clock, not 100%. The pitch does not follow
the override, and that was predicted before the run from the calibration,
with no fault anywhere needed to explain it.

**Separation gate — passed for the issue's three candidates; one pair of
*mechanisms* is left unseparated.** At word 1 = 120:

| | reference | kit | excess |
| --- | ---: | ---: | ---: |
| pass | 41.502 µs | 47.761 µs | +15.08% |
| of which the calibrated delay, `N · t_d` | 33.190 | 38.318 | **+5.128 µs** |
| of which everything else in the pass | 8.312 | 9.444 | **+1.132 µs** |

The pitch deficit the probe reproduces is 13.10%, split **10.74 points to the
calibrated delay** and **2.37 points to the rest of the pass**.

**Against fes#1837's own three candidates** the gate is met: its "governor's
ISA charges" has at most 2.4 points, its "governor's pacing of the delay loop" has
10.7, and and its third — "the loop costing the kit's 386 a different
number of clocks than a 286, whatever the clock table says" — is excluded
outright, because the kit's measured clocks *equal* its charges at every
frequency, so the fabric is not costing the loop anything the table does not
say. One candidate carries more than 9 points and the others under 3 each.

The 10.7 points map to #1837's second candidate by **mechanism** — the
governor's pacing of the game's software delay — rather than loop for loop:
what is mischarged is the *calibration* loop's cost relative to the delay
loop's, and it is the calibration loop that carries most of the error.

**Against the three hypotheses `method.md` named** the gate's "otherwise"
clause applies instead, and the record says so: H2 (the ISA charges) has its
2.4 points, but H1 (a missing fetch term) and H3 (table entries) *share* the
10.7 and are not separated from each other. The first amendment proposed that
the five-byte `cmp` costing more than the four-byte one would decide it for
H1; after seeing that result the record **declines that claim**, because the
two forms are different cost classes and a table can give them different
numbers. Declining a claim the evidence does not carry is the only direction
that move may go, and `method.md` records it under the same date.

The 2.37 points are the per-pass overhead. #1453's ISA charge on port 61h is
14 clocks, 1.167 µs at 12 MHz, which is the same size as the whole 1.132 µs
excess — and matches #1474's "about two points" — but the reference's own
cost for those two port accesses was not measured separately, so the record
says *overhead*, not *ISA*, and does not claim the ISA charge is all of it.

## The game itself, under an override: run `h1`

The probe stands in for the lute; it does not replace it. Run `h1` played
Ultima VI's lute at preset 1 with word 1 = **160** through PR #1838's own
command flow (160 rather than 80 because that flow's fixed sleeps were timed
at 12 MHz, and a faster machine can only reach each point early). The
`u6-used` grab shows `Use-lute`, so the keys landed; `result.json` and `acks.jsonl` hold its two
`"state": "active"` loads with `fes.pc386.machine` among their interfaces and
no `error` for any command, and the run ended `HIL rc=0` with Kit A released
and its baseline re-verified read-only afterwards.

**Committed before the run, from the measured pass rates alone:** key `6` at
840.72 × 769/736 = **878.4 Hz**, checked at ±3%, so 852.1 to 904.8.

| | predicted | measured (median of 32 notes) | |
| --- | ---: | ---: | ---: |
| key `6` | 878.4 Hz | **873.59 Hz** | **−0.55%** |
| key `1` | 522.20 Hz* | **521.40 Hz** | −0.15% |

\* Only key `6`'s 878.4 Hz was committed before the run. Key `1`'s figure is
the same scaling applied to its own 499.79 Hz afterwards, and is shown as a
consistency check, not as a prediction under the gate.

31 notes in each band — the 30 presses plus one the closing `1`-`0` scale
contributes — and the medians are over those 31. They are tight: 872.94 to
873.72 Hz (0.09%) and 521.27 to 521.49 Hz (0.04%). The other ten of the 72
detections are the rest of that closing scale.

**Passed.** The prediction is the kit's own 840.72 Hz at 12 MHz scaled by the
probe's two pass rates, 769/736, and nothing else. The game's own note at a
frequency it had never been played at comes out within 1% of it — and it is still about 9% below the reference's 962.76 Hz,
because raising the clock cannot fix a calibrated delay.

Notes in [`h1-lute-f160.json`](h1-lute-f160.json), from `notes_pitch.py`, the
same autocorrelation PR #1838 used. The WAV stays on Powerboat.

## Where in the model the fault is

The two loops use **the same three cost classes** — `unary_m` for `inc`/`dec`
of memory, `test_m` for `cmp r/m, imm`, `jcc` for the branch — and the same
memory accesses. The governor charges them additively: 24 and 17 clocks, a
ratio of 0.7083. The reference costs them 29.02 and 18.04, a ratio of 0.6217.
**The fault is in the cost table's 286 column**, in what it charges these
forms relative to each other. Two adjustments are needed, and only one of
them is pinned to a class:

- The delay loop needs **one clock more** (17 → 18.04). It contains one
  `unary_m` and one taken `jcc`, and nothing here says which of the two is
  short, so that clock is **underdetermined**: raising `unary_m` 7 → 8 fits
  the measurements exactly as well as raising `jcc` taken 8 → 9, and the
  second would retime every taken branch on every preset. Separating them
  needs a loop that has one and not the other.
- The calibration loop then needs **about four clocks more** on top. The only
  class it has that the delay loop does not is `test_m`
  (`CMP`/`TEST r/m, imm`), charged 6 where that instruction costs the
  reference 9.00 in the `B5` loop — but **that 9.00 is not a class
  measurement**. `B5` adds a *five-byte* instruction to the delay loop, so
  its delta carries whatever instruction fetch costs as well, and this record
  does not separate fetch from a table entry. Under the fetch reading
  `test_m` could stay at 6 and the two extra clocks be fetch. So **no entry
  here is measurement-backed as a class value**: 6 → 8 is a fit, and 6 → 10
  is a further fit that also absorbs the order effect below, which no
  additive class entry can express at all.

With the delay loop at 18 and the calibration pass at 29, the kit's ratio is
0.621 against the reference's 0.6217, `N` falls from 27 to 22, the pass goes
to about 42.4 µs against the reference's 41.50, and the note lands about
**2% flat — inside the ±5.9% band**.

**And the 29 is a fit to the game's own loop.** Taking the `B5` delta at face
value instead — a `test_m` of 8 — charges the calibration pass 27 against the
reference's real 29.02, the ratio is 0.667, `N` falls only to 24, and the
note is about **9% flat, still outside the band**. The two clocks between
those two fits are the order effect below, which no additive per-class table
can express. So the table route reaches the band only by being fitted to this
one game's loop, which is a reason to settle the fetch question before
choosing any entry.

That is the shape of a fix, not a fix: it moves every preset and every title
and it is Deano's to weigh. **Nothing is changed in this PR, and the pitch
still fails #1368's band.**

Both entries are *undercharges*, which is also fes#1840's direction.

`governor.toml` gives `test_m` and `cmp_load` the same 6 clocks in the 286
column, from the compiled data-sheet table its header cites. The reference does not: 9.00 against
7.00. So this is not the table departing from its stated source — it is the
stated source departing from the machine the acceptance contract names.

## What this does not settle

**Why the reference charges more is not separated.** The `cmp` pair —
9.00 clocks for the five-byte immediate form, 7.00 for the four-byte register
form, same class of work and same accesses — is *consistent with* the model
having no instruction-fetch term, which it has not: the governor's ports
(`fes_i386_governor.sv:47-76`) carry its retirement flags, its `gov_t`, its
memory accesses and its I/O and MMIO. `gov_t` is
`{two, op[7:0], mreg[2:0], rm_reg, o32, rep, ea3}`
(`fes_i386_pkg.sv:589-597`): the opcode, the ModRM fields, the operand size,
a REP prefix and whether the address has base, index and displacement —
**no instruction address and no instruction length**.

But `decoded_t` carries `logic [3:0] length` immediately after its `gov_t`
(`fes_i386_pkg.sv:601-604`), so the length exists one level up and is simply
not passed down. **The governor is not given the length, though the length is
available where its `gov_t` is built.** Giving the cost model a fetch term is
therefore a one-field interface extension, not an architectural limit — a
wire, not a redesign — which is worth knowing to anyone weighing whether to
fund it. The right word is *does not*, not *cannot*. The nearest thing to a fetch term the model has
is the data sheets' `m`, which `governor.toml` takes as one throughout.

But it is **not separated from a table entry**, and the two tests that would
have separated them both came back null: the six-byte delay loop costs the
same at an odd start as at an even one (`BO` = `BD`), and so does the
eleven-byte, bus-bound `cmp` loop (`BQ` = `BE`). A rule predicting otherwise
was committed before run `c5` and is **falsified**; `method.md` reports it in
place rather than replacing it.

So this record **does not claim that fes#1840 shares this cause**, and does
not exclude it either. What would separate them is a cost model that can be
given an instruction's length, or a reference whose internals can be read.

**One thing no per-class table can reproduce.** The game's calibration loop
costs 29.02 clocks and the `BQ` loop 27.04, with the same three classes, the
same eleven bytes, the same three accesses and the same start parity — they
differ only in the order of their instructions (`inc` for `dec` and `je` for
`jne` are the same classes either way). The two costs come from different
methods — the calibration loop is counted against one tick, `BQ` in 1000-pass
blocks — and the kit is the control for that: there the same two methods read
24.02 and 24.04 clocks, 0.08% apart, so the method does not make 2 clocks.
An additive per-class table
gives both the same number, so the reference's cost is not an additive
function of instruction classes, and a table fitted to one of them is fitted,
not correct. The two entries named above are fitted to the game's loop.

**Not examined.** #1457's stray speaker bursts on a 58.6 ms grid: this work
did not look at them, and no port 61h/42h/43h trace was taken. The
reference's own cost for the lute pass's two port-61h accesses was not
measured separately, so the 2.37 points of overhead are not attributed to the
ISA charge beyond the size coincidence noted above.

**#1837's "reference scaled to 12.0 MHz" column is not valid** and is not
used here: under a tick-calibrated delay the pitch does not scale with the
clock, as the measured curve shows.
