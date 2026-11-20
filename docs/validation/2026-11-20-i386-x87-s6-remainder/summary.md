# x87 slice 6.2, first milestone: the remainder ops on the engine, and `X87_BACKEND = 0` deleted

The first milestone of 6.2 in the x87 slice 6 plan
([PR #2027](https://github.com/DeanoC/fes/pull/2027)), as Deano directed on
20 November 2026 (relayed by the coordinating session):

- **D5:** FPREM, FPREM1, FRNDINT, FSCALE and FXTRACT move onto the DE25
  backend.
- **D6:** the sequencer arithmetic is deleted outright.
- **Judged on area** by step 6.0's method
  ([PR #2028](https://github.com/DeanoC/fes/pull/2028)).

The method and its bands were committed before any fit:
[method.md](method.md). Every figure is in [results.md](results.md), which
[generate.py](generate.py) writes from the committed evidence. This file
quotes them and adds no figure of its own.

## What was done

- **The five forms run on the transcendental engine, `fes_x87_trans`.**
  - **FPREM and FPREM1** reuse the pi/4 reduction's divider, one bit a
    clock, with y's significand as the divisor (doubled where the exponent
    difference is −1). FPREM1's round-to-nearest shares the reduction's
    P − r subtractor.
  - **FRNDINT** adds x ± 2^63 rounded under RC (a new `ADDRC`), then
    subtracts the offset exactly. The engine reports PE and C1.
  - **FSCALE** truncates y to a saturated scale (`SETE TRUNC`), then returns
    x through a new `FIN X` kind, which carries a denormal's exponent.
  - **FXTRACT** is `SETE XE`, `ITOF`, and a `FIN` with the significand as
    its second result.
  - The microprogram's new words and constants are appended, so every
    transcendental word and ROM address is unchanged.
- **`X87_BACKEND = 0` is deleted, with no shim.**
  - **Gone:** the sequenced add and multiply, the Goldschmidt divide, the
    square root, the remainder and the FRNDINT/FSCALE/FXTRACT arms, with
    their seed tables, registers and helpers.
  - **Kept:** the multiplier, now only as the shifter.
  - **The parameter:** removed from the unit, the decoder and the CPU, and
    the `de25` argument from `x87_format` and `x87_decode`.
  - **The tests:** the runners' `--backend`, the benches' not-built paths,
    and `x87_envelope_fit.py` with its timing script and test are gone.
- **The DE10 default is explicit.** One arithmetic remains. The DE10 builds
  the same unit with the identity off: `pc386_system` ties `x87_present` to
  `1'b0`, and the unit folds.
- **The lockstep's transcendental bound defaults to the agreed 2^21.** So
  the default suite, which the sealer requires, runs the transcendentals.
  Before, it ran none.

## What it measured

- **The bound, by ablation:** removing the five forms' arms frees **750.5
  ALMs**, against a 138-ALM spread. So the move was built (part 1).
- **The DE10 fold check:** D1 is **236 ALMs smaller** than D0, with equal
  DSP, RAM blocks and timing.
  - The ±200 rule as committed calls that a miss, and it is recorded as one.
  - The deletion proceeds on Deano's acceptance, not on the rule.
  - He has since made every area band one-sided: only growth fails (part 2).
- **The move, on the DE25:** 387.5 ALMs below 6.0a, with RAM blocks and DSP
  unchanged. It passes one-sided (part 3).
- **The head as it ships (the move and the deletion together): 472.0 ALMs
  below 6.0a**, a median of 42,576.5, with RAM blocks and DSP unchanged. It
  passes one-sided (part 5).
- **The real board, with a shell:** against an 8,400-ALM shell (#2036's
  RTL-arbiter figure, one seed), the machine's gap to the 92% line goes from
  +7,741.5 to **+7,269.5 ALMs**. results.md gives the same for the 6,600 and
  12,100 bounds.
- **RAM blocks:** the move costs none. That is stated against the 58 the
  real DE25 board has left.
- **The forms' clocks:**
  - FRNDINT goes from 22 to 57 clocks, FSCALE from 15 to 23 and FPREM from
    17 to 86, on the unit bench's latency cases.
  - At the census rates FRNDINT's change is under 0.1% of a 61.44 MHz
    machine for Screamer, the heaviest user.
  - No title executes FPREM, FPREM1 or FXTRACT.

## The gates, at the head `4f1546783`

| Gate | Result |
| --- | --- |
| Engine bench, the RTL against its model | pass ([evidence/engine.json](evidence/engine.json)) |
| Unit bench against the host x87, RTL | 18,337 of 18,337 ([evidence/unit-rtl.json](evidence/unit-rtl.json)) |
| ... on the locked-Yosys netlist | 18,337 of 18,337 ([evidence/unit-netlist.json](evidence/unit-netlist.json)) |
| Lockstep against Bochs, default suite, RTL | pass, `default_suite` true ([evidence/lockstep-rtl.json](evidence/lockstep-rtl.json)) |
| ... on the locked-Yosys netlist | pass, `default_suite` true ([evidence/lockstep-netlist.json](evidence/lockstep-netlist.json)) |
| test386 | POST FFh, halt ([evidence/test386.txt](evidence/test386.txt)) |
| Quartus 17.0.2 seal, DE10, identity off (registered producer, `--with-lockstep`, seed 1, CUDA slot) | passes every analysis: setup +0.393 ns, hold +0.085 ns; 36,446 of 41,910 ALMs, 41 DSP, 282 RAM blocks; BUILD_ID `1f328c9d`, package `06db2433…` ([seal/](seal/)) |
| Kit B, from that seal | pass: the FreeDOS prompt, then Wolfenstein 3D drawn and walked; the kit restored to its baseline, idle and lease-free ([kit-b-k2037b-result.json](kit-b-k2037b-result.json)) |

**Three deliberate faults were caught.** Before trusting the unit bench on
the new paths, three faults were put into the engine one at a time:

- `ADDRC` ignoring RC: 83 failures;
- FSCALE's truncation one place off: 116 failures;
- `FIN X` dropping x's sign: 52 failures.

The deliberate faults were then removed, and none is in the code.

## Caveats

- **Two different elaborations.** The A/B fits were elaborated on
  chip-30x0-ai-dev by the CUDA-slot Yosys, as step 6.0's were. The seal used
  the registered producer's own netlist.
- **The move's base.** The move's fits are of main `9951d04f0` and 6.0a's
  of `8a162c536`. They differ by #2034, which changes build scripts and no
  design input.
- **Unit-bench latencies are per case.** Each figure is one case a form and
  precision, so it measures the path, not what a title pays.
- **Virtual pins, not a shell.** The DE25 fits still use virtual pins. The
  shell rows in results.md are arithmetic on stated figures, not a fit with
  a shell.
