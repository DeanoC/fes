# i486: random programs at level 4 and the governor bench at `I486 = 1`, 21 November 2026

Tracker [#1962](https://github.com/DeanoC/fes/issues/1962). This record closes
the two gaps that slice 2 ([PR #2045](https://github.com/DeanoC/fes/pull/2045),
record [`2026-11-21-i486-s2-isa`](../2026-11-21-i486-s2-isa/summary.md)) left
open:
- random programs against the level-4 oracle;
- a governor bench at `I486 = 1`.

The method, written before the first full run, is
[`method.md`](method.md). Everything ran as host simulation on Powerboat,
with at most 4 CPU threads at a time: no Quartus run, no seal, no kit.

**Result.** The code head is `ad328d78c`: this branch's changes merged with
main `0fb3ce80c` ([the merge of main](#the-merge-of-main)), with #AC's error
code at 0. Every gate below ran there, unless its row names `98cbd9927`, the
head before the merge and before the error-code change. Later commits change
only this record.

| Gate | Result |
| --- | --- |
| Level-4 directed cases, 486SX (`I486 = 1`) | 148 of 148 agree (108 cases and 40 `ud-` probes), on the RTL and the netlist, two runs each. The oracle's self-check passes 108 of 108 cases and 7 of 7 reset fields ([`directed/both-rtl.json`](directed/both-rtl.json), and the netlist's in `lockstep/level4-netlist.json`) |
| Level-4 directed cases, 386 (`I486 = 0`) | 56 agree, 92 stop, equal to `i486-expected-stops.txt` on the RTL and the netlist, two runs each. Of its 5 new rows, `dr6-bit12` agrees (a 386 keeps bit 12 too) and the other 4 stop at their probe |
| Level-4 random programs, 486SX, RTL | All 56,000 keep step: random 32,000, steady (no latency) 4,000, protected mode 8,000, tasks 4,000, debug 4,000, undefined 4,000; 11,708,925 retirements. The two steady seeds retire the same traces as with latency. Every form in `REQUIRED_FORMS` retired: each 486 form at least 380 times, 718 #AC deliveries (`ac-fault`), 328 CPL-0 writes stopped by WP (`wp-fault`). Every set's traces equal the `98cbd9927` run's ([`lockstep/level4-486sx-rtl.json`](lockstep/level4-486sx-rtl.json)) |
| Level-4 random programs, 486SX, netlist | The same 56,000 pass on the seal's Yosys netlist at `I486 = 1` (`2f9160cf…`). Every set's trace hashes and retirements, every form count and every rule count equal the RTL's ([`lockstep/level4-netlist.json`](lockstep/level4-netlist.json)) |
| Governor bench, `I486 = 1`, RTL | Passes. Each 486 loop's charge equals `governor.toml`'s clocks under every preset, CPUID's 12 included. The 386 part equals slice 2's 386 report in all 1,178 values ([`governor/i486-rtl.json`](governor/i486-rtl.json)) |
| Level-3 lockstep, 386, RTL | Passes. Equal to main `0fb3ce80c`'s report, run the same way, in all values but 11 source and suite digests: every count, trace hash and cycle figure, the x87 sections included ([`lockstep/level3-rtl.json.gz`](lockstep/level3-rtl.json.gz), main's [`lockstep/main-0fb3ce80c-level3-rtl.json.gz`](lockstep/main-0fb3ce80c-level3-rtl.json.gz)) |
| Level-3 lockstep, 386, netlist | The seal's own full netlist lockstep (below) passes. It equals main `0fb3ce80c`'s netlist report in all values but 12 source, oracle-build and netlist digests ([`seal/lockstep-report.json.gz`](seal/lockstep-report.json.gz), main's [`lockstep/main-0fb3ce80c-level3-netlist.json.gz`](lockstep/main-0fb3ce80c-level3-netlist.json.gz)) |
| DE10 seal of the 386 package | Registered producer `build_pc386_product.py --with-lockstep`, seed 1, Quartus 17.0.2 Lite, on Powerboat under a Lite lease slot, from a clean worktree of `ad328d78c`: passes. BUILD_ID `766bd82aaeed4898149883a6ccc6a012`, package `b3cd67c0…`, RBF `ce45a49e…`, 36,435 ALMs, setup +0.339 ns, hold +0.083 ns. `verify_pc386_product_seal.py` gives `SEAL_OK` ([`seal/`](seal/)) |
| Kit run of that package | Kit B, run `k2087a`: pass. FreeDOS prompt in 0.6 s, then Wolfenstein 3D on floor 1 with graphics. The kit was restored to its baseline and logged FREE ([the kit run](#the-kit-run)) |
| The head still builds the sealed design | `head_matches_seal.py` checks that the head's design identity equals the seal's |
| test386.asm, 386, RTL (`98cbd9927`) | POST codes end at FFh; halts after 79,640,765 retirements, no errors. Equal to the last RTL report on record (`2026-11-12-i386-ud-1701`) but its `text` and `verilator` fields ([`test386/rtl.json`](test386/rtl.json)) |
| test386.asm, oracle (merged head `cdbdccd7b`) | Equal to slice 2's report in all 707 values ([`test386/oracle.json`](test386/oracle.json)) |
| Governor bench, 386 (`I486 = 0`), RTL | Passes. Equal to slice 2's report in all 1,178 values ([`governor/386-rtl.json`](governor/386-rtl.json)) |

## What was built

- **A level-4 random mode** (`lockstep_tb.cpp`, `random` against the level-4
  oracle). The level-3 suite's generator runs with `Generator::isa486` and a
  486's start state. Its choices come from the `side` stream, so every
  level-3 seed's programs are byte for byte as before. The sim README's
  [Random programs at level 4](../../../sources/misteross/cores/fes-common/sim/i386/README.md#random-programs-at-level-4)
  gives the start state, the forms and the rules.
- **The runner** (`scripts/test_fes_i486_lockstep.py`). It runs the level-3
  suite's five sets at the 486SX identity with level 3's seeds and sizes,
  plus two seeds again without latency. They run after the oracle's
  self-check and before the directed cases in lockstep.
  `--random-only` runs the sets alone; `--netlist` runs them on the seal's
  netlist.
- **The governor bench at `I486 = 1`** (`scripts/test_fes_i386_governor.py
  --i486`). It runs the 386 bench unchanged, then `governor_bench_486.asm`:
  loops of BSWAP, XADD, CMPXCHG, CPUID, INVD and WBINVD, and INVLPG, at max
  and under every preset. NASM files CMPXCHG and CPUID under the Pentium, so
  the program writes those two as bytes under `cpu 486`.

## What the random programs found

Each divergence was classed as the method says: an RTL bug, fixed with a
directed case, or a difference of the oracle's model, given a narrow rule
with its source.

### RTL bugs

- **XADD to memory read its operand twice.** `SEQ_XADDM`'s first step was a
  read-modify-write after the load into T1, so an MMIO operand saw two reads
  (`mmio-reads.x.2x` in the first smoke run). The ADD's step is now a store
  of r plus T1. A 486 reads r/m once, as the RTL's CMPXCHG and XCHG already
  did. Directed case `xadd-m16-mmio`, with two CMPXCHG MMIO cases beside it.
  Counterexample: [`directed/486sx-rtl-before-xadd-fix.json`](directed/486sx-rtl-before-xadd-fix.json),
  where the case stops at `@100e.mmio-reads.2.4`.
- **DR6 bit 12 read 0 at the 486 identity.** `dr6_rd` forced it to 0 at
  `I486 = 1`. The Intel SDM (vol. 3, the debug registers' compatibility
  notes) says "it is possible to write a 1 in this bit on the Intel486
  processor", and Bochs's level 4 agrees. Reset still clears it
  (`init_dr6`). Found at seed 1, program 1842. Directed case `dr6-bit12`.
  Counterexample: [`directed/486sx-rtl-before-dr6-fix.json`](directed/486sx-rtl-before-dr6-fix.json),
  where the case stops at `@1006.dr6 ffff1ff0 ffff0ff0`.

Both fixes are under `I486`, so the 386 identity does not change. At
`I486 = 0`, `dr6_hi[0] && !I486` was `dr6_hi[0]`, as now, and `SEQ_XADDM`
is inside `if (I486)`.

### #AC on a CPL-3 delivery's pushes (Deano, 21 November 2026)

At CPL 3 with AM and AC set, a delivery that stays at CPL 3 pushes onto
the current stack, which may be misaligned. Slice 2's case
`ac-delivery-same-privilege` read as "neither side takes #AC". Its row held
only the first exception, #UD. In fact both sides take #AC there:
- the RTL, whose same-privilege pushes are ordinary CPL-3 stack accesses;
- Bochs, whose stack writes check alignment at CPL 3.

The random programs reach this through a gate whose selector lands on the
DPL-3 code descriptor. When gate 17 is also to CPL 3, #AC's own delivery
raises #AC again without end:
- the RTL loops in the delivery and times out;
- Bochs recurses through `exception()` until the testbench's stack overflows
  (seed 1, program 1309: a SIGSEGV).

The first full run stopped before every such delivery, which also hid the
deliveries that end. Asked which behaviour the 486SX should have, Deano chose
(21 November 2026, relayed by the coordinator: "A: yes, like hardware") that
it takes #AC there, as hardware does.

The question rested on two premises the work then disproved:
- that slice 2 had chosen no #AC there;
- that Bochs checks only on its fast stack path. Its slow path
  (`access.cc`) checks at CPL 3 too.

So the relayed instructions to change the RTL and to mask a Bochs
difference had nothing to act on. CVE-2015-5307 is a ring-3 #AC handler on an unaligned stack
looping in microcode on Intel and AMD parts. No 486 document read settles
it. So:
- **The RTL is unchanged.** It already behaves so.
- **`ac-delivery-same-privilege`** now delivers by INT 6, whose first
  exception is the #AC (error code 0). It agrees on the oracle's self-check
  and in lockstep.
- **`ac-exception-delivery-same-privilege`** keeps the #UD form. Its #AC
  handler now pops the error code into EAX (below).

### #AC's error code inside a delivery (Deano, 21 November 2026)

An #AC raised in another event's delivery pushed error code 1 on both
sides: EXT set, as for any fault in a delivery. The PRM's Table 9-8 says
#AC's error code is always zero. Asked, Deano chose 0: "Push 0 now". So:
- **The RTL** sets no EXT for an #AC under `I486` (`wb_err` in
  `fes_i386_cpu.sv`). At `I486 = 0` the term folds back to the 386's.
- **The #AC error code rule** (`ac-error-zero`, level 4): where the oracle's
  last event is an #AC with error code 1, the RTL's error code and the bytes
  it pushed must be 0; the oracle then takes them.
- **`ac-exception-delivery-same-privilege`**'s #AC handler pops the error
  code into EAX. The row holds Bochs's 1; the lockstep holds the RTL to 0.
  Without the RTL change the case stops at `@104d.ac-error 0 1`
  ([`directed/486sx-rtl-before-ac-error-fix.json`](directed/486sx-rtl-before-ac-error-fix.json)).

The rule did not fire in the random programs, which reached no #AC inside
a delivery that ends; the directed case covers it.
- **The stop now covers the loop alone** (`ac-delivery-loop`). It needs all of:
  - the RTL timed out in #AC's delivery;
  - the step began at CPL 3, with AM and AC set, on a stack that was not
    dword-aligned;
  - gate 17, read from the IDT, is an interrupt or trap gate to a conforming
    or DPL-3 code segment.

  The run stops before the oracle steps. An RTL that hung delivering #AC to
  a CPL-0 handler still fails.
- **The generator's ring-3 alignment case** misaligns the stack a fourth of
  the time: DEC ESP, PUSH EAX, POP EAX, INC ESP.
- **`ac-in-delivery`** counts the #AC raised inside a delivery that ends. It
  is 0 in the final runs, so only the two directed cases cover that path.
  A loop stop returns before the oracle steps, so it is not counted there.

### Differences of the oracle's model

| Rule | Retirements | What it does | Source |
| --- | --- | --- | --- |
| CPUID (`cpuid-486sx`) | 23,152 | At a CPUID of a leaf other than 0 that retires with no event, the oracle's EAX, EBX, ECX and EDX must be the DX4's leaf 1 (`0480h`, 0, 0, 1); they are then set to the 486SX's (leaf 1 `042Bh` and zeros, zeros above), which the RTL is held to | `cpu/cpudb/intel/i486dx4.cc`; `042Bh` is Deano's (21 November 2026); data book Appendix B for zeros above leaf 1 |
| DR6 of disabled breakpoints (`dr6-disabled-b`) | 2 | At a #DB, DR6's B0-B3 for breakpoints DR7 does not enable come from the RTL; an enabled one's is compared | Intel SDM vol. 3, DR6: a B bit "may or may not be set if the breakpoint is not enabled by the Ln or the Gn flags" |
| #AC's error code (`ac-error-zero`) | 0 (the directed case only) | Above: where Bochs pushes 1 for an #AC in a delivery, the RTL must push 0, and the oracle takes it | PRM Table 9-8 ("always zero"); Deano, 21 November 2026 |
| The #AC delivery loop (`ac-delivery-loop`) | 6 stops | Above: the run stops where the RTL timed out delivering #AC and gate 17 leads to CPL 3 | Deano, 21 November 2026; CVE-2015-5307 |

`method.md` named only the CPUID rule before the run. The DR6 rule was
added when the debug set's 6 failures in the first full run showed it: Bochs keeps a disabled
breakpoint's match pending into a later #DB. The SDM leaves that open, and
the rule takes only those bits from the RTL.

### Level-3 rules applied unchanged

The level-3 rules and `divergences.toml` apply at level 4 as they are.
Where a 486's answer is not documented, the 486SX keeps the 386's. The plan
leaves those behaviours as the 386's (plan open question 6).

These fired at level 4 in the RTL run:

| Rule | Retirements or stops |
| --- | --- |
| The vectors' undefined flags (`divergences.toml` `[[vectors]]`) | `shift-rotate-of` 142,164, `shift-af` 135,074, `imul-flags` 118,300, `bit-test-of` 66,502, `aad-flags` 45,689, `div-flags` 40,451, `daa-das-of` 39,748, `aaa-aas-flags` 38,156, `bsf-bsr-flags` 35,652, `mul-flags` 35,046, `double-shift-wide-count` 26,755, `shift-cf-wide-count` 22,635, `aam-zero-flags` 28 |
| Other `[[vectors]]` entries | `popad-esp-high-word` 6,181, `interrupt-vector-before-push` 125, `stack-wrap-partial-transfer` 108, `invalid-sib-scales-base` 72 |
| `dr6-sticky` (`dr6_rule`) | 28,296 |
| The CS-limit rule (`cs_rule`) | 888 |
| The IRET save rule (`iret_save_rule`) | 110 |
| The V86 IOPL rule (`v86-iopl-gp`) | 86 |
| The #SS error rule (`ss_error_rule`) | 1 |

These carried-over rules counted 0 at level 4, so this run does not test
them on the 486 identity: `db-frame-rf` (`db_rf_rule`), the first-fetch
rule (`fetch_rule`), `icebp-gd`, `umov` and `mov-tr`. The directed cases
cover MOV TRn at level 4.

`dr6-sticky` follows the 80386 PRM 12.2.3 ("the bits of DR6 are never
cleared by the processor"). The Intel SDM keeps that sentence for later
parts, apart from "certain debug exceptions may clear bits 0-3". The Intel486
manual's own text was not found.

The 125 `interrupt-vector-before-push` retirements include a stop added
here. Seed 10, program 150 of the random set failed with `nested 8` in a
full run at `f3a50e5a7`. In real mode a #GP's frame lay over vector 8's
entry, and that #GP's delivery faulted:
- Bochs pushes before it reads a vector, so it read the #DF's vector from
  the pushed FLAGS and CS (it ended at `FFFF:0002`);
- the RTL, as the 386EX, reads first and writes nothing of a delivery that
  faults, so the #DF's own fetch faulted and it shut down.

That is the same entry, one delivery deeper. The stop applies only in the
branch that failed: the oracle delivered a nested event the RTL did not,
and the step's frame overlaps that event's entry. So no level-3 run that
passed can change, and the level-3 reports above equal slice 2's, so it
never fired there. The new `ac_ring3` choices only reshuffled the programs
until one reached it. Replays of that program before and after the stop are
[`lockstep/seed10-150-before.txt`](lockstep/seed10-150-before.txt) and
[`lockstep/seed10-150-after.txt`](lockstep/seed10-150-after.txt).

## Governor bench at `I486 = 1`

`test_fes_i386_governor.py --i486` builds `I486 = 1`. Every loop's charge
equals the table's clocks (`charged` = `table_clocks` in every row). Each cell
gives the clocks charged and the pacing error against the table's cycles;
"beyond" marks a preset faster than the CPU at max on that loop:

| Preset | bswap | xadd | cmpxchg | cpuid | cache | invlpg |
| --- | --- | --- | --- | --- | --- | --- |
| max (cycles) | 18,039 | 36,069 | 84,029 | 6,039 | 2,039 | 25,029 |
| 286-12 | 42,009 (-0.09%) | 44,009 (+0.02%) | 138,009 (+0.01%) | 19,009 (+0.05%) | 9,509 (+0.11%) | 34,009 (+0.03%) |
| 386SX-16 | 42,019 (-0.09%) | 44,019 (+0.05%) | 136,019 (+0.02%) | 19,019 (+0.11%) | 9,519 (+0.21%) | 34,019 (+0.06%) |
| 386DX-25 | 42,025 (-0.12%) | 40,025 (+0.07%) | 96,025 (+0.03%) | 19,025 (+0.13%) | 9,525 (+0.26%) | 34,025 (+0.08%) |
| 386DX-33 | 42,031 (-0.12%) | 40,031 (+0.07%) | 96,031 (+0.03%) | 19,031 (+0.16%) | 9,531 (+0.29%) | 34,031 (+0.09%) |
| 386DX-40 | 42,036 (-0.12%) | 40,036 (+0.09%) | 96,036 (+0.04%) | 19,036 (+0.17%) | 9,536 (+0.33%) | 34,036 (+0.11%) |
| 486DX-33 | 24,038 (-0.20%) | 28,038 (+0.12%) | 76,038 (+0.04%) | 15,038 (+0.24%) | 6,538 (+0.46%) | 28,038 (+0.14%) |
| 486DX2-66 | 24,062 (-0.17%) | 28,062 (+39.89%, beyond) | 76,062 (+20.01%, beyond) | 15,062 (+0.09%) | 6,562 (+0.69%) | 28,062 (+0.23%, beyond) |

The 486DX2-66 preset is faster than the RTL on xadd, cmpxchg and invlpg, as
it is on three of the 386 loops already (slice 2's governor record). Every
other cell is within 0.7% of the table.

## The 386 identity

The 386 identity's behaviour is unchanged:
- **Level-3 lockstep.** At `ad328d78c` the RTL report equals main
  `0fb3ce80c`'s, run the same way on a clean worktree of main. The seal's
  netlist lockstep equals main's netlist report. Only source, suite,
  oracle-build and netlist digests differ. At `98cbd9927`, before the merge,
  both reports equalled slice 2's head reports (`bf7afef73`), the baseline
  `method.md` set. Main's x87 slice 6.3 (#2059) changes the x87 sections
  against slice 2's, so main is the baseline at the merged head.
- **Governor bench at `I486 = 0`.** Equal to slice 2's in all 1,178 values.
- **test386.** The oracle report equals slice 2's in all 707 values. That
  run steps Bochs alone, and slice 2's report is that kind. The RTL run
  (`98cbd9927`) is held to the 12 November `2026-11-12-i386-ud-1701` RTL
  report instead; slice 2 kept none. It equals it but for the `text` field,
  which later runners add, and the Verilator record.

Before the merge, slice 2's `netlist/elaborate.py` with `proc; opt -fast;
stat -width` showed the identity-off netlist at `98cbd9927` equal to slice
2's in every cell type and width ([`netlist/stat-h0.txt`](netlist/stat-h0.txt):
40,791 cells, 29,548 flip-flop bits), though its digest moved. The reseal
below supersedes that check for the merged head.

## The DE10 seal

`lockstep_tb.cpp` is in the oracle suite's sources (`SUITE_SOURCES`), which
pc386's build record hashes, and `fes_i386_seq.sv` and `fes_i386_cpu.sv` feed
the DE10 386 bitstream (their changes under `I486`, which every package
builds as 0). So pc386's `BUILD_ID` moved, and Deano asked (21 November
2026) for a reseal and a kit run at the final head.

The registered producer, `build_pc386_product.py --with-lockstep --seed 1
--gpu-router HIP`, ran on Powerboat from a clean detached worktree of
`ad328d78c`:
- under the Lite lease slot `/tmp/fes-quartus-lite-1.lock`;
- pinned to 4 CPUs with `taskset` for this session's cap;
- `--gpu-router HIP` only picks the authenticated toolchain slot.

It ran from 00:52 to 02:03 EET on 22 November:
- **The full netlist lockstep passed.** It is the level-3 netlist row above.
- **Quartus 17.0.2 compiled with 0 errors and passed every timing analysis**
  at 61.44 MHz system, 74.27 MHz pixel and 12.29 MHz audio. Worst setup
  slack +0.339 ns, hold +0.083 ns, recovery +7.213 ns, removal +0.509 ns,
  minimum pulse width +1.122 ns.
- **Resources:** 36,435 of 41,910 ALMs (slice 2's seal: 36,354), 31,903
  registers, 282 RAM blocks, 41 DSP blocks.
- **Identities:** BUILD_ID `766bd82aaeed4898149883a6ccc6a012`, package
  `b3cd67c0df13b47768f26bfb192dc73308bfe02bb5cacb9bff27cb826cb22ea7`, RBF
  SHA-256 `ce45a49e…`.

`verify_pc386_product_seal.py` passes against `ad328d78c` (`SEAL_OK`, 154
bound files). [`head_matches_seal.py`](head_matches_seal.py) shows a
checkout builds the sealed design, for later record-only commits. The seal's
`build-summary.json`, `build-inputs.json` and lockstep report are in
[`seal/`](seal/).

## The kit run

Kit B, run `k2087a`, 22 November 02:46 to 02:48 EET. It used slice 2's
core-only runner (`hil_s4_dos.py`, with its claim, baseline check and
restore wrapper):
- **Claim.** The wrapper claimed the kit in `kitB-STATUS.md` only after
  finding its last line FREE, the kit on that line's baseline (image
  `179affbb…`, bundle `c9ae79e2…`), the agent idle and the lease free.
- **What ran.** The package was packed as a canonical `.fcore`. The packer
  reproduces slice 2's `k2044b` archive byte for byte; the package ID was
  checked after packing. It ran on the baseline with no install and no
  reboot, booting the user-supplied FreeDOS drive `a6977555…` as a fresh
  library game.
- **Result** ([`kit/kit-b-k2087a-result.json`](kit/kit-b-k2087a-result.json)):
  - the FreeDOS prompt was detected in 0.6 s;
  - Wolfenstein 3D reached floor 1 with graphics on screen (0.938 of the
    frame lit), and 0.562 of the frame changed after a walk;
  - no load error.
- **Restore.** The kit is on the same image and bundle with the same boot
  ID, the agent idle, the lease free, and none of the run's FogCast data
  left. The run is logged IN USE and then FREE in `kitB-STATUS.md`.

## The merge of main

Slice 2 ([#2045](https://github.com/DeanoC/fes/pull/2045)) landed as
`f07cd3384` during the first runs, so this branch merged main (`cdbdccd7b`,
no rebase). The merged tree is main plus this branch's changes exactly.
Main also brought x87 slice 6.3
([#2059](https://github.com/DeanoC/fes/pull/2059), `57bdbb31f`). The #AC
error-code change followed (`ad328d78c`), and every run in the table ran
again there except test386 on the RTL. The level-4 runner's unit tests
(`tests/test_fes_i486_oracle.py`, with the governor, lockstep,
governor-inventory and lockstep-seal tests) pass. Two of the runner's tests
described the old behaviour and now describe the new: the level-4 oracle
runs the `random` mode too, and the 486SX list has no stops.

## What this does not establish

- **A real 486SX's answers.** The oracle is Bochs's DX4. The random
  programs show the RTL keeps step with it, under the rules above, not with
  a 486.
- **Undefined flags and the other 386 rules on a 486.** They are carried
  over from the 386, as listed above.
- **#AC inside a delivery that ends, at random.** The directed cases cover
  it; the random programs did not reach it.
- **The x87 empty socket at `I486 = 1`.** The random programs run no ESC.
- **The 486SX's timing or fit.** The seal is of the 386 package
  (`I486 = 0`); slice 2's 486SX fit stands.
- **test386 on the RTL at the merged head.** It ran at `98cbd9927`.
- **Games beyond the kit run's FreeDOS and Wolfenstein 3D** on this seal.
