# Plan: treat FES cores as apps (per-core toolchain pins, nightly bumps, app-style packaging)
**Status: approved by Deano with changes on 2026-10-18 (revision 2, adding change 1: the recipe captures all nextpnr options). Issue #843 delivers migration steps 1–2. Issue #882 makes the nextpnr pin per lock: each recipe lists the options of its own lock's nextpnr (`9128800f` or `552ba246`), so a core can move its lock alone. No lock moves in #882. Later steps remain proposals.** Option audit, including the `552ba246` classification: [recipe-option-audit.md](recipe-option-audit.md).
Lander, 2026-10-18 12:50 EEST. This plan-only assessment predates the #843 implementation. The gate proposal and the #213 revert are on hold, and fes stays pinned to nextpnr `9128800f`.
Sources: fes main `9b463dce` tree, `config/core-recipes.toml`, `config/core-library.toml`, `docs/kit-core-library.md`, `check.yml`, `gh run list`, and `/workspace/lander/791-STATUS.md`.

## Facts that shape the plan
- **There is no per-core lock today.** The only lock file in fes main is `sources/misteross/toolchain.lock`, and every recipe in `config/core-recipes.toml` points `lock_path` at it. I found no `c64.lock` on main. If one exists on a branch or on Powerboat, step 1 adopts it.
- **The per-core plumbing is already there.** `lock_path` is a per-core field, the toolchain cache keys a slot by lock (9e819bba for the current lock), each recipe has its own producer, and the kit already has a **durable core library** on FESDATA3. That library takes `POST /v1/core-packages` to install a sealed, immutable package, and `PUT /v1/core-selections/{core_id}` with a compare-and-swap to switch versions, without reimaging (docs/kit-core-library.md). The factory version stays as the fallback.
- **fes PR CI doesn't place cores.** `check.yml` runs on GitHub `ubuntu-24.04` runners (plan, parent-tests, host, runtime, contracts, fpga-producer, simulation, integration). The last 13 green runs took 8.5 / **21.8** / 28.7 min (min / median / max). The hours Deano sees come from **toolchain bumps**: re-closing every core on Powerboat for a lock bump (fes#791 took about 3 h of GPU before it stopped), the ST alone needing 30 min per seed, and then whole-image HIL. This plan targets that cost. It barely changes PR CI.

## 1. Per-core pins and recipes
- Add `cores/<core>/toolchain.lock`, in the same format as today's lock (nextpnr, yosys and mistral SHAs plus the build flags), and set each recipe's `lock_path` to it. To start, every core's lock is a copy of the current one, so nothing rebuilds.
- Move the placement constants (seeds, weights, critexp, clocks, timeout, extra args) out of the `build_fes_*.py` constants into a `[recipes.pnr]` table in `core-recipes.toml`. The producers read that table, so each core has one source of truth.
- Toolchain slots are cached per distinct lock, so N cores spread over a few locks cost a few cached slots, not N builds. Garbage-collect any slot no lock references.
- **CI rule:** a core gets re-placed only if its RTL/SDC/QSF, its recipe or its lock changed. Shared RTL (the shell, ABI or board files) maps to every core that depends on it, through a path→core map that CI checks. Bumping the global `toolchain.lock` no longer touches any core.

## 1b. The recipe is the complete argv and env source (change 1, design only)
The audit ([recipe-option-audit.md](recipe-option-audit.md)) found that only seed, weight, critexp, clocks and the GPU backend are explicit today. Every fork flag (`--arrival-pin-assignment`, `--isolated-lut-placement`, critical-cohort, remap-*, replicate-enables), `--threads`, the `NEXTPNR_MISTRAL_PLACED_*`/`RETAINED_ENABLE` env vars and most timeouts fall back to defaults or the caller's shell, and `--gpu-device` is hard-coded for menu.
- **Schema** (`[recipes.pnr]` per core, alongside the per-core lock):
  - `seeds`, `timing_weights`, `critexp`, `clocks=[{name,mhz}]`, `freq`, `sdc`, `qsf`
  - `router` (gpu|router1|router2), `gpu_backend` (HIP|CUDA), `threads`, `timeout_s`, `timing_allow_fail` (bool, search only)
  - `[recipes.pnr.flags]`: one key per known nextpnr option, with booleans for switches (e.g. `arrival_pin_assignment=false`, `isolated_lut_placement=false`) and values for the rest. **Every known fork flag is listed with its value, even when it's off**, so a changed nextpnr default can't change a core silently.
  - `extra=[..]`: a raw passthrough for options not yet in the schema. It's still recorded, hashed and reviewed, and it gets promoted into `flags` once used twice.
  - `[recipes.pnr.env]`: the complete environment allowlist with values (`CUDA_DEVICE_ORDER`, `NEXTPNR_MISTRAL_*`, `OMP_NUM_THREADS`). Everything else is scrubbed. GPU visibility is the one exception: it comes from the gpu-run lease and `--gpu-device $FES_GPU_IDS`, and it's recorded but never pinned.
  - `settings={}`: `gpurouter/*`-style keys. Reserved, because they need a generic `--set key=value` in nextpnr first, and that would be its own nextpnr PR.
  - `playbook=[..]`: the core's documented option matrix (section 2b).
- **Single builder:** `pnr_recipe.argv(core, slot, synth, seed, gpu)` returns `(argv, env)`. Producers, `search_placer_qor.py`, the nightly, gates, sweeps, alphamister and agents all call it.
- **Enforcement test:** for each core, run its producer in `--print-commands` (dry) mode and compare against the builder's output. The test fails if the producer passes any option or env var not in the recipe, or leaves out a recipe value. A second test lists every `add_options()` name and `getenv` in the pinned nextpnr's `mistral/main.cc`, `command.cc` and getenv calls. Any option or env var the recipe schema doesn't classify (as set, default-recorded, or irrelevant: debug/output) fails the per-core lock bump. A nextpnr that adds a flag therefore forces a decision.
- **Record:** the routed evidence stores the full argv, env, recipe hash and slot, so every result can be replayed exactly.

## 2b. Timing-failure playbook (driver and nightly)
On a miss, the driver and nightly don't just reroll seeds:
- Confirm the miss with a same-GPU replay.
- Then walk the core's documented matrix: weight {prod, 300, 1000, 2000} × critexp {prod, 2, 5}, then the known-helpful fork flags one at a time on the best two pairs, then any core-specific entries, each at production seed + 2.
- The budget is at most 6 GPU-h per core per incident (ST: 10), via gpu-run, at most 2 GPUs. **Approved by Deano 2026-10-18.**
- Record every run as `runs.jsonl`. A combination that closes on 2 of 3 seeds becomes a **per-core recipe-change PR**. It is never a global default.
- The nightly gets 1 matrix step per core per night inside its window. If nothing closes, the core stays pinned and its issue is updated.
- Constraints and seals are never relaxed.
- Long term: an alphamister-style automatic search over the same option space and budget.
- The timing-failure playbook above is design context. It does not change the current `AGENTS.md` instructions.

## 2. Nightly per-core bump job
- **What it does:** each night, for each core, build a candidate slot from the tips of nextpnr, yosys and mistral main (once per night, shared by all cores), synthesise, and run that core's production recipe (its first-pass seed list, capped at production seed + 2) under `gpu-run --owner nightly`.
  - **If it meets every clock:** open or refresh one PR per core named `bump(<core>): toolchain → <shas>`. It carries only that core's lock change, a per-clock fmax table against its current pin, and the routed artifacts.
  - **If it misses:** the core stays pinned. One rolling issue per core, `<core>: latest toolchain misses timing`, gets updated with the table. It's closed automatically when a later night passes.
- **Auto-merge rule (proposed):**
  - The bump merges only if CI is green, every clock is met, no clock lost more than 3% of fmax (1.5% for marginal cores), and the seal or check that applies is green. For cores in the image that means a kit HIL under the claim rules: boot, Home, launch the core and stop it. App cores need only a package install and inspection on a kit.
  - C64-style seals are never relaxed: a failed seal blocks the bump.
  - It needs Bob's or Deano's OK to enable. Until then the PRs go to Bob like any other.
- **Where it runs:** production builds route on HIP, so the nightly must too. **Powerboat**, 00:30–07:00 EEST, at most 2 GPUs through gpu-run in FIFO order. It never preempts and stops starting new cores at 07:00. Deano's Codex harness keeps its normal place in the gpu-run queue.
  - The **CUDA box** (chip-30x0-ai-dev) gets cheap CUDA pre-screens of the same candidates. Its results don't count as seal evidence (core-recipes.toml already says this for apple2). It could take real load later, once HIP and CUDA verdicts are shown to agree.
  - **The 2-GPU cap:** the nightly gets its own owner tag, `nightly`. Should it share lander's 2 GPUs, or get its own night-only allowance? That's open question 2.
- **Measured cost per night** (route times from 791-STATUS were inflated 1.5–2× by contention):
  - The 9 non-ST cores at 1–3 seeds take about 0.5–1.2 GPU-h.
  - The ST, at 1 seed of 30 min (timeout), takes 0.5 GPU-h. Run it weekly while it's known-failing (#833).
  - The candidate toolchain build takes about 15 min of CPU.
  - Total: **about 1–2 GPU-h a night**, roughly 1 h of wall time on 2 GPUs.

## 3. Packaging: system cores in the image, the rest as apps
- **In the image:** menu, ramtest (a diagnostic, needed for kit qualification), and one example core (Pong, the smallest and quickest to close).
- **Library cores:** zx81, coleco, sms, sg1000, c64, spectrum, atari-st and apple2 (core-library.toml already lists their standings). Each one is built and sealed by its own pipeline as a versioned package, and installed through the existing core-library API by fes-update or the bundle tool.
  - The kit keeps the immutable versions, so rollback is just reselecting the previous package.
  - Seals (C64, ST native shell) ride inside the package and are checked by the native runtime's read-only inspection before selection.
- **Monorepo with per-core pipelines (recommended)**
  - For: shared RTL, ABI, shell and board stay atomic. One CI, the existing producers and bundle tooling. Path filters give most of the isolation, and it's the cheapest migration.
  - Against: one repo's CI config grows, and the path→core map has to be maintained.
- **Split repos (one per core)**
  - For: hard isolation, and independent release cadence and ownership.
  - Against: ABI and shell changes become N coordinated PRs. Shared RTL gets vendored or submoduled, with drift risk. Split repos also mean N CIs, more automated reviews (SuperGrok/Codex/Bugbot) and more agent overhead, plus breaking up a large repo.
- **Recommendation:** stay a monorepo. Split out only a core that is truly independent, for example a third-party core.

## 4. Expected effect
| Item | Today | After |
|---|---|---|
| fes PR CI (GitHub, no P&R) | median 21.8 min (8.5–28.7) | about the same; package tests add 1–2 min |
| Toolchain bump | all 10 image cores re-closed together, about 3–6 GPU-h + whole-image HIL; one bad core (zx81 on #213) blocks every core | per core, in the nightly; about 0.1–0.5 GPU-h per core; nothing blocks globally |
| PR touching one core | factory re-places that core (and the image) | the same, but only that core; package HIL only |
| HIL | a whole-image run per bump/image PR | image cores: boot/Home/Pong/stop smoke test per image change; library cores: install, select, launch, stop for that core only (about 10 min of kit time) |
| Release image | 10 cores; rebuilt when any core changes | 3 cores; smaller and faster to build and burn; changes only with system cores or the platform |

## 5. Migration path
| Step | Deliverable | Risk | Cost |
|---|---|---|---|
| 1 (about 1 day) | Add per-core locks, all identical to today's lock; point `lock_path` at them; CI checks every lock resolves to a cached slot. No rebuild. | none | about 2 h of Luna 6.1 agent time, 0 GPU |
| 2 (2–3 days) | Full recipe schema (all flags, extra, env) in `core-recipes.toml` [recipes.pnr], plus the single argv builder and the producer-vs-recipe and nextpnr-option-coverage tests; producer argv unchanged | low | about 4 h agent time; one replay of each core's production seed (about 1 GPU-h) to prove identical results |
| 3 (1 day) | path→core map plus CI change detection ("re-place only touched cores") | low | about 3 h agent |
| 4 (2 days) | nightly runner on Powerboat, report-only (issues, no PRs) | low | about 4 h agent; 1–2 GPU-h a night |
| 5 (after 1 week of nightly data) | nightly opens bump PRs; Bob merges by hand | medium | none extra |
| 6 (with Deano's OK) | auto-merge for passing bumps | medium | none |
| 7 (2–3 days) | image slimmed to menu, ramtest and Pong; library cores shipped by fes-update/bundle into the core library, with a kit HIL on A and B | medium (kit flow) | about 6 h agent; 2 kit sessions |
Steps 1–2 are the current #843 delivery scope and preserve the effective producer commands. Steps 3–7 remain proposed follow-up work.

## 6. Risks
- **Lock skew:** cores on different toolchains mean more cached slots and disk space, and a core whose bump never passes ends up on a very old toolchain. Mitigations: show each core's pin age on a dashboard, and file an issue after 30 days unbumped.
- **Shared RTL changes** still re-place every dependent core. The path→core map has to be right, or a core goes stale unseen.
- **Auto-merge** could ship a passing but worse core, which is why the fmax-drop tolerance and HIL are required.
- **GPU contention** with Deano's harness at night: the nightly yields only through queue position, so a long night shift by the harness delays bumps, but never blocks fes.
- **Kit format compatibility:** library packages need an ABI version check against the image. The native runtime's inspection already does this, but old kits must reject newer ABIs cleanly.

## Open questions for Deano
1. Image contents: menu, ramtest and Pong? Or leave ramtest out of the image and install it as a diagnostic app?
2. The nightly's GPU budget: share lander's 2 GPUs, or get its own night-time allowance? And what window (00:30–07:00 EEST proposed)?
3. Auto-merge: allowed at all, and under which conditions? Is a kit HIL required for library cores, or is package inspection enough?
4. ST while it's known-failing (#833): weekly nightly only, or excluded?
5. Do per-core `c64.lock` or similar files exist off main that should be adopted?
6. Monorepo with per-core pipelines (recommended), or does he want split repos for some cores?
