# x87 slice 6.3: FBLD on the engine, and the fast lane as one stage

Slice 6.3 of the x87 slice 6 plan
([PR #2027](https://github.com/DeanoC/fes/pull/2027)) follows the first
milestone ([PR #2037](https://github.com/DeanoC/fes/pull/2037)).

**Deano's decision (21 November 2026, relayed by the coordinating session):**
the first DE25 pc386 shell ships with the 387 off. 387-on on the DE25 is a
later step, once about 3,500 ALMs are found across the shell, the devices and
the x87. Under that decision this milestone builds the two x87-internal
reductions the bounds supported.

The method and its bands were committed before any fit: [method.md](method.md),
with amendment 2 for this build. Every figure is in [results.md](results.md),
which [generate.py](generate.py) writes from the committed evidence. This
file quotes those figures and adds none of its own.

## What was done

- **FBLD runs on the transcendental engine** (`b4213b4d6`).
  - **How it converts:** each of its eighteen digits is a table constant,
    digit × 10^k.
    - The table holds 288 entries in the engine's constant ROM: sixteen a
      place, so digits A to F convert as the host x87 and the oracle do.
    - A new constant mode takes the digit itself as the low four address bits.
  - **Exactness:** the sum is exact, so the unit takes it as the loaded value
    with no rounding.
  - **Deleted:** the envelope's FBLD accumulator (`bl_acc`, `bl_dig`, `bl_n`),
    its two states, and the load path's packed-decimal branch.
  - **FBSTP stays in the envelope.** The microprogram ROM cannot hold it.
- **The fast lane is one registered stage in `fes_i386_x87`** (`d74d8d419`).
  - **One datapath:** `fes_x87_fast`'s datapath is written once, with one
    block a stage.
  - **The parameter:** `PIPELINED` (on `fes_x87_fast` and `fes_x87_core`)
    chooses registers or wires at its six stage boundaries.
    - `fes_i386_x87` and the engine bench build `PIPELINED = 0`; the unit has
      one operation in flight.
    - `fes_x87_pipe` keeps `PIPELINED = 1`, since #1892's standalone backend
      needs the throughput.
  - **This spends the clock headroom:** a later DE25 clock raise needs the
    pipelining back.
- **Benches:**
  - The engine bench gains 800 FBLD vectors.
  - The unit bench counts an FBLD conversion as an engine run.
  - Both now build the fast lane the way the unit does.
- **`elaborate.py`** imports only `scripts.pc386_quartus`. The fallback for
  #2037's tree is deleted.

## What it measured

| What | Result |
| --- | --- |
| **The baseline** | Main `b206f9aaf` (with #2039) did not elaborate to #2037's head netlist, so it was refitted: median 42,688.0 ALMs, spread 90. |
| **FBLD alone** | **262.5 ALMs saved**, measurable against a 126 spread. One-sided pass. |
| **The head (both changes)** | **460.5 ALMs saved** (median 42,227.5, spread 49). One-sided pass; every seed passes timing at 61.44 MHz. |
| **The fast lane's share** | 198.0 (FBLD alone less the head), close to its bound of 191.0. |
| **Against #2037's head** | 349.0 ALMs below. |
| **Clock headroom** | The head's worst setup at 61.44 MHz is +0.387 to +2.361 ns, against the baseline's +7.505 to +8.044 ns. |
| **RAM and DSP blocks** | Unchanged: 145 RAM blocks and 42 DSP, so a board would still have about 43 RAM blocks free. |

**The remaining 387-on gap on the board:** 49,955.5 ALMs with the measured
7,728-ALM shell (8,400 estimate in brackets):

- **+6,899.5** over the 92% line (+7,571.5);
- **+3,155.5** over the device (+3,827.5).

### The forms' clocks

**The fast lane is faster.**
- **The forms:** FADD, FSUB and FMUL take 19 → 13 clocks on the unit bench,
  and FRNDINT 57 → 45.
- **The transcendentals:** these drive the fast lane too, and run 25–35%
  fewer engine clocks ([evidence/engine-clocks.md](evidence/engine-clocks.md)).
- **At the census rates:** about 2.8 M clocks a second fewer in Quake's
  window (4.6% of the machine) and 0.84 M in Chris's 3D Benchmark (1.4%).

**FBLD is slower** and no census title runs it.
- **On the engine:** 163 clocks from engine start.
- **In the envelope before:** 18 accumulate steps, then the normalising shift.

## The gates, at the head `d74d8d419`

| Gate | Result |
| --- | --- |
| Engine bench, the RTL against its model | 23,795 of 23,795, including 800 FBLD ([evidence/engine.json](evidence/engine.json)) |
| ... a deliberate fault in FBLD's digit select | fails all 800 FBLD vectors; removed, and not in the code |
| Unit bench against the host x87, RTL | 18,337 of 18,337 ([evidence/unit-rtl.json](evidence/unit-rtl.json)) |
| ... on the locked-Yosys netlist | 18,337 of 18,337 ([evidence/unit-netlist.json](evidence/unit-netlist.json)) |
| `fes_x87_pipe`'s own bench (`make sim-fes-de25-x87`), still pipelined | 117,238 pass, 0 fail |
| Lockstep against Bochs, default suite, RTL | pass, `default_suite` true ([evidence/lockstep-rtl.json](evidence/lockstep-rtl.json)) |
| ... on the locked-Yosys netlist | pass, `default_suite` true ([evidence/lockstep-netlist.json](evidence/lockstep-netlist.json)) |
| test386 | POST FFh, halt ([evidence/test386.txt](evidence/test386.txt)) |
| Quartus 17.0.2 seal, DE10, identity off (registered producer, `--with-lockstep`, seed 1, CUDA slot) | passes every analysis: setup +0.717 ns, hold +0.118 ns; 36,459 of 41,910 ALMs, 41 DSP, 282 RAM blocks; BUILD_ID `1569d6b6`, package `0323622b…` ([seal/](seal/)) |
| The head still builds the sealed design | [head_matches_seal.py](head_matches_seal.py) against [seal-build-inputs.json](seal-build-inputs.json): SAME DESIGN, BUILD_ID `1569d6b6` |
| Kit B, from that seal | pass: the FreeDOS prompt, then Wolfenstein 3D drawn and walked; the kit restored to its baseline, idle and lease-free ([kit-b-k63a-result.json](kit-b-k63a-result.json)) |

## Caveats

- **Two hosts.**
  - **Chip-30x0-ai-dev:** every netlist was elaborated there, and the
    baseline and three FBLD-alone seeds were fitted there.
  - **Powerboat:** the other five fits ran there. They used the same Quartus
    Pro build on the same netlist bytes, after chip's shared lease went to
    other sessions' jobs.
- **FBLD's figure is the engine's alone.** The envelope's old FBLD time is
  given only as its states, not measured.
- **Unit-bench latencies are per case:** they measure the path, not what a
  title pays.
- **Virtual pins, not a shell.** The board rows are arithmetic on the measured
  shell, not a fit with one.
