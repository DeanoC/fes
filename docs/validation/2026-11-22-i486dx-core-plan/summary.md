# A separate i486DX core for the DE25: a measure-first plan

Tracker: [#2135](https://github.com/DeanoC/fes/issues/2135).

This record measures and plans. It builds no RTL. The ablation patches in
[patches/](patches/) are scratch measurement edits, never merged. The method,
with its bands and rules, was committed before any fit or simulation:
[method.md](method.md), with dated amendments. Every figure in section A and
section C comes from [results.md](results.md), which [generate.py](generate.py)
writes from the committed evidence ([fits/](fits/), [profiles/](profiles/),
[latency/](latency/), [lockstep.json](lockstep.json)).

**The question (Deano, 22 November 2026).**
- The integer 386/486SX core (`fes_i386_cpu`) carries about 90% of the target games.
  - It stays speed-tuned on both boards, because the DE10's Fmax is low.
- A separate 486DX core on the DE25-Nano may trade integer and float CPI and Fmax for area, so that the 387 fits.

## The answer in brief

- **The Phase 4 speed features are not where the DX finds its area.**
  - Turned off together, they save **1,716 ALMs**, 5% of the CPU's 34,651 on Agilex 5.
  - **Branch prediction is 1,361 of that**, and it is worth **49% of DOOM's CPI** (h2 CPI 1.668 → 2.489).
  - Store-to-load forwarding, the early ALU, the return stack and indirect prediction each save nothing measurable (under the 100-ALM noise band).
  - **Load forwarding saves 360 ALMs for 1.2% of DOOM's CPI.** That is the one Phase 4 trade worth making on the DX.
- **So the DX should keep the Phase 4 set, except load forwarding.** The rest of the area has to come from:
  - the x87 (7,315 ALMs in this record's exploratory hierarchy fit; 7,557 in #2028's);
  - the base pipeline, most of which predates Phase 4: the front end at 3,813, the L1s at 3,904, the BIU at 1,609;
  - the shell and devices.
  This record measures only module bounds for those, not savings. Measuring the savings is the plan's next step.
- **The DX identity itself costs 290 ALMs** over the 386 identity, on the same CPU.
- **The gap to close.** The 387-on board is 49,955.5 ALMs after #2059 (386 identity). As a DX it is about 50,245:
  - **+3,445 over the device**;
  - **+7,189 over the 92% line**.
- **Clock.** The first DX should build at the DE25's 61.437908 MHz. The way to buy lost CPI back with Agilex Fmax is a faster integer clock domain, with the x87 left at the machine clock. That needs a PLL check first, with no RTL.
- **Fork mechanism.**
  - A DE25-only tree under `boards/de25nano/pc386/i486dx/`.
  - `derive.py` for anchored small edits; forked files for rewritten modules.
  - The decoder, microcode, oracle and lockstep corpus shared.
  - CI running lockstep on both cores.
  - This follows the 22 November finding that a parameter moves the DE10 netlist even when off.

The decisions for Deano are [at the end](#decisions-for-deano).

## 1. Per-feature area of the integer core on Agilex 5

**The method** ([method.md](method.md) A):
- **What is fitted:** `fes_i386_cpu` alone, through `scripts/fit_i386_device.py`, in #1899's virtual-pin envelope.
  - Device: A5EB013BB23BE4SCS, with Quartus Pro 26.3, at 61.44 MHz.
  - Source: main `dafabb6db` with `I486 = 1` (the DX identity), with the 387 alive.
- **The ablations:** one patch per feature, each turning the feature off at its use so Quartus sweeps the logic only it reads.
- **Correctness:** every functional ablation passes the lockstep subset with no new failures ([lockstep.json](lockstep.json)).
- **Noise band:** three seeds of the baseline span 100 ALMs, so the band is 100.

**This method differs from #2028's per-module table.**
- #2028 split the 387-on machine by module.
- Most Phase 4 features are not modules. They are terms in the CPU's own logic, which #2028 could only report as an anonymous 9,349 ALMs.
- An ablation measures them where they are.

**The baseline.** The DX-identity CPU needs **34,651 ALMs** (seeds 34,606 to 34,706), 22 DSP and 60 M20K.
- 2,352 of those ALMs hold the envelope's virtual pins, the same in every fit.
- The 386 identity of the same source needs 34,361, so **the 486 identity costs 290**.
- **Against #2028's CPU entity** (31,426 inside the 387-on machine) the envelope adds 2,352 ALMs of virtual pins. The CPU's `init_*` and `arch_x87_*` ports become free inputs and outputs here, where the machine ties or drops them. The remaining difference, about 580 ALMs, is not apportioned.
- For scale, #1899's CPU fit was 29,300 on 16 November, before the DE25 x87 backend.

| Feature (PR) | ALMs it saves when off | CPI it buys on DOOM (h2) | Per 100 ALMs |
| --- | ---: | --- | --- |
| **All prediction: BTB #1511 + RAS #1512 + indirect #1571** | **1,361** (and 4 M20K) | **CPI 2.489 → 1.668: a 49% loss without it** | 3.6% of CPI |
| RAS #1512 alone (a lower bound) | −7, under noise | 1.744 → 1.668: 4.6% | free |
| Indirect #1571 alone (a lower bound) | −76, under noise | 1.683 → 1.668: 0.9% | free |
| Store-to-load #1515 | 78, under noise | 1.695 → 1.668: 1.6% | free |
| Early ALU #1517 | −22, under noise | 1.682 → 1.668: 0.9% | free |
| Load forwarding #1522 | **360** | 1.687 → 1.668: 1.2% | **0.33%** |
| **The speed set** (prediction, #1515, #1517, #1522) | **1,716** (the parts sum to 1,777) | 2.539 → 1.668: 52% | 3.05% |
| Front-end split #1524 | none: #1524 changed no RTL | its record splits `fe_empty` into its reasons | — |
| **Paging** (an area bound; a DX needs it) | 1,913 | not functional off | — |

**What the table says:**
- **Prediction is the cheapest CPI in the core.** Every other row saves no area at all, except load forwarding's 360 ALMs.
- **The DX keeps prediction.** Dropping it would trade half of DOOM's speed for 1,361 ALMs, under half of the gap.
- **The free rows are free because they reuse logic that is already there.**
  - The early ALU uses AG's address adder.
  - The store-to-load test is a 12-bit compare.
  - Removing them takes nothing out.

**Module bounds on Agilex 5.** The table has two sources, kept in separate columns:

- **This record, exploratory** (amendment 3, added after the results; [results.md](results.md)): a hierarchy-kept fit of `dx`, 34,818 ALMs, +0.48% from the flattened fit, inside the ±5% gate borrowed from #2028 after the fact.
- **#2028, precommitted (rule 7):** the 387-on machine's hierarchy fit (main `8a162c536`, before #2037 and #2059; `docs/validation/2026-11-20-i386-x87-s6-measure/entities.tsv`), each module's total ALMs inside that machine.

Both bound what a module's removal or redesign could save; neither is a saving:

| Module | ALMs, this record (exploratory) | ALMs, #2028 (rule 7) | Registers, this record | What a DX could do there |
| --- | ---: | ---: | ---: | --- |
| **x87 (`fes_i386_x87`), all** | **7,315** | **7,557** | 5,065 | Iterative arithmetic, serial conversions (question 5) |
| — the envelope's own logic | 3,510 | 4,408 | 2,303 | |
| — the transcendental engine | 1,502 | 1,114 | 789 | Needed: it now also runs FBLD and the remainder ops (#2037, #2059) |
| — `fes_x87_core`: fast lane 1,134, slow lane 594, own 421 | 2,148 | 1,885 | 1,455 | The fast lane holds the clock-limiting path |
| — multiplier | 155 | 151 | 518 | |
| The CPU's own logic (this record's includes the 2,352 virtual-pin ALMs) | 12,977 | 9,349 | 8,997 | Not apportioned |
| Front end: fetch buffer with predecode (decoders 674, sums 542) | 2,629 | 2,664 | 2,919 | Fewer predecode bytes; a narrower buffer |
| Front end: queue align (byte rotators) | 1,107 | 1,095 | 259 | A narrower issue |
| L1D (16 KiB, 4-way) | 2,259 | 2,312 | 1,828 | 8 KiB. A real 486 has 8 KiB unified, and the valid bits are flip-flops. |
| L1I (16 KiB, 4-way) with the ITLB (239) | 1,645 | 1,609 | 992 | As L1D |
| BIU | 1,609 | 1,506 | 1,754 | |
| `muldiv` | 816 | 833 | 558 | A slower divider; the 486's divide is slow |
| `dfwd` | 809 | 873 | 73 | |
| DTLB (16 entries × 2 ports) | 708 | 735 | 660 | 8 entries with a second level (#1584's fallback) |

The two columns give the same picture. #2028's x87 split differs because #2037
and #2059 since moved FBLD and the remainder ops onto the engine and made the
fast lane one stage.

## 2. What each speed feature buys

- **CPI** is measured on DOOM v1.9 `-timedemo demo3`. The handover is at page flip 40, and the RTL runs for 1.0 s, as in the Phase 4 records ([method.md](method.md) C).
- **The baseline is the same window Phase 4 ended on.** Its h2 CPI is 1.668, against the 1.667 #1571's record gives.
- **Each ablation starts from the same oracle state.** Every handover is at cycle 2,493,936,063.
- **The table** is in section 1, and the full one in [results.md](results.md).

**The Phase 4 records' own figures, in landing order, on an older base.** Each was measured against its predecessor, not as an ablation of today's core:

| Slice | DOOM h2 CPI | Kit `demo3` fps |
| --- | --- | --- |
| Before Phase 4 | 2.539 | 26.7 |
| BTB | 1.811 | 37.5 |
| RAS | 1.742 | 38.7 |
| Store-to-load | 1.781 (pre-merge tree) | 39.2 |
| Early ALU | 1.701 | 39.5 |
| Load forwarding | 1.680 | 39.8 |
| Indirect | 1.667 | 40.05 |

The ablations agree with that order of size. Prediction is most of Phase 4's
gain, and the rest are each about a percent.

**The latency bench** ([results.md](results.md)) shows the mechanism behind each figure:
- **No prediction:** `jcc_loop` takes 47,999 clocks against 8,039, `call_ind1` 328,007 against 88,068, and `call_ret` 199,118 against 106,148.
- **The store-to-load ablation:** `store_load_other` and `store_load_line` go back to 26,059 and 26,050, their values before #1515.
- **Load forwarding** changes no bench loop. The bench has none that exercises it, so DOOM is its only measure.

## 3. The DX performance target under the governor

- **How the governor works** (#2000, the local-bus video record):
  - It only slows the CPU.
  - A raw CPU faster than the preset is paced down to the preset.
  - A raw CPU slower than the preset runs at its own speed.
  - So lost CPI costs nothing until raw speed falls below the target.
- **Today's raw speed on DOOM `MAX.CFG`:** `max` takes 2,062 realtics on Kit A, against 2,912 for the DX2-66 preset and 4,682 for DX-33.
- **The headroom over DX2-66 is 1.41×,** in DOOM's own time, after the local-bus video fix.
- **The headroom over DX-33 is 2.27×.**

**#2000's reach verdicts stand as they are.**
- After the local-bus fix, DX-50 is not reached on 3 of 6 benchmarks, and DX2-66 on 4 of 6.
- DX-33 is the calibration point and is reached.

**The bound** ([method.md](method.md), fixed before measuring):
- A variant with DOOM CPI ratio r keeps a preset if 2,062 × r is at most that preset's realtics.
- It treats all of `max`'s time as CPU time, so it overstates the loss.

| Variant | r | Bound (realtics) | DX2-66 | DX-33 |
| --- | ---: | ---: | --- | --- |
| baseline | 1.000 | 2,062 | kept | kept |
| no prediction | 1.493 | 3,078 | not settled by the bound | kept |
| no return stack | 1.046 | 2,157 | kept | kept |
| no indirect | 1.009 | 2,081 | kept | kept |
| no store-to-load test | 1.016 | 2,096 | kept | kept |
| no early ALU | 1.009 | 2,080 | kept | kept |
| **no load forwarding** | **1.012** | **2,086** | **kept** | **kept** |
| no speed set | 1.523 | 3,140 | not settled by the bound | kept |

**Recommended target:**
- **DX-33 sustained** is the requirement. Every variant here keeps it, even with no prediction.
- **DX2-66 is the stretch.** Today's core does not reach it on 4 of 6 benchmarks at 61.44 MHz.
- **DX-50 is not a separate target.** It lies between the other two, and #2000's override selects it as preset 6 with word 1 = 500.

**The bound covers DOOM `MAX.CFG` only.**
- **Hidden by the governor:** a DX that keeps the speed set loses nothing at DX-33, or on DOOM `MAX.CFG` at DX2-66.
- **Not hidden:** on the four benchmarks where #2000 already misses DX2-66, the core runs at its own raw speed. There a CPI loss shows one for one.
- **The x87's own slowdown** is a separate question, priced below.

## 4. The clock

### What binds the clock

- **The DE25 pc386 system clock** is 61.437908 MHz, which is 50 × 188/9 MHz /17. One VCO makes it beside audio's 12.287582 MHz ([the shell plan](../2026-11-20-de25-pc386-shell-plan/summary.md), "The clock").
- **387 off, the machine has headroom.** It closed at 120 MHz on four seeds, and that was the ladder's top rung (#2028, 6.0c). The CPU alone, with the slice-2 387, closed at 130 MHz (#1899).
- **387 on, the headroom is gone.**
  - #2059 made the x87 fast lane one combinational stage to save 198 ALMs.
  - The machine's worst setup at 61.44 MHz fell from +7.5 to +0.387 ns.
  - In this record, the CPU alone with the 387 alive estimates **63.4 to 72.6 MHz** at its worst corner over three seeds of one netlist ([results.md](results.md)). That is down from #1899's 130 MHz closure, before the DE25 backend.
  - **The x87 fast lane is the clock-limiting path.** In the exploratory hierarchy-kept fit (amendment 3), all 20 of the worst setup paths at the slowest corner run from `x87|core|normalized` into `x87|core|fast` (69.8 MHz estimate; [results.md](results.md)). That is the lane #2059 made one stage.
- **The consequence:** any clock the x87 sees above about 63 MHz needs the x87's pipelining back. That undoes #2059's saving, or more.

### Options

| | Option | What it buys | Crossing cost | Risk |
| --- | --- | --- | --- | --- |
| A | **Stay at 61.437908 MHz** | Nothing: lost CPI is lost speed | None | None |
| B | **Machine clock raise (D3)**: all of `pc386_system` faster | Every CPU-bound clock, in proportion | None inside the machine. Every guest rate is rescaled, so `guest_rates` takes a `Fraction` (shell plan). | The 387-on machine must close at the new clock, which undoes the x87 reductions. Every device is retimed and its lockstep and sound runs repeated. |
| C | **The integer core in its own faster domain, ratio-locked** to the machine clock, with the x87 on a clock enable at the machine rate and multicycle constraints | CPU-bound integer work, up to the ratio k | Synchronous crossing: request and acknowledge registers on MMIO, I/O, DMA, flush, INTR/INTA and FERR/IRQ13, with a phase enable and no synchronisers | The PLL may not give an integer multiple (below). The multicycle constraints on the x87 and the crossing must be exact. The governor's `GOV_N/GOV_D` and the ISA/VGA charges count CPU clocks, so they are rescaled by k. |
| D | **The integer core in an asynchronous faster domain** | As C, at any frequency, for example VCO/9 = 116.05 MHz | Asynchronous handshakes on every port: about 2 machine plus 2 CPU clocks a transaction, hidden under the governor's ISA charges (720 ns a half-word on presets 1–5, 150 ns a dword on 6 and 7) | CDC verification. The x87 inside the CPU must then close at the CPU clock, which is B's problem for the x87 alone. |

### The PLL

- **The only integer ratio within the core's reach is k = 2, 122.88 MHz.** k = 3, 184 MHz, is far above any closure measured.
- **Today's VCO** is 1,044.444 MHz. Its integer divisions give 130.56 (/8), 116.05 (/9), 104.44 (/10), 87.04 (/12) and 61.44 (/17) MHz.
- **No integer multiple of 61.437908 MHz is among them.** Twice it would need /8.5.
- **So option C needs a different VCO setting** that still makes the audio clock. That is unchecked and needs `quartus_ipgenerate`, as the shell plan's own VCO limits do.

### The memory and cache path

- **What moves with the CPU clock.** The L1s and the BIU are inside `fes_i386_cpu`, so in C and D they run at the CPU clock.
- **Port 0, the CPU's only memory path, crosses in the shell.**
  - It is 128-bit Avalon into HPS DDR through the F2SDRAM adapter.
  - In C and D it is clocked from the CPU domain, and the adapter's own clock crossing carries it. The fabric needs no new FIFO.
- **What gets worse: a miss costs more CPU clocks.** HPS DDR latency is fixed in time, so at about 116 MHz a refill costs about 1.9× today's clocks.
  - The profile bench charges port 0's first data 18 clocks at its 100 MHz model, against 11 at 61.44 MHz (the Phase 4 profile record).
  - **L1D and port 0 are 16.1%** of the baseline's DOOM h2 clocks (8,036,203 of 50,000,000; [results.md](results.md), "Where `base`'s h2 clocks go"), at the bench's own 100 MHz memory model (18 clocks to first data). At the kit's 11 clocks it is less.
  - So the speed-up from k is less than k on DOOM.
- **DMA snoops** reach the CPU's L1 invalidation through the `dma_*` port, and cross with the rest of it.

### Recommendation

- **Build the first DX at 61.437908 MHz** (option A).
- **Plan option C as the DX's speed lever** once the fork fits.
  - It is the one option that keeps the x87 at the slow clock, where its area savings are.
  - It moves the integer pipe, where CPI is traded, to Agilex's Fmax.
- **First step: a PLL check, with no RTL.** Find a VCO that gives 61.437908 MHz, 12.287582 MHz and an integer multiple of the system clock together.
- **Fall back to D** only if no such VCO exists. D also needs the x87 to close at the CPU clock.

## 5. The 387 on the DX

- **Size.** The x87 is 7,315 ALMs in this record's exploratory hierarchy fit (#2028 measured 7,557 before #2037 and #2059). Its parts:
  - the envelope's own logic, 3,510;
  - `fes_x87_core`, 2,148: the fast lane 1,134, the slow lane 594 and its own logic 421;
  - the transcendental engine, 1,502;
  - the multiplier, 155.
- **What #2037 and #2059 took.** They moved the remainder ops and FBLD onto the engine, and made the fast lane one stage, saving 472 and 460.5 ALMs. The candidates from slice 6's plan that are left:

| Candidate | Bound | What it costs, and why it is acceptable on the DX |
| --- | ---: | --- |
| **C, rest:** de-pipeline `fes_x87_core`'s slow lane and normaliser; one operation in flight | part of 2,148 (1,455 registers) | Slower FMUL, FDIV and FSQRT. Acceptable on the DX: a real 387 takes tens to hundreds of clocks for these, and the integer side no longer needs a high clock (option C keeps the x87 at the machine clock) |
| **Iterative multiply and divide** on one DSP: radix-4 or radix-16 over several clocks in place of the full-width array | the multiplier's 155 plus part of the core | A few clocks more an FMUL; the 9 DSP drop to 2 or 3. DSP is slack on the DE25 (the 387-on machine uses 42 of 188, #2059), so this is an ALM trade only |
| **Serial conversions** (FILD, FIST, FBSTP and the extended load/store) through the engine, as FBLD now is | part of the envelope's 3,510 | Clocks per conversion. Loads and stores are 33% and 39% of Quake's and Screamer's x87 instructions (`2026-11-15-i386-x87-game-census/tables.md`), and that table does not split the integer conversions out. So this candidate's cost has to be measured on Quake before it is chosen. |
| **D: state and constants into M20K/MLAB** (the register stack's tags, the engine's ROM is already M20K) | unmeasured | A read port; M20K is the scarcer resource with the 387 on (about 43 RAM blocks free on the board) |
| **A: share one multiplier with the integer `muldiv`** | 151 | Ranked last (#2028); the DX makes it easier, since the integer side no longer needs MUL at full rate |

- **What the integer side no longer forces.** #2059 found that a raised clock would need the x87 pipelined again. On the DX the x87 stays at the machine clock under option C, so de-pipelining no longer conflicts with a faster integer clock.
- **None of these savings is measured.** Each needs its own A/B against the DX baseline, the method #2028 and #2059 used.

## 6. Fork and sync

### Why a fork, not parameters

- **A parameter moves the DE10's netlist even when it is off.**
  - On 22 November, DMA controller 2's register-file cut (A-dma2) found that any `pc386_system` edit moves Yosys's source lines and auto-names (`c2732b8f1` on `deano/de25-pc386-dma2-regfile`).
  - So a parameter could not keep the DE10 netlist byte-identical.
- **That branch's derivation is the precedent.**
  - `boards/de25nano/pc386/derive.py` swaps `pc386_at_core`'s `dma2` instance for a DE25-only file, `pc386_dma2_registers.sv`.
  - The DE10's seal netlist is byte-identical before and after (`c56f515f…`).
  - The c64, atari-st and spectrum boards have the same pattern.
- **The DE10's CPU closure is an explicit list.**
  - `scripts/yosys_netlist.py` `CPU_SOURCES` names every `fes_i386_*` and `fes_x87_*` file in read order.
  - A file outside that list, and outside the Python the DE10 producer imports, cannot move the DE10's `BUILD_ID` (AGENTS.md D12; the record-closure rule).

### The shape

| Part | Where | Relation to the integer core |
| --- | --- | --- |
| **Shared verbatim** | `cores/fes-common/rtl/i386/`: `fes_i386_pkg`, `fes_i386_ucode_pkg`, `fes_i386_ucode_rom` and `ucode.toml`, `fes_i386_decode`, `fes_i386_seq`, `fes_i386_oplen`/`lendec`/`lstage`, `fes_i386_gov_*`, `fes_x87_pkg`, `fes_x87_trans_pkg`, `fes_x87_trans_rom` | The same bytes. A decoder or microcode fix lands once and moves both, as today. |
| **Derived** (anchored small edits) | `boards/de25nano/pc386/i486dx/derive.py`, run by the DE25 producer only | Anchored replacements with pinned SHA-256s, as the dma2 branch's `derive.py`. An anchor that moves fails the DE25 build. |
| **Forked** (rewritten modules) | `boards/de25nano/pc386/i486dx/fes_i486dx_*.sv`: for example a smaller L1, an iterative x87 core, a narrower front end | Each file's header names its upstream file and the upstream commit it was last synced to. The top is renamed `fes_i486dx_cpu`, so no tool sees two modules of one name. |
| **The file list** | the DE25 producer's own module (`boards/de25nano/pc386/build.py`) | **Not** `scripts/yosys_netlist.py`. A list there would move the DE10's record closure. |

**Derive or fork.** A change stays a derivation while it is a few anchored lines. A module is forked once a derivation would replace most of it.

### What stays shared

- **The architecture.** The decoder and the microcode, with its `ucode.toml` rules and ROM, are shared. The cores differ in how they execute, not in what.
- **The oracle.** That is the Bochs oracle (level 3 and level 4) and `divergences.toml`.
- **The lockstep corpus and its generators.** They are design-v2 `evidence` for pc386, so a `--cpu dx` selector moves the evidence digest, not the DE10's design ID.
  - `test_fes_i386_lockstep.py` and `test_fes_i486_lockstep.py`;
  - the SingleStepTests corpus;
  - the random, PM, task, debug and undefined program generators.
- **The x87 benches.** These are the unit, testfloat and transcendental benches.

### CI

- **Any PR touching a shared file** runs the full lockstep on both cores: the integer core (386 and `I486 = 1`) and the DX. A path rule maps the shared list to both jobs.
- **A PR touching only DX files** runs the DX lockstep and the x87 benches.
- **A sync check, `scripts/i486dx_sync.py --check`, compares each forked file with its upstream at the recorded commit.**
  - It fails on any upstream commit the fork has not acknowledged.
  - The PR then syncs the fork, or records "not applicable" with a reason.
  - This is how the two cores learn from each other, mechanically.
- **`derive.py`'s anchors** are tested on every PR, as the dma2 branch's `test_de25_pc386_dma2_derive.py` does.
- **The DE10 netlist identity check** (D12) runs on any PR touching `boards/de25nano/pc386/i486dx/`, to show the fork kept the DE10 netlist byte-identical.

## The budget

The 387-on board, from #2059's median and the measured shell (7,728):

| Step | ALMs | Over the device (46,800) | Over 92% (43,056) | Status |
| --- | ---: | ---: | ---: | --- |
| 387-on board, 386 identity (#2059) | 49,955.5 | +3,155.5 | +6,899.5 | measured |
| DX identity (+290, this record, CPU-only) | about 50,245 | +3,445 | +7,189 | measured on the CPU |
| Shell and device cuts (S-ocm 696, S-lat 946, A-dma2 412, A-fifo 170) | about −2,224 | about +1,221 | about +4,965 | measured separately, never fitted together |
| Load forwarding off (if its CPI is acceptable) | −360 | about +861 | about +4,605 | measured, CPU-only |
| **Remaining** | | **about 900** | **about 4,600** | to come from the x87, the L1s and the front end |

The Phase 4 speed set, all of it, would take another 1,356 (1,716 less
load forwarding). That costs DOOM's 49%, which the plan does not recommend.
The remaining 900 to 4,600 has to come from the rows of question 5 and the
module bounds of question 1, none of which is measured as a saving yet.

## Decisions (23 Nov 2026)

Deano decided the seven questions below on 23 November 2026, relayed through
the coordinating session. His choices, quoted:

| # | Question | Deano's choice | What it settles |
| ---: | --- | --- | --- |
| 1–5 | Fork, speed target, integer trades, clock, x87 | "Accept all (Recommended)" | The recommendations of 1–5 below stand as written: the DE25-only fork tree; DX-33 required, DX2-66 the stretch; Phase 4 kept except load forwarding; the first DX at 61.437908 MHz, then the PLL check for k = 2; slower x87 forms ranked by measured A/Bs. |
| 7 | The line to fit under | "Device + 4-seed timing" | The DX passes when it fits the device and closes timing on four seeds. About 900 ALMs remain to find, not 4,600. The 92% line is not the DX's target. |
| 6 | The next step | "Start now" | The precommitted ablation and A/B round runs now, beside the Codex review of this PR: 8 KiB L1s (and a unified 8 KiB), a narrower front end, the x87 candidates and a slower divider, in ALMs and DOOM, Quake and 3DBench CPI. No RTL is merged. |

## Decisions for Deano

The questions as they were put, with the recommendations Deano accepted.

1. **The fork mechanism (question 6).**
   - The DX lives in a DE25-only tree, `boards/de25nano/pc386/i486dx/`. It uses `derive.py` for small anchored edits and forked `fes_i486dx_*` files for rewritten modules.
   - Its file list sits in the DE25 producer, not in `yosys_netlist.py`.
   - The decoder, microcode, oracle and lockstep corpus stay shared.
   - CI runs the full lockstep on both cores for any shared-file change, plus a sync check on forked files.
   - **Recommend: yes.** This is the dma2 branch's pattern, and it keeps the DE10 netlist byte-identical.
2. **The DX's speed target (question 3).**
   - **Recommend:**
     - DX-33 sustained is the requirement.
     - DX2-66 is the stretch, as on the integer core, which misses it on 4 of 6 benchmarks today.
     - DX-50 is no separate target.
   - Under the governor, every Phase 4 trade except prediction keeps both presets by the bound.
3. **Integer trades on the DX (questions 1–2).**
   - **Recommend:**
     - Keep prediction, the return stack, indirect prediction, the store-to-load test and the early ALU. They save nothing measurable, or, for prediction, cost 49% of DOOM.
     - Drop load forwarding on the DX: −360 ALMs for +1.2% of DOOM's CPI.
       - The governor hides that at DX-33 and on DOOM `MAX.CFG` at DX2-66.
       - Where DX2-66 is already missed, the loss shows one for one, at about 1.2% at most.
       - #1522's record measured 3DBench (16-bit code) unchanged, clock for clock, and its PCPBench 13.2 → 13.3 is within the kit's display.
   - The integer core keeps all of them.
4. **The clock (question 4).**
   - **Recommend:**
     - The first DX builds at 61.437908 MHz.
     - The next step is a PLL check, with no RTL, for a VCO that gives twice the system clock together with the audio clock. That would make option C possible: the integer core at 2 × 61.437908 = 122.88 MHz, the x87 and the machine at 61.44.
     - **The margin for k = 2 is thin.**
       - The CPU closed 130 MHz on #1899's older base, with the slice-2 387, and missed 150.
       - The 387-off machine closes at least 120 MHz (#2028).
       - So 122.88 MHz is inside the first and just above the second.
       - Integer Fmax with the x87's paths out has not been measured, and the next step measures it.
     - No machine clock raise (option B) for the DX, since it would undo the x87 savings.
5. **The x87 on the DX (question 5).**
   - **Recommend:** allow slower x87 forms on the DX, as candidates ranked by measured A/Bs, each against this record's DX baseline:
     - de-pipeline `fes_x87_core`'s slow lane;
     - iterative multiply and divide on fewer DSPs;
     - serial conversions through the engine;
     - state into memory.
   - The integer core's x87 stays as it is.
6. **The next step: measure the module-level cuts (no RTL merged).** The Phase 4 set leaves about 900 ALMs to find to fit the device, and about 4,600 to reach the 92% line.
   - **Recommend:** a precommitted ablation and A/B round on the DX baseline, covering:
     - L1I and L1D at 8 KiB each (and a unified 8 KiB, the real 486's shape);
     - a narrower front end (fewer predecode bytes);
     - the x87 candidates above;
     - a slower divider.
     Each is measured in ALMs and DOOM, Quake and 3DBench CPI, as this record does.
7. **The line to fit under.** D2 of the x87 plan kept 92% (43,056) as the working line.
   - **Ask:** should the DX aim for the 92% line (about 4,600 more to find), or for fitting the device and passing timing on four seeds (about 900)?
   - The 92% line is the DE10 lane's prior. On Agilex 5, #2028's four seeds at 43,008 to 43,080 ALMs (about 92%) all passed timing at 61.44 MHz. No fit well above that has been tried.

## Caveats

- **Exploration-grade.** Every netlist was elaborated with `YOSYS_MAX_THREADS=1`, because the OPT_CLEAN fix (DeanoC/yosys#81, PR #2130) is not on main. These fits are not seals and do not replace a fit on the fixed lock.
- **CPU-only envelope.** The CPU fits sit in the virtual-pin envelope, not in the machine. Savings are differences inside that envelope. The budget adds them to a machine figure, which is first-order.
- **One workload.** CPI is DOOM's alone. The front end and prediction matter more on 3DBench, by the Phase 4 records.
- **Ablations are not designs.** An ablation turns a feature off. A DX designed without it might be smaller still, since logic around it would be simplified too.
- **Fmax estimates are not closure.** The per-fit Fmax estimates vary 63 to 74 MHz across seeds of one netlist, and nothing is judged on them.
- **Module rows understate features.** #2028's per-module table gives the BTB 32 ALMs and the return stack 43. The `pred` ablation saves 1,361, because prediction's cost is in the fetch buffer and the CPU's own logic, not in those modules. The next step's module bounds must be read the same way: they bound a module, not a feature.
- **The DX-specific logic is unmeasured.** The `dx` baseline is the 486SX identity with the 387 alive. i486 slice 4 (the DX identity) has not landed, so its own logic is not in these figures.
- **Rebuilt Yosys.** `speed` and `pg` were elaborated by a rebuilt binary of the same Yosys commit (amendment 1). Its digest differs from the first build's, but it writes `dx`'s netlist byte-identical (`586ed476…`; [rebuilt-yosys-check.json](rebuilt-yosys-check.json)).
- **Dependencies.**
  - The fork's location assumes the DE25 pc386 board directory of #2036, which is not merged.
  - The `derive.py` precedent is on branch `deano/de25-pc386-dma2-regfile`, which has no PR yet.
