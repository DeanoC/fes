# pc386 PS/2 FIFO area: method (committed before the board fits)

Issue #2108. This is the shell area study's **A-fifo** cut (#2047,
[`docs/validation/2026-11-20-de25-shell-area/`](../2026-11-20-de25-shell-area/README.md)).
There it measured 170 ALMs (spread 69), as an ablation against the 387-on envelope.

## The change

`pc386_ps2_fifo` (`pc386_kbc.sv`) is the 16-entry FIFO that holds bytes from
the keyboard and from the mouse on their way to the 8042.
- **Before:** it wrote its five arrays from two places, entry 0 on a clear with a
  push and `write_at` otherwise. Yosys and Quartus therefore saw two write ports,
  so each array became registers with two write decoders.
- **After:** one write port. The clear's push and a normal push share one address
  (`clear ? 0 : write_at`) and one enable. The arrays carry
  `ramstyle = "MLAB"` and are read asynchronously, exactly as before.

There is no parameter selecting the old form. The DE10 build takes the same RTL,
so its pc386 netlist and `BUILD_ID` move. CLAUDE.md forbids keeping the old form
as a compatibility branch.

## What counts as behaviour-preserving

The FIFO must be **cycle-exact at its ports**. In every clock, each output must
equal the base commit's: `valid`, `head`, `head_last`, `head_key_bit`,
`head_key_make`, `head_key_repeat` and `space`. That holds while the FIFO is
empty too, because the head outputs are asynchronous reads of whatever the array
holds.

**The proof is a differential bench, [diff/](diff/).**
- The base commit's FIFO, renamed `pc386_ps2_fifo_ref`, runs beside this
  branch's on the same inputs.
- The bench checks every output twice per clock:
  - after the inputs change, which covers the asynchronous reads;
  - after the edge.
- The traffic is random: clear (with and without a push), push, pop and every
  data field. Phases bias towards filling or draining the FIFO, so pushes
  while it is full and pops while it is empty are frequent. Each seed reports
  how many of each it saw.
- **RTL:** RTL against RTL, 16 seeds × 2M clocks.
- **Netlist:** the base RTL against the netlist that the seal's Yosys recipe
  (`yosys_netlist.SEAL`, the locked pc386 Yosys) writes for this branch's FIFO,
  16 seeds × 2M clocks.
  - Until the fixed Yosys lock lands, Yosys runs with `YOSYS_MAX_THREADS=1`
    (DeanoC/yosys#81).
- **Mutation check:** [diff/mutate.sh](diff/mutate.sh) plants five bugs, one in
  each new piece of logic: the clear's address, the full check, the clear's push,
  and two data fields. The bench must fail on each one.

**The existing suites must pass:**
- `sim_fes_pc386.py --suite devices`.
- `--suite system`, reaching the same cycle as the base commit.

## The DE25 measurement

**Board:** the 387-off DE25 pc386 board build from #2036
(`boards/de25nano/pc386/build.py build`).

**Arms:**
- **Baseline:** #2036's head at the time of the fits, with the fixed Yosys
  lock. The baseline is shared with the shell-study S-lat pair if that cut is
  built.
- **Arm:** the baseline plus this PR's `pc386_kbc.sv`. Nothing else changes.

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

- The DE10 pc386 netlist moves. The registered producer reseals with a fresh
  lockstep, and the seal must pass every analysis at 61.44 MHz.
- A claimed kit run follows: the kit is claimed and restored per CLAUDE.md, and
  the run goes to the FreeDOS prompt with keyboard input. Mouse input is
  exercised where a title or program uses it.
