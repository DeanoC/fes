# The first i486DX on the DE25 (slice 1): method, committed before any fit or sim

Tracker [#2135](https://github.com/DeanoC/fes/issues/2135); plan
`docs/validation/2026-11-22-i486dx-core-plan/summary.md` (#2146). Deano,
24 Nov 2026 (relayed): "agreed, start the first DX slice with cuts".

This file was committed before any fit, lockstep run, bench or CPI window of
this slice ran on the DX. The one run before it is disclosed under
[What ran before this method](#what-ran-before-this-method). Later changes
are dated amendments below, each saying what had been seen by then. No pass
rule is changed after a result exists; a gate that cannot be met is recorded
as "fail" or "not measurable".

## The design under test

The DE25 pc386 machine as an i486DX, on branch `deano/i486dx-s1-623202`,
stacked on #2171 (`deano/de25-486sx`, base `2d4edf547`):

- **The 486 identity:** `pc386_system` `I486 = 1` (#2171).
- **The 387 on:** `pc386_system` `X87_PRESENT = 1`.
- **Round 2's `cuts`** (PR #2149), and nothing else round 2 measured:
  - L1I and L1D at **8 KiB, 2-way** (128 sets of 32-byte lines, as before):
    `boards/de25nano/pc386/i486dx/fes_i486dx_l1i.sv` and `fes_i486dx_l1d.sv`,
    forks of `fes_i386_l1i.sv` and `fes_i386_l1d.sv` at `499c3c3ea` with the
    way count halved. Round 2's ablations held ways 2–3 invalid; these forks
    remove them.
  - **Load forwarding off:** `d_out.ldfwd = 1'b0`.
  - Not `xshift`, `xdisp` or `qhalf`.
- **The structure (decision 1):** `boards/de25nano/pc386/i486dx/derive.py`
  derives `fes_i486dx_cpu` from `fes_i386_cpu.sv` with anchored edits (the
  module name, the two L1 instances, `ldfwd`), and pins the upstream bytes of
  each fork. `boards/de25nano/pc386/derive.py` retargets `pc386_system`'s CPU
  instance. The DE25 producer (`boards/de25nano/pc386/build.py`, model
  `486dx`) swaps the DX's files into its own source list. No shared RTL
  changes, and `scripts/yosys_netlist.py` and `scripts/pc386_quartus.py` are
  untouched.
- **Benches** (as amended, amendment 1): the DX runs on the shared benches
  in its benches' form, `derive_cpu(rename=False)`: every edit but the top's
  name, so the benches read its hierarchy as the integer core's.
  `boards/de25nano/pc386/i486dx/lockstep.py level3|level4` runs the shared
  lockstep runners with the DX's CPU sources, and `sim_fes_pc386.py
  --cpu-core i486dx` builds `machine_tb` with them. No shared bench or
  lockstep file changes.

**The 387-on base** is the shared CPU, `fes_i386_cpu`, at this branch's head,
on the same bench with the same settings, run in this slice. The DX and the
base differ only in the cuts.

## Gates and pass rules

### G1. DE10 identity

- **Netlist:** `docs/validation/2026-11-23-de25-486sx/de10_netlist.py` at
  `2d4edf547` (the base) and at the DX head, from one checkout path, with the
  pc386 lock's Yosys (`d273c1cb`) and `YOSYS_MAX_THREADS=1`. **Pass:** the two
  netlists are equal under `cmp`.
- **Identities:** `de10_design_id.py` at both. **Pass:** the design
  (`BUILD_ID`), record and toolchain identities are equal. The evidence
  identity may move: the lockstep scripts and benches above are pc386
  design-v2 evidence (plan, "What stays shared"). Its two values are reported.

### G2. Lockstep and the x87 suites

Each run is made twice, on the base (the shared runner) and on the DX (the
same runner through `i486dx/lockstep.py`), from one checkout of the DX head, with the toolchain
cache's locked Verilator:

1. **Level 3 with the x87 suites:** `scripts/test_fes_i386_lockstep.py
   --download --random-programs 500 --random-seeds 8 --limit-per-file 20`,
   round 2's subset, at its default `I486 = 0`. It includes the directed,
   unreal, protected-mode, paging, no-387, x87 (the i386dx+387 identity on),
   BCD and transcendental cases, the built-form comparison and the random,
   PM, task, debug and undefined programs.
2. **Level 4:** `scripts/test_fes_i486_lockstep.py --download --identity
   486sx` at its defaults: the 148 directed cases and the random suite at
   `I486 = 1`, the DX's identity.
3. **The x87 unit benches:** `scripts/test_fes_i386_x87_unit.py --download`,
   `scripts/test_fes_i386_x87_testfloat.py --download` and
   `scripts/test_fes_x87_trans_engine.py` at their defaults. They Verilate the
   x87 unit alone, whose files the DX shares byte for byte, so they run once;
   the record gives each x87 file's SHA-256 at the head to show it.

**Pass:** for 1 and 2, the DX's report has no failure the base's report on
the same settings does not have (failures compared as the report's failure
lines, with the CPU's own counters, such as clocks, left out). For 3, each
bench passes, or has only failures its last recorded run on main has.

### G3. DE25 machine fit: device plus four-seed timing (decision 7)

As `2026-11-23-de25-486sx/method.md`, with the DX:

- **Seed 1** is the registered producer,
  `boards/de25nano/producers/build_pc386_package.py`, model `486dx`, with its
  gates (package timing admission, the reviewed signoff findings).
- **Seeds 2, 3, 4** are copies of seed 1's generated project with only
  `SEED` changed, compiled with `quartus_sh --flow compile`; each copy's
  `fes_mailbox/pc386_system.v` must have seed 1's SHA-256.
- Quartus Pro 26.3, from a clean worktree of this branch's pushed commit, the
  pc386 lock's Yosys, `YOSYS_MAX_THREADS=1`. Every Quartus run holds a Pro
  slot of the Quartus lease (`scripts/quartus_jobs.py`); `uptime` is checked
  first; output on `/data`.

**Pass, all of:**
- every seed fits the device (A5EB013, 46,800 ALMs) and compiles;
- every slack is ≥ 0 for every analysis (setup, hold, recovery, removal,
  minimum pulse width, and the skew, net-delay and data-delay checks) at
  every corner and clock, the machine clock at 61.437908 MHz;
- seed 1's producer admits the package on its own gates.

**The shell under the fit.** This branch carries neither shell cut the
plan's budget counts, S-lat (#2177, 1,047 ALMs) or A-dma2 (#2178, 284.5):
both are drafts stacked on #2036. Whether to merge them in before the fit
was asked of the coordinator on 24 Nov 2026. If they are merged, a dated
amendment says so **before the fit starts**, and the fit is of the merged
head. The first-order expectation without them is about 47,440 ALMs (#2171's
41,563 + the x87's about 7,400 + `cuts`' about −1,520), over the device, so
this gate is expected to fail without them; with them, about 46,110. These
are expectations only, and no rule depends on them.

A seed that misses is a fail; three of four is a fail. A producer refusal,
including a reviewed signoff basis that no longer matches the DX netlist,
is a fail with its reason, and stops the run. No constraint, basis or check
changes in this slice. **The 92% line is not a gate** (decision 7); the median
is reported against it.

**Reported:** ALMs needed per seed and the median; RAM and DSP blocks; for
each seed, the worst slack of each analysis, with its clock and corner, and
the worst setup and hold slack at each corner. The median is compared, as a
report and not a band, with #2171's 486SX median (41,563.4) and with round
2's first-order expectation.

### G4. CPI windows

Round 2's windows, unchanged (`2026-11-23-i486dx-cuts/method.md` C1, C2 and
amendment 1), on `machine_tb` built by `sim_fes_pc386.py --suite
cpu-profile`, base and DX (`--cpu-core i486dx`), at level 3 (`I486 = 0`),
with no x87 in the window (neither workload issues an x87 instruction):

- **DOOM:** v1.9 `-timedemo demo3`, the census `pmode` drive,
  `--rtl-at-flip 40 --rtl-for 1.0 --cpu-profile-every 0.5`; the primary
  window is h2.
- **3DBench:** the census `bench` drive, `T3D_KEYS='7.3:|'`, `--rtl-from
  7.5`, the window 8.0–11.5 s.

Both arms must hand over at the same cycle from the same oracle state;
otherwise the row is "not comparable". **Reported:** each CPI and the ratio
r = DX / base. **Pass (the plan's fixed bound):** DX-33 is kept on DOOM,
2,062 × r ≤ 4,682 realtics. DX2-66 (≤ 2,912) is reported, not gated (it is
the stretch). The ratios are reported beside round 2's `cuts` figures. The
bench charges port 0's first data 18 clocks against the kit's 11, so the L1
losses here are upper bounds.

### G5. Quake on the RTL

Round 2's bring-up (`/data/projects/i486dx-plan/r2/sims/quake-base/`,
main `dafabb6db` with `bench-x87.patch`) stopped with `STOP stall` at
`00a7:0005256c` on `DB E3` (FNINIT) and timed out.

1. **The question:** is the stall the scratch bench edit, which drove only
   `x87_present` into `machine_tb`'s RTL CPU and left IRQ 13 and ports
   F0h/F1h unwired, or a real FNINIT bug?
2. **Method:** reproduce on the base CPU with the x87 unit's issue state and
   the last retirements printed at the stall, then reproduce the cause in
   the lockstep or the x87 unit bench. A stall that reproduces in a lockstep
   case against the oracle is real; one that needs the bench's unwired IRQ 13
   or F0h/F1h is the bench's.
3. **If real:** a lockstep or x87-suite case that fails before the fix and
   passes after it, and the fix following the 386/387's documented behaviour
   where the RTL and Bochs differ (387 PRM, 386 HRM). The fix is in the shared
   x87 unit, which moves the DE10 netlist, so it goes in its own PR against
   main, not in this PR, whose G1 covers this PR's diff. Quake is then run
   past the stall on a scratch merge of that fix.
4. **If the bench's:** `machine_tb` wires the RTL CPU's x87 to the machine as
   `pc386_system` does (`x87_present`, F0h/F1h in, IRQ 13 out), a sim-only
   change that stays in this PR.
5. **Pass:** Quake (census `bench` drive, `quakes`, round 2's command line
   and `--rtl-at-prompt 1`) runs on the RTL past `00a7:0005256c` with no CPU
   stop through to its first frame of the demo. If that cannot be reached in
   this slice, G5 is recorded as "not met" with what was found.

## What this does not establish

- **The CPUID and reset signature are the 486SX's** (#1962 slice 2). The DX
  identity (i486 plan slice 4) is not in this slice.
- **The 387's record word and FogCast setting** (x87 slice 7) are not here:
  `X87_PRESENT` is a build parameter, and no kit runs.
- **No x87 instruction runs at `I486 = 1` in lockstep:** the level-4 suite
  carries no x87 cases. The x87 cases run at level 3.
- **No D12 proof, no kit run, no seal.** The fits are the producer's and its
  copies; the DE10 is unchanged by G1.

## What ran before this method

On 24 Nov 2026, before this file was committed, a diagnostic run of round 2's
Quake bring-up was started on Powerboat for G5 (`/data/projects/i486dx-s1/
qdiag/`): `2d4edf547`, the base CPU (not the DX), with scratch edits to
`machine_tb.cpp` (round 2's `x87_present` line, and a print of the x87
unit's issue state and the last 64 retirements at a stall) and to
`cpu_profile.vlt` (the unit's state made readable). Its first attempt failed
to compile and was fixed and restarted. It measures no gate; its output had
not been read when this file was committed.

## Amendments

1. **24 Nov 2026, before any G2–G5 run on the DX.** Seen by then: G1's first
   run, at `cc136b1a6` (this method's commit). Its two netlist steps failed
   before Yosys ran (`pc386-rom.hex` missing: the worktree had no firmware
   build), so there is no netlist result. Its identities: at `2d4edf547` and
   at `cc136b1a6` the design (`426e518d…`) and toolchain (`9e5d2b2e…`) are
   equal; the evidence (`243554b4…` → `de6db338…`) and the record
   (`4075e47b…` → `283a0708…`) differ. The record digest is the SHA-256 of
   the whole record, every source input included (`input_identity.py`
   `identities`), so any evidence change moves it. The cause was this
   commit's edits to the lockstep runners, `lockstep_tb.cpp` and
   `cpu_profile.h`.
   - **The change:** those edits are removed. The benches take the DX in its
     benches' form through `i486dx/lockstep.py` and `sim_fes_pc386.py
     --cpu-core i486dx` (see "The design under test"), so no evidence file
     changes.
   - **G1 is unchanged** and applies to the head the record reports on. The
     result at `cc136b1a6` is kept in the record beside it.
   - **G1's netlist step** builds the firmware in the checkout first
     (`scripts/build_pc386_firmware.py --download`), which the sealer
     requires.
   - **G2.3, TestFloat:** `test_fes_i386_x87_testfloat.py` has no
     `--download`; it takes `--testfloat-gen`, a TestFloat build at its
     pinned revision. It runs with such a build made for this slice; if none
     can be made, its row reads "not run" with the reason. The other two
     benches are as written.

2. **24 Nov 2026, before the fit (G3) started.** Seen by then: G1 at
   `63aa526c8` (pass), and no G2–G5 result. The coordinator answered the
   question G3 names: the approved DX configuration includes both shell
   cuts, so both are merged before the fit, with plain merges:
   - A-dma2, #2178, `deano/de25-pc386-dma2-regfile` at `7147b7d05`
     (merge `0a357fc62`);
   - S-lat, #2177, `deano/de25-shell-lat` at `642e22f50` (merge
     `7f9a8954e`); its head may still move while its D12 reruns finish, and
     later heads are merged in as they move.
   - **This PR depends on #2036, #2171, #2177 and #2178 landing.**
   - **The first-order sum before the fit:** 41,563 (#2171's median) + about
     7,400 (the x87) − about 1,520 (`cuts`) − 1,047 (S-lat) − 284.5 (A-dma2)
     ≈ **46,110 ALMs, 98.5% of the device**. An expectation only: G3's rule
     is unchanged, and a seed that misses the device or timing is a fail.
     No cut is added to rescue it.
   - G3's fit is of the merged head. G2 and G4 run at `63aa526c8`: neither
     merge changes a CPU file, `machine_tb` or a lockstep source, so the
     CPU they test is the merged head's; G1 is rerun at the head the record
     reports on.
   - **TestFloat (G2.3):** the TestFloat build used is named in the record
     with its revision.
