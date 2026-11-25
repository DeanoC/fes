# C64 PAL-N timing diagnostics

The original PAL-N production search at `411375b68f` exhausted its ten
weight-2000 seeds without a passing package. Its completed seed 4 reached
69.10 MHz pixel and 51.39 MHz system, against 74.25 and 52.224 MHz.

The shared C64 frame-store bank mux was then pipelined. The following
diagnostics route the retained synthesis from `836a68ab4c`, SHA-256
`6fb41ced4dbaacbc7221d0c4969459280747091177b5404dc85288f15d13fd4c`.
They use HIP, a 600-second candidate limit and the unchanged clock constraints.
Both passing routes independently passed `validate_build_evidence`, including
the routed shell, memory, resource, PLL and timing checks.

| Seed | Weight | Arrival-pin assignment | Result | Pixel MHz | System MHz | Audio MHz |
| --- | --- | --- | --- | ---: | ---: | ---: |
| 5 | 2000 | no | timeout in refinement | — | — | — |
| 5 | 300 | yes | diagnostic pass | 88.18 | 58.34 | 206.14 |
| 12 | 300 | yes | diagnostic pass | 88.79 | 55.30 | 201.25 |

The PAL-N recipe uses weight 300 and arrival-pin assignment, with seeds 5 and
12 first. These routes are diagnostics, not an exact-head production seal or
hardware acceptance. The production build must independently meet all clocks.
