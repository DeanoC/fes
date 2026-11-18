# i486 slice 1: what the 486SX delta and a runtime strap cost, 18 November 2026

This record is slice 1 of the [i486 plan](../2026-11-17-i486-plan/summary.md),
the fit spike. It measures what the 486SX delta costs in ALMs and timing
inside `pc386_system` on the DE10-Nano and the DE25-Nano, and whether one
bitstream holding both the 386 and the 486 behind a runtime strap costs
materially more than a 486 build parameter. No CPU RTL merges: the spike RTL
is on the unmerged branch `deano/i486-s1-spike-rtl`, and is copied here as
patches. Tracker [#1962](https://github.com/DeanoC/fes/issues/1962); slice
issue [#1990](https://github.com/DeanoC/fes/issues/1990). The bands and what
each outcome means were committed before any fit:
[`method.md`](method.md) (`4fdd24de2`).

Every number in the block below is written by [`generate.py`](generate.py)
from the committed reports in `fits/`, `elaboration/` and `lockstep/`;
[`results.md`](results.md) holds the full tables. `generate.py --check`
fails when either is stale.

<!-- generated:start -->
**Result.**
- **The 486SX delta on the DE10 (arm b minus arm a): -53.5 ALMs between medians**, against an uncertainty of 55 (seed spread 55, netlist floor 10.3). Reading against the plan's 350 to 850: **not resolved: under 55 ALMs**. It is 1.5 ALMs short of resolving; resolved or not it reads below 350, so neither band edge is near. Synthesis, which no seed moves, adds +195 ALUTs and +44 registers; the per-module fits (one seed) put 309.3 ALMs of change in `fes_i386_cpu` (299 in the whole machine); the modules are in `results.md`.
- **The 486SX machine on the DE10: 36,405.5 ALMs median (86.87%)**, against arm a's 36,459: at or under 36,880 (88%): inside the range the machine has closed at before.
- **DE10 timing at 61.44 MHz:** arm a closes on 4 of 4 seeds, arm b on 4, arm c on 4; median CPU-clock setup slack +0.482, +0.559 and +0.582 ns, worst +0.338, +0.118 and +0.474 ns. Reading: no timing cost the spike can resolve.
- **The DE25 (Agilex 5 ALMs, a different unit, not held to the band): 204.5 ALMs** between medians (uncertainty 108); arm b at 35,763.5 (76.42%), at or under 41,184 (88%). Its uncertainty is the seed spread alone (no per-module DE25 fit). Quartus Pro's synthesis, which no seed moves, adds +46 estimated ALMs and +46 registers. Every DE25 fit closes at 61.44 MHz; the worst-corner `clk` Fmax medians are 122.9, 117.1 and 121.7 MHz for arms a, b and c; both clock domains were analysed at every corner in 12 of 12 DE25 fits. Every arm b seed's Fmax is below every arm a seed's, but arm c, the same logic plus the strap, is back near arm a's (121.7 against 122.9 MHz): a placement effect, not the delta's logic depth, by this record's reading.
- **Arm c, the shipped package if the strap is chosen:** 36,443 ALMs median on the DE10 (86.96%), 16 under arm a; 35,670.5 on the DE25 (76.22%), 111.5 over arm a.
- **The strap over the build parameter (arm c minus arm b): 37.5 ALMs on the DE10** (uncertainty 59) **and -93 on the DE25** (uncertainty 108). DE10 reading: **not resolved, timing equal within a seed: the strap costs under 59 ALMs**. DE25 reading: not resolved, timing equal within a seed: the strap costs under 108 ALMs. On the DE10, arm c's synthesis has +1 register and +38 ALUTs over arm b: the strap's latch and its gating; on the DE25, +3 registers and -46 estimated ALMs.
- **The 486 state is in the fits.** Arm b's netlist has +53 flip-flop bits over arm a's (TR3's 32, CR0's five new bits, and AC and ID in each EFLAGS copy) and arm c's +3 over arm b's; Quartus's synthesis keeps +44 registers of arm b's on the DE10 and +46 on the DE25. Which individual bits it kept was not checked.
- **Lockstep:** with the identity on, 45 of 48 directed cases agree with the level-4 oracle (the 386 agrees on 6); the 3 that stop are the ones `method.md` names as stops by design. With the identity off, the stops equal slice 0's committed list.
- **test386 with the identity off** (the spike RTL, CPU alone): POST codes end at FFh with 0 errors and no stop, 79,640,765 retirements to its halt.
- **Extension rule:** did not fire; four seeds an arm on both boards.

**Recommendation on the strap, for Deano to decide.** Measured: arm c minus arm b is 37.5 ALMs on the DE10 against an uncertainty of 59, and arm c closes on 4 DE10 seeds to arm b's 4. The strap costs nothing this spike can measure over the build parameter, so a runtime strap is affordable on area and timing. What it costs instead is outside the fit: a record bit, the ABI and FogCast fields, and lockstep, test386 and kit runs of both identities on one netlist. One more difference is not area: with a strap the 486 logic is in the package every 386 title runs, so that package's seal and kit runs move with the strap's first merge, where a build parameter left at 0 leaves the 386 package's logic as it is (the tracker's rule that the identity defaults off holds either way). The spike recommends the strap if per-title identity selection is wanted; otherwise the build parameter alone is the smaller change.
<!-- generated:end -->

## What was fitted

| Arm | Commit | What it is |
| --- | --- | --- |
| a | `1ec59267039b` (main) | The base, unchanged |
| b | `15d53ac41016` | The spike RTL; `pc386_system` ties the CPU's `i486` input to 1 |
| c | `c4d5cb71b0e4` | The same RTL; `i486` is bit 1 of the `fes.pc386.machine` record's word 2 (`settings[33]`), latched by the CPU at each reset as `queue16` is |

- **One netlist per arm.** Each arm was elaborated once on Powerboat with
  the pc386 lock's HIP Yosys slot, at `fit_i386_device.py`'s fixed
  elaboration path, by [`fit_arm.py`](fit_arm.py), and that one file was
  fitted at every seed on both boards. `generate.py` checks that every fit
  of an arm names its arm's netlist digest and commit, and `fit_arm.py`
  refuses an arm whose commit changes anything outside the CPU RTL and
  `pc386_system.sv`.
- **DE10:** Quartus 17.0.2 Lite in the pc386 sealer's own project
  (`build_fes_pc386_quartus.write_project`: board shell, pins, SDC,
  settings), `BUILD_ID` all zeros in every arm, seeds 1 to 4, on Powerboat
  under the Lite builder lock. "Closes" is the sealer's own timing check
  (`validate_timing_report`): every setup, hold, recovery, removal and
  pulse-width slack at every corner non-negative, no unconstrained clocks,
  each clock at its frequency. One more fit per arm, seed 1, with Yosys's
  `flatten` dropped, gives the per-module split.
- **DE25:** Quartus Pro 26.3.0 through `fit_i386_device.py --design machine
  --device de25 --netlist`, the virtual-pin envelope of the Agilex fit and
  DE25 x87 records, at the sealed 61.44 MHz, seeds 1 to 4, on
  chip-30x0-ai-dev under its builder lock. Its two recorded attribute
  rewrites (M10K to M20K, the repeated `parallel_case`) are in each fit's
  `netlist.json`. The harness ran from a `git archive` of the base, so its
  own `fes_revision` reads `unknown`; each fit names its netlist's source
  commit through `--netlist-source`.
- **The strap is live in arm c, and folded away in arm b.** In arm c's
  netlist Yosys keeps the latch (`cpu_decode_i486 <=
  storage_machine_settings[33]` at reset) and the reset-value muxes; arm b's
  netlist has neither. On the DE25, each arm c fit's fitted netlist names
  the latch and Quartus's removed-registers table does not
  (`fits/de25-c-*/strap.json`). On the DE10 the synthesis report names
  no line with the latch in any arm c fit (`strap_map_lines` in each
  `summary.json` is empty), so Quartus's removed-registers table does not
  list it, though whether that table was complete was not recorded; the
  positive evidence there is arm c's synthesis counts over arm b's, in the
  generated block.

## The spike RTL

`spike-arm-b.patch` is arm b against the base (eight files, 399 lines in,
45 out); `spike-arm-c-over-b.patch` is the strap. What each item is, as
costed:

| Item | Shape in the spike |
| --- | --- |
| BSWAP | One uop; a byte-reversal lane in EX's result mux |
| XADD | Registers: XCHG's three flagless XORs, then the ADD (four uops). Memory: r/m into T1, a read-modify-write ADD, r from T1 (three uops). New sequencer states, no new datapath |
| CMPXCHG | Registers: CMP, then two flag-selected moves (a new EX lane, `cc ? y : x`). Memory: load into T1, CMP, select into T1, store (always, as a 486 writes), select into eAX (five uops) |
| INVD, WBINVD | CPL 0 NOPs; the caches are coherent |
| INVLPG | CPL 0; flushes both TLBs whole at WB, which the architecture permits; the operand's address is not formed |
| CPUID | Four uops, each one register from a constant table in EX by EAX's leaf; leaf 0 "GenuineIntel" and highest leaf 1, leaf 1 a 486SX signature (`00000427h`) with no feature flags |
| CR0 | NE, WP, AM, NW, CD stored; reads give the 486's layout with ET 1; NW without CD raises #GP(0) |
| WP | In the DTLB's rights check and in the walker's fault condition |
| EFLAGS.AC and ID | EFLAGS and every pipeline copy widened from 18 to 22 bits; POPFD, IRETD and a task switch's load write both; PUSHF's image carries them |
| #AC | At M1, on the linear address, for 2-, 4-, 6- and 8-byte data accesses at CPL 3 with AM and AC set; vector 17, error code 0. Under AM at CPL 3, D holds an access behind an older EFLAGS load |
| TR3 | Storage beside TR4 to TR7 |
| Reset | DX `00000427h`, CR0 `60000010h`, DR6 bit 12 clear, all through the machine's `init_*` ports; the identity input is sampled at reset |

**Shortcuts a real slice 2 would not take,** each of which makes the spike
cheaper than slice 2 may be:
- the three new results ride in the segment-class uops because `alu_op_e`
  is full; slice 2 may widen the op field instead;
- #AC is checked before the page walk, so a misaligned access to a
  non-present page faults #AC where a 486 gives #PF;
- INVLPG flushes everything, and INVD and WBINVD do nothing;
- no governor cost rows for the new instructions, no BIOS change for CD and
  NW, no FogCast or ABI change for the strap's record bit;
- BSWAP with a 16-bit operand swaps and keeps the low word, which a 486
  leaves undefined.

## What the lockstep shows, and does not

Slice 0's 48 directed cases (PR #1976 at `da4aee953`) ran against the
level-4 oracle on a throwaway branch merging slice 0 with the spike, with
the testbench driving the CPU's `i486` input from an environment variable
([`lockstep-scratch.patch`](lockstep-scratch.patch); nothing of it merges).
The counts are in the generated block. With the identity on, the three
cases that stop are the three `method.md` names as stops by design: the two
`mov-cr4` cases (the target has no CR4, by decision) and `cpuid-1` (the
oracle is Bochs's i486DX4, signature `0480h` with the FPU flag; the spike
returns a 486SX's). With it off, the stops equal slice 0's committed list,
which shows the strapped RTL is the 386 with the strap off.

test386.asm also ran on the spike RTL alone with the identity at its
default, off (`test386/identity-off-rtl.json`; counts in the generated
block). It shows the 386 identity still passes the 386 conformance ROM. It
was run without the SingleStepTests cross-check, whose corpus is not on the
host, and it was not compared step for step with main's RTL.

That is all it shows. The spike RTL is not validated past those runs: no
random programs, no level-3 lockstep, no netlist lockstep, no census, no
seal and no kit run.

## Three measures of the SX delta, and what each establishes

The method's statistic is the median of ALMs needed in the sealer's
flattened flow, and its reading is in the generated block. Two other
measures are reported beside it, because on the DE10 they do not agree:

- **Flattened fits, four seeds** (the method's statistic). Quartus packs
  the whole flattened machine at about 87% of the device; ALMs needed
  counts packed ALMs, so what it measures is how much the delta moves the
  packed total. Within these seeds that is not distinguishable from
  nothing.
- **Synthesis counts** (Analysis & Synthesis, before placement, which no
  seed moves): ALUTs and registers the delta adds. They establish that the
  logic is there and roughly how much, in ALUTs, not ALMs.
- **Per-module fits, one seed** (no flatten): the ALMs the fitter books to
  `fes_i386_cpu` and to each module under it. They establish where the
  delta lands. They rest on one seed, and keeping the hierarchy changes
  packing, so they say where the logic is, not what the sealed flow pays
  for it.

So the honest statement is the one the method fixes: in the flow a DE10
seal uses, the delta is under the uncertainty the generated block gives;
the logic itself is real, and its size in the per-module fit is stated
beside it. The plan's 350 to 850 was an estimate item by item, and nothing
here measures it item by item; but every measure in the generated block,
the DE25's included, is below its low end.

**The "under N ALMs" figures are not bounds on the cost.** The bands and
readings stand as `method.md` committed them. But two deltas in the
generated block are impossible as true costs: the DE10's SX delta, which is
negative while synthesis shows the logic added, and the DE25's strap delta,
negative where arm c holds arm b's logic plus a latch. So a change of
netlist moves ALMs needed by more than the precommitted uncertainty
captured on either board: the netlist floor read the logic outside the CPU
in the per-module fits, where the change barely moved it, and the DE25's
uncertainty is its seed spread alone. Read "not resolved, under N" as "this
flow cannot separate it from the netlist's own noise", not as "it costs
less than N". For the same reason the DE25's resolved SX delta is weaker
than its reading looks.

**Why the spike is cheap (this record's reading, not a measurement).** It
builds the new instructions out of machinery the CPU already has: XADD and
CMPXCHG are uop sequences through T1 like XCHG's, three new results are
lanes in EX's existing result mux, INVLPG reuses the CR3 write's TLB flush,
and CPUID is a constant table. The plan's estimate assumed new datapath for
some of these. The parts that are new logic, the 22-bit EFLAGS in every
pipeline copy, WP's term in two rights checks and #AC's compare at M1, are
also the parts on the memory stage's fault path, and on these seeds none of
them cost the DE10 a closing seed.

## Departures from the method

- **The DE10 strap evidence was added after arm b's first fits.** The
  `strap_map_lines` field in `fit_arm.py`'s summary was added while arm b's
  DE10 fits were running and before any arm c fit; arm a's seeds 1 and 2
  and arm b's seed 1 do not carry it. It reads the synthesis report and changes
  nothing fitted.
- **Read after the fits, from retained files:** the DE25 strap check
  (`strap.json`) and synthesis counts (`synthesis.json`), from each fit's
  synthesis report and fitted netlist; and each arm's flip-flop bits
  (`elaboration/stat-width-*.txt`, Yosys `stat -width` on the arm's
  netlist). None changes a fit.
- **The two DE10 queues ran interleaved** under the one Lite lock, so arm
  a's per-module fit ran after arm b's first seeds. Order does not change a
  fit.
- **The DE10 fits read the board files from a checkout at another arm's
  commit.** The elaboration checkout moved to arm b's, then arm c's, commit
  while arm a's later DE10 fits (seeds 3 and 4 and its per-module fit) and
  all of arm b's were queued. `fit_arm.py de10` takes only the board
  shell's project files (QSF, pins, SDC, the toolchain locks) from the
  checkout; the netlist is the arm's own file, checked against its digest.
  None of those project files differs between the three commits
  (`fit_arm.py` refuses an arm that changes anything outside the CPU RTL and
  `pc386_system.sv`).
- **The lockstep ran on** the throwaway branch `scratch/i486-s1-lockstep`
  at `d7ca63ec2` (local, not pushed): slice 0's head `da4aee953` merged with the spike's
  RTL at `15d53ac41016` (arm b's commit; arm c changes only
  `pc386_system.sv`, which the CPU-only lockstep does not build), plus the
  testbench line in `lockstep-scratch.patch`. test386 ran on the spike
  branch at `c4d5cb71b0e4`, whose CPU RTL is arm b's.
- **test386 ran** (the method allowed it "if time allows"), with
  `--no-cross-check`, its Bochs source copied from the lockstep worktree's
  verified fetch rather than fetched again. The level-3 lockstep did not
  run.

## What was not run, and what rests on one seed

- **One seed:** arm b's worst DE10 setup slack in the generated block is
  one seed's; every per-module figure (the CPU subtree's delta and the
  netlist floor) is from one fit per arm, seed 1, on the DE10.
- **Not run:** the level-3 lockstep with the identity off; a DE25 per-module split (the envelope harness flattens, and
  its port handling is defined for the flattened netlist only); any seed
  past 4 (see the generated block for whether the method's extension rule
  fired); the lockstep and validation runs listed above; a seal, a package
  and a kit run, as the method says.

## For Deano

The recommendation on the strap is in the generated block, with the
measurement beside it. The decision is Deano's.
