# pc386 PS/2 FIFO area: method (committed before the board fits)

Issue #2108. This is the shell area study's **A-fifo** cut (#2047,
[`docs/validation/2026-11-20-de25-shell-area/`](../2026-11-20-de25-shell-area/README.md)).
There it measured 170 ALMs (spread 69), as an ablation against the 387-on envelope.

## The change

`pc386_ps2_fifo` (`pc386_kbc.sv`) is the 16-entry FIFO that holds bytes from
the keyboard and from the mouse on their way to the 8042.
- **DE10, unchanged:** it writes its five arrays from two places, entry 0 on a
  clear with a push and `write_at` otherwise. Yosys and Quartus therefore see
  two write ports, so each array becomes registers with two write decoders.
- **DE25:** `boards/de25nano/pc386/pc386_ps2_fifo_mlab.sv` has one write port.
  The clear's push and a normal push share one address (`clear ? 0 : write_at`)
  and one enable. The arrays carry `ramstyle = "MLAB"` and are read
  asynchronously, exactly as before. `derive.py` retargets `pc386_kbc`'s two
  FIFO instances to it for the DE25 build only, and pins the DE10 module by its
  SHA-256, so a change to the DE10 FIFO stops the derivation.

There is no parameter. The shared RTL is #2036's, so the DE10 pc386 netlist and
`BUILD_ID` do not move.

**Why a derivation (Deano, 23 Nov 2026).** The first form edited the shared
`pc386_kbc.sv`. Its DE10 reseal, the registered producer with a fresh lockstep at
`00f4dd621`, seed 1, passed the lockstep but missed setup: worst slack
−0.135 ns, TNS −0.399 ns, on `video_clock` in the Slow 1100mV −40C model.
`system_clock`, where the FIFO is, had +0.952 ns there. Deano chose the DE25-only
derivation, the same pattern as #2118, over trying other seeds. No other seed
was run. That reseal is superseded and is recorded in
[de10-reseal-00f4dd621.md](de10-reseal-00f4dd621.md).

## What counts as behaviour-preserving

The DE25 FIFO must be **cycle-exact at its ports** with the DE10 one. In every
clock, each output must be equal: `valid`, `head`, `head_last`,
`head_key_bit`, `head_key_make`, `head_key_repeat` and `space`. That holds
while the FIFO is empty too, because the head outputs are asynchronous reads of
whatever the array holds.

**The proof is a differential bench, [diff/](diff/).**
- The DE10 `pc386_ps2_fifo`, taken from `pc386_kbc.sv`, runs beside
  `pc386_ps2_fifo_mlab` on the same inputs.
- The bench checks every output twice per clock:
  - after the inputs change, which covers the asynchronous reads;
  - after the edge.
- The traffic is random: clear (with and without a push), push, pop and every
  data field. Phases bias towards filling or draining the FIFO, so pushes
  while it is full and pops while it is empty are frequent. Each seed reports
  how many of each it saw.
- **RTL:** RTL against RTL, 16 seeds × 2M clocks.
- **Netlist:** the DE10 RTL against the netlist that the seal's Yosys recipe
  (`yosys_netlist.SEAL`, the pc386 lock's Yosys, `d273c1cb` after #2130) writes
  for `pc386_ps2_fifo_mlab`, 16 seeds × 2M clocks.
- **Mutation check:** [diff/mutate.sh](diff/mutate.sh) plants five bugs, one in
  each new piece of logic: the clear's address, the full check, the clear's push,
  and two data fields. The bench must fail on each one.

**Derivation tests:** `tests/test_de25_pc386_ps2_fifo_derive.py`. The DE10
source keeps its FIFO, the DE25 `pc386_kbc` uses the MLAB one at both instances,
and a changed DE10 module or a moved instance is refused.

**DE10 identity:** [`de10_netlist.py`](de10_netlist.py) writes the DE10 seal's
machine netlist from one checkout path at #2036's head and at this branch, and
`cmp` must find them identical (the fixed Yosys lock, one thread).

**DE25 system:** `boards/de25nano/pc386/sim.py` must reach the prompt with check
lines identical to #2036's head. Result: [suites/](suites/README.md), the two outputs are byte-identical.

## The DE25 measurement

**Board:** the 387-off DE25 pc386 board build from #2036
(`boards/de25nano/pc386/build.py build`).

**Arms:**
- **Baseline:** #2036's head at the time of the fits, with the fixed Yosys
  lock. The baseline is shared with #2117 (S-lat) and #2118 (A-dma2).
- **Arm:** the baseline plus this PR's derivation (`derive.py`, `build.py`,
  `pc386_ps2_fifo_mlab.sv`). Nothing else changes.

**Seeds:**
- Seeds 1–4 per arm.
- Seed 1 is the board build. Seeds 2–4 are copies of its generated project with
  only `SEED` changed, as in #2036's pair and #2077.

**Lease and tools:**
- Quartus Prime Pro 26.3, A5EB013BB23BE4SCS.
- Every Quartus run holds a Pro slot of the Quartus lease. A seed's
  `quartus_sh` runs under `quartus_jobs.py run --edition pro`, and a slot is
  taken per seed.

**Netlist identity:** each arm's seeds 2–4 compile the exact machine netlist
bytes (`fes_mailbox/pc386_system.v`) that its seed 1 staged. The run records
their SHA-256.

**Recorded pairs wait for the fixed Yosys lock on main.** Earlier builds are
exploratory, and they are not counted.

## The quantity

- **ALMs:** whole-design `ALMs needed`, the median over four seeds per arm.
- **Saving:** the baseline median minus the arm median.
- **Measured** only when the saving exceeds the larger of the two arms' spreads
  (max − min). Otherwise the result is reported as "within the spread". No other
  gate is added after the results are seen.
- **Bands:** area bands are one-sided. The arm fails only if it grows.
- **Also reported:** RAM blocks (M20K), MLAB memory ALMs and DSPs.

## Timing

- Report the worst setup, hold, recovery, removal and minimum pulse width over
  every clock, for every seed of both arms, at 61.437908 MHz.
- Every seed of both arms must close, with every slack ≥ 0. A seed that misses
  stops the work and is reported. No timing check is relaxed.

## DE10

The DE10 pc386 netlist does not move (DE10 identity, above), so the DE10 needs
no reseal and no kit run.
