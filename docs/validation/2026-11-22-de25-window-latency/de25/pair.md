# DE25 four-seed pair: #2036's head and the S-lat head

Fits from [board-pair.sh](board-pair.sh) on 23 Nov 2026, collected by
[cuts_collect.py](cuts_collect.py) into [collect.json](collect.json)
(`cuts_collect.py DIR base23b slat23b`).

- **Baseline (`base23b`):** #2036's head `dfacd28e1`, Yosys lock d273c1cb.
  Seed 1 staged machine netlist `f7bff7c7…`; seeds 2–4 compiled the same bytes.
  Shared with #2108 and #2118.
- **Arm (`slat23b`):** this branch at `e4129d9aa` (`dfacd28e1` merged plus the
  window-profile change). Seed 1 staged `f6c7fb69…`; seeds 2–4 compiled the
  same bytes.
- Quartus Prime Pro 26.3, A5EB013BB23BE4SCS, the 387-off DE25 pc386 board
  build. Every compile held a Pro slot of the Quartus lease.
- The arm's first seed-2 attempt hit `quartus_jobs.py`'s 240-minute cap
  waiting for a Pro slot and started nothing. It was rerun on the same project
  under the lease (17:08–17:52Z).

## Area

| Seed | Base ALMs | Arm ALMs |
|---|---|---|
| 1 | 41,401 | 40,418 |
| 2 | 41,460 | 40,407 |
| 3 | 41,443 | 40,387 |
| 4 | 41,445 | 40,356 |
| **Median** | **41,444** | **40,397** |
| Spread | 59 | 62 |

- **Saving: 1,047 ALMs, measured.** 41,444 − 40,397 = 1,047, which exceeds
  the larger spread (62).
- **Band:** the arm does not grow (one-sided band passes).
- **Also reported, every seed of each arm:** 168 M20K, 24 DSPs, 410 MLAB memory
  ALMs, 5,637 MLAB bits. Unchanged.

## Timing at 61.437908 MHz

Worst slack over every clock, ns. Every seed of both arms closes; no slack is
negative. Hold and removal per seed, as the 22 Nov decision asks, are in the
table.

| Arm | Seed | Setup | Hold | Recovery | Removal | Min pulse width |
|---|---|---|---|---|---|---|
| base | 1 | 1.240 | 0.000 | 7.417 | 0.003 | 0.220 |
| base | 2 | 1.567 | 0.000 | 7.048 | 0.009 | 0.220 |
| base | 3 | 1.653 | 0.000 | 7.102 | 0.017 | 0.220 |
| base | 4 | 1.649 | 0.000 | 7.033 | 0.027 | 0.220 |
| arm | 1 | 1.732 | 0.001 | 6.628 | 0.052 | 0.220 |
| arm | 2 | 1.425 | 0.001 | 6.437 | 0.056 | 0.220 |
| arm | 3 | 1.654 | 0.001 | 6.883 | 0.053 | 0.220 |
| arm | 4 | 1.412 | 0.001 | 6.316 | 0.049 | 0.220 |

`golden_top.sta.summary` prints slack to three decimals; a hold of 0.000 is
not negative there.
