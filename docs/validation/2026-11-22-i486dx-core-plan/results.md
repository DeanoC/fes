# Results (written by generate.py from the committed evidence; do not edit)

## A. Area: `fes_i386_cpu` on Agilex 5 (A5EB013BB23BE4SCS, Quartus Pro 26.3, 61.44 MHz, virtual pins)

`dx` (main `dafabb6db` + `I486 = 1`, 387 alive), three seeds of one netlist:

| Seed | ALMs needed | Registers | DSP | M20K | Fit | Worst-corner Fmax estimate (MHz) |
| ---: | ---: | ---: | ---: | ---: | --- | ---: |
| 1 | 34,651 | 24,718 | 22 | 60 | pass | 72.62 |
| 2 | 34,606 | 24,018 | 22 | 60 | pass | 63.76 |
| 3 | 34,706 | 24,012 | 22 | 60 | pass | 63.40 |

Median 34,651, spread 100; noise band N = max(spread, 100) = **100 ALMs** (method rule 1). Every fit's ALMs include 2,352 ALMs that hold virtual pins (the envelope's, not the CPU's); they are the same in every fit and cancel in a saving.

| Variant | What is off | ALMs needed (seed 1) | Saving vs `dx` seed 1 | Verdict | Registers | M20K | Worst-corner Fmax estimate (MHz) |
| --- | --- | ---: | ---: | --- | ---: | ---: | ---: |
| `386` | `I486 = 0`: the 386 identity (scale, not an ablation) | 34,361 | +290 | scale: the 486 identity's cost | 24,019 | 60 | 65.37 |
| `pred` | all prediction off: BTB #1511, RAS #1512, indirect #1571 | 33,290 | +1,361 | saving | 23,352 | 56 | 67.82 |
| `ras` | return-stack prediction off (lower bound) | 34,658 | -7 | under noise | 23,942 | 60 | 63.79 |
| `ind` | indirect prediction off (lower bound) | 34,727 | -76 | under noise | 24,052 | 60 | 64.22 |
| `stld` | store-to-load overlap test #1515 off | 34,573 | +78 | under noise | 24,592 | 60 | 71.09 |
| `ealu` | early ALU at AG #1517 off | 34,673 | -22 | under noise | 24,660 | 60 | 73.58 |
| `lfwd` | load forwarding into an address #1522 off | 34,291 | +360 | saving | 24,014 | 60 | 64.64 |
| `speed` | `pred` + `stld` + `ealu` + `lfwd` together | 32,935 | +1,716 | saving | 22,742 | 56 | 63.56 |
| `pg` | paging off (area bound only, not functional) | 32,738 | +1,913 | saving; bound only, never a candidate | 22,982 | 60 | 65.26 |

Rule 4: `speed` saves 1,716; its parts' single savings sum to 1,777 (difference -61).

## B. Lockstep of each functional ablation

| Variant | Lockstep subset | Failures beyond `base` |
| --- | --- | ---: |
| `base` | passed: --random-programs 500 --random-seeds 8 --limit-per-file 20 | 0 |
| `pred` | passed: --random-programs 500 --random-seeds 8 --limit-per-file 20 | 0 |
| `ras` | passed: --random-programs 500 --random-seeds 8 --limit-per-file 20 | 0 |
| `ind` | passed: --random-programs 500 --random-seeds 8 --limit-per-file 20 | 0 |
| `stld` | passed: --random-programs 500 --random-seeds 8 --limit-per-file 20 | 0 |
| `ealu` | passed: --random-programs 500 --random-seeds 8 --limit-per-file 20 | 0 |
| `lfwd` | passed: --random-programs 500 --random-seeds 8 --limit-per-file 20 | 0 |
| `speed` | passed: --random-programs 500 --random-seeds 8 --limit-per-file 20 | 0 |

## C. CPI: DOOM v1.9 `-timedemo demo3`, handover at page flip 40, RTL for 1.0 s

| Variant | Handover cycle | h1 CPI | h2 CPI | h2 CPI / `base` | h2 retired | Saving (ALMs) | h2 CPI loss per 100 ALMs saved |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `base` | 2493936063 | 1.476 | 1.668 | 1.000 | 29,983,986 | — | — |
| `pred` | 2493936063 | 3.107 | 2.489 | 1.493 | 20,089,447 | +1,361 | 3.62% |
| `ras` | — | — | — | not run | — | — | — |
| `ind` | — | — | — | not run | — | — | — |
| `stld` | 2493936063 | 1.488 | 1.695 | 1.016 | 29,503,135 | +78 | — |
| `ealu` | — | — | — | not run | — | — | — |
| `lfwd` | — | — | — | not run | — | — | — |
| `speed` | — | — | — | not run | — | — | — |

### The governor bound (method, question 3)

| Variant | h2 CPI ratio r | `max` realtics bound 2,062 × r | DX2-66 (2,912) | DX-33 (4,682) |
| --- | ---: | ---: | --- | --- |
| `base` | 1.000 | 2,062 | kept by the bound | kept by the bound |
| `pred` | 1.493 | 3,078 | not settled by the bound | kept by the bound |
| `stld` | 1.016 | 2,096 | kept by the bound | kept by the bound |

## Latency bench (clocks per loop; RTL alone at max)

| Loop | `base` | `pred` | `stld` |
| --- | ---: | ---: | ---: |
| jcc_loop | 8,039 | 47,999 | 8,039 |
| jcc_taken | 77,045 | 103,999 | 77,045 |
| call_ret | 106,148 | 199,118 | 106,148 |
| call_reg | 173,999 | 245,999 | 173,999 |
| call_mem | 153,070 | 246,029 | 153,070 |
| far_call | 259,060 | 269,029 | 259,060 |
| loop | 42,270 | 90,999 | 42,270 |
| agi_load | 35,039 | 44,999 | 35,039 |
| agi_alu | 22,029 | 31,999 | 22,029 |
| store_load_same | 26,059 | 36,029 | 26,059 |
| store_load_other | 18,059 | 28,031 | 26,059 |
| store_load_line | 18,057 | 28,029 | 26,050 |
| store_load_part | 26,030 | 35,999 | 26,030 |
| store_load_alias | 26,059 | 36,029 | 26,059 |
| call_ind1 | 88,068 | 328,007 | 88,068 |
| jmp_ind1 | 32,062 | 184,023 | 32,062 |
| call_alt | 184,081 | 336,042 | 184,081 |
| v86_int_iretd | 168,080 | 178,049 | 168,080 |
| v86_in | 211,059 | 221,019 | 211,059 |
