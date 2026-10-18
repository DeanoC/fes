# Recipe option audit: nextpnr options vs what each fes core passes
Lander, 2026-10-18 13:05 EEST. Read-only audit. Sources: DeanoC/nextpnr at `e2f1e029` (main) and `9128800f` (fes pin), `common/kernel/command.cc` and `mistral/main.cc` options, `ctx->settings` keys read under mistral/, common/place and common/route, and every `getenv`. fes main `9b463dce`, `sources/misteross/scripts/build_fes_*.py` and `search_placer_qor.py`. Linked from cores-as-apps-plan.md.

Legend: **E** = explicit in the producer (value), **H** = hard-coded in the producer's argv builder (not a tunable constant), **D** = nextpnr's implicit default (not passed), **–** = not applicable or missing.
`config/core-recipes.toml` carries only `gpu_router` (HIP) and `hip_architectures` (gfx1100;gfx1201). Every other value lives in the producer.

## A. nextpnr options that can change placement or routing
**Added between 9128800f and e2f1e029** (present on main, not in the fes pin):
- CLI: `--arrival-pin-assignment`, `--isolated-lut-placement`, `--critical-cohort-budget`, `--critical-cohort-report`, `--critical-cohort-model-out`, `--router2-profile`
- Settings: `mistral/arrival_pin_assignment`, `mistral/isolated_lut_placement`, `mistral/fes_reservations_v1`, `gpurouter/repairBbExpand`, `gpurouter/archRetryLimit`

**Present in both:**
- **Kernel CLI:**
  - `--seed`, `--randomize-seed`, `--freq`, `--sdc`, `--threads`
  - `--placer-heap-{timingweight,critexp,alpha,beta,cell-placement-timeout,no-ctrl-set}`, `--parallel-refine`, `--starttemp`, `--cstrweight`
  - `--router`, `--tmg-ripup`, `--router2-{tmg-ripup,alt-weights}`
  - `--gpu-{device,batches,cpu,opt,perf,telemetry}`
  - `--no-tmdriv`, `--ignore-loops`, `--ignore-rel-clk`, `--timing-allow-fail`, `--seed-racing-contract`
- **Mistral CLI:**
  - `--mistral-clkb`, `--mistral-ff4`, `--replicate-enables`, `--balance-reduction-root`
  - `--remap-*`: about 20 remap/LUT-pair/decompose options
  - `--fes-scaffold`, `--fes-slot-clock`, `--fes-cart`, `--fes-cart-region`, `--fes-cram-region`
- **Settings keys with no CLI flag:**
  - about 60 `gpurouter/*` keys: repair*, analogue*, bbMargin, expandK, critExponent, histCongWeight, maxIter, localInputEscape, and so on
  - `placerHeap/{cellRipupLimit,netShareWeight}`
  - `router1/*`, `router2/*` (bbMargin, estimateWeight, ipinCostAdder, and others), `placer1/*`, `timing/io_delays`
- **Setting them:** the command.cc on either commit has no `-x/--set` passthrough I could see. Settings keys can only be reached through CLI flags or a pre-loaded design JSON (`--pre-place` etc.). This means the `gpurouter/*` keys aren't tunable from producers today.
- **Env vars that affect results:**
  - `NEXTPNR_MISTRAL_PLACED_REDUCTION`, `NEXTPNR_MISTRAL_PLACED_TIMEOUT`, `NEXTPNR_MISTRAL_PLACED_TIMEOUT_REGION`, `NEXTPNR_MISTRAL_RETAINED_ENABLE`
  - Debug-only: `NEXTPNR_MISTRAL_ARC_DUMP`, `*_CAPTURE_*LOCALITY`, `*_REPLICATION_DUMP`
  - Outside nextpnr: `HIP_VISIBLE_DEVICES`, `CUDA_VISIBLE_DEVICES`, `CUDA_DEVICE_ORDER`, `ROCR_VISIBLE_DEVICES`

## B. Core × option (production producer)
| Option | menu | pong | zx81 | coleco nv | sms | sg1000 | c64 | spectrum | ramtest | atari-st | apple2 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| seed list | E 5,1..8 | E 1..8 | E 10,5,12,… | E 3,4,5,… | E 3,10,12,… | E 2,3,4,… | E 5,4,2,… | E 5,4,2,… | E 2,6,1,… | E 4,5,2,… | E 5,4,2,… |
| timingweight | E 10 (search only) | E 10 (search only) | E 300 (+1000,2000,100,10) | E list, `factory` w100 stale | E 1000,300 | E 2000,1000 | E 2000 | E 2000 | E 10 (search only) | E 2000 | E 2000 |
| critexp | E 2 (search only) | E 2 (search only) | E 5 | E 5 | E 5 | E 5 | E 5 | E 5 | E 2 (search only) | E 5 | E 5 |
| placer-heap flags in direct argv | – (D) | – (D) | H | H | H | H | H | H | – (D) | H | H |
| --freq | H 74.25 | H 74.25 | H | H 74.25 | H 74.25 | H | H 74.25 | H | H 74.25 | H | H |
| QOR clocks | E 74.25 | E 74.25 | E 52.224/74.25/12.288 | E same | E same | E same | E same | E same | E 74.25+ram | E same | E same |
| --router | H gpu | H gpu | H gpu | H gpu | H gpu | H gpu | H gpu | H gpu (+--cpu path) | H gpu | H gpu | H gpu |
| GPU backend | recipe HIP | recipe HIP | recipe HIP, CLI override | recipe HIP, CLI override | same | same | same | same | recipe HIP | same | same (CUDA = not seal) |
| --gpu-device | **H 0** | CLI (default 0) | `--gpu-devices` (default 0) | CLI | CLI | CLI | CLI | CLI | CLI | CLI | CLI |
| --timing-allow-fail | – | – | H | H | H | H | H | H | – | H | H |
| route timeout | E ROUTE_TIMEOUT | E ROUTE_TIMEOUT | D (search default) | E 600 s | D | D | D | D | D | E 1800 s | D |
| --threads | D | D | D | D | D | D | D | D | D | D | D |
| --arrival-pin-assignment / --isolated-lut-placement | – (D off) | – | – | – | – | – | – | – | – | – | – |
| critical-cohort / remap-* / replicate-enables / mistral-ff4 / clkb | D | D | D | D | D | D | D | D | D | D | D |
| gpurouter/* (repairBbExpand, archRetryLimit…) | D (not reachable) | D | D | D | D | D | D | D | D | D | D |
| env: CUDA_DEVICE_ORDER | – | – | E PCI_BUS_ID on CUDA | E on CUDA | E on CUDA | E on CUDA | E on CUDA (c64:163) | E on CUDA | – | E on CUDA | E on CUDA |
| env: HIP_VISIBLE_DEVICES | **scrubbed** (lease ids never reach nextpnr, 791 cycle 4) | scrubbed | scrubbed | scrubbed | scrubbed | scrubbed | scrubbed | scrubbed | scrubbed | scrubbed | scrubbed |
| env: NEXTPNR_MISTRAL_PLACED_* / RETAINED_ENABLE | D unset (not pinned, inherited if set) | D | D | D | D | D | D | D | D | D | D |
| fes-* mistral flags (scaffold/slot-clock/cart) | not in these producers (slot-card builders only) | | | | | | | | | | |

"search only" means the constant goes into `search_placer_qor.py` (which passes `--placer-heap-*`), but the producer's own recorded direct argv doesn't pass it.

## C. Findings
1. **Nothing a fork flag controls is recorded or pinned.** `--arrival-pin-assignment`, `--isolated-lut-placement` and the critical-cohort and remap options all rely on nextpnr defaults. A change to one of those defaults in nextpnr silently changes every core. Fix: list each flag with its value in the recipe.
2. **Env vars aren't pinned.** The `NEXTPNR_MISTRAL_PLACED_*` and `RETAINED_ENABLE` vars are inherited from the caller's shell if set. GPU visibility is scrubbed, but `--gpu-device` defaults to 0 or is hard-coded to 0 for menu.
3. **`--threads` is never set.** On the CPU or placer paths results can depend on host core count. Pin it in the recipe.
4. **The `gpurouter/*` keys aren't reachable** without a `-x/--set` passthrough. If a core needs, say, `repairBbExpand`, nextpnr first needs a generic `--set key=value` flag. That would be a nextpnr PR. It's not proposed here as action.
5. **Route timeouts differ**: 600 s for coleco, 1800 s for the ST, and the search default elsewhere. They belong in the recipe.
6. **coleco is split across two files**: `build_fes_coleco_oss.py` still holds SEED=5 and w100, which v2 doesn't use.
