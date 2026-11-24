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

### Hierarchy kept (method amendment 3: exploratory, added after the results)

`dx` with `flatten` removed, seed 1: 34,818 ALMs, +0.48% against `dx` seed 1, within the ±5% gate, so the table below is the module bounds. Worst-corner Fmax estimate 69.79 MHz. Rows of 100 ALMs or more, from [entities-dx-hier.tsv](entities-dx-hier.tsv) (Quartus Pro's 'Fitter Resource Utilization by Entity', read by [entities.py](entities.py)); `<top>` own includes the virtual pins:

| Depth | Entity | ALMs (total) | ALMs (own) | Registers (total) | Registers (own) |
| ---: | --- | ---: | ---: | ---: | ---: |
| 0 | `<top>` | 34,832.4 | 12,977.0 | 23,378 | 8,997 |
| 1 | `<top>/agu` | 127.0 | 127.0 | 0 | 0 |
| 1 | `<top>/biu` | 1,609.3 | 1,609.3 | 1,754 | 1,754 |
| 1 | `<top>/decode` | 827.1 | 827.1 | 0 | 0 |
| 1 | `<top>/dfwd` | 808.5 | 808.5 | 73 | 73 |
| 1 | `<top>/dops` | 493.5 | 493.5 | 74 | 74 |
| 1 | `<top>/dtlb` | 707.8 | 707.8 | 660 | 660 |
| 1 | `<top>/ex` | 742.4 | 519.2 | 0 | 0 |
| 2 | `<top>/ex/shifter` | 223.2 | 223.2 | 0 | 0 |
| 1 | `<top>/frontend` | 3,812.9 | 0.0 | 3,214 | 0 |
| 2 | `<top>/frontend/align` | 1,107.0 | 1,107.0 | 259 | 259 |
| 2 | `<top>/frontend/fetch` | 2,629.4 | 1,413.6 | 2,919 | 2,919 |
| 3 | `<top>/frontend/fetch/decoders` | 673.7 | 115.7 | 0 | 0 |
| 3 | `<top>/frontend/fetch/sums` | 542.1 | 542.1 | 0 | 0 |
| 1 | `<top>/governor` | 401.3 | 401.3 | 163 | 163 |
| 1 | `<top>/l1d` | 2,259.3 | 2,259.3 | 1,828 | 1,828 |
| 1 | `<top>/l1i` | 1,645.2 | 1,406.5 | 992 | 693 |
| 2 | `<top>/l1i/itlb` | 238.7 | 238.7 | 299 | 299 |
| 1 | `<top>/muldiv` | 816.4 | 816.4 | 558 | 558 |
| 1 | `<top>/seq` | 185.3 | 185.3 | 0 | 0 |
| 1 | `<top>/x87` | 7,315.1 | 3,510.3 | 5,065 | 2,303 |
| 2 | `<top>/x87/core` | 2,148.3 | 420.6 | 1,455 | 436 |
| 3 | `<top>/x87/core/fast` | 1,134.2 | 1,134.2 | 305 | 305 |
| 3 | `<top>/x87/core/slow` | 593.5 | 593.5 | 714 | 714 |
| 2 | `<top>/x87/mul` | 154.5 | 154.5 | 518 | 518 |
| 2 | `<top>/x87/trans` | 1,502.1 | 1,502.1 | 789 | 789 |

Worst setup paths at Slow fix4 0C: 20 of the 20 reported run from `x87|` to `x87|` (the first: `x87|core|normalized[18]~_Duplicate1` to `x87|core|fast|shift_left_0~342_BLOCK_INPUT_MUX_PASSTHROUGH_X18_Y46_N0_I55_dff`; [corner1-setup-top20.txt](fits/dx-hier-s1/corner1-setup-top20.txt)).

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
| `ras` | 2493936063 | 2.049 | 1.744 | 1.046 | 28,664,013 | -7 | — |
| `ind` | 2493936063 | 1.482 | 1.683 | 1.009 | 29,708,256 | -76 | — |
| `stld` | 2493936063 | 1.488 | 1.695 | 1.016 | 29,503,135 | +78 | — |
| `ealu` | 2493936063 | 1.483 | 1.682 | 1.009 | 29,730,078 | -22 | — |
| `lfwd` | 2493936063 | 1.484 | 1.687 | 1.012 | 29,631,931 | +360 | 0.33% |
| `speed` | 2493936063 | 3.178 | 2.539 | 1.523 | 19,692,938 | +1,716 | 3.05% |

### The governor bound (method, question 3)

| Variant | h2 CPI ratio r | `max` realtics bound 2,062 × r | DX2-66 (2,912) | DX-33 (4,682) |
| --- | ---: | ---: | --- | --- |
| `base` | 1.000 | 2,062 | kept by the bound | kept by the bound |
| `pred` | 1.493 | 3,078 | not settled by the bound | kept by the bound |
| `ras` | 1.046 | 2,157 | kept by the bound | kept by the bound |
| `ind` | 1.009 | 2,081 | kept by the bound | kept by the bound |
| `stld` | 1.016 | 2,096 | kept by the bound | kept by the bound |
| `ealu` | 1.009 | 2,080 | kept by the bound | kept by the bound |
| `lfwd` | 1.012 | 2,086 | kept by the bound | kept by the bound |
| `speed` | 1.523 | 3,140 | not settled by the bound | kept by the bound |

### Where `base`'s h2 clocks go

`profiles/base-h2.json`, 50,000,000 clocks. The profile's own cause buckets; the bench's memory model is its 100 MHz one (18 clocks to port 0's first data).

| Cause | Clocks | Share |
| --- | ---: | ---: |
| issued uops | 32,114,923 | 64.2% |
| redirect: refill | 1,526,676 | 3.1% |
| fetch and L1I | 1,416,939 | 2.8% |
| L1D and port 0 | 8,036,203 | 16.1% |
| microcode | 993,305 | 2.0% |
| interlock: AGI | 3,253,925 | 6.5% |
| interlock: store-to-load | 32,762 | 0.1% |
| interlock: flags | 1,113 | 0.0% |
| interlock: ESP | 111,936 | 0.2% |
| interlock: segment | 13,247 | 0.0% |
| multiply and divide | 1,544,834 | 3.1% |
| I/O and VGA: I/O | 691,799 | 1.4% |
| I/O and VGA: MMIO load | 38,365 | 0.1% |
| I/O and VGA: store buffer full | 167,858 | 0.3% |
| halt | 0 | 0.0% |
| other | 56,115 | 0.1% |

## Latency bench (clocks per loop; RTL alone at max)

| Loop | `base` | `pred` | `ras` | `ind` | `stld` | `ealu` | `lfwd` | `speed` |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| jcc_loop | 8,039 | 47,999 | 8,039 | 8,039 | 8,039 | 8,039 | 8,039 | 47,999 |
| jcc_taken | 77,045 | 103,999 | 77,045 | 77,045 | 77,045 | 77,045 | 77,045 | 103,999 |
| call_ret | 106,148 | 199,118 | 190,127 | 106,148 | 106,148 | 106,148 | 106,148 | 199,118 |
| call_reg | 173,999 | 245,999 | 237,008 | 164,029 | 173,999 | 173,999 | 173,999 | 245,999 |
| call_mem | 153,070 | 246,029 | 236,039 | 164,059 | 153,070 | 153,070 | 153,070 | 246,029 |
| far_call | 259,060 | 269,029 | 259,060 | 259,060 | 259,060 | 259,060 | 259,060 | 269,029 |
| loop | 42,270 | 90,999 | 42,250 | 42,250 | 42,270 | 42,270 | 42,270 | 90,999 |
| agi_load | 35,039 | 44,999 | 35,039 | 35,039 | 35,039 | 35,039 | 35,039 | 44,999 |
| agi_alu | 22,029 | 31,999 | 22,029 | 22,029 | 22,029 | 43,029 | 22,029 | 52,999 |
| store_load_same | 26,059 | 36,029 | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 36,029 |
| store_load_other | 18,059 | 28,031 | 18,059 | 18,059 | 26,059 | 18,059 | 18,059 | 36,029 |
| store_load_line | 18,057 | 28,029 | 18,057 | 18,057 | 26,050 | 18,057 | 18,057 | 36,020 |
| store_load_part | 26,030 | 35,999 | 26,030 | 26,030 | 26,030 | 26,030 | 26,030 | 35,999 |
| store_load_alias | 26,059 | 36,029 | 26,059 | 26,059 | 26,059 | 26,059 | 26,059 | 36,029 |
| call_ind1 | 88,068 | 328,007 | 168,047 | 176,046 | 88,068 | 88,068 | 88,068 | 328,007 |
| jmp_ind1 | 32,062 | 184,023 | 32,062 | 104,053 | 32,062 | 32,062 | 32,062 | 184,023 |
| call_alt | 184,081 | 336,042 | 336,042 | 184,081 | 184,081 | 184,081 | 184,081 | 336,042 |
| v86_int_iretd | 168,080 | 178,049 | 168,080 | 168,080 | 168,080 | 168,080 | 168,080 | 178,049 |
| v86_in | 211,059 | 221,019 | 211,059 | 211,059 | 211,059 | 211,059 | 211,059 | 221,019 |
