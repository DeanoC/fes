# PC/XT controlled local-placement experiment, 20 October 2026

**One previously failing CPU endpoint now passes with 1.452 ns setup slack at 100 MHz. Whole-board timing remains open.** This is a diagnostic physical-placement witness, not a promoted compiler policy or accepted core. PR #836 remains parked on #840.

## Scope and fixed inputs

FES base is `220aef777b8621906609a9efaa5408fbc1585216`. The owner is FES/misteross for the retained diagnostic and nextpnr for the inspected physical implementation. No production RTL, compiler pin, recipe, package identity or shared contract changes.

This continues the [CPU-locality investigation](../2026-10-20-pcxt-cpu-locality/summary.md), using its private two-level ADD-mux/arithmetic-region seed-1 profile. That profile includes the earlier private PIC shortcut, fixed microcode RAM sites and 3500 ps ROM phase; none is newly adopted here. The original full-flow route achieved 87.757782 MHz. A no-route reload emits the **identical RBF** and the same final analogue frequencies. Its per-hop analogue dump is the source of `model-comparison.json`; individual hop values are integer picoseconds, so their sums differ slightly from full-precision path reports.

The retained pre-route checkpoint contains 10,462 cells, all at the same sites as those cells in the original routed design. `placed.json.gz` contains those exact checkpoint bytes, with hashes in `fixture.json`. This avoids treating the 1,889 route-through cells subsequently inserted by routing as original placement inputs. The checkpoint is an unsealed diagnostic, not an FPGA payload.

All routes use the original nextpnr `c2c01bda6d93cfe6e4d32d54e4d408995396e068` executable, SHA-256 `02ae857f470daf5847cfe8a0fb8ed11321b3e95eb9d6204d5798964ad282744e`, with the same seed, clocks, QSF and SDC. Neither the router nor its timing model is patched. A separate host-only executable adds the retained `probe_hook.inc` to inspect and enumerate local moves. It cannot run packing, placement or routing while the probe is active.

## What explains the detour

The original worst path crosses from carry at (39,4) to the result mux at (45,7), then back to a last-result register at (39,4). Those two routed connections cost 1.573 + 2.030 = **3.603 ns**. The mux output drives 16 consumers distributed through the CPU. Its site serves several destinations; optimizing only the return to the last-result register ignores that tradeoff.

The existing placement model predicts 1.410 ns for each of these two connections. Another path segment, operand mux to carry, is predicted at 1.270 ns and measures 1.970 ns. These are concrete local prediction errors, not proof that all routing-model costs are optimistic or that changing one coefficient will close the board.

Moving the result mux into the carry LAB is rejected by existing legality checks. The target LAB starts at the conservative 42-input budget; candidate sites 49, 54 and 55 raise the sum to 46, 47 and 47 respectively. Site 49 also shares an ALM with arithmetic logic, which cannot mix with a normal LUT under the current model. `check_lab_input_count` explicitly reserves four of the 46 TD inputs and conservatively sums ALM inputs. Its comments identify local feedback and cross-ALM input sharing as opportunities requiring routing-aware treatment. This experiment does **not** relax that rule or establish that the rejected placements are physically routable.

Instead, exchange two existing ALUT5 sites:

| Cell, under `system.machine.cpu.cpu.u_eu_core.` | Before | After |
| --- | --- | --- |
| `eu_alu_out_MISTRAL_ALUT5_Q_1` | `MISTRAL_COMB.45.7.42` | `MISTRAL_COMB.40.4.19` |
| `idiv_opcode_MISTRAL_ALUT5_A_E_MISTRAL_ALUT5_Q_1_E_MISTRAL_ALUT6_Q_F_MISTRAL_ALUT5_Q` | `MISTRAL_COMB.40.4.19` | `MISTRAL_COMB.45.7.42` |

The direct candidate changes **only these two BEL attributes**. Cell functions, connected ports, pin snapshots, constraints and every other original cell placement are unchanged. Both affected LABs pass the native local checks. The whole-checkpoint checker also reports the same 26 pre-existing failures in unrelated LAB (24,8) in both inputs; those are retained, not silently counted as a whole-design legality pass. Their cause is not diagnosed here. Both actual routes complete routing and final analogue analysis; the system setup gate fails.

## Controlled result

A saved-placement replay is a different routing entry point from the earlier complete flow. Its control result is therefore the comparison baseline, not the historical 87.758 MHz result.

| Run | System MHz | Pixel MHz | ROM MHz | Capture MHz | Target setup slack |
| --- | ---: | ---: | ---: | ---: | ---: |
| Fixed-placement control | 90.777054 | 94.375237 | 143.646408 | 242.248077 | −1.016 ns |
| Two-cell swap, reserialized checkpoint | 90.851280 | 92.250923 | 136.755737 | 242.248077 | +1.452 ns |
| Two-cell swap, direct two-field edit | 90.851280 | 92.250923 | 136.755737 | 242.248077 | +1.452 ns |

The two swap runs emit **byte-identical RBFs**. The second eliminates checkpoint reserialization as a variable. All required clocks other than the 100 MHz system clock pass; native hold checks pass in all three runs. Full per-clock summaries, endpoint evidence, commands, logs and artifact digests are in `runs.jsonl`.

The target endpoint is `eu_alu_last_result_MISTRAL_FF_Q_5`. Its setup slack improves by **2.468 ns**, with no extra pipeline cycle or logic change. The carry-to-result-mux route drops from **1.593 ns to 0.480 ns**. This is evidence that this CPU path can meet 100 MHz through physical implementation changes.

Whole-board worst timing improves only slightly: the critical endpoint moves to `eu_register_di_MISTRAL_FF_Q_12`. Other connections can worsen: the result mux's sampled route toward AX logic rises from 0.915 to 2.048 ns. The swap is therefore a useful physical witness, not a sufficient core fix. Neither 100 MHz feasibility for every CPU path nor repeatable whole-core closure is established.

## Reproduce and validate

Host-only evidence validation:

```sh
python3 docs/validation/2026-10-20-pcxt-local-repair/check.py
```

The script checks fixture hashes, exactly two changed BEL fields, all three terminal route records, unchanged timing requirements, hold results, the target improvement, the identical swap RBF hashes and the explicitly retained legality limitation.

Replay with the exact compiler binary specified in `fixture.json`, from this FES checkout:

```sh
python3 docs/validation/2026-10-20-pcxt-local-repair/replay.py \
  --nextpnr /absolute/path/to/c2c01bda/nextpnr-mistral \
  --variant control --output /absolute/new/control-directory
python3 docs/validation/2026-10-20-pcxt-local-repair/replay.py \
  --nextpnr /absolute/path/to/c2c01bda/nextpnr-mistral \
  --variant swap --output /absolute/new/swap-directory
```

The helper enforces the compiler hash, uses the existing FES phase-lease wrapper, rejects ambient GPU pinning and caps each invocation at 600 seconds of own work. It reports failing setup as failure. It is not a producer or a seal command.

`build_probe.py --source <clean-c2c01bda-nextpnr> --build <existing-matching-Ninja-build> --output <new-directory>` builds the host-only probe separately, reusing the matching build objects. It changes neither that checkout nor its existing executable. Set `PCXT_DIAGNOSTIC_MOVE_PLAN` to a JSON file containing `cell`, `x`, `y`, and optionally a chosen `bel`; run against an unrouted placed checkpoint with `--no-pack --no-place --no-route`. An empty-site probe reports local legality and input counts. A plan with coordinates outside the device performs the baseline audit without moving anything. This is a retained diagnostic hook, not a supported production nextpnr option.

Parent `make check`, documentation-only `make test-changed` against the base, Python compilation, helper argument parsing, fixture validation and `git diff --check` pass. No new RTL simulation, Quartus qualification, producer seal or hardware run is claimed.

## Bound and next integration step

The user explicitly authorized this local implementation investigation after the previous campaign. It was bounded to three routes of at most 600 own-work seconds each. They charge **761.293 seconds** total, excluding only logged lease waits rounded down. Shared FIFO phase leases use owner `pcxt-agent`, maximum two GPUs, strict live-GPU routing and no vendor/device pin. Historical plus current work totals 22,182.035 seconds (6.162 hours); this explicitly authorized follow-up is not represented as remaining under the original six-hour cap. All jobs are terminal.

The useful next compiler change is report-guided placement of several critical carry/result-mux/register paths together, with swaps into occupied nearby sites and evaluation of all affected consumers. The witness shows why empty-site moves alone are insufficient, and why blindly concentrating everything into the carry LAB fails. A generic repair should preserve packing rules and accept changes based on routed endpoint improvement without new setup/hold regressions; a PC/XT-specific cell-name rule or a global timing-model coefficient change is not justified by this one witness.

The frozen checkpoint and direct two-cell transformation provide a reproducible benchmark for that work. No production patch is promoted from this investigation. Acceptance still requires a same-recipe two-of-three candidate, a fresh full producer build and exact-artifact timing qualification. #836 is not ready to merge.
