# Why Ultima VI's lute is 2.4 semitones flat on the kit

fes#1837. The method, with every prediction and pass rule, is in
[`method.md`](method.md), committed before any capture and amended three
times, each amendment dated and saying what had been seen.

**The cause.** The game times its own delay loop against the BIOS tick at
start-up and then plays the lute from that calibration. The kit's cost model
charges the *calibration* loop and the *delay* loop at the wrong ratio
relative to the reference machine — 0.709 against 0.622 — so the calibrated
delay unit comes out about 14% too long and every note is flat by the same
factor. That is **10.7 of the 13.1 points**. The other **2.4** are the
per-pass overhead, essentially all of it #1453's ISA charge on port 61h,
which matches #1474's "about two points" exactly.

**So the fix is in the governor's clock table, not in the ISA charge model
and not in the CPU's instruction timing.** It is not applied here: it moves
every preset and every title, which is Deano's to weigh.

**The one number to check.** `cmp word [mem], imm` costs **9.00** clocks on
the reference and **7** on the kit, while `cmp ax, [mem]` costs **7.00** on
both — the same class of work, the same memory accesses, one byte more of
encoding. The first of those two instructions is in the game's calibration
loop and not in its delay loop.

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
the note at the same pitch. The two findings are not in tension and, on this
evidence, do not share a cause; see "What this does not settle" below.

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

`u6cal.asm` → `U6CAL.COM`, 678 bytes, NASM 2.16.03. It reports `C` (five
repeats), the game's own integer `CAL` and `N`, and block counts over 64
ticks for the delay loop (`BD`), that loop with five `nop`s (`BP`), with
`cmp word [m],0` in front (`B5`), with `cmp ax,[m]` in front (`B4`), and for
the lute pass at `N = 0` (`P0`) and at the calibrated `N` (`PN`). `u6cal2.asm`
adds `BO`, the delay loop at an odd start address. `analyse.py` turns the
counts into clocks; the tick period cancels from every ratio.

Binaries: `U6CAL.COM` `1aa15a91…` (runs `c3`, `c1cal`; run `c1` used the
earlier `d9dcc85f…`, which lacked `B5`/`B4`), `U6CAL2.COM` `2fac2445…`
(run `c4`).

## The runs

| run | machine | what |
| --- | --- | --- |
| `c1` | 86Box `award286`, Intel 286 at 12.5 MHz | reference, first probe |
| `c2` | the same | **failed**: `refbox.py` could not read the window geometry at the first grab; no figures taken |
| `c3` | the same | reference, amended probe |
| `c4` | the same | reference, `BO` |
| `c1cal` | Kit A, preset 1, sealed package `fc01c956…` | word 1 = 0, 80, 120, 160; three repeats each |

The reference is PR #1838's own VM (`vm/u6ref-286`), unchanged but for the
drive, which is that record's reference image plus `U6CAL.COM`. The kit ran
core-only on its baseline software `c9ae79e2` and image `179affbb…`, claimed
and released in `kitA-STATUS.md`, with no install, image change or reboot.
Every kit load was verified from `runs/c1cal/result.json` and `acks.jsonl`.

`run-u6cal.sh` is `run-u6ref.sh` with one duplicate idleness check — the
kit-local `"phase":"idle"`, which Kit A's bundle does not serve — replaced by
the host API's own `"state":"idle"`, the source the next check already uses.
Every other baseline, lease and restore check is unchanged.

## What the probe measured

