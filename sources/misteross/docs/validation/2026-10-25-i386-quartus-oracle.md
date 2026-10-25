# i386 whole CPU through Quartus: the OSS gap is place and route

Dated record, 2026-10-25. Host-only diagnostic on Powerboat. No package, RBF
or kit. Quartus is an oracle here and never the product path.

The whole-CPU timing below was measured from older, pre-`at_start` Q-aligner
source snapshots, not the RTL in this tree. Their JSONL qalign hashes are
`55ebf99b` (branch snapshot) and `f4ffbb1d` (scratch snapshot); neither matches
the qalign source at HEAD. Current `fes_i386_qalign.sv` reads `dec[start]` once
into `at_start` and uses that copy for both `.run` and `.prefix`. Do not read
these figures as timing for the current RTL.

The question, from Deano: does the whole CPU's 53-62 MHz on the OSS flow
come from the RTL or from the flow? Quartus 17.0.2 Lite fitted the same
design on the same device, 5CSEBA6U23I7, with the same `spike.qsf` pins. The
target was 100 MHz, at seeds 1-4. The comparison is against nextpnr
`bd7a1c0d` on the i386 lock (Yosys `238eac9a49`, Mistral `8fcc4cb4`). Run
data: [`2026-10-25-i386-quartus-oracle.jsonl`](2026-10-25-i386-quartus-oracle.jsonl).

## Verdict

- **Mostly place and route.** Both flows got the same RTL. nextpnr routes
  Yosys's mapping of the whole CPU at **58-62 MHz**. Quartus fits Yosys's
  elaborated netlist of it at **85-89 MHz**. EX limits both.
  - nextpnr's whole-CPU routes target 80 MHz, as in #1109, and Quartus's
    target 100.
  - Re-routed at 100 MHz, nextpnr gives 62.43, 59.78, 57.80 and 59.43, so the
    target is not the gap.
- **Not synthesis.** The two flows produce a similar number of logic levels.
  The worst paths carry 11-13 cells in nextpnr and 9-11 in Quartus. Where they
  differ is routing delay: 10.6-11.4 ns in nextpnr against 6.7-8.1 ns in
  Quartus.
- **Part of the gap is constant, and part grows with design size.**
  - On the small spikes Quartus is a steady **1.20-1.26×** faster. That holds
    for the front end's Q and L2, for D, AG and for M2. F2, behind an M10K
    read, is 1.41×.
  - On the whole CPU the factor grows to **1.29-1.46×**, and to 1.45× on the
    limiting stage, EX.
  - The extra part is placement scaling. From spike to whole CPU, nextpnr's D
    drops 24% and Quartus's only 11%; Q drops 8% against 1%.
  - This record cannot tell how much of the constant 1.2× is nextpnr's timing
    model and how much is the quality of its placement. The fork already
    tracks model gaps against TimeQuest:
    - DeanoC/nextpnr#232: Mistral's M10K clock-to-out reads about 1.5 ns
      optimistic. F2 is the stage behind an M10K read, and its ratio is the
      outlier.
    - DeanoC/nextpnr#201: identical placement and routing report different
      figures on different commits.
- **The measured RTL still matters for 100 MHz.** Even Quartus stops at
  85-89 MHz, on EX at 9-11 logic levels. D reaches 92-100 MHz and Q 91-99.
  So on any flow, 100 MHz needs another cut in EX. On a better P&R this
  measured netlist sits near 87 MHz, not near 60.
- **DeanoC/yosys#59 is real on Quartus too.** On the captured pre-copy branch
  snapshot, Q limits Quartus at 74-78 MHz.
  - With the same Quartus settings, the `at_start` copy alone lifts Q to
    79-83 MHz.
  - Building the remaining stride multiply in logic, as Yosys does, lifts it
    to 91-99 MHz.

The nextpnr gap is filed as DeanoC/nextpnr#300 with this record as evidence.

## How Quartus reads the RTL

Quartus 17.0.2's SystemVerilog front end rejects the i386 RTL in three ways:
- genvars declared inside a `for`;
- generate loops outside `generate`;
- a variable index into a packed-struct member that shares a module signal's
  name.

So `scripts/quartus_fes_i386.py` feeds Quartus the netlist the i386 lock's
Yosys elaborates. It runs `read_verilog -sv; hierarchy; proc -norom; flatten;
opt -purge`. The result is not technology-mapped, and Quartus does all
synthesis, placement and routing itself. It is the netlist the lockstep checks
with `--yosys`.

That netlist still spells packed-array strides as constant multiplies, such as
`start*42` for `dec[start]`.
- **Quartus.** It maps the widest of these to a DSP block. The DSP sits in Q's
  worst path.
- **Yosys.** `synth_intel_alm` folds the same multiplies into logic, so
  nextpnr's netlist has no DSP once #59 is fixed.
- **`--dsp-in-logic`.** This flag sets Quartus's `DSP_BLOCK_BALANCING` to
  logic elements, so Quartus builds them the same way. It is the fair setting
  wherever Yosys's netlist has no DSP: the scratch whole CPU and the front-end
  spike.
- **Branch RTL.** On the branch RTL both flows have the DSP: Yosys maps #59's
  shared multiply to a `MUL9X9`. So those rows compare as they stand.

Quartus also turns some pipeline delay chains into M10K shift taps. The whole
CPU uses 23 RAM blocks in Quartus against 19 M10K in nextpnr.

## Whole CPU, MHz, seeds 1-4

The scratch snapshot also includes the review's decode fix, a one-term change
in D's group-1 arm. The nextpnr routes ran HIP through `gpu-run` with
`NEXTPNR_REQUIRE_GPU=1`, with an 80 MHz target.

| | nextpnr, scratch | Quartus, scratch, multiplies in logic | Quartus, scratch, DSP |
| --- | --- | --- | --- |
| Fmax | 62.43, 59.60, 57.80, 59.43 | 85.22, 87.31, 88.83, 86.10 | 82.47, 83.22, 79.73, 79.15 |
| Limiting | EX ×4 | EX ×4 | Q ×4 |
| EX | 57.8-62.4 | 85.2-88.8 | 82.1-86.4 |
| D | 61.6-69.4 | 91.6-100.2 | 90.6-94.3 |
| Q | 70.3-77.0 | 91.1-98.6 | 79.2-83.2 |
| F2 | 73.1-78.7 | 104.9-113.2 | 101.8-108.5 |
| L2 | 73.6-83.8 | 102.5-107.9 | 95.1-100.9 |

On the captured pre-copy branch snapshot, as in #1109, both flows have #59's DSP:

| | nextpnr | Quartus |
| --- | --- | --- |
| Fmax | 57.39, 52.99, 56.59, 57.76 | 73.81, 77.64, 77.36, 77.26 |
| Limiting | Q ×3, EX ×1 | Q ×4 |
| Q | 53.0-58.3 | 73.8-77.6 |
| EX | 57.8-62.2 | 80.2-83.9 |
| D | 64.0-69.0 | 82.6-90.7 |
| F2 | 70.3-75.8 | 88.6-101.7 |
| L2 | 74.2-77.5 | 87.8-97.7 |

## Where the time goes

The worst path of each seed, with routing (interconnect) as a share of the
data path:

| Flow, RTL | Path | Delay ns | Logic ns | Routing ns | Routing share | Cells or levels |
| --- | --- | --- | --- | --- | --- | --- |
| nextpnr, scratch | EX `ex.u`/`ex.exc` to `flags_in`/`wb_s` | 16.0-17.3 | 5.4-5.9 | 10.6-11.4 | 65-67% | 11-13 |
| Quartus, scratch, logic | EX to `flags_in` | 10.6-11.5 | 3.4-4.3 | 6.7-8.1 | 61-70% | 9-11 |
| nextpnr, branch | Q `linear_here` to `q` (s1-s3) | 17.4-18.9 | 7.0-7.6 | 10.3-11.3 | 59-60% | 9-10 |
| Quartus, branch | Q `start` to `q`, through the DSP | 12.2-12.9 | 5.9-6.6 | 5.7-6.4 | 46-52% | 7 |

Logic delay is close. nextpnr's figure includes clock-to-out and setup;
Quartus's excludes setup. Routing delay is where the flows part: nextpnr
spends 3-5 ns more on its worst path in every seed.

## Calibration: the same logic alone

The spikes are the branch RTL, routed for this record by nextpnr at 100 MHz,
seeds 1-4.
- **D+AG and M2.** They route identically to the
  [238eac9a49 record](2026-10-25-i386-yosys-238eac9a49.md): their netlists
  have not changed.
- **Front end.** #1109's `mem_valid` changes it, and it routes at 81.23,
  78.43, 81.20 and 82.24.
- **Quartus's front end.** It is with multiplies in logic, because that
  spike's Yosys netlist has no DSP.

| Spike, stage | nextpnr | Quartus | Ratio |
| --- | --- | --- | --- |
| Front end, Q | 78.4-82.2 | 92.0-100.0 | 1.20 |
| Front end, L2 | 85.6-93.5 | 105.3-110.6 | 1.21 |
| Front end, F2 | 83.9-88.3 | 117.6-125.4 | 1.41 |
| D+AG, D | 84.1-89.4 | 103.6-110.8 | 1.24 |
| D+AG, AG | 86.8-95.7 | 108.2-121.5 | 1.26 |
| M2 | 97.8-106.5 | 121.2-127.7 | 1.22 |
| Whole CPU (scratch), EX | 57.8-62.4 | 85.2-88.8 | 1.45 |
| Whole CPU (scratch), D | 61.6-69.4 | 91.6-100.2 | 1.46 |
| Whole CPU (scratch), Q | 70.3-77.0 | 91.1-98.6 | 1.29 |

Ratios are of midpoints. With the DSP, Quartus's front end lands at 78.7-80.0
MHz, level with nextpnr. That is the DSP's cost, not the flow's.

The same stage, spike to whole CPU:

| Stage | nextpnr | Quartus |
| --- | --- | --- |
| D | 84-89 → 62-69 (−24%) | 104-111 → 92-100 (−11%) |
| Q | 78-82 → 70-77 (−8%) | 92-100 → 91-99 (−1%) |
| L2 | 86-94 → 74-84 (−12%) | 105-111 → 102-108 (−3%) |

## Synthesis alone

Logic levels before placement:
- **Yosys** counts LUT levels on its mapped netlist.
- **Quartus** gives levels post-map (TimeQuest on the post-map netlist), on the
  scratch RTL with multiplies in logic.

| Stage | Yosys | Quartus post-map | Quartus routed worst path |
| --- | --- | --- | --- |
| EX | 14 | 11 | 9-11 |
| D | 9 | 9 | 6-8 |
| Q | 8 | 8 | 5-6 |
| L2 | 9 | 7 | 4-6 |

Yosys's mapping is up to three levels deeper on EX and L2. But nextpnr's routed
EX worst paths carry 11-13 cells, against Quartus's 9-11, so at most about 2
cells. At nextpnr's ~0.5 ns per cell that is about 1 ns of a 5-6 ns gap.
Synthesis is a minor share of the gap, and no Yosys issue is filed.

Mapped size:
- **Yosys:** 10,257 LUTs, 272 carry cells and 6,366 FFs.
- **Quartus:** 6,220-6,224 ALMs and 5,285-5,339 registers. Some of the
  missing registers are in the M10K shift taps.

## Reproduce

```bash
QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus \
  python3 scripts/quartus_fes_i386.py --yosys <i386 lock's yosys> [--spike frontend|dag|m2] [--dsp-in-logic]
```

Each run writes `build/quartus/fes-i386/<fingerprint>/summary.json`. The
record's fingerprints:

| Fingerprint | Run |
| --- | --- |
| `3d5f28cd6137` | Whole CPU, branch RTL |
| `581bf0484c7c` | Whole CPU, scratch RTL, DSP |
| `0b581cd6753a` | Whole CPU, scratch RTL, multiplies in logic |
| `32c04305ba02` | Front end, DSP |
| `1a87e1fb11bc` | Front end, multiplies in logic |
| `9f48537e359b` | D+AG |
| `64192a625b55` | M2 |

The nextpnr routes ran through `scripts/benchmark_fes_i386.py`:

| Fingerprint | Run |
| --- | --- |
| `6670a1dac014` | Branch spikes, `--spike frontend --spike dag --spike m2` |
| `2435e5e75ca3` | Scratch whole CPU, `--spike cpu --target-mhz 80` |
| `24484bd005f2` | Scratch whole CPU, `--spike cpu --target-mhz 100` |

Each seed fits one shared synthesis. TimeQuest reports one worst setup path
per register at the slow corner (1100 mV, 100 C). The script labels each path
by its endpoint's stage, with the benchmark's labels, and records its logic
levels and interconnect.
