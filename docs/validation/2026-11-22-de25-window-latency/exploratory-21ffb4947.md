# Exploratory board build at 21ffb4947 (not counted)

`pc386/build.py build` at `21ffb4947` on chip-30x0-ai-dev under the Quartus lease
(`quartus_jobs.py remote --host chip-30x0-ai-dev`), 22 Nov 2026, CUDA Yosys slot
(`FES_GPU_ROUTER=CUDA`, `/data/fes-toolchain-cache`), `YOSYS_MAX_THREADS=1`, seed 1.
Before the fixed Yosys lock; not part of any recorded pair.
Run directory: `/data/projects/fes-quartus/runs/21ffb4947df7-20261122T153416Z-slat-explore-8167fa`.

- `generation.json` window `interconnect_latency`: `max_additional_latency` 0 on all 3 qsys_top
  connections; the F2SDRAM interconnect (`qsys_top_altera_mm_interconnect_1920_klysfca.v`) has 0
  pipeline stages.
- ALMs needed 40,637.1 / 46,800; RAM blocks 168 / 358.
- Worst slack over every clock: setup +1.655, hold 0.000, recovery +7.428, removal +0.046,
  minimum pulse width +0.220.

Earlier attempts at 12ed6ff2d never reached Quartus: three host-environment misses (HIP slot on
a CUDA host, nvcc not on PATH, the lease's default cache without the CUDA Yosys), then
`get_domain_assignment {$system}`, which Platform Designer refuses; fb47dd5f3 and 21ffb4947
replaced that pre-check with the post-generation check.
