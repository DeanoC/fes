# i386 Phase 5 slice 1: paging, 8 November 2026

This record covers CPU Phase 5's slice 1
([`i386-cpu-design.md`, "Phase 5: V86, paging and task switches"](../../../sources/misteross/docs/i386-cpu-design.md#phase-5-v86-paging-and-task-switches),
tracker [#1548](https://github.com/DeanoC/fes/issues/1548)). `fes_i386_cpu`
now pages: CR0.PG, CR3 and CR2, a 16-entry DTLB at M1, an 8-entry ITLB in
F1, a page walker that sets A and D, #PF with its error code, physical
lines for the self-modifying-code check, and a MOV CR0 that sets PE and PG
together.

**Result:**
- **test386.asm** on the RTL passes POSTs 08 and 09, which turn paging on,
  and stops in POST 20 at `call RING0_GATE`, a call gate (slice 2).
- **Lockstep.** Everything passes against Bochs, on the RTL and on the
  locked Yosys's netlist, with identical retirement traces: the full
  corpus, 32,000 random programs, 8,000 protected-mode programs (126,251
  retirements with paging on, 824 #PF deliveries) and 54 new directed
  paging cases. Seven one-line mutants of the paging RTL each fail cases.
- **Seal.** Quartus 17.0.2 meets every analysis at 61.44 MHz: setup
  +0.620 ns (main's last seal +1.180), hold +0.111 ns. The worst path is
  D's store-to-load check into `issue`, whose enable now reaches the
  decoded instruction's paging tags. ALMs grow from 33,455 to 36,751
  (87.7%), more than the plan's estimate for the slice.
- **Kit B, max.** Time Circle (Borland 32RTM, paging at CPL 3) loads and
  reaches its main menu. Paging off keeps main's speed: DOOM v1.9
  `-timedemo demo3` 40.08 fps (main 40.05), and the benchmarks match
  main's to a tick. Wolfenstein 3D, Ultima VI, DOOM, The Black Gate and
  Serpent Isle all start and play.

## Design as built

The RTL README's
[Paging](../../../sources/misteross/cores/fes-common/rtl/i386/README.md#paging)
has the full description, and the design doc's slice 1 lists where it
departs from the plan. In short:
- **DTLB.** 16 entries in flip-flops (`fes_i386_tlb`), two lookups at M1:
  the memory operand's page, and a st2 store's page (MOVS, ENTER's copies)
  or the operand's next page. The L1D compares each tag with every entry's
  physical page in parallel and selects with the matching entry, so the
  hit path keeps its depth plus an AND-OR. Every uop's physical pages go on
  in its record (`pg_a0`, `pg_a1`, `pg_t0`, `pg_t1`): WB's lines, the store
  merge's memory types, the self-modifying-code check and the governor's
  MMIO charge all use them. With paging off they are `phys`'s, as before.
- **The walker** (in the L1D's slow path) runs only for the oldest uop.
  A uop the DTLB cannot serve (a page it does not hold, a right a user
  access lacks, a write to a page with D clear, a st2 uop's third page, a
  load meeting an older store through an alias, a fetch walk) waits as an
  uncached load does, then walks, or replays and issues alone to the end of
  its REP element. The walk reads the PDE and PTE through the bus
  interface, faults or sets A (and D) with a dword store through the write
  buffer, and fills the TLB. The access then reads memory without a fill
  or forwarding; nothing replays after a walk.
- **ITLB.** 8 entries looked up in F1 beside the L1I's read. A block whose
  page it does not hold reads as a hole with a mark; each block carries its
  entry's U/S and physical page bits 19:12 to Q. D turns an instruction
  with a mark, or one from a supervisor page at CPL 3, into a walk uop that
  fills the ITLB and restarts the instruction, or raises #PF.
- **#PF** carries CR2 and its error code to WB; CR2 is written as WB takes
  it. Its error code has no EXT, and its double-fault class is its own
  (Bochs's `is_exception_OK`).
- **Order.** A probe of a slot to be pushed translates as a write, so a
  delivery or far CALL faults in its pushes' order, as Bochs pushes. A
  MOVS whose source page faults raises #PF before its destination's
  segment fault. Deliveries under paging issue one uop at a time.
- **Flushes.** A CR3 write, a CR0 write that changes PG, and a change of
  A20M or the attribute map flush both TLBs.

## Lockstep

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of 2,000
random programs, 16 seeds of 500 protected-mode programs, the full corpus) at
`93928cea2`, the sealed checkout, on the RTL and on the netlist from the i386
lock's Yosys (`238eac9a4`):

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 (8,131,380 retirements) | same |
| Protected-mode programs | 8,000 of 8,000 (453,731 retirements, 126,251 paged, 824 #PF deliveries) | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 147, 54, 1,933 and 27, all pass | same |
| Holds without latency | pass | pass |

Every corpus file's and random seed's retirement trace hash is the same on
both. With paging off nothing changed: the corpus, random and directed
results are as main's.

**What the random programs found.** The first full run, before the fixes
below, failed 18 of 40,000 programs; each failure was one of these, now
fixed and kept by a directed case where it is the CPU's:
- A delivery's probes checked every slot's limit before any push, so a
  later slot's wrap (#SS) came before the first push's #PF, which Bochs
  raises: probes now translate as writes (`pg-delivery-push-pf`).
- A MOVS whose source page faults raised its destination's segment fault
  first; Bochs reads first (`pg-movs-src-pf-es-limit`).
- XCHG m and ARPL m loaded their operand as a read, so a #PF on it lacked
  W/R; Bochs reads it for a read-modify-write, so the load now translates
  as a write.
- The bench's: the arena burst emitted FF /2 and F7 /0; a random far jump
  could land in 32-bit code with 16-bit code after it; the stack.flags
  rule looked for an inner-privilege delivery's FLAGS image on the old
  stack. All three are fixed in the bench.

**New directed cases.** `run_paging_cases` (`FES_I386_MODE=paging`), 54
cases. Each enters protected mode with CR3 at a page directory, runs 32-bit
flat code with paging on, and compares every retirement with Bochs, CR2
included. A fault's vector, error code and CR2 are checked apart, and A
and D through the PTEs a case reads back into registers:
- walks and A/D: `pg-read-a`, `pg-write-d`, `pg-read-then-write` (D on the
  first write through a read's entry), `pg-rmw-d`, `pg-a-set-kept`,
  `pg-r0-ro-write` (CPL 0 ignores R/W);
- #PF at CPL 0: `pg-np-read`, `pg-np-write`, `pg-pde-np-read`,
  `pg-pde-np-write`, `pg-pde-np-rmw`;
- CPL 3, each PDE and PTE combination of U/S and R/W: `pg-r3-pte-sup-read`,
  `pg-r3-pte-sup-write`, `pg-r3-pte-ro-write`, `pg-r3-pte-ro-read`,
  `pg-r3-np-read`, `pg-r3-np-write`, `pg-r3-pde-ro-write`,
  `pg-r3-pde-sup-read`, `pg-r3-user-rw`, `pg-r3-rmw-ro`;
- CPL 3 running from a supervisor page the ITLB holds (`pg-r3-exec-sup`);
  descriptor reads as supervisor accesses at CPL 3 (`pg-r3-gdt-sup`, and
  `pg-r3-ldt-np`, a #PF without U/S);
- page-crossing data in non-adjacent frames and their second-page #PF
  (`pg-cross-read`, `pg-cross-write`, `pg-cross-read-pf`,
  `pg-cross-write-pf`, `pg-r3-cross-sup`);
- page-crossing instructions (`pg-cross-insn`, `pg-cross-insn-pf`,
  `pg-r3-cross-insn-sup`, `pg-r3-cross-insn-np`) and a fetch from a page
  not present (`pg-fetch-np`);
- self-modifying code through an alias (`pg-smc-alias`,
  `pg-smc-alias-deep`, whose rewritten instruction has left the fetch
  buffer) and into a crossing instruction's second page
  (`pg-smc-cross-insn`); a load meeting a store through an alias
  (`pg-fwd-alias`);
- a CR3 write's flush (`pg-cr3-flush`, with a PTE stored before it that
  neither TLB sees until the flush);
- a wrong path's load sets no A (`pg-wrong-path-a`);
- REP strings: `pg-rep-movs-pf`, `pg-rep-stos-pf`, `pg-movs-dst-pf`,
  `pg-movs-pages`, `pg-rep-movs-long`, `pg-movs-src-pf-es-limit`;
- frames: past guest RAM (`pg-frame-hole`), in the reset shadow
  (`pg-frame-shadow`), at 1 MiB under A20M (`pg-a20m-frame`);
- double fault and shutdown: `pg-pf-gp-df`, `pg-stack-np-shutdown`,
  `pg-delivery-push-pf`;
- `pg-pe-pg-together` (32RTM's MOV CR0) and `pg-toggle-farjmp` (CWSDPMI's).

**The cases catch broken RTL.** Seven mutants, each a one-line change, each
fail cases: no D bit in the walker's store (7 cases), no flush on a CR3
write (1), no user check (9), no alias check (1, `pg-fwd-alias`), no
supervisor-page mark at D (1), EXT in a #PF's error code (9), and the
self-modifying-code check comparing an instruction's linear page (1,
`pg-smc-alias-deep`). `pg-movs-src-pf-es-limit` and `pg-delivery-push-pf`
fail on the RTL before their fixes.

**Random programs.** Protected-mode blocks turn paging on half the time,
over identity tables at F00000h whose pages are now and then not present,
read-only or supervisor, and access a 16-page arena over 8 frames (so
aliases) with random rights: loads, stores, read-modify-writes, PUSH and
POP of memory, REP STOS and MOVS across pages, and CR3 rewrites. A store
into the tables ends the run, since both TLBs keep what they hold.

## test386.asm

`scripts/test_fes_i386_test386.py --cpu rtl`: POST codes 00, 01, 02, 03, 04,
05, 06, 08, 09 and 20, no error, then a stop as not built after 795,527
retirements at 00ABh:2DF2h (`call RING0_GATE|3:0`, a call gate, slice 2).
POST 08 builds a page directory and two page tables and turns paging on,
so every protected-mode POST runs paged.

## Seal

The seal is Quartus 17.0.2, seed 1, from clean checkout `93928cea2`
([seal-summary.json](seal-summary.json)).
- **Package.** `416e332f…`, build `4f13faee…`, with `core.rbf` `851f1120…`.
- **Analyses.** Every one is met:
  - setup +0.620 ns;
  - hold +0.111 ns;
  - recovery +6.025 ns;
  - removal +0.624 ns;
  - minimum pulse width +1.122 ns.
- **Clocks.** The system clock is 61.44 MHz.
- **Resources.**

  | Resource | This seal | Main's last seal (#1571, `54c89405`) |
  | --- | ---: | ---: |
  | ALMs | 36,751 | 33,455 |
  | Registers | 32,350 | 30,380 |
  | M10K blocks | 265 | 265 |
  | DSP blocks | 38 | 38 |

  The plan estimated about 1,300 ALMs for this slice; it takes 3,296. Most
  of the difference is width the plan did not count: four physical pages
  (60 bits) and a #PF's address and code in every uop's record from M2 on,
  each fetch block's and instruction's paging tags through the fetch
  buffer, Q, DC and D, the forwarding compares' page and alias terms, and
  the self-modifying-code compares' physical pages. 5,159 ALMs (12.3%)
  are left for slices 2-5, which the plan estimates at about 1,350.

[stage-slack.json](stage-slack.json) gives each register group's worst setup
slack at the slow −40 °C corner. The paths the brief asked to watch:

| Path | Main (#1571) | This seal |
| --- | ---: | ---: |
| System clock worst: D's store-to-load check into `issue`, the enable of a register group of the decoded instruction | +1.18 (`dc0`) | +0.62 |
| D's issue register (`dc0`) | +1.18 | +1.07 |
| Q's start and prefix accumulator | +1.57 | +1.47 |
| M1 to M2's load register (`m2_load`) | — | +1.38 |
| M1 to the L1D's walker request (`q_tx`) and freeze register (`stall`) | — | +1.44, +1.55 |
| F1 to the L1I's page registers (the ITLB) | — | +1.58 |
| The walker's DTLB fill | — | +1.65 |
| L1D hit registers (the DTLB's parallel compare) | — | +3.47 |
| M2's forwarding hits | — | +3.76 |
| D's EIP | +1.84 | +1.72 |
| EX | +5.90 | +6.18 |

The worst path starts at D's forwarding of an address register, adds
`d_lo` and `st_e` (the store-to-load check, which this slice does not
change) and reaches `issue`, whose enable fans out to 235 registers. The
slice adds the decoded instruction's paging tags to that group.

## Kit

Runs `k1`, `k2` and `k3` used Kit B, core-only, on 8 November from 19:34
to 20:17 ([kit-scores.json](kit-scores.json)).
- **Package.** `416e332f…`, the seal above, at max (preset 0), each launch
  after a SCRUBX scrub.
- **Kit.** Baseline software `a8cd30c4` on image `f904961e…`, with no
  install, no image change and no reboot. After each run Kit B was checked
  against its snapshot and logged FREE, and the run's FogCast data trees
  were removed and logged.
- **Disks.** The Phase 4 bench disk (`fe81c9c4…`) and the #1533 game drives
  `dpmi` (`48ab2972…`), `real` (`719dfedb…`), `pmode` (`75d909ca…`), `xms`
  (`e83b8185…`) and `fpu` (`152c83df…`), from Powerboat by SHA-256. None
  is committed.

| Benchmark | Main (#1571's r1, Kit A) | This slice (k1, Kit B) |
| --- | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,376 realtics, 40.05 fps | 3,863 in 3,374, 40.08 fps |
| Pack DOOM, maximum detail | 2,134 in 2,062, 36.2 fps | 2,134 in 2,061, 36.2 fps |
| Pack DOOM, minimum detail | 2,134 in 748, 99.9 fps | 2,134 in 748, 99.9 fps |
| 3DBench | 76.9 | 76.9 |
| PCPBench | 13.3 | 13.3 |
| TopBench | 309 | 310 |

| Title | Drive | Result |
| --- | --- | --- |
| Time Circle (32RTM), newly unlocked | `dpmi` | Its loader turns on paging (PE and PG in one MOV CR0) and runs at CPL 3; the title reaches its main menu (1 PLAYER, 2 PLAYERS, setup, EXIT) |
| Wolfenstein 3D | `real` | A new game, in E1M1 |
| Ultima VI | `real` | Its introduction plays |
| DOOM v1.9 | `pmode` | Its title and demos play |
| Ultima VII: The Black Gate | `xms` | Its introduction plays |
| Ultima VII: Serpent Isle | `fpu` | Its introduction plays to the main menu |

Each acceptance title was launched and watched for 100-120 s; this is a
recheck that paging leaves them running, not #1368's full acceptance
(saves, level exits). The DOOM and Ultima rows use the census drives, not
the acceptance disks of #1368.

## What this record does not cover

- **A CR3 write in the middle of a REP string**, from the plan's case list:
  nothing but an interrupt handler can write CR3 between a string's
  elements, and no directed case does. The random protected-mode programs
  inject interrupts between REP elements, but their handlers write no CR3.
- **A20M toggled while paging is on.** `pg-a20m-frame` runs with A20M
  held; the bench changes A20M only between runs. A change flushes both
  TLBs (`tlb_flush`), which no case checks against Bochs.
- **DMA into a page table** between the walker's read of an entry and its
  A/D store: the store is not locked against DMA, as the plan allowed.
- **Speed with paging on.** No kit benchmark pages; Time Circle's run
  above is its only paged load. A walk replays its instruction when its
  uop is not the oldest, and each new page of a REP string costs one such
  replay.
- **test386's #PF table** (POST 11) runs only after POST 22, so the slice's
  #PF coverage is the directed cases'.
- **nextpnr.** No route was made (fes#1235: Quartus at its best is the i386
  reference until the CPU works).

## Changes after the seal

The seal, lockstep and test386 runs are from `93928cea2`. After it, only
documentation and the bench's comments changed (the RTL README, the design
doc, `lockstep_tb.cpp`'s mode list, the sim README); no RTL file differs.

## Commands

From `sources/misteross`:

```sh
python3 scripts/test_fes_i386_lockstep.py --jobs 12
python3 scripts/test_fes_i386_lockstep.py --jobs 12 --yosys <i386 lock's yosys 238eac9a4>
FES_I386_MODE=paging build/sim/fes-i386-lockstep/rtl/Vfes_i386_cpu   # the paging cases alone
python3 scripts/test_fes_i386_test386.py --cpu rtl
python3 scripts/build_fes_pc386_quartus.py --root "$PWD" --cache-root "$CACHE_ROOT" --seed 1
```

The seal ran with `CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains`
and `QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus`. The kit runs
used scratch runners on Powerboat (`~/fes-scratch/p5pg/kit`), which are not
committed.
