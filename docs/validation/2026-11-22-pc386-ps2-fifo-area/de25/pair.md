# DE25 four-seed pair: #2036's head and the derived A-fifo head

Fits from [board-pair.sh](board-pair.sh) on 23 Nov 2026, collected by
[cuts_collect.py](cuts_collect.py) into [collect.json](collect.json)
(`cuts_collect.py DIR base23b fifo23c`).

- **Baseline (`base23b`):** #2036's head `dfacd28e1`, Yosys lock d273c1cb.
  Seed 1 staged machine netlist `f7bff7c7…`; seeds 2–4 compiled the same bytes.
- **Arm (`fifo23c`):** this branch's derivation at `d0ad36901` (the merge of
  `dfacd28e1` plus the DE25-only derivation). Seed 1 staged `9dfc69e8…`;
  seeds 2–4 compiled the same bytes.
- Quartus Prime Pro 26.3, A5EB013BB23BE4SCS, the 387-off DE25 pc386 board
  build. Every compile held a Pro slot of the Quartus lease.

## Area

| Seed | Base ALMs | Arm ALMs |
|---|---|---|
| 1 | 41,401 | 41,355 |
| 2 | 41,460 | 41,357 |
| 3 | 41,443 | 41,443 |
| 4 | 41,445 | 41,368 |
| **Median** | **41,444** | **41,362.5** |
| Spread | 59 | 88 |

- **Saving:** 41,444 − 41,362.5 = **81.5 ALMs**.
- **Result: within the spread.** The saving does not exceed the larger spread
  (88), so it is not a measured saving under the method's rule.
- **Band:** the arm does not grow (one-sided band passes).
- **Also reported, every seed of each arm:** 168 M20K, 24 DSPs. MLAB memory
  ALMs 410 → 470 and MLAB bits 5,637 → 6,069, the five FIFO arrays moving from
  registers into MLABs.

## Timing at 61.437908 MHz

Worst slack over every clock, ns. Every seed of both arms closes; no slack is
negative.

| Arm | Seed | Setup | Hold | Recovery | Removal | Min pulse width |
|---|---|---|---|---|---|---|
| base | 1 | 1.240 | 0.000 | 7.417 | 0.003 | 0.220 |
| base | 2 | 1.567 | 0.000 | 7.048 | 0.009 | 0.220 |
| base | 3 | 1.653 | 0.000 | 7.102 | 0.017 | 0.220 |
| base | 4 | 1.649 | 0.000 | 7.033 | 0.027 | 0.220 |
| arm | 1 | 1.472 | 0.000 | 7.532 | 0.012 | 0.220 |
| arm | 2 | 1.668 | 0.000 | 7.597 | 0.179 | 0.220 |
| arm | 3 | 1.420 | 0.000 | 7.245 | 0.074 | 0.220 |
| arm | 4 | 1.441 | 0.000 | 7.740 | 0.049 | 0.220 |

`golden_top.sta.summary` prints slack to three decimals; a hold of 0.000 is
not negative there.
