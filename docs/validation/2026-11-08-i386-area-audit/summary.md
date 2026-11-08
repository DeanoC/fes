# i386 area audit after paging, 8 November 2026

Phase 5's paging slice, [#1579](https://github.com/DeanoC/fes/pull/1579),
took the pc386 seal from 33,455 to 36,751 of the Cyclone V's 41,910 ALMs.
That is about 3,300 ALMs, where the plan said about 1,300. This record shows
where the design's ALMs go and what paging added, module by module. It names
the cheap wins and re-budgets Phase 5's slices 2–5 against what is left.
It changes no RTL. Tracker: [#1548](https://github.com/DeanoC/fes/issues/1548).
Toolchain follow-up: [#1581](https://github.com/DeanoC/fes/issues/1581).

**Result.**
- **Paging cost 3,478 ALMs in the per-module fits (2,171 registers).** The
  DTLB is 1,273 of them, the ITLB 535, the L1D's walker and tag compares 744,
  the L1I 114, and `fes_i386_cpu`'s own logic 1,051. The plan said 500, 250,
  400 and 150 for those parts. The two TLBs and the CPU's own logic are where
  the estimate missed.
- **Nothing paging added went into MLAB or M10K, and the design uses no MLAB
  at all.** RAM blocks stay at 265. Both TLBs are flip-flops, as designed: a
  fully associative compare needs every entry at once.
- **A third of the TLBs' cost is a write path one line of RTL makes.** The
  entry array is written `e[slot] <= fill_ent`, a variable index into a
  packed vector. Yosys turns that into shift logic over all 640 bits, and
  Quartus keeps it. With an enable per entry instead, an analysis fit saves
  **744 ALMs in the CPU** (DTLB −495, ITLB −342, the other CPU modules +93 of noise). That
  fit also met setup with +0.832 ns. This is the clear quick win. It needs
  #1579 merged first, since the TLB is only on that branch.
- **The Yosys front end's other losses are small.** Yosys writes its 613
  one-hot muxes as `casez` with a `parallel_case` attribute. A fit that adds
  Quartus's own `// synthesis parallel_case` directive gives identical
  synthesis: 56,331 ALUTs, 32,350 registers and 36,751 ALMs both times. So
  the attribute form costs nothing.
  Quartus's messages on whether it honours the attribute disagree:
  - the seal's map report calls those 613 attributes unrecognised (10335);
  - the experiment's report says the directive overrides an existing value
    (10890). Keeping the hierarchy instead of
  flattening it moves the total by 43 ALMs. The M4 record's two memories that
  Quartus "could not infer" (warning 10999) are now M10K, split per byte.
  What Yosys still turns into flip-flops is 30 small memories of about
  2,500 bits in all, mostly in the machine.
- **Slices 2–5 need about 2,000 ALMs (high 2,650, pessimistic 3,600).**
  Without wins the design would sit at about 38,750 ALMs (92.5%). The TLB
  fix saves 744 ALMs in the CPU and 500 in the whole fit (the machine moved
  +244 by noise). Counting the conservative 500, the estimate lands at about
  38,250 (91.3%) and the high case at about 38,900 (92.8%). Quartus closes at 88% today with
  +0.620 ns of setup slack and "Low" packing difficulty. It has never been
  run above 88%, so anything past about 92% is untested. The plan fits if
  the TLB fix lands. More wins are listed [below](#options-if-the-slices-run-over)
  if the slices run over.

## Method

The pc386 producer elaborates the machine with Yosys and runs `flatten`
before Quartus reads it. So the seal's fit report has a single
`pc386_system` entity (35,979 ALMs in its own logic), and about half the
atom names are anonymous (`_06042_`, `Add616`, `Equal6015`). Per-module
figures need a fit without the flatten.

| Fit | Commit | What | ALMs | Registers | Setup, system clock |
| --- | --- | --- | ---: | ---: | ---: |
| Seal `416e332f` | `93928cea2` | #1579's sealed head (paging) | 36,751 | 32,350 | +0.620 ns |
| Paging, per module | `93928cea2` | the producer with `flatten` removed | 36,794 | 32,507 | met |
| Base, per module | `97a58487f` | main where #1579 branched: #1571 and #1552 in, the EMS board (#1556) not | 33,315 | 30,336 | met |
| parallel_case | `93928cea2` | the seal's netlist, the 613 Yosys `$pmux` `casez` functions marked `// synthesis parallel_case` | 36,751 | 32,350 | +0.843 ns |
| TLB write enable | `93928cea2` + scratch commit | per-module, `fes_i386_tlb` writing each entry under its own enable | 36,294 | 32,520 | +0.832 ns |

- Every fit is Quartus 17.0.2, seed 1, the producer's settings. All five had
  positive slack on every setup and hold analysis in `top.sta.summary`; the
  worst was hold, +0.076 to +0.118 ns. None of the analysis fits is a seal; their packages
  are scratch.
- The per-module fits change only the Yosys script: the producer's
  `machine_program` without `flatten; `, and with a `stat` before
  `write_verilog`. Quartus still optimises across module boundaries, and the
  per-module total lands 43 ALMs above the seal.
- **Noise.** The machine's RTL is the same in the base and paging fits, but
  its ALMs move by up to 182 (VGA −170, BIU −105). Main at `d6589de42` with
  the EMS board sealed at 33,264 (#1553's seal), against 33,455 at #1571's.
  So a total moves by ±200 from fit to fit. Read deltas under about 150 ALMs
  as noise.
- Main has gained the EMS board (#1556) since #1579 branched. Its cost is
  within that noise: 33,264 with it against 33,315 and 33,455 without.

Tables: [`entities.tsv`](entities.tsv) holds every entity with 20 ALMs or
more in either per-module fit: ALMs, registers, ALUTs and M10Ks, base and
paging.

## Where the ALMs go

The paging fit, by block. "Own" is a module's logic outside its submodules.

| Block | ALMs | Registers | ALUTs | M10K | Notes |
| --- | ---: | ---: | ---: | ---: | --- |
| **`fes_i386_cpu`, all** | **25,197** | 18,614 | 37,686 | 81 | 68% of the device's used ALMs |
| CPU own logic (D, M1, M2, WB, register file, segment caches, CRs, pipeline registers) | 8,456 | 7,960 | | | +1,051 for paging |
| Front end (`fes_i386_frontend`) | 5,726 | 4,212 | 7,468 | 4 | |
| — fetch buffer (`fes_i386_fetchbuf`) | 3,271 | 3,393 | 4,839 | 0 | 1,563 of its registers are per-byte predecode (`dec`) |
| — queue align (`fes_i386_qalign`) | 2,078 | 271 | 2,527 | 0 | byte rotators: 586 shift ALUTs |
| — return stack (`fes_i386_ras`) | 289 | 520 | 50 | 0 | a 16×32 memory Yosys made into flip-flops |
| L1D (`fes_i386_l1d`) | 2,208 | 1,502 | 3,314 | 40 | 559 valid-bit flip-flops, 714 ALUTs on them |
| L1I (`fes_i386_l1i`), with the ITLB | 2,112 | 1,268 | 3,575 | 24 | 1,257 ALUTs on valid bits |
| DTLB (`fes_i386_tlb`, 16×2) | 1,273 | 881 | 1,944 | 0 | new |
| BIU (`fes_i386_biu`) | 1,188 | 1,831 | 1,678 | 0 | |
| `fes_i386_muldiv` | 925 | 630 | 1,465 | 0 | |
| `fes_i386_dfwd` | 822 | 87 | 1,257 | 0 | |
| `fes_i386_ex` (with the shifter) | 803 | 0 | 1,353 | 0 | |
| `fes_i386_decode` | 586 | 0 | 1,000 | 0 | |
| `fes_i386_dops`, `agu`, `exctl`, `seq`, governor | 1,073 | 228 | | 2 | |
| **`pc386_machine`, all** | **8,383** | 8,462 | 13,747 | 44 | |
| — AT core (`pc386_at_core`) | 5,150 | 4,288 | 8,410 | 6 | |
| — — KBC (`pc386_kbc`) | 2,221 | 1,750 | 3,387 | 0 | |
| — — — HID keyboard model (`pc386_hid_keyboard`) | 1,313 | 744 | 1,982 | 0 | four 144-bit key-state vectors |
| — — ATA, RTC, two DMA, PIT, PIC, I/O decoder | 2,763 | | | 5 | |
| — VGA (`pc386_vga`, with scanout) | 1,925 | 1,876 | 3,002 | 20 | CRTC, palette and line tags are flip-flops |
| — presenter, display plane, bridge, port 2 | 1,308 | | | 18 | |
| Audio (OPL2, Sound Blaster, mixer) | 1,502 | 1,881 | 2,440 | 9 | |
| fes.computer storage and mailbox | 639 | 953 | | 3 | |
| HPS DDR guards (board shell) | 575 | 1,348 | 339 | 0 | |
| ROM, disk and status ring (382); logic outside the listed modules in `pc386_system` (99) and `top` (17) | 498 | | | 128 | the rows sum to the fit's 36,794 |

The CPU's own logic, 8,456 ALMs, is the largest single item. Most of its
atoms are anonymous in the fit, so this record can't split it further. A
later audit could do that by moving a stage into its own module.

## What paging added

| Part (plan's row) | Plan | Measured | Registers | Where it went |
| --- | ---: | ---: | ---: | --- |
| DTLB, 16 entries, 2 ports | 500 (with the 64 tag compares) | 1,273 | +881 | 847 entry flip-flops (640 needed; Quartus duplicated about 200 for fan-out); 757 + 166 ALUTs of variable-index write logic; 310 for the two ports' compares; 322 for the two ports' AND-OR select; 138 for the fill's own compare (`same`) |
| L1D: walker, the 64 tag compares, A/D store | 400 (walker) | 744 | +496 | four 101-ALUT AND-OR terms (the 16-entry × 4-way physical compare plus paging off); walker state (`w_lin`, `w_pte`, `w_pde`, `w_ent`, `pf_lin`, `pw_data`) |
| ITLB, 8 entries | 250 | 535 | +343 | 324 entry flip-flops; 362 + 136 ALUTs of write logic, as in the DTLB |
| L1I, page registers and spoil | (in ITLB's 250) | 114 | +19 | valid-bit clears for the A/D store, page and attribute select |
| CPU own logic: CR2, CR3, PG, #PF, the pages carried M2 to WB, the M1 rights checks, physical lines for self-modifying code | 150 | 1,051 | +386 | `dt_*` select of the matching entries' pages and rights (653 ALUTs in the seal), `pg_a0/a1/t0/t1` carried down the pipe, `m1_*` checks |
| **Total** | **1,300** | **3,717** (3,478 net) | **+2,125** | the machine's noise (−182) and BIU (−105) bring the sum to the measured total |

The misses have three causes:
- **Flip-flop arrays cost more than their compares.** The plan costed the
  CAM compares. It didn't cost storing, writing and selecting 640 + 320 bits
  of entries, which is most of both TLBs.
- **The write path is an artefact.** It is 1,420 ALUTs, and the scratch fit
  saves 744 ALMs of it.
- **Pipeline plumbing.** The plan gave 150 ALMs to "physical lines, CR2, CR3
  and PG". The physical pages ride the pipe from M1 to WB, and M1 selects and
  checks rights for two access ports. That added 1,051 ALMs to the CPU's own
  logic. Phase 5's remaining slices are mostly this kind of control, so the
  re-budget below costs them from [measured control slices](#re-budget-slices-25),
  not from the plan.

## Memories: what landed where

- **M10K: 265 blocks, unchanged by paging.** The 10999 warnings Quartus
  prints still name the RTC RAM, the VGA line buffer, the CMOS words and the
  eight L1D byte-lane arrays. But each is in M10K now, split per byte lane
  (`cpu_l1d_ev0[0][31]__1`, `machine_at_rtc_ram_memory[0][15]__33`,
  `machine_vga_line_data_memory[0][31]__39`). The roughly 2,000 registers
  the [M4 record](../2026-10-29-pc386-m4-quartus/summary.md) counted for the
  RTC RAM and the VGA line buffer are gone.
- **MLAB: none.** `ALMs used for memory` is 0.0 for every entity, and
  `Total MLAB memory bits` is 0.
- **Memories Yosys makes into flip-flops before Quartus reads them.** The
  producer's `memory_map` maps every memory with more than one write port
  and no `ramstyle`. Quartus would reject Yosys's form of those as having
  more than one driver. There are 30 of them, about 2,500 bits in all:

  | Memory | Size | Bits |
  | --- | --- | ---: |
  | `cpu.frontend.ras.stack` | 16×32 | 512 |
  | `machine.at.kbc.ram` | 32×8 | 256 |
  | `machine.vga.crtc` | 25×8 | 200 |
  | `machine.vga.palette`, `line_mask`; KBC keyboard and mouse FIFOs | 16×8 or 4×32 each | 640 |
  | DMA channel registers (two 8237s, 10 arrays) | 4×16 / 4×8 | 576 |
  | 15 more, 2–52 bits each | | 344 |

  Most of these are true register files: they're written from more than one
  place and read every clock. The return stack (289 ALMs for 512 bits) and
  the KBC RAM are the only ones big enough to be worth an MLAB.
- **Quartus leaves 8 small OPL2 memories uninferred** (276007, asynchronous
  read; 276004, size). The four `mem_single_bank` instances that remain
  flip-flops total about 220 ALMs.
- **Arrays in flip-flops by design.** These are the two TLBs (a CAM must
  compare every entry at once), the L1 valid bits (a flush or a DMA write
  clears them at once), the register file and the segment caches. None of
  them can be an MLAB as designed.

## The Yosys route's cost, for #1581

| Item | ALMs | Evidence |
| --- | ---: | --- |
| `$pmux` as `casez` with a `parallel_case` attribute | 0 | The fit with Quartus's own directive added gives identical synthesis (56,331 ALUTs, 32,350 registers, 36,751 ALMs). The seal's map report flags those 613 attributes unrecognised (10335); the experiment's says the directive overrides an existing value (10890). Either way, they cost nothing. |
| `flatten` before Quartus | about 0 (−43) | The per-module fit totals 36,794 against the seal's 36,751. |
| Variable-index write into a packed array (`e[slot] <= x`) | 744 in the TLBs | the scratch fit; the same idiom elsewhere is [below](#other-findings) |
| Multi-write-port memories as flip-flops | about 250–400 | The return stack is 289 ALMs and would be about 20 in two MLABs; the KBC RAM and VGA CRTC are a few tens each. The rest are register files that stay registers either way. |
| `full_case` ignored (12,028 sites) | not measured | Yosys writes each default out (`'hx` or the held value), so the attribute adds nothing Quartus lacks; no fit changed it. |

Quartus 17.0.2's own front end can't read the RTL. It stops on the OPL2
import's casts and the CPU's generate loops (the producer's docstring), so
that route can't be compared directly. Per this record, #1581's first
primitives should be:
- a register array with a per-entry write enable (the TLB idiom);
- a single-write-port register file Quartus can infer as MLAB (the return
  stack, the KBC RAM).

One-hot muxes come out the same either way.

## Other findings

- **The fill's own compare.** `fes_i386_tlb` compares the filled page with
  every entry (`same`, 138 ALUTs in the DTLB and 75 in the ITLB) so a refill
  for D replaces the read's entry. The walk started from a lookup that
  already compared that page. Carrying that port's `match` to the fill
  would delete the second compare: about 100 ALMs, less timing-sensitive
  than the write path.
- **DTLB entry duplication.** Quartus holds 847 entry flip-flops for 640
  bits, to cut fan-out into the 64 L1D compares and the two selects. With
  per-entry enables it still holds 870 registers. This is the fitter's call,
  not the RTL's.
- **The HID keyboard model is 1,313 ALMs.** It holds four 144-bit key-state
  vectors (`queued`, `stable`, `observed`, `sent`) in flip-flops. Its diff
  scan indexes them a bit a clock. But it also compares and copies whole
  vectors (the rows' stability check, snapshot and reconcile), which
  limited-port memory can't do in one clock. Putting them in memory
  therefore means redesigning it around a serial snapshot and diff, and some
  compare and control logic stays. The saving is not measured. It could be
  anywhere from a few hundred ALMs to about half the module, and needs an
  analysis fit of a redesign before the budget counts it. It's a machine
  change, independent of the CPU.
- **The L1 valid bits** cost 559 flip-flops and 714 ALUTs in the L1D, and
  706 and 1,257 in the L1I. They are flip-flops so that a flush clears them
  in one clock. A generation-count scheme could put them in MLAB, but it
  changes the flush's design; it's a Phase 7 idea, not a quick win.
- **The front end's byte rotators** (`qalign`, 586 shift ALUTs) and per-byte
  predecode (`fetchbuf`, 1,563 registers) are Phase 4's speed. They're large,
  but cutting them costs DOOM frame rate.

## Re-budget: slices 2–5

The plan's per-slice designs are in
[`i386-cpu-design.md`, Phase 5](../../../sources/misteross/docs/i386-cpu-design.md#phase-5-v86-paging-and-task-switches).
Slices 2–5 add no CAM and no new array, apart from the debug registers'
4×32 bits. They are microcode, D/EX checks and a few pipeline bits.
Measured control slices are the better guide for that kind of work:

| Measured slice | Kind | ALMs |
| --- | --- | ---: |
| Phase 2 slice 3, ring 3 (#1552) | privilege checks, TSS stack read, IRET to outer, inner deliveries, I/O bitmap for IN/OUT | +318 |
| Phase 2 IDT slice (#1411) | descriptor-table deliveries | +520 (three slices together) |
| CS limit (#1434) | one general check at D | +28 |
| Paging's CPU-own-logic part | pages and rights carried down the pipe, two-port checks | +1,051 |

| Slice | Plan | Estimate | High | What drives it |
| --- | ---: | ---: | ---: | --- |
| 2: DR0–3, DR6, DR7, MOV DRn, GD; inner call gates, parameter copy, RETF outward | 350 | 400 | 550 | about 170 debug-register bits with load enables, joined to the MOV CRn read mux; call gates reuse #1552's stack switch, adding a parameter-copy count and loop |
| 3: V86 | 400 | 550 + 3 M10K | 700 | VM checks beside #1552's CPL and IOPL checks in D, the #GP(vector × 8 + 2) code, V86 segment loads on the real-mode path, INS and OUTS through the bitmap, the 30–40 uop delivery and IRETD sequences |
| 4: task switches | 300 + 3 M10K | 550 | 800 | new uop forms (TSS field access through TR, CR3 load in a routine, busy-bit store), a register index from a sequencer count to save and load the GPRs, DR7's local-enable clear |
| 5: breakpoints and single step | 300 | 500 | 600 | four 32-bit compares at AG and, with length masks, at M1's two ports; B bits and BS carried AG to WB (the paging lesson); DR6 update, RF, TF's trap and the MOV SS shadow |
| **Slices 2–5** | **1,350** | **2,000** | **2,650** | |

- **Microcode ROM.** It holds 227 of 256 words now. Slice 3's 30–40 uops
  outgrow it, so it goes to 512 words (6 M10K, +3) in slice 3, not slice 4.
  That costs 3 of the 288 free M10K blocks and one address bit. The
  estimate column charges the M10K to slice 3; the plan column keeps the
  plan's own figures. If slice 2's call-gate sequences use the last 29
  words, the growth moves to slice 2.
- **Pessimistic case.** Paging's own ratio, 2.7× the plan, applied to the
  plan's 1,350 gives about 3,600. These slices are mostly control, as #1552
  was, so this record doesn't expect that. It's the bound to plan the
  options against.

| Case | Slices 2–5 | Without the TLB fix | With it, fit total (−500) | With it, CPU delta (−744) |
| --- | ---: | ---: | ---: | ---: |
| Estimate | 2,000 | 38,750 (92.5%) | 38,250 (91.3%) | 38,000 (90.7%) |
| High | 2,650 | 39,400 (94.0%) | 38,900 (92.8%) | 38,650 (92.2%) |
| Pessimistic | 3,600 | 40,350 (96.3%) | 39,850 (95.1%) | 39,600 (94.5%) |

The TLB fit's total fell by 500. The CPU fell by 744 and the unchanged
machine rose by 244, which is within the [noise](#method). The conservative
column counts 500.

**Headroom for timing.** At 36,751 ALMs the seal reports 97% of LABs partly
used and "Low" packing difficulty. It reports 2,335 ALMs recoverable by
dense packing and a peak interconnect use of 73%. Its setup slack is
+0.620 ns, and the worst path is D's `st_e` to the issue enable. Quartus
has met this design only up to 88%; above that is untested. This record
takes about 92% (38,500 ALMs) as the working ceiling. Past that, dense
packing starts merging unrelated logic into shared LABs, and the critical
paths' placement gets worse.

**So:**
- With the TLB fix, the estimate fits under that ceiling with 250–500 ALMs
  to spare.
- The high case is over the ceiling, by 150–400 ALMs, unless a second win
  lands too. The measured candidates are the fill compare (about −100) and
  the return stack in MLAB (about −250). Leaving breakpoints out of slice 5
  is a scope choice worth about −300. The HID keyboard redesign may be
  larger, but it is unmeasured.
- Without the fix, even the estimate is over the ceiling.
- The TLB fix should land before slice 2. Each slice's seal should be
  checked against the estimate here; a slice that comes in 30% over is the
  signal to take the next option.

## Options if the slices run over

| Option | ALMs | Cost or risk |
| --- | ---: | --- |
| Per-entry write enable in `fes_i386_tlb` | −500 to −744 (measured) | Five lines; lockstep and seal as any RTL change. Needs #1579 merged. The scratch form also loads an entry when a fill and a flush coincide; the flush's cleared valid bit hides that, but the real change should keep `!flush` in the enable so it matches today's RTL exactly. |
| Fill reuses the lookup's `match` (drop `same`) | about −100 | a small change to the walker's fill; timing-neutral |
| HID keyboard redesigned around a serial snapshot and diff, its vectors in M10K/MLAB | unmeasured; up to about −650 | machine RTL, independent of the CPU. Whole-vector compares and copies have to become serial passes, so an analysis fit of the redesign must come before the budget counts it; keyboard tests and a kit check. |
| Return stack as a single-write-port memory in MLAB | about −250 | needs its two writes (push and repair) on one port; #1581's primitive |
| Breakpoints left out of slice 5 (MOV DRn, GD and TF kept) | about −300 | no census title arms DR7 or raises #DB; test386 doesn't touch the debug registers. A breakpoint would silently never fire, a compatibility gap to record |
| Return stack 16 → 8 entries | about −150 | Phase 4's measured hit rate drops; a DOOM rerun decides |
| DTLB 16 → 8 entries with a 64-entry MLAB second level (the plan's own fallback) | about −400 | the plan's fallback for timing; 8 entries alone miss 2.86% under JEMM386, so the second level is needed |
| OPL2's four small register memories as MLAB | about −150 | vendor code |

The measured TLB fix plus the estimated fill-compare and return-stack rows
come to −850 to −1,100. That leaves the pessimistic case at 39,250–39,500
ALMs (94%), over the ceiling. It needs breakpoints left out or the keyboard
redesign as well.

## AlphaMister

AlphaMister ([DeanoC/alphamister](https://github.com/DeanoC/alphamister))
has no Quartus backend, and i386 is Quartus-only (#1235), so this audit
didn't use it. Its kind of rewrite would have found the TLB write path,
which is worth 744 ALMs here. Every other loss this audit could measure in
the Yosys-to-Quartus route came out at zero. So a Quartus backend wouldn't
have paid for itself in this audit. It's worth reconsidering after Phase 5
only if #1581's primitives leave a measurable gap.

## Reproducing

The scripts are on Powerboat in `~/fes-scratch/area1548`:
- `run_hier_area1548.sh TAG` runs the producer without `flatten` in the
  worktree `wt-TAG`, through `hier_build_area1548.py`;
- `run_exp_area1548.sh` is the parallel_case fit;
- `atoms.tcl` dumps every post-fit atom (type, location, name);
- `cmp.py` and `grp.py` make the entity table and group atoms by name.

`run_hier_area1548.sh TAG` expects a detached worktree at `wt-TAG` with
the pc386 firmware copied into its `build/firmware/pc386`. Each fit
takes 20–40 minutes on Powerboat. The worktrees have been removed. The fit,
map and timing reports of all four analysis fits are kept in `reports/`
(`pg`, `base`, `tlb`, `exp`), with the TLB change as
`reports/tlb/tlb-write-enable.diff`. The seal `416e332f`'s project is in
`~/fes-scratch/p5pg/seal-s1`.
