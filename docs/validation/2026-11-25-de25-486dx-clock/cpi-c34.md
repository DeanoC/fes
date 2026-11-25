# C0 = 34 CPI replay

G4 passed on candidate `3df878912962ede8557166d56c93ff8896869943` at
92.156863 MHz in the host RTL simulation. The two arms of each benchmark
had the same handover cycle, PC, registers and control fields. Each window
completed without a CPU stop or oracle fallback.

| Workload | Handover cycle | Window cycles | Shared CPU CPI | 486DX CPI | DX / shared |
| --- | ---: | ---: | ---: | ---: | ---: |
| DOOM h2 | 2,429,812,817 | 46,078,400 | 1.671510 | 1.759742 | 1.052786 |
| 3DBench 8.0–11.5 s | 691,176,474 | 322,548,992 | 2.179263 | 2.212964 | 1.015464 |

DOOM's fixed bound gives 2,062 × 1.052786 = 2,170.84 realtics, below
the DX-33 limit of 4,682. Both 3DBench arms captured the title at 7.00 s
and showed the benchmark rendering at the end of the CPI interval.

The [receipt](sim-c34.json) binds the analysis, commands, logs,
profiles and captured frames to their retained file digests.
