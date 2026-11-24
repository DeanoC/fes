# DE25 four-seed pair: #2036's head and the A-dma2 head

Fits from [board-pair.sh](board-pair.sh) on 23 Nov 2026, collected by
[cuts_collect.py](cuts_collect.py) into [collect.json](collect.json)
(`cuts_collect.py DIR base23b dma223b`).

- **Baseline (`base23b`):** #2036's head `dfacd28e1`, Yosys lock d273c1cb.
  Seed 1 staged machine netlist `f7bff7c7…`; seeds 2–4 compiled the same bytes.
  Shared with #2108 and #2117.
- **Arm (`dma223b`):** this branch at `f27f23f97` (`dfacd28e1` merged plus the
  DE25-only register-file derivation). Seed 1 staged `dabf964c…`; seeds 2–4
  compiled the same bytes.
- Quartus Prime Pro 26.3, A5EB013BB23BE4SCS, the 387-off DE25 pc386 board
  build. Every compile held a Pro slot of the Quartus lease.

## Area

| Seed | Base ALMs | Arm ALMs |
|---|---|---|
| 1 | 41,401 | 41,119 |
| 2 | 41,460 | 41,152 |
| 3 | 41,443 | 41,171 |
| 4 | 41,445 | 41,167 |
| **Median** | **41,444** | **41,159.5** |
| Spread | 59 | 52 |

- **Saving: 284.5 ALMs, measured.** 41,444 − 41,159.5 = 284.5, which exceeds
  the larger spread (59).
- **Band:** the arm does not grow (one-sided band passes).
- **Also reported, every seed of each arm:** 168 M20K, 24 DSPs, 410 MLAB memory
  ALMs, 5,637 MLAB bits. Unchanged.

## Timing at 61.437908 MHz

Worst slack over every clock, ns. Every seed of both arms closes; no slack is
negative.

| Arm | Seed | Setup | Hold | Recovery | Removal | Min pulse width |
|---|---|---|---|---|---|---|
| base | 1 | 1.240 | 0.000 | 7.417 | 0.003 | 0.220 |
| base | 2 | 1.567 | 0.000 | 7.048 | 0.009 | 0.220 |
| base | 3 | 1.653 | 0.000 | 7.102 | 0.017 | 0.220 |
| base | 4 | 1.649 | 0.000 | 7.033 | 0.027 | 0.220 |
| arm | 1 | 1.563 | 0.000 | 6.432 | 0.079 | 0.220 |
| arm | 2 | 1.386 | 0.000 | 7.360 | 0.021 | 0.220 |
| arm | 3 | 1.504 | 0.000 | 7.710 | 0.024 | 0.220 |
| arm | 4 | 1.630 | 0.000 | 7.466 | 0.016 | 0.220 |

`golden_top.sta.summary` prints slack to three decimals; a hold of 0.000 is
not negative there.
