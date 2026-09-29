# RAM-test: generic report-guided local remapping

Dated host-only record, 2026-09-29, for [issue #264](https://github.com/DeanoC/fes/issues/264).
This experiment implements and measures a generic mapping/placement pass in nextpnr.
The production RAM-test recipe and the diagnostic compiler pins remain unchanged.

## Comparison inputs

The generic compiler stack (Yosys `acf441cc`, nextpnr `d91c902b`, Mistral `7ed06e21`),
GPU 1, seed 2, HeAP timing weight 10/criticality exponent 2 and enable-replication
budget 4 produces 109.051254 MHz memory / 81.307426 MHz pixel on current committed
RTL and diagnostic BUILD_ID `52991ce22369904849b68bdd900f9ae1`. The same generic
stack on the frozen older RTL/BUILD_ID produces 114.142227 / 88.487747 MHz.
The historical 116.918037 MHz included two default-off RAM-test-specific probes;
it is not the current RTL's generic result. Earlier experiments are recorded in
[PR #304](https://github.com/DeanoC/fes/pull/304).

A fresh Quartus 17.0.2 fit with all 13 current RTL files and BUILD_ID matched reaches
134.17 MHz memory. Its ready-to-skid path crosses one MLABCELL (3.830 ns data delay,
+2.968 ns setup slack at 130 MHz). The OSS path crosses two LUTs plus long
interconnect: 7.171 ns routing, 0.800 ns logic. Tool-specific PLL/SDC inputs still
differ; this is a mapping/locality lead, not equivalent-route delay calibration.
Raw matched Quartus evidence is `out/ramtest-compiler-gains/quartus-current-matched/`.

## Compiler change

The opt-in `--remap-critical <previous-timing.json>` validates relevant reported
path cells, ports, edges and placements against a fresh live HeAP Context. It
composes the final two ordinary LUTs of a failing FF-enable path into at most six
inputs, preserving both original LUTs and their side consumers. A new LUT serves
one complete LAB enable group. Bounded variants retain the register positions or
translate the primary group by one neighboring tile with unchanged z coordinates.

Selection uses graph structure and timing, with no RAM-test names, coordinates,
truth tables or Quartus dependency. Constants, inversion and shared inputs enter
the truth-table calculation. Full occupied-BEL legality, at least 250 ps predicted
primary-group improvement, all-clock predicted Fmax and hold guards qualify candidates.
Modeled hard data consumers require constrained clocks and individually
nonregressing timed setup slack; untimed sentinels, clock/I/O consumers and frozen
boundaries remain excluded. Translation retains stricter output-boundary checks.

`--remap-groups N` (1–8, default 1) permits additional whole LAB groups to share
the copy. At most 32 additional groups are examined in order of original worst
setup slack and LAB coordinates. Every added endpoint must improve by at least
250 ps. Additional groups stay placed; the final retained subset gets fresh
STA, legality, hold and boundary checks. `--remap-candidate 0` retains the first
qualified candidate for full routing; omitting the candidate lists and restores.

The initial strict boundary policy rejected current RTL because a composed input
also feeds HPS command-valid. That attempt did not route. The refined policy
admits a modeled, constrained, nonregressing data endpoint. The frozen design
instead has seven distinct inputs in its critical ALUT6/ALUT3 pair; both strict
and refined preflights decline it before routing. There is no new frozen Fmax.

## Routed results

| Current RTL, same seed/options | Memory MHz | Pixel MHz | Capture MHz |
| --- | ---: | ---: | ---: |
| Generic baseline | 109.051254 | 81.307426 | 464.968536 |
| Single-group remap | 108.365845 | 83.063377 | 464.968536 |
| Multi-group remap, budget 8 | **112.170502** | **82.453827** | **464.968536** |

Both remap trials completed legal routes and RBF generation with final analogue
signoff and no final reported hold violations. The multi-group result improves
memory by 2.86% and pixel by 1.41% over baseline. It remains below the historical
116.918037 MHz and the 130 MHz target. This is one design/seed measurement,
retained as an experimental gain; it is not a broadly qualified default.

The single-group trial composed an ALUT4 (`0x4440`) at LAB (18,10), moved one
register from (17,10), and predicted 1.575 ns improvement. Final routing regressed
memory from 109.051254 to 108.365845 MHz; the critical path moved from that register
to another register in the destination LAB still served by the original LUT chain.
This trial is rejected. The multi-group follow-up addresses this specific observed
limitation through general whole-group selection. Its final critical path is
HPS `cmd_ready_1` → slot-free ALUT2 (27,21) → a separate ALUT4 (17,22) →
`hps_ddr.m1_burstcount_MISTRAL_FF_Q_7.ENA` (13,14), with 6.997 ns routing,
0.800 ns logic and 8.915 ns effective setup total. The mapped cell suffix is not
a claim about the logical bit index. Only one original register placement moves;
its route-through moves too, and another route-through disappears through normal
LAB preparation. Both original composed LUTs remain for their other consumers.

The next concrete compiler lead is applying equivalent mapping/locality improvements
to that separate burst-count cone while retaining this gain. The present pass
retains one candidate per invocation; it does not automatically stack remaps from
successive routed reports. The frozen design also needs a richer LUT cut than
this pass supports. Neither lead is implemented or assumed to improve timing.

## Verification and scope

Compiler source is committed through `095b7e4c84e327007aab48e8d546703ed1e0b2fa`
on `feat/mistral-local-path-remap` (earlier implementation commits `b7893a4e`,
`486243da`). Full backend: 43 tests passed, one optional external-snapshot test
skipped. Tests cover exhaustive small truth tables, constants, inversions, aliases,
six-input limits, whole-group mutation, translation, exact indexed-user restoration,
stale/protected reports, implicit clocks and modeled/unrelated hard capture clocks.
Independent source review found no remaining concrete issue.

Pinned, disabled, initial list-only and latest multi-group list-only current
placement outputs are byte-identical: 14,862 cells, SHA-256
`7008ed7126181998f43222ad387b667b963dc10abb05fe374ddd3fd9a28c97ee`.
The latest list-only run explored twelve qualified candidates and restored the
entire exported module: cells, nets, attributes and settings. The selected
multi-group candidate rewired seven enables in four original LAB groups.
These placement checks and predicted gains are not routed Fmax.

The pass is an opt-in experiment, not a timing-closure guarantee. Predictions
are screening criteria; completed routing and final analogue timing determine
adoption. No RAM-test RTL, production recipe, diagnostic lock, shared contract,
ABI or hardware changed. The installed pinned compiler is untouched.

## Reproduction and evidence

Compiler implementation is on nextpnr branch `feat/mistral-local-path-remap`,
based on `d91c902b`; see its `docs/local-path-remap.md` for bounds and exclusions.
The adjacent JSON records compiler commits, binary/input hashes, exact commands,
completed results and critical paths. Raw local evidence is
`out/ramtest-compiler-gains/local-remap/`; baselines and frozen synthesis are
adjacent under `out/ramtest-compiler-gains/`, while current input/baseline artifacts
are under `sources/misteross/build/fes-ramtest-timing-130/`.

Run from `sources/misteross`, using the same synthesized input and a completed
baseline timing report, with device `5CSEBA6U23I7`, QSF
`cores/fes-ramtest/constraints.qsf`, SDC `boards/de10nano/clocks.sdc`, `--freq 74.25`,
`--seed 2 --placer-heap-timingweight 10 --placer-heap-critexp 2 --replicate-enables 4
--router gpu --gpu-device 1 --timing-allow-fail --detailed-timing-report`, plus
`--remap-critical <baseline-report> --remap-candidate 0 --remap-groups 8` and
`--write`, `--report`, `--rbf`, `--compress-rbf` output options. Clear diagnostic
`NEXTPNR_MISTRAL_*` environment hooks. A list-only control substitutes `--no-route`
for RBF output options and omits `--remap-candidate`.

Raw outputs are ignored: a fresh clone needs to regenerate baseline inputs and
reports or receive these saved artifacts. Report path/placement validation rejects
stale relevant paths. No saved-placement import or hardware acceptance is claimed.
