# i486 plan: method for the level-4 oracle runs, 17 November 2026

This file is committed before any of the runs it describes has a result. The
results go in [`summary.md`](summary.md) and `census.jsonl`. Nothing here
changes RTL, seals a build or uses a kit.

## Questions

1. What breaks when the Bochs oracle is configured at CPU level 4 instead of
   level 3: the build, the adapter, the lockstep suite, test386, the
   SingleStepTests vectors and the machine boot.
2. Which titles and system software in the existing drives execute a 486
   instruction, or test for a 486, when the CPU is one.

## Tree and patches

- Main at `e2b8130b5` in a scratch worktree on Powerboat
  (`~/fes-scratch/i486plan`), separate from this PR's worktree.
- Two scratch patches, not committed:
  - `scripts/fes_i386_oracle.py`: `--enable-cpu-level=3` becomes
    `--enable-cpu-level=4`. Nothing else in the configure line changes, so
    the oracle keeps its FPU. A second build with `--disable-fpu` is tried
    for question 1 only (does it build and boot; it is the nearest Bochs
    has to a 486SX).
  - `cores/fes-pc386/sim/census.h`: two keys, `ac:<stem>` and `id:<stem>`,
    counting a retired instruction that changes EFLAGS bit 18 or bit 21.
    A 386 cannot change either, so the level-3 census had no use for them.
- The oracle's Bochs is the pinned `30cfd566ecc8`. What "level 4" models is
  read from that source's `BX_CPU_LEVEL` conditionals and recorded, not
  inferred from the runs.

## Question 1: what breaks

Each is run unchanged against the level-4 oracle, and its result recorded as
it comes, pass or fail, with the first failures named:

- `sim_fes_pc386.py --suite boot` (FreeDOS on the oracle);
- `test_fes_i386_test386.py` (test386.asm on the oracle);
- `test_fes_i386_vectors.py` (SingleStepTests/80386), if its corpus is on
  the host;
- `test_fes_i386_lockstep.py` (the RTL against the oracle). The RTL is a
  386, so differences are expected here; the run lists which cases differ
  and why, as the size of the lockstep rule change.

A failure is a finding, not something to fix in this record.

## Question 2: the census

- **Pilot, run first.** Duke Nukem 3D on the `pmode` drive, 50 simulated
  seconds, the launch and keys of its drive record. At level 3 it takes
  exception 6 at 0180:00222BE2 after 21.45 s
  ([Phase 5 census](../2026-11-12-pc386-census-p5/summary.md#duke-nukem-3d)),
  and that record never named the opcode. The pilot names the stem at that
  address and says whether the title goes on past it.
- **Sweep.** `sim_fes_pc386.py --suite game --cpu oracle --x87-fitted
  --instruction-census` for every title in the store's drives (`real`,
  `xms`, `pmode`, `dpmi`, `fpu`, `bench`), each with its drive record's
  launch, keys and boot entry, 20 simulated seconds (50 for Duke Nukem 3D,
  25 for Quake, 30 for the `fpu` and `dpmi` buckets, as the x87 plan ran
  them). WfW 3.11 on `wfw-msdos` and the three Linux drives run if their
  suites take the census flag; if not, the record says they were not run.
  The 387 is fitted so that no title stops at its first ESC before it gets
  to its CPU test.
- **What is counted,** per run, from `census.txt`:
  - the 486 stems: BSWAP (`0FC8`–`0FCF`), XADD (`0FC0`, `0FC1`), CMPXCHG
    (`0FB0`, `0FB1`), INVD (`0F08`), WBINVD (`0F09`), INVLPG (`0F01` /7),
    CPUID (`0FA2`), each with its mode suffix;
  - `ac:` and `id:` (an EFLAGS.AC or ID toggle that took effect);
  - MOV to CR0 that sets NE, WP, AM, CD or NW, where the census shows it;
  - exception 17 and exception 16 events.
- **Classification, fixed now.**
  - **Requires a 486:** the title stops on a 386 at a 486 instruction. The
    evidence for that is a level-3 stop already in a committed record, plus
    this sweep showing the instruction and the title going on. A level-4
    run alone cannot show it.
  - **Uses 486 instructions when it finds a 486:** 486 stems retired at
    level 4, and the title is recorded as running on the 386.
  - **Tests for a 486 only:** `ac:`, `id:` or CPUID counts, no other 486
    stem.
  - **Nothing:** none of the above in the window.
- **What the sweep cannot show.**
  - A 20- to 50-second launch window covers start-up and menus. A title
    that uses a 486 instruction only in play is undercounted.
  - The drives were assembled for a 386 core: titles were chosen because a
    386 might run them. A count of 486 users in this set is a count in a
    386 corpus and is not the demand for a 486. The record says so beside
    the count.
  - The oracle runs at the instruction level. It says nothing about speed,
    so "too slow on a 386" is not measured here.
  - Bochs's level 4 is its own model. Where it differs from the Intel486
    data sheets (CPUID on a chip that has none, for instance), a title's
    behaviour on it is behaviour on Bochs.

## What is committed

`census.jsonl`: one row per run, with the title's id, drive hash prefix,
run length, total steps, and the counts above by stem name. No game bytes,
frames, screen text or instruction bytes. Drives, frames, logs and the two
patches stay on the host.
