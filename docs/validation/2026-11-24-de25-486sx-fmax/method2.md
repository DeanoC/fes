# Ladder 2: above 111.90 MHz (method, committed before any of its fits)

[method.md](method.md)'s ladder topped out: every rung's seed 1 passed and
111.904762 MHz (C0 = 28, its top rung) closed on four seeds. That is a floor,
and the task asks for the first step that fails and what limits it. method.md
is not amended: this is a second ladder with its own rungs, committed and
pushed before any of its fits is queued. No rung, seed or rule here changes
after one of its fits has output.

## What had been seen

Every result in [summary.md](summary.md) at `138f6e238`: ladder 1's seven
seed-1 fits and the four 111.90 MHz seeds (worst setup +0.846 to +1.120 ns,
the timing analyser's machine-clock Fmax estimates 123.61–127.94 MHz), the
envelope fits (CPU alone: 125.333 MHz two of four seeds, 120.513 MHz four of
four; CPU with the machine: 125.333 MHz four of four), and the producer's
refusal at every rung. Also seen, not yet in the record: the reviewed signoff
basis `LOADED` (`boards/de25nano/pc386/signoff_bases.py`) argues from a
loaded-row report that is still for at least five machine clocks against a
50.4 ns need, which holds only while 5T ≥ 50.4 ns (machine clock at most
99.2 MHz).

## The rungs

| Rung | C0 | Machine MHz | Period ns |
| ---: | ---: | ---: | ---: |
| 7 | 27 | 116.049383 | 8.617 |
| 8 | 26 | 120.512821 | 8.298 |
| 9 | 25 | 125.333333 | 7.979 |
| 10 | 24 | 130.555556 | 7.660 |

Rung 10 is above the highest Fmax estimate seen (127.94 MHz), so the ladder
cannot top out on an estimate alone. Each rung is a branch
`deano/de25-486sx-fmax-cNN` made by `make_rung.py` on the method commit
`c1ca3e31c`, as ladder 1's were.

## How each fit runs

Exactly as ladder 1 under method.md's amendment 1: `fitrung.sh REV 1`
generates the rung's project through the producer (which is expected to stop
at the shared FIFO reset gate, and whose exit and refusal line are recorded),
then `compile_rung.sh REV SEED…` compiles the control's generated project
with only the rung's PLL IP and netlist swapped in, then `paths.tcl`. Same
lease, caps, host and pass rule (every row of the timing summary ≥ 0 at every
corner, ALMs ≤ 46,800).

## The rule

1. Seed 1 runs at every rung of this ladder, whatever the lower rungs show.
2. Take the highest rung R, over both ladders, at which seed 1 passes and
   every lower rung of both ladders also passed at seed 1. If R is above
   111.90 MHz, seeds 2–4 run at R; if all pass, the machine closes at R on
   four seeds. If any misses, seeds 2–4 run once at the next lower rung, and
   the claim is that rung if all three pass there (at 111.90 MHz they already
   have). The ladder then stops.
3. **The first failing step** is the lowest rung of this ladder whose seed 1
   fails. Seeds 2–4 also run there, so its limit is read from four seeds, not
   one. If no rung of this ladder fails, that is the result and no rung is
   added.
4. A seed 1 pass above a failing rung is reported and is not a closure.

## What is reported, in addition to method.md's quantities

- **Q2 at the first failing step:** every negative-slack path of each seed by
  analysis, clock pair and owner (resolved as `rung_summary.py` does, with the
  unresolved counts kept), and the worst paths named.
- **Signoff rules:** for every kept project of both ladders, the rule ID and
  violation count of `golden_top.tq.drc.signoff.rpt` (the design assistant's
  signoff report), against the control's. The producer's reviewed-findings
  verdict is not claimed for any rung; a rule or count that differs from the
  control's is what breaks.
- **Period-tied bases:** for every rung of both ladders, each reviewed basis
  whose argument names the machine period (`LOADED`: 5T against 50.4 ns;
  `FETCH` and `WINDOW_BASE`: 2T against the 10 ns bound), computed, not
  measured, with the rungs at which its argument no longer holds as written.
