# i386 TLB: each entry loads under its own enable, 9 November 2026

This record covers one change to `fes_i386_tlb`, the module behind the
16-entry DTLB and the 8-entry ITLB that Phase 5's paging slice built
([#1579](https://github.com/DeanoC/fes/pull/1579)). The fill wrote
`e[slot] <= fill_ent`, a variable-index write into the packed entry array.
The Yosys-elaborated netlist turns that into shift logic over every entry
bit. Each entry now loads under its own enable (`!flush && fill && slot ==
k`). The flush and fill behaviour is the same as before. The
area audit ([#1584](https://github.com/DeanoC/fes/pull/1584), record
`docs/validation/2026-11-08-i386-area-audit/`) found the cost and
measured the saving in an analysis fit. Tracker:
[#1548](https://github.com/DeanoC/fes/issues/1548).

**Result.**
- **Area.** The seal uses 36,225 ALMs. Main at `18c9d99c1` uses 37,139 at
  seed 2, so the change saves **914 ALMs**. The fit-to-fit noise is about
  ±200.
- **Main does not route at seed 1**
  ([#1586](https://github.com/DeanoC/fes/issues/1586)). Main's seed-1 build
  places, then its router stops on congestion from hold fixes ("Can't fit
  design in device"). At seed 2 it seals.
  Main has no seal since #1579 merged; #1579's seal predates the merge with
  main. This change seals at seed 1.
- **Seal.** Quartus 17.0.2 meets every analysis at 61.44 MHz: setup
  +0.396 ns, hold +0.083 ns. The worst path is D's register-forwarding
  target (`dfwd_ready` into `r_dst`), which this change doesn't touch. Every
  register group the TLBs feed gains slack, between +0.6 and +1.5 ns.
- **Lockstep.** Everything passes against Bochs on the RTL and on the
  locked Yosys's netlist. All 980 retirement-trace hashes match each other
  and #1579's RTL run: the corpus, 32,000 random programs, 8,000
  protected-mode programs (126,251 paged retirements, 824 #PF deliveries)
  and the 54 paging cases.
- **test386.asm** on the RTL is as #1579's: POSTs 08 and 09 pass, and it
  stops in POST 20 at the call gate after 795,527 retirements.
- **Kit B, max.** DOOM v1.9 `-timedemo demo3` runs at 40.05 fps (#1579's
  seal 40.08). Time Circle reaches its main menu. Wolfenstein 3D, Ultima VI and DOOM start and play.

## The change

`sources/misteross/cores/fes-common/rtl/i386/fes_i386_tlb.sv`:

```systemverilog
// before, in the always_ff that also clears v on a flush
end else if (fill) begin
    v[slot] <= 1'b1;
    e[slot] <= fill_ent;
    ...
// after
for (genvar k = 0; k < N; k++) begin : g_entry
    always_ff @(posedge clk) begin
        if (!flush && fill && slot == IW'(k)) e[k] <= fill_ent;
    end
end
```

`slot`, `v` and the round robin are unchanged. A flush still clears every
valid bit and loads no entry. The audit's analysis fit left out `!flush`;
this change keeps it, so the RTL loads exactly the entries it loaded before.

## Lockstep

`scripts/test_fes_i386_lockstep.py --jobs 12`, with every default, on
`020107e14`. It ran on the RTL and on the netlist from the i386 lock's Yosys
(`238eac9a4`):

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 (8,131,380 retirements) | same |
| Protected-mode programs | 8,000 of 8,000 (453,731 retirements, 126,251 paged, 824 #PF deliveries) | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 147, 54, 1,933 and 27, all pass | same |
| Holds without latency | pass | pass |

The two reports' 980 trace hashes are identical to each other. They are
also identical to #1579's RTL report at `93928cea2`. The TLB holds the same
entries at every clock, so nothing a test can see has changed.

## Seal

The seal is Quartus 17.0.2, seed 1, from clean checkout `020107e14`
([seal-summary.json](seal-summary.json)).
- **Package.** `79c5009c…`, build `20d54561…`, with `core.rbf`
  `b113f84c…`.
- **Analyses.** Every one is met:
  - setup +0.396 ns;
  - hold +0.083 ns;
  - recovery +7.022 ns;
  - removal +0.266 ns;
  - minimum pulse width +1.122 ns.
- **Clocks.** The system clock is 61.44 MHz.

| Resource | This seal | Main `18c9d99c1`, seed 2 | #1579's seal `416e332f` (before main merged in) |
| --- | ---: | ---: | ---: |
| ALMs | 36,225 | 37,139 | 36,751 |
| Registers | 32,843 | 32,655 | 32,350 |
| M10K blocks | 265 | 265 | 265 |
| DSP blocks | 38 | 38 | 38 |
| Setup | +0.396 ns | +0.674 ns | +0.620 ns |
| Hold | +0.083 ns | +0.069 ns | +0.111 ns |

**Main at seed 1.** `scripts/build_fes_pc386_quartus.py --seed 1` on main
`18c9d99c1` stops in the fitter. Placement succeeds. The router then
reports congestion around X45–55, Y35–45, from routing delay added for hold
times, ends the routing phase and fails with "Can't fit design in device"
([main-seed1-fit-failure.txt](main-seed1-fit-failure.txt)). At seed 2 it
routes and passes ([main-seed2-summary.json](main-seed2-summary.json)).
Seed 2 is the comparison above. With this change, seed 1 routes with about
900 ALMs to spare.

[stage-slack.json](stage-slack.json) gives each register group's worst setup
slack at the slow −40 °C corner, for this seal and #1579's. The groups the
TLBs feed:

| Path | #1579's seal | This seal |
| --- | ---: | ---: |
| The walker's DTLB fill (`cpu_dtlb_`) | +1.65 | +3.16 |
| F1 to the L1I's ITLB (`cpu_l1i_itlb_`) | +2.74 | +3.60 |
| F1 to the L1I's page registers (`cpu_l1i_page`) | +1.58 | +2.47 |
| M1 to the L1D's walker request (`cpu_l1d_q_`) | +1.44 | +2.07 |
| L1D hit registers, the DTLB's parallel compare (`cpu_l1d_hit`) | +3.47 | +4.11 |
| M1 to M2's load register (`m2_`) | +1.38 | +2.41 |
| D's register-forwarding targets (`r_dst`, `r_src`, `r_cnt`) | +1.11, +1.47, +2.04 | +0.40, +0.63, +0.87 |
| D's issue register (`dc0`) | +1.07 | +1.07 |

The worst path moved to D's forwarding into `r_dst` (`dfwd_ready` to
`r_dst`, 15.2 ns of data delay). This change doesn't touch it: its slack
moves with placement, and here it lost what the TLB paths gained.

## Kit

Runs `k1` and `k2` used Kit B, core-only, on 8 and 9 November
([kit-scores.json](kit-scores.json)).
- **Package.** `79c5009c…`, the seal above, at max (preset 0), with each
  launch after a SCRUBX scrub.
- **Kit.** Baseline software `a8cd30c4` on image `f904961e…`, with no
  install, no image change and no reboot. After each run Kit B was checked
  against its snapshot and logged FREE. Then the run's FogCast data trees
  were removed, and that was logged too.
- **Disks.** The Phase 4 bench disk (`fe81c9c4…`) and the #1533 game drives
  `dpmi` (`48ab2972…`), `real` (`719dfedb…`) and `pmode` (`75d909ca…`),
  read from Powerboat by SHA-256. None is committed.

| Benchmark | #1579's seal (k1, Kit B) | This seal (k1, Kit B) |
| --- | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,374 realtics, 40.08 fps | 3,863 in 3,376, 40.05 fps |
| Pack DOOM, maximum detail | 2,134 in 2,061, 36.2 fps | 2,134 in 2,061, 36.2 fps |
| Pack DOOM, minimum detail | 2,134 in 748, 99.9 fps | 2,134 in 747, 100.0 fps |
| 3DBench | 76.9 | 76.9 |
| PCPBench | 13.3 | 13.3 |
| TopBench | 310 | 307 |

The change alters no clock-level behaviour (the lockstep's traces are
identical), so these differences are run-to-run timing noise. Main's
#1571 run read 3,376 realtics and TopBench 309.

| Title | Drive | Result |
| --- | --- | --- |
| Time Circle (32RTM, paging at CPL 3) | `dpmi` (k1) | Reaches its main menu (1 PLAYER, 2 PLAYERS, setup, EXIT) |
| Wolfenstein 3D | `real` (k2) | A new game, in E1M1 |
| Ultima VI | `real` (k2) | Its introduction plays |
| DOOM v1.9 | `pmode` (k2) | Its title and demos play |

Each title was launched and watched for about 100 s. This spot check
shows the acceptance titles still run. It is not #1368's full acceptance
(saves and level exits).

## What this record does not cover

- **The fill compare.** `same`, the fill's own compare that finds the entry
  already holding the page, stays. The audit estimates it at about 100 ALMs;
  deleting it changes the walker's fill and is a separate change.
- **nextpnr.** No route was made (fes#1235: Quartus at its best is the i386
  reference until the CPU works).

## Commands

From `sources/misteross`:

```sh
python3 scripts/test_fes_i386_lockstep.py --jobs 12
python3 scripts/test_fes_i386_lockstep.py --jobs 12 --yosys <i386 lock's yosys 238eac9a4>
python3 scripts/test_fes_i386_test386.py --cpu rtl
python3 scripts/build_fes_pc386_quartus.py --root "$PWD" --cache-root "$CACHE_ROOT" --seed 1
```

The seal ran with `CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains`
and `QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus`. Main's
reference builds ran the same producer at `18c9d99c1` with `--seed 1` and
`--seed 2`. The kit runs used scratch runners on Powerboat
(`~/fes-scratch/tlbwe/kit`), copied from #1579's, which aren't committed.
