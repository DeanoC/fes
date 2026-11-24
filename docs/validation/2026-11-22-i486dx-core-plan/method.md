# i486DX core plan: the measurement method, committed before any fit or simulation

Committed 22 November 2026, before any fit, lockstep run or profile run in this
record was queued. Later amendments are dated and say what had been seen by
then. No band, window or rule is changed after a result exists.

This record measures; it changes no RTL. The patches under
[`patches/`](patches/) are scratch ablations for measurement only. They are
never merged, and nothing here is a seal.

## The question

Deano, 22 November 2026: a separate i486DX core for the DE25-Nano only. It
would carry the integer unit plus the 387 and be optimised to fit, not for
CPI or Fmax. It forks from `fes_i386_cpu`, which stays speed-tuned for both
boards. Before the fork, the plan needs measured answers to two questions:
what each integer speed feature costs in Agilex 5 ALMs, and what it buys in
CPI.

## What is measured

### A. Area: ablation fits of `fes_i386_cpu` on Agilex 5

- **Source:** `origin/main` at `dafabb6db`, as a detached worktree per variant.
- **Design:** `fes_i386_cpu` alone, through `scripts/fit_i386_device.py --design cpu --device de25`, unmodified.
  - This is the virtual-pin envelope of #1899's method.
  - `x87_present` is a free virtual input, so the whole 387 is alive, as in fit C of #1899.
  - Fixed: 61.44 MHz (the script's default period), the A5EB013BB23BE4SCS device, and Quartus Pro 26.3.
- **Baseline `dx`:** main plus [`patches/dx.patch`](patches/dx.patch).
  - The patch sets `fes_i386_cpu`'s `I486` default to 1, so the fitted CPU is the 486 identity, with the 387 alive.
  - This is the core a 486DX would fork from.
- **One scale fit `386`:** main unpatched (`I486 = 0`), seed 1.
- **Ablations:** `dx` plus one patch each. Each patch turns one feature off at its use, so Quartus sweeps the logic that only that feature reads:

  | Variant | Patch | What it turns off | Functional? |
  | --- | --- | --- | --- |
  | `pred` | `pred.patch` | All branch prediction: `pred_on` is 0, which is today's `queue16` behaviour. The BTB (#1511), return stack (#1512) and indirect (#1571) are all unread. | yes |
  | `ras` | `ras.patch` | Return-stack prediction: no RET entries are written, fetch never pops the stack, and a RET entry's target is never the stack top. | yes |
  | `ind` | `ind.patch` | Indirect prediction: no indirect JMP or CALL entries are written. | yes |
  | `stld` | `stld.patch` | The bits-11:0 overlap test (#1515): every load behind a store in AG waits, as before #1515. | yes |
  | `ealu` | `ealu.patch` | The early ALU at AG (#1517): `dc_q.ealu` is 0. | yes |
  | `lfwd` | `lfwd.patch` | Load forwarding into an address (#1522): `ldfwd` is 0. | yes |
  | `speed` | `speed.patch` | `pred`, `stld`, `ealu` and `lfwd` together: the Phase 4 speed set off. | yes |
  | `pg` | `pg.patch` | Paging: `pg` is 0, so the TLB and walker logic is unread. **An area bound only.** A DX needs paging (Windows 3.x enhanced mode, Linux, DOS extenders), so this variant is never a candidate. | no |

  **Lower bounds.** `ras` and `ind` are lower bounds, not exact costs. `ras` leaves the EX-side target compare in place. `ind` turns indirect prediction off only by never writing indirect entries, so the logic that reads them is not provably dead to the compiler. `pred` bounds all three together. #1524 (the front-end split) changed no RTL, so it gets no ablation. Its CPI breakdown is cited from its record.
- **Seeds:** `dx` at seeds 1, 2 and 3; every other variant at seed 1.
- **Netlists:** one netlist per variant, elaborated by the i386 lock's Yosys through the script's own `cpu_netlist`.
  - Every elaboration runs with `YOSYS_MAX_THREADS=1`. The Yosys OPT_CLEAN thread race fix (DeanoC/yosys#81, PR #2130) is not on main, so one thread is the stable setting.
  - `dx` seeds 2 and 3 reuse seed 1's netlist through `--netlist`, so between those three fits only the seed varies.
  - **These are exploration fits.** They do not stand in for a fit on the fixed Yosys lock.
- **The figure:** Quartus's "ALMs needed [=A-B+C]". The LAB row is not used (see #1899's record).
- **Timing:** each fit's gate result is reported, but nothing is judged on it. These fits measure area; they do not claim a clock.

**Rules, fixed now:**

1. **Noise band.** The band is N = max(the spread of `dx`'s three seeds, 100 ALMs). #1899 measured the CPU's seed spread at 73.
2. **Saving.** A variant's saving is `dx` seed 1 minus that variant at seed 1. The median of `dx`'s three seeds is printed beside it.
3. **Under noise.** A saving of N or less is reported as "under noise", with the figure shown.
4. **The combined fit.** `speed`'s saving is reported against the sum of its parts' savings. The difference is reported, and nothing is inferred from single deltas adding up.
5. **What a fit that fails proves.** If a variant's fit fails to compile or hits the 90-minute cap, its row says so. Nothing is estimated in its place.
6. **The ALM unit.** No figure here is compared with a Cyclone V figure.
7. **Per-module rows.** These come from #2028's hierarchy fit of the 387-on machine (`docs/validation/2026-11-20-i386-x87-s6-measure/entities.tsv`, main `8a162c536`). They are labelled with that base. No new hierarchy fit is run.

### B. Correctness of each functional ablation

A fit of an ablation counts only if the ablated RTL passes the lockstep subset
below. Otherwise Quartus may have swept more than the feature.

- **The run:** `scripts/test_fes_i386_lockstep.py`, on that variant's worktree, with these settings:
  - `--random-programs 500 --random-seeds 8 --limit-per-file 20`;
  - the protected-mode, task and debug program counts at their defaults;
  - the i386 lock's Verilator.
- **The rule:** a variant passes when its report has the same failures as `base` (main with no patch) on the same settings, and no new ones.
- **`pg`:** it fails lockstep by construction, so it is not run. It is an area bound only.

### C. CPI: a DOOM window on the RTL of each functional variant

- **The bench:** `machine_tb` (`Vpc386_machine`), built in each variant's worktree by the cpu-profile suite (`scripts/sim_fes_pc386.py --suite cpu-profile`), with the system Verilator as the pc386 simulations use it.
  - The machine's CPU is `I486 = 0` here, the pc386 default. The identity changes the instruction set, not the timing of these paths.
- **The disk:** the census `pmode` drive, `b4be6e29…`, which carries DOOM v1.9 (`06ee36b4…`).
- **The command:**

  ```
  machine_tb --bios pc386-bios.bin --vgabios pc386-vgabios.bin --disk pmode.img --cpu rtl \
    --cpu-profile cpu.prof --limit-seconds 300 --type 'cd \doom|doom -timedemo demo3|' \
    --rtl-at-flip 40 --rtl-for 1.0 --cpu-profile-every 0.5
  ```

  These are the Phase 4 records' flags; only the disk and the `cd` path differ.
- **The windows:**
  - **h2**, the primary window: from the handover snapshot plus 0.5 s to the end snapshot (`scripts/pc386_cpu_profile.py --from --to`).
  - **h1**, also reported: from the handover to the handover plus 0.5 s.
- **CPI:** the window's clocks divided by the instructions it retired.
- **The comparison:** each variant's CPI over `base`'s CPI in the same window. The oracle hands over at the same flip, so every variant runs the same instructions from the same state.
  - If two variants' handover times differ, the oracle did not reach the same state, and that row is reported as not comparable.
- **Latency bench:** `scripts/test_fes_i386_latency.py` is run per variant, and its per-loop clocks are reported. A changed loop is expected; that is what the bench shows.

**The governor rule (question 3), fixed now.** The figures come from
`docs/validation/2026-11-19-pc386-local-bus-video/summary.md`, Kit A, DOOM
`MAX.CFG` realtics:

| Preset | Realtics |
| --- | ---: |
| `max` | 2,062 |
| 486DX2-66 | 2,912 |
| 486DX-33 | 4,682 |

For a variant with h2 CPI ratio r, its `max` realtics are taken as at most
2,062 × r. This first-order bound treats all of `max`'s time as CPU time,
which overstates the loss, since part of that time is I/O and VGA.

- A preset's DOOM speed is **kept by the bound** when 2,062 × r ≤ that preset's realtics.
- Otherwise the result is **not settled by the bound**, because a kit run would be needed.
- #2000's own reach verdicts (DX-50 not reached on 3 of 6 benchmarks, DX2-66 on 4 of 6, after the local-bus fix) are cited as they stand.

## Where it runs

- **Host:** powerboat.
- **Quartus:** Pro 26.3, under the shared lease the fit script takes for itself. Every fit holds one slot.
- **Workspace:** `/data/projects/i486dx-plan`, outside `/tmp`. The fit outputs, profiles and lockstep reports stay there. The record copies each fit's `fit.json`, `timing.json` and `inputs.json`, each profile window's JSON, and each lockstep report's summary.
- **Guards:** jobs refuse to start under 30 GiB free, and they write a heartbeat. Each fit's cap is the script's 90 minutes.
- **No GPU** is used: Quartus places and routes on the CPU. No kit is touched.

## Amendments

1. **22–23 November 2026. Two fits re-run; nothing in a band, window or rule changed.**
   - **What failed.** The `pg` and `speed` fits stopped before elaboration. Their error was "install is not ready". The pc386 lock's Yosys install (`64386b95…`) and then its Mistral install had been pruned from the shared toolchain cache while the lanes ran.
   - **What was seen first.** By then the fits `386`, `dx` (seeds 1 to 3), `pred`, `ras`, `ind`, `stld`, `ealu` and `lfwd` had finished.
   - **The repair.** `make toolchain-fes-pc386` re-provisioned the same locked commits under the same install keys. `pg` and `speed` then ran once each, with the same scripts and inputs.
   - **Evidence kept.** The failed output directories are kept beside the reruns on powerboat (`*.failed-*`).
2. **22 November 2026. The simulation lanes run in parallel; nothing in a window or rule changed.**
   - **Why.** The oracle takes over an hour to reach the DOOM handover, so running the eight variants in sequence would have taken about 12 hours.
   - **What changed.** The two sequential lanes were stopped between variants, by their own PIDs. `base`, `pred` and `stld` were left running.
   - **The other five.** `ras`, `ind`, `ealu`, `lfwd` and `speed` started at once, from a copy of the runner (`runners/i486dxplan_sim8h.sh`). The copy differs only in `machine_tb`'s cap, 8 hours instead of 4.
   - **No DOOM window had produced a profile yet.**
3. **23 November 2026: one exploratory hierarchy-kept fit of `dx` is added. Rule 7 is not replaced. Every result in results.md had been seen when this was written.**
   - **What it is.** An exploratory study, added after the results, not part of the precommitted method. Rule 7 stands: the precommitted per-module rows are #2028's, labelled with that base, and the summary cites them as such.
   - **Why it was added.** #2028's table predates #2037 and #2059, and it is a machine fit, not this record's CPU envelope. Section 4 of the summary also needed to know which module the slow path is in.
   - **What is fitted.** `dx`'s source is elaborated with Yosys's `flatten` removed exactly once and modules given plain names, as #2028's `elaborate.py --hierarchy` did. The script is [`elaborate_hier.py`](elaborate_hier.py), which uses the fit script's own `cpu_netlist`. That netlist is fitted at seed 1 through `fit_i386_device.py --netlist`.
   - **Its ±5% gate was chosen after the results.** It is #2028's gate, borrowed for consistency, and it cannot make the study precommitted. Within it, the per-entity table is reported as exploratory module bounds; outside it, as indicative. No saving and no decision rule reads either.
   - **The slow path.** The worst setup path's endpoints are reported by entity name, as an exploratory observation.
