# Coleco native Scanlines timing investigation

The Scanlines part for the Coleco native shell built from FES `c721fd44acc9c089cf69cd5f9d14b2425d3f45c5` failed its production pixel-clock gate at 71.5973 MHz against 74.25 MHz. The shell itself sealed and the Direct part built. The production shell had package ID `4d9aad3ea47d7036680a8ca2de7818fa3c3f879daf70afa64ceb8d8fa4dd2df2` and build ID `3a44cff783fc50a6dcf22f8f58292708`. This was a candidate build for #1714, not an accepted package.

A replay of the same frozen scaffold, normalized cart netlist, constraints and authenticated nextpnr reproduced all three clocks exactly. `timing.json` had the same SHA-256, `6731b8070976d0b8e1f7114850fd1b5a1c1510a10bf6188fe703b2663b1ff88e`. Each probe used a private copy of the frozen inputs and the same nextpnr binary, through automatic GPU phase leases. The complete arguments, environment, hashes, backend, GPU grants, lease waits, per-clock results and conservative budget accounting are in [runs.jsonl](runs.jsonl).

| Weight | Effective critexp | Seed 13 pixel MHz | Seed 14 pixel MHz | Seed 15 pixel MHz | Result |
| ---: | ---: | ---: | ---: | ---: | --- |
| 300 | 5 | 71.5973 | 73.9645 | 71.3012 | 0/3 pass |
| 300 | 2, explicit | 75.2276 | 75.2559 | 77.3036 | 3/3 pass |
| 1000 | 5 | 70.2247 | 75.6888 | 70.2642 | 1/3 pass |

All runs in the table had system clock 54.8727/52.2248 MHz and audio clock 179.6945/12.2880 MHz. The first recorded jobs named `c2` did **not** pass that option: the frozen scaffold carried exponent 5, as their serialized routed settings show. Explicit `--placer-heap-critexp 5` reproduced those results. An interrupted duplicate is recorded and charged. The explicit exponent-2 probes serialize exponent 2 in the routed settings; they are the candidate. No clock, timing gate or seal was relaxed.

The full original core/part build is conservatively charged at 744 seconds, and every probe is charged its entire wall time as an upper bound on GPU time, including 46 seconds for the interrupted probe. The total is 2153.5 seconds, under the six-GPU-hour incident budget. Phase leases recorded live HIP use and wait times; host CPU contention was not measured.

These frozen Scanlines routes are candidate evidence, not a production rebuild, a Direct-part check, or hardware acceptance. The main-based recipe change must build a fresh shell and both exact-shell native parts with normal timing, CRAM containment and seal checks. Because the failed shell came from the unmerged #1714 branch, a passing independent main build does not establish that the later combined #1714 shell closes. A full build and applicable hardware acceptance must be recorded for the actual combined artifact.
