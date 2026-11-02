# i386 Phase 2: the general CS-limit check, 2 November 2026

Issue [#1414](https://github.com/DeanoC/fes/issues/1414), part of the i386
design's Phase 2, after slice 2 ([#1406](https://github.com/DeanoC/fes/issues/1406),
PR #1411), whose DOOM census found DOS/4GW's 16-bit kernel code segments
under CS limits below FFFFh, where `fes_i386_cpu` stopped every instruction
as not built.

**Result:**
- `fes_i386_cpu` checks CS's limit under any limit, in 16- and 32-bit code,
  in protected and real mode, as Bochs 3.1 does. An instruction that starts
  or runs past the limit raises #GP(0) at its fetch. A near branch, call or
  return whose target is past it raises #GP(0) at the branch, only when the
  branch is taken. A real-mode far transfer's offset, and a real-mode INT's
  vector offset, are checked against the limit CS already has
  ([What changes](#what-changes)). `WHY_CS_LIMIT`, `cs_odd`, `wrap16` and
  the 16-bit-only special cases are gone.
- The RTL and the i386 lock's Yosys netlist give the same lockstep report
  (but for its `yosys` field) ([Lockstep](#lockstep)):
  - every corpus state passes (1,758,699), with every trace hash equal to
    slice 2's;
  - every protected-mode program passes (8,000);
  - every directed case passes, among them 38 new CS-limit cases, 37 of
    which fail on slice 2's RTL.

  The new generator block runs 23,199 instructions and raises 1,299 #GP
  deliveries under small CS limits. 6 of the 32,000 random programs fail:
  they fail the same way on slice 2's RTL, and the reshuffled random stream
  exposed them. Five are slice 2's protected-mode IRET reading its lower
  slots before the FLAGS slot's #SS
  ([#1424](https://github.com/DeanoC/fes/issues/1424)); one is a harness gap
  for a REP string that stores over its own bytes
  ([#1425](https://github.com/DeanoC/fes/issues/1425)). Deano chose to record
  these and carry on.
- The pc386 suites, the governor bench and test386 on the RTL:
  RESULTS ([Machine](#pc386-suites-governor-and-test386)).
- Quartus 17.0.2 seals `fes.pc386`: SEAL ([Seal](#quartus-seal)).
- DOOM's start on `machine_tb --cpu rtl`: DOOM
  ([DOOM](#dooms-start-on-the-rtl)).

## Inputs

- FES branch `deano/i386-cs-limit`, stacked on `deano/i386-phase2-idt`
  (PR #1411) at `7224b11f6`. `01bc050ef` (#1411's record only) is merged in
  as `53bef7644`.
- The RTL is `5d231b804`'s throughout; later commits change only docs and
  this record. The seal is of a clean checkout of `5d231b804`. The lockstep
  runs used `9adde623c` (`5d231b804` with the README, sim README and design
  doc), and so did the pc386 suites, the governor bench and test386.
- Oracle: Bochs 3.1 at CPU level 3 (the `fes-i386-oracle` build), corpus
  SingleStepTests/80386 `459d49fbe628`.
- Verilator 5.032 on Powerboat. The netlist lockstep used the i386 lock's
  Yosys `238eac9a4`. The seal used Quartus Prime Lite 17.0.2 with the
  netlist from the pc386 lock's Yosys `0a7927bf`.
- DOOM v1.9 under DOS/4GW, on slice 2's census disk (the games image,
  HIMEMX's full XMS, DOOM in `C:\DOOM`, SHA-256 `fa26f60b…`), a scratch copy
  only. No game bytes are in the repository.

## What changes

Bochs 3.1's rules (`cpu/cpu.cc` `prefetch`, `cpu/icache.cc`
`boundaryFetch`, `cpu/ctrl_xfer16.cc` and `ctrl_xfer32.cc`, `cpu/exception.cc`
`real_mode_int`), which the RTL follows:
- `prefetch` raises #GP(0) when EIP is past CS's limit, and narrows the
  fetch window to the limit, so an instruction whose bytes run past it
  faults in `boundaryFetch`. The fault restores EIP to the instruction's
  first byte. Nothing the decode found comes first.
- `branch_near16` and `branch_near32` (Jcc, JMP rel, LOOP, JCXZ), the near
  CALL, JMP and RET handlers check the new EIP, the 16-bit one wrapped to 16
  bits, against the limit: #GP(0) at the branch, only when it is taken. A
  CALL pushes first and a RET pops first (their #SS comes first), and the
  speculative ESP is put back.
- A real-mode far JMP or CALL checks its offset against the limit CS has
  before the transfer, ahead of its pushes; RETF and IRET after their pops.
  `real_mode_int` checks the vector's offset after its pushes, ESP put back.
- In 16-bit code EIP is not cut to 16 bits: an instruction that ends at
  FFFFh leaves EIP 10000h, which faults next under a 64 KiB limit and runs
  on under a larger one.

The [RTL README](../../../sources/misteross/cores/fes-common/rtl/i386/README.md)
has the detail. In `fes_i386_cpu`:

| Where | What |
| --- | --- |
| D | EIP plus the length against the limit plus one (`cs_gp`), split at bit 4 so it runs beside `d_eip_next`'s adder: the low nibble plus the length carries at most 1 into EIP's upper 28 bits, which are compared with the limit plus one's upper bits and those less 1, registered as CS is written (`cs_fl`). A delivery's entry, at length 0, passes at the EIP of an instruction that starts just past the limit. `d_eip_next` is EIP plus the length in 16-bit code too |
| D | A far transfer's offset (`far_chk`, CALL r/m near's push too) against the limit: at a far CALL's probe, ahead of the probe's own check; at any other uop (`cs_late`) after the uop's own checks, at M1 |
| AG | CALL rel's push: its target, the next EIP plus the displacement wrapped by operand size, beside the AGU's chains (`ag_call`) |
| M1 | A near branch's or CALL rel's push's target against the limit (`tchk`): #GP(0) here for the push, after its own checks; carried to EX for a branch |
| EX | #GP(0) for a taken branch whose target is past the limit; RET's and JMP r/m's from EX's operand; a real-mode INT's probe whose vector offset (T1, retired by then) is past it |
| Redirect | A delivery's D EIP is the faulting instruction's, no longer cut to 16 bits in real mode |

Deleted: `WHY_CS_LIMIT` (so `WHY_TASK_RETURN` is now 8), `cs_odd`,
`wrap16`, `past`, `eip16`, the 16-bit CALL rel32 target trick
(`call_target_*`), `seg_cs_ffff`, `seg_cs_small`, `seg_cs_flat` and their
staged `ds_*` copies, and the real-mode cut of a delivery's EIP. Nothing in
the change waits: no uop issues later than before.

## Lockstep

`scripts/test_fes_i386_lockstep.py` at `9adde623c`
([report](lockstep-report.json)), and with `--yosys` on the i386 lock's
Yosys `238eac9a4` ([report](lockstep-netlist-report.json)). The two reports
are identical but for the netlist's `yosys` field.

| Run | Result |
| --- | --- |
| Corpus (SingleStepTests/80386, 941 files) | 1,758,699 pass, 0 fail; 3,517,401 retirements compared; 147,537 in an exception's handler |
| Random programs (16 seeds × 2,000) | 31,994 pass, 6 fail (pre-existing, below); 8,204,246 retirements; 3,020 end at a shutdown, 707 at a fault in a delivery delivered |
| Protected-mode programs (16 seeds × 500) | 8,000 pass, 0 fail; 464,949 retirements, 374,688 of them (with the random programs') with PE set; 14,235 deliveries through the IDT; 1,483 shutdowns; 165 not-built stops |
| Unbuilt encodings | 52,320 outside SCOPE, all stopped as not built |
| Directed cases | 40 pass |
| Unreal-mode cases | 6 pass |
| Protected-mode cases | 85 pass (38 new) |
| Memory cases (DMA, FLUSH) | 26 pass |

The latency-free reruns of seeds 1 and 2 retire the same trace.

**The new directed cases** (`--mode pmode`, 38 of the 85). Each far-jumps
to code it places under a code descriptor of a small limit:
- 16-bit and 32-bit code (78h, 80h, limit 0Fh):
  - running off the end, an instruction across the limit, a HLT at the
    last byte;
  - JMP to the last byte and past it, a Jcc not taken and taken to past it,
    LOOP, CALL rel16 and rel32 (66h), CALL and JMP r/m, RET and RET imm16
    to the last byte and past it, JMP rel16 (66h) in 32-bit code;
  - the far jump's own offset past the limit.
- Page-granular limits (88h 32-bit, 90h 16-bit, limit 1FFFh): off the end,
  the last byte, a 66h prefix as the last byte.
- 16-bit code under a 4 GiB limit (base FFFF1C08h) running from offset
  FFF8h on past FFFFh to a HLT at 10008h.
- Real mode under the limit CS kept (1Fh), after LIDT of the real-mode
  table and PE clear: off the end, INT n past the limit (#GP inside its
  delivery) and under it, JMP rel8, and far JMP, CALL, RETF and IRET past
  it.

Each ending is checked (the #GP's handler, error code 0 in protected mode,
or the HLT), and every retirement against Bochs. With slice 2's RTL 37 of
the 38 fail (stops as not built, or #GP at the wrong place); the 38th, the
far jump's own offset, was already built.

**The random protected-mode programs** gain two code descriptors at the
program's CS base in place of two of the GDT's random entries: 22 (B0h),
16-bit, and 23 (B8h), 32-bit, each with a small limit. A new block
(`pm_limit`, a 1-in-18 pick among a protected-mode block's items)
far-jumps to one of them. The program's first such jump sets that
descriptor's limit up to 1K past its target, mostly a few bytes. Then come
instructions, near jumps and Jcc (rel8, rel16 and rel32, 66h at random),
LOOP, JCXZ, JMP through a register, and CALL rel or through a register to
a subroutine that returns with RET or RET imm16, each forward over a few
instructions, any of which may lie past the limit. The block ends with a
far jump back to 30h. 16-bit code may leave the jump out, so the rest of
the block, and its exit to real mode, run under the small limit. The
32-bit code has its own encodings (NOP, INC, DEC, MOV, ADD, CMP, XOR,
branches), not the 16-bit generator's.

A new tally (`cs_limit`) counts what ran under a CS limit other than FFFFh
and FFFFFFFFh: 23,199 retirements and 1,299 #GP deliveries with PE set, and
223 retirements in real mode. The always-accept rule for not-built reason 8
(`WHY_CS_LIMIT`) is gone; reason 8 is now an IRET with NT set.

**Six random programs fail, before and after this change.** Each reproduces
identically on slice 2's RTL with this branch's testbench, which generates
the same programs. None is about CS's limit:
- `random#4#691`, `#5#910`, `#7#1161`, `#14#399` and `#15#234`
  ([#1424](https://github.com/DeanoC/fes/issues/1424)) share one cause. An
  injected interrupt arrives with SP near 0 on a 16-bit stack, so its frame
  wraps to SP FFFCh or FFFEh. The handler's IRET or IRETD then faults #SS(0)
  on its FLAGS slot at 10000h on both sides. Bochs's `iret_protected` reads
  FLAGS first; the RTL reads the EIP and CS slots first. Those reads land
  in the MMIO window and Bochs makes none (`mmio-reads.0.N`). Slice 2's own
  testbench and RTL meet it on a fresh seed too (seeds 17-24 × 2,000:
  `random#17#229`, the only failure).
- `random#1#1473` ([#1425](https://github.com/DeanoC/fes/issues/1425)): a
  real-mode `CS: REP MOVSB` stores over its own first byte. The RTL runs all
  8 elements, as a 386 with the instruction decoded would, and Bochs stops
  after the first. The harness's rule for an unplanned store into the
  program's code applies only after it has compared that step.

**Against slice 2's report** (`01bc050ef`'s `lockstep-report.json`):
- **Corpus:** every result field and trace hash is equal. The cycles grow
  by 2,628 of 174,952,508, spread up and down over 44 opcode files. They
  trace to one kind of test, `FE.0#106` the first: an 8-byte instruction at
  FFF8h ends at FFFFh, so the HLT after it, at 10000h, faults #GP(0).
  - Slice 2 cut D's EIP to 0000h for that delivery. The corpus's 386EX
    prefetch-queue model (`queue16`) places its self-modifying-code window
    from the uops' next EIP, so the window sat 64 KiB below the code.
  - With the cut gone the window is where the code is. That test alone
    runs 33 cycles faster, with 2 fewer DDR reads and writes, and the same
    trace hash.
  - The fetch-latency draws of the file's later tests move with it.
- **Directed cases:** the trace hash is slice 2's (`68d44755edca35dc`);
  the cycles grow from 5,781 to 5,993 and the DDR writes fall from 98 to 96.
  Run without fetch latency, two cases differ, both a real-mode CALL rel32
  (66h) to past the limit: `call-rel32-cs-limit` (+22 cycles) and
  `gp-stack-wrap` (+57). Their #GP now comes from M1, after the push's own
  check, as Bochs pushes first, where slice 2 raised it in D. The rest of
  the difference is the later cases' fetch-latency draws moving.
- **Unreal-mode and memory cases:** equal, trace hashes and cycles
  included.
- **Random and protected-mode programs:** not comparable, as the generator's
  protected-mode blocks now draw the new block and its descriptors.

## pc386 suites, governor and test386

SUITES

## Quartus seal

SEAL

## DOOM's start on the RTL

DOOM

## Known limits

- Bochs's real-mode far transfers and INT check the new offset against the
  limit CS has before the transfer, which the RTL follows. After a
  real-mode CS load the RTL sets CS's limit to FFFFh, the 386 rule, where
  Bochs keeps the cache; the lockstep gives the oracle the RTL's CS cache
  there (`cs_rule`), as before.
- The two pre-existing failures above, [#1424](https://github.com/DeanoC/fes/issues/1424)
  and [#1425](https://github.com/DeanoC/fes/issues/1425).
