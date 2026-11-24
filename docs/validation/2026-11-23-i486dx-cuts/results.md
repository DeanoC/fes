# Results (written by generate.py from the committed evidence; do not edit)

## A. Area: `fes_i386_cpu` on Agilex 5 (A5EB013BB23BE4SCS, Quartus Pro 26.3, 61.44 MHz, virtual pins)

`dx` is the plan record's three seeds, copied unchanged:

| Seed | ALMs needed | DSP | M20K | Fit | Worst-corner Fmax estimate (MHz) |
| ---: | ---: | ---: | ---: | --- | ---: |
| 1 | 34,651 | 22 | 60 | pass | 72.62 |
| 2 | 34,606 | 22 | 60 | pass | 63.76 |
| 3 | 34,706 | 22 | 60 | pass | 63.40 |

Median 34,651, spread 100.

| Variant | What it changes | ALMs, seeds 1 / 2 / 3 | Median | Spread | Band N | Saving (medians, rule 1) | Saving at seed 1 | Verdict | DSP Δ | M20K Δ | Fmax estimates (MHz) | Correctness (B) |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | --- | --- |
| `l1i2` | L1I at 8 KiB: 2-way, 128 sets | 34,152 / 34,090 / 34,098 | 34,098 | 62 | 100 | +553 | +499 | saving | +0 | -12 | 62.31 / 63.05 / 64.65 | passes |
| `l1d2` | L1D at 8 KiB: 2-way, 128 sets | 33,990 / 34,008 / 33,995 | 33,995 | 18 | 100 | +656 | +661 | saving | +0 | -8 | 71.84 / 62.50 / 71.05 | passes |
| `l12` | `l1i2` + `l1d2` | 33,475 / 33,497 / 33,416 | 33,475 | 81 | 100 | +1,176 | +1,176 | saving | +0 | -20 | 63.25 / 64.82 / 63.90 | passes |
| `xshift` | the envelope's multiplier as a serial shifter | 34,582 / 34,527 / 34,541 | 34,541 | 55 | 100 | +110 | +69 | saving | -9 | +0 | 70.85 / 64.55 / 63.85 | passes |
| `xdisp` | the x87 core's `dispatch` register removed | 34,660 / 34,681 / 34,625 | 34,660 | 56 | 100 | -9 | -9 | under noise | +0 | +0 | 70.06 / 63.42 / 70.03 | passes |
| `x87` | `xshift` + `xdisp` | 34,515 / 34,503 / 34,520 | 34,515 | 17 | 100 | +136 | +136 | saving | -9 | +0 | 65.07 / 63.77 / 63.88 | passes |
| `cuts` | `l12` + `xshift` + `xdisp` + round 1's `lfwd` | 33,016 / 32,994 / 32,966 | 32,994 | 50 | 100 | +1,657 | +1,635 | saving | -9 | -20 | 63.64 / 62.75 / 63.51 | passes |

DSP and M20K deltas are the same at every seed of a variant (one netlist); they are not converted into ALMs (rule 4).

**Rule 3, combined fits against their parts:**

- `l12` saves +1,176; `l1i2` +553 + `l1d2` +656 sum to +1,209 (difference -33).
- `x87` saves +136; `xshift` +110 + `xdisp` -9 sum to +101 (difference +35).
- `cuts` saves +1,657; `l12` +1,176 + `xshift` +110 + `xdisp` -9 + `lfwd` +360 (round 1) sum to +1,637 (difference +20).

**Rule 5, the budget:** `cuts` saves 1,657 against the plan's 1,221 ALMs over the device; it closes the gap first-order, with 436 to spare. This is a CPU-envelope saving set against machine figures, not a fit of the DX.

**Rule 6, `xdisp`'s timing:** its estimates are 70.06, 63.42, 70.03 MHz against `dx`'s lowest, 63.4 MHz: none below it. Timing is reported, not judged.

## B. Correctness

### Lockstep (`--random-programs 500 --random-seeds 8 --limit-per-file 20`)

| Variant | Exit | New failures beyond `base` | Verdict |
| --- | ---: | --- | --- |
| `base` | 0 | (the plan record's run; failures: 0) | reference |
| `l1i2` | 0 | none | passes |
| `l1d2` | 0 | none | passes |
| `l12` | 0 | none | passes |
| `xshift` | 0 | none | passes |
| `xdisp` | 0 | none | passes |
| `x87` | 0 | none | passes |
| `cuts` | 0 | none | passes |
| `qhalf` | 1 | FAIL flag hold: 3 after_shift retirements 2 cycles apart; FAIL stack hold: 1 stack_stack retirements 2 cycles apart | FAILS (rule B) |

`qhalf`'s suites, each line's pass and fail counts against `base`'s (the hold checks above are the run's only failures):

| Suite | `base` pass | `base` fail | `qhalf` pass | `qhalf` fail |
| --- | ---: | ---: | ---: | ---: |
| corpus | 18,820 | 0 | 18,820 | 0 |
| random | 4,000 | 0 | 4,000 | 0 |
| protected mode | 4,000 | 0 | 4,000 | 0 |
| task switches | 2,000 | 0 | 2,000 | 0 |
| breakpoints and single step | 2,000 | 0 | 2,000 | 0 |
| undefined encodings | 2,000 | 0 | 2,000 | 0 |
| BIU DMA/store | 1 | 0 | 1 | 0 |
| reset-store-on-reset | 1 | 0 | 1 | 0 |
| directed cases | 84 | 0 | 84 | 0 |
| unreal-mode cases | 7 | 0 | 7 | 0 |
| protected-mode cases | 726 | 0 | 726 | 0 |
| paging cases | 170 | 0 | 170 | 0 |
| x87 cases (the identity on) | 125 | 0 | 125 | 0 |
| x87 BCD cases | 28 | 0 | 28 | 0 |
| x87 transcendental cases | 181 | 0 | 181 | 0 |
| x87 built forms, the bench's copy against the RTL's over 2048 opcodes | — | 0 | — | 0 |
| no-387 cases | 1,933 | 0 | 1,933 | 0 |
| memory cases (DMA, FLUSH) | 27 | 0 | 27 | 0 |

### The x87 unit bench (`scripts/test_fes_i386_x87_unit.py --download`)

| Build | Exit | Summary |
| --- | ---: | --- |
| `base` | 0 | fes_i386_x87 against the host x87: 18337 passed, 0 failed [packed decimal: 61 cases, FBLD 6845 issued 6842 converted, FBSTP 13371 issued 11618 converted] |
| `xshift` | 0 | fes_i386_x87 against the host x87: 18337 passed, 0 failed [packed decimal: 61 cases, FBLD 6845 issued 6842 converted, FBSTP 13371 issued 11598 converted] |
| `xdisp` | 0 | fes_i386_x87 against the host x87: 18337 passed, 0 failed [packed decimal: 61 cases, FBLD 6845 issued 6842 converted, FBSTP 13371 issued 11618 converted] |
| `x87` | 0 | fes_i386_x87 against the host x87: 18337 passed, 0 failed [packed decimal: 61 cases, FBLD 6845 issued 6842 converted, FBSTP 13371 issued 11598 converted] |
| `cuts` | 0 | fes_i386_x87 against the host x87: 18337 passed, 0 failed [packed decimal: 61 cases, FBLD 6845 issued 6842 converted, FBSTP 13371 issued 11598 converted] |

Rule B compares the pass and fail counts (the text before the bracket). The bracket's FBSTP "converted" count differs between runs of the bench.

## C. CPI

The bench charges port 0's first data 18 clocks against the kit's 11, so the L1 variants' losses are upper bounds (method, section C).

### C1. DOOM v1.9 `-timedemo demo3`, handover at page flip 40, window h2

| Variant | Window start cycle | Clocks | Retired | CPI | CPI / `base` | Saving (ALMs) | CPI loss per 100 ALMs saved | Section B |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `base` | 2,543,936,063 | 50,000,000 | 29,983,986 | 1.668 | 1.000 | — | — | reference |
| `l1i2` | 2,543,936,063 | 50,000,000 | 29,706,069 | 1.683 | 1.009 | +553 | 0.17% | passes |
| `l1d2` | 2,543,936,063 | 50,000,000 | 28,933,381 | 1.728 | 1.036 | +656 | 0.55% | passes |
| `l12` | 2,543,936,063 | 50,000,000 | 28,654,555 | 1.745 | 1.046 | +1,176 | 0.39% | passes |
| `qhalf` | 2,543,936,063 | 50,000,000 | 20,326,010 | 2.460 | 1.475 | — | — | FAILS lockstep (rule B) |
| `cuts` | 2,543,936,063 | 50,000,000 | 28,333,925 | 1.765 | 1.058 | +1,657 | 0.35% | passes |

**The governor bound** (2,062 × r against the presets' realtics):

| Variant | r | Bound | DX2-66 (2,912) | DX-33 (4,682) |
| --- | ---: | ---: | --- | --- |
| `l1i2` | 1.009 | 2,081 | kept by the bound | kept by the bound |
| `l1d2` | 1.036 | 2,137 | kept by the bound | kept by the bound |
| `l12` | 1.046 | 2,158 | kept by the bound | kept by the bound |
| `qhalf` | 1.475 | 3,042 | not settled by the bound | kept by the bound |
| `cuts` | 1.058 | 2,182 | kept by the bound | kept by the bound |

The x87 variants (`xshift`, `xdisp`, `x87`): not applicable (DOOM issues no x87 instruction).

### C2. 3DBench 1.0, RTL from 7.5 s, window 8.0–11.5 s (amendment 1)

| Variant | Window start cycle | Clocks | Retired | CPI | CPI / `base` | Saving (ALMs) | CPI loss per 100 ALMs saved | Section B |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `base` | 800,000,005 | 350,000,000 | 162,482,821 | 2.154 | 1.000 | — | — | reference |
| `l1i2` | 800,000,005 | 350,000,000 | 161,856,648 | 2.162 | 1.004 | +553 | 0.07% | passes |
| `l1d2` | 800,000,005 | 350,000,000 | 160,388,137 | 2.182 | 1.013 | +656 | 0.20% | passes |
| `l12` | 800,000,005 | 350,000,000 | 159,902,194 | 2.189 | 1.016 | +1,176 | 0.14% | passes |
| `qhalf` | 800,000,005 | 350,000,000 | 123,452,490 | 2.835 | 1.316 | — | — | FAILS lockstep (rule B) |
| `cuts` | 800,000,005 | 350,000,000 | 159,902,194 | 2.189 | 1.016 | +1,657 | 0.10% | passes |

`cuts`'s 3DBench window is identical to `l12`'s in every counter: in this real-mode window, load forwarding and the x87 change nothing.

Every 3DBench run ends at its `--limit-seconds` (`END limit`, which `machine_tb` exits 1 for); the handover and end lines are in [runs.json](runs.json).

### C3. Quake bring-up

`base`'s attempt: `PROMPT 460018143`; `RTL-BEGIN 460018150 flips=2`; `STOP stall 977741085 00a7:0005256c dbe3 DB.4`; the runner's status `cpuprof-suite rc=0 quake rc=124 SIM_DONE`.

The RTL stopped before the window (`STOP stall 977741085 00a7:0005256c dbe3 DB.4`), so the bring-up failed and **every Quake cell is not measurable** (method C3). `xshift`, `xdisp`, `x87` and `cuts` were not run on Quake.

### C4. Latency bench (clocks per loop; RTL alone at max)

| Loop | `base` | `l1i2` | `l1d2` | `l12` | `xshift` | `xdisp` | `x87` | `cuts` | `qhalf` |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| jcc_loop | 8,039 | 8,039 | 8,039 | 8,039 | 8,039 | 8,039 | 8,039 | 8,039 | 16,036 |
| jcc_taken | 77,045 | 77,045 | 77,045 | 77,045 | 77,045 | 77,045 | 77,045 | 77,045 | 79,045 |
| call_ret | 106,148 | 106,148 | 106,148 | 106,148 | 106,148 | 106,148 | 106,148 | 106,148 | 108,149 |
| call_reg | 173,999 | 173,999 | 173,999 | 173,999 | 173,999 | 173,999 | 173,999 | 173,999 | 175,001 |
| call_mem | 153,070 | 153,070 | 153,070 | 153,070 | 153,070 | 153,070 | 153,070 | 153,070 | 154,075 |
| far_call | 259,060 | 259,060 | 259,060 | 259,060 | 259,060 | 259,060 | 259,060 | 259,060 | 261,059 |
| loop | 42,270 | 42,270 | 42,270 | 42,270 | 42,270 | 42,270 | 42,270 | 42,270 | 50,009 |
| agi_load | 35,039 | 35,039 | 35,039 | 35,039 | 35,039 | 35,039 | 35,039 | 35,039 | 35,040 |
| agi_alu | 22,029 | 22,029 | 22,029 | 22,029 | 22,029 | 22,029 | 22,029 | 22,029 | 38,026 |
| store_load_same | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 36,054 |
| store_load_other | 18,059 | 18,059 | 18,059 | 18,059 | 18,059 | 18,059 | 18,059 | 18,059 | 36,055 |
| store_load_line | 18,057 | 18,057 | 18,057 | 18,057 | 18,057 | 18,057 | 18,057 | 18,057 | 36,042 |
| store_load_part | 26,030 | 26,030 | 26,030 | 26,030 | 26,030 | 26,030 | 26,030 | 26,030 | 36,027 |
| store_load_alias | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 36,054 |
| call_ind1 | 88,068 | 88,068 | 88,068 | 88,068 | 88,068 | 88,068 | 88,068 | 88,068 | 96,075 |
| jmp_ind1 | 32,062 | 32,062 | 32,062 | 32,062 | 32,062 | 32,062 | 32,062 | 32,062 | 48,072 |
| call_alt | 184,081 | 184,081 | 184,081 | 184,081 | 184,081 | 184,081 | 184,081 | 184,081 | 208,061 |
| v86_int_iretd | 168,080 | 168,080 | 168,080 | 168,080 | 168,080 | 168,080 | 168,080 | 168,080 | 170,079 |
| v86_in | 211,059 | 211,059 | 211,059 | 211,059 | 211,059 | 211,059 | 211,059 | 211,059 | 211,060 |
