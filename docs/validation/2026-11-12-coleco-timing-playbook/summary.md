# Coleco native Scanlines timing investigation

The Scanlines part for the Coleco native shell built from FES `c721fd44acc9c089cf69cd5f9d14b2425d3f45c5` failed its production pixel-clock gate at 71.5973 MHz against 74.25 MHz. The shell itself sealed and the Direct part built. The production shell had package ID `4d9aad3ea47d7036680a8ca2de7818fa3c3f879daf70afa64ceb8d8fa4dd2df2` and build ID `3a44cff783fc50a6dcf22f8f58292708`. This was a candidate build for #1714, not an accepted package.

A replay of the same frozen scaffold, normalized cart netlist, constraints and authenticated nextpnr reproduced all three clocks exactly. `timing.json` had the same SHA-256, `6731b8070976d0b8e1f7114850fd1b5a1c1510a10bf6188fe703b2663b1ff88e`. Each probe used a private copy of the frozen inputs and the same nextpnr binary, through automatic GPU phase leases. The complete arguments, environment, hashes, backend, GPU grants, lease waits, per-clock results and conservative budget accounting are in [runs.jsonl](runs.jsonl).

| Weight | Effective critexp | Seed 13 pixel MHz | Seed 14 pixel MHz | Seed 15 pixel MHz | Result |
| ---: | ---: | ---: | ---: | ---: | --- |
| 300 | 5 | 71.5973 | 73.9645 | 71.3012 | 0/3 pass |
| 300 | 2, explicit | 75.2276 | 75.2559 | 77.3036 | 3/3 pass |
| 1000 | 5 | 70.2247 | 75.6888 | 70.2642 | 1/3 pass |

All runs in the table had system clock 54.8727/52.2248 MHz and audio clock 179.6945/12.2880 MHz. The first recorded jobs named `c2` did **not** pass that option: the frozen scaffold carried exponent 5, as their serialized routed settings show. Explicit `--placer-heap-critexp 5` reproduced those results. An interrupted duplicate is recorded and charged. The explicit exponent-2 probes serialize exponent 2 in the routed settings; they are the candidate. No clock, timing gate or seal was relaxed.

The full original core/part build is conservatively charged at 744 seconds, and every probe is charged its entire wall time as an upper bound on GPU time, including 46 seconds for the interrupted probe. A fresh production build below took 307 seconds; charging that entire time separately to all three sequential routes gives a conservative total of 3074.5 seconds, under the six-GPU-hour incident budget. Phase leases recorded live HIP use and wait times; host CPU contention was not measured.

## Main-based production build

The committed recipe source `757575c740bc3b303166f187bdb50dcf31ff02a3`, based on main `c6bf1c9b68197825c8a7bdb6404b0b72533bc44f`, built a fresh native shell and both exact-shell video parts on Powerboat with the authenticated HIP slot. The production job did not reuse a cached shell or video part. Its normal timing, seal and strict CRAM containment checks passed; each part's routed settings record weight 300 and exponent 2.

| Artifact | Pixel MHz | System MHz | Audio MHz |
| --- | ---: | ---: | ---: |
| Shell | 87.0777 | 54.6807 | 173.4605 |
| Direct | 79.1954 | 54.6807 | 173.4605 |
| Scanlines | 78.8395 | 54.6807 | 173.4605 |
| Required | 74.2501 | 52.2248 | 12.2880 |

The shell package ID is `e8ce3e46a23a0528d406812f7588e6445018656507c3ac2fc45b779f544f1aa1` and build ID is `7b9f7253673c572b83879c956f23f5cb`. The Direct part ID is `678cba471948e32e02bc064e4289b2faeaf03cd290076aee9f9ad415cbb19392`; Scanlines is `1b4e468e8397c22fc99a544106661950b6ea9d3acae41fd8cf77fdc68fcd9145`. Both manifests bind that exact shell. The retained build is under `/home/deano/fes/out/dev/coleco-video-timing-main-757575c74` on Powerboat.

The production build tested commit `757575c740bc3b303166f187bdb50dcf31ff02a3`; the subsequent commit adding this evidence changes documentation only. This is build and seal evidence, not hardware acceptance. Because the original failed shell came from the separate #1714 branch, this main-based build does not establish that the later combined #1714 shell closes. Its actual combined artifact needs its own production build and applicable hardware acceptance.
