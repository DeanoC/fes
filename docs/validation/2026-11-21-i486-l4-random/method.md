# i486: random programs at level 4 and the governor bench at `I486 = 1` — method

Written before the first full run, 21 November 2026. Tracker
[#1962](https://github.com/DeanoC/fes/issues/1962). This work closes two gaps
that slice 2 ([PR #2045](https://github.com/DeanoC/fes/pull/2045), record
[`2026-11-21-i486-s2-isa`](../2026-11-21-i486-s2-isa/summary.md)) left open:
- random programs against the level-4 oracle;
- a governor bench at `I486 = 1`.

Everything runs as host simulation on Powerboat. No Quartus run, no kit, at
most 4 CPU threads at a time.

## What is built

- **A level-4 random mode.** `lockstep_tb.cpp`'s `random` mode now also runs
  against the level-4 oracle. It runs the level-3 suite's generator with
  `Generator::isa486` set and a 486's start state:
  - **Start state.** CR0 has ET set. CD and NW are both set, CD alone or
    neither, never NW without CD. AM is set three times in four; WP and NE
    are random. EFLAGS.AC and ID are random.
  - **486 forms, in place of an instruction:** BSWAP r32 and r16; XADD and
    CMPXCHG of each size, register and memory forms, with LOCK on memory
    forms now and then (rarely on a register form, which is #UD); INVD;
    WBINVD; INVLPG m; CPUID of leaf 0, 1, 2 or a random leaf; AC toggled by
    POPFD.
  - **Paging bursts:** INVLPG, and PTE remaps followed by the INVLPG of
    their page.
  - **Undefined encodings:** the generator's lists drop what a 486 runs.

  Every new choice is drawn from the generator's `side` stream, so with the
  flag off (level 3) every seed's programs are byte for byte as before.
- **The runner.** `scripts/test_fes_i486_lockstep.py` runs the level-3 suite's
  five random sets at the 486SX identity, with the same seeds and sizes as
  `test_fes_i386_lockstep.py`. It adds two seeds again without latency.
- **The governor bench.** `scripts/test_fes_i386_governor.py --i486` builds
  `I486 = 1` and runs the 386 bench unchanged. It then runs
  `governor_bench_486.asm`, loops of BSWAP, XADD, CMPXCHG, CPUID, INVD and
  WBINVD, and INVLPG, under max and every preset.

## Runs and pass rules

1. **Level-4 random programs, 486SX, RTL.** Defaults:
   - random: 16 seeds × 2,000 (32,000, as level 3), plus 2 seeds again
     without latency;
   - protected mode: 16 × 500;
   - tasks: 16 × 250;
   - debug: 16 × 250;
   - undefined: 16 × 250.

   **Pass:**
   - every program keeps step;
   - the latency-free traces equal the latency runs' traces;
   - each form in `REQUIRED_FORMS` retires at least once, including #AC
     delivered (`ac-fault`) and a CPL-0 write stopped by WP (`wp-fault`).
2. **The same on the netlist** (`--netlist`, the seal's Yosys program with
   `I486 = 1`). The same pass rule.
3. **The directed level-4 cases, both identities, RTL and netlist.** Each
   identity's stops must equal its committed list. A list may change only
   for a reason this record states.
4. **Governor bench at `I486 = 1`, RTL.**
   - Its report passes.
   - Its 386 part equals the slice-2 386 report
     (`2026-11-21-i486-s2-isa/governor/head.json.gz`) in every value
     except the Verilator and netlist identity fields.
   - Each 486 loop's charge equals `governor.toml`'s clocks (CPUID 12).
5. **The 386 identity is unchanged.** Each of these must equal the slice-2
   head's report (`bf7afef73`; that record shows it equals main's), apart
   from source, oracle-build and netlist digests:
   - the level-3 lockstep, on the RTL and on the netlist;
   - test386;
   - the governor bench at `I486 = 0`.

## How a divergence is classed

Every divergence is one of two things.

- **An RTL bug.** It is fixed in the RTL and gets a directed case in
  `fes_i486_cases.py`, whose row a 486's documents give.
- **An oracle-model difference.** Bochs's level 4 is not the 486SX there. It
  gets a narrow rule in `lockstep_tb.cpp` that applies at level 4 only. The
  rule:
  - names the instruction and the fields;
  - holds both sides to known values where a document or Deano's decision
    gives them;
  - cites its source;
  - counts its retirements in `divergence_retirements`.

  There are no broad skips: no opcode-class mask without fields, and no
  per-seed or per-program exclusions.

Accepted 486 defaults (Deano, 21 November 2026):
- CPUID `042Bh`;
- a 16-bit BSWAP zeroes the low word;
- a real-mode delivery clears AC;
- CPUID costs 12 clocks.

Where Bochs and real hardware differ on an edge, real hardware wins.

The one rule known before the run is CPUID's. Bochs is a DX4 and answers
every leaf but 0 with leaf 1, `0480h` and EDX 1. The rule checks those
values exactly on the oracle, then sets the 486SX's answer, which the RTL is
held to: leaf 1 `042Bh` and zeros, zeros above it. With the rule, the
directed `cpuid-1` and `cpuid-2` cases should agree at the 486SX identity, so
its expected list loses those two stops.

## What this does not establish

- **Identity of the 486 forms on a real 486SX.** The oracle is Bochs's DX4.
- **The x87 empty socket at `I486 = 1`.** Random programs run no ESC.
- **The pc386 `BUILD_ID`.** `lockstep_tb.cpp` is one of the files in the
  oracle suite's digest, which pc386's build record hashes. Changing it
  therefore moves the `BUILD_ID` with no RTL change, as it did in slice 0.
  This work runs no seal and no D12 comparison, and says so in the PR.
