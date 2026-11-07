# i386 Phase 2, third slice: privilege transitions for ring-3 DPMI hosts, 7 November 2026

Issue [#1535](https://github.com/DeanoC/fes/issues/1535), the i386 design's
Phase 2 after slice 1 (#1373), slice 2 (#1411) and the CS-limit check
(#1434). The pc386 compatibility census
([#1533](https://github.com/DeanoC/fes/pull/1533)) found 9 kit failures at
the CPU's not-built privilege transitions. This slice takes its scope from
what those DPMI hosts use on the Bochs CPU.

**Result.**
- **Scope from evidence** ([census](#census-what-the-hosts-use)). Borland's
  DPMI16BI and RTM (Jazz Jackrabbit, Tyrian, Cyberdogs) need IRET from CPL 0
  to CPL 3, deliveries from CPL 3 to CPL 0 through 286 interrupt gates on a
  286 TSS's 16-bit stack, far CALL, RETF and IRET at CPL 3, segment loads
  with CPL checks, and #GP and #NP from CPL 3. They run at IOPL 3, so they
  need no I/O bitmap. CWSDPMI (Jump 'n Bump, Angband, NetHack, Chris's 3D
  Benchmark, Quake) and Borland's 32RTM (Time Circle) turn paging on and
  switch tasks, so they are Phase 5's.
- **Built** ([what the slice builds](#what-the-slice-builds)): CPL as
  architectural state with every descriptor check against it; IRET to an
  outer privilege with SS:ESP and data-segment nulling; deliveries to an
  inner privilege through 286 and 386 gates on 286 and 386 TSSs, with #TS;
  #GP(0) for the instructions only CPL 0 runs; CLI and STI under IOPL;
  POPF's and IRET's IF and IOPL by CPL; the I/O permission bitmap for IN and
  OUT.
- **Lockstep** ([below](#lockstep)): the RTL and the i386 lock's Yosys
  netlist pass every run against Bochs, with identical reports: 1,758,699
  corpus states, 32,000 random programs, 8,000 protected-mode programs that
  now run at CPL 3 (27,841 retirements at CPL 3, 5,785 returns to an outer
  privilege, 3,768 deliveries to an inner one, 453 bitmap-checked IN and
  OUT), and 143 directed protected-mode cases, 51 of them new.
- **Seal** ([below](#quartus-seal)): Quartus 17.0.2 meets every analysis:
  worst setup +0.914 ns (main +0.853), hold +0.082 ns (main +0.117), 33,313
  ALMs (main 33,118).
- **Kit B**: KIT-RESULT

## Inputs

- Branch `deano/cranky-liskov-88eaa0`, from main `607922c63`.
- Oracle: Bochs 3.1 at CPU level 3 (`fes-i386-oracle`), corpus
  SingleStepTests/80386 `459d49fbe628`, test386.asm `cfd052d1e64d`.
- Verilator 5.032; the i386 lock's Yosys `238eac9a4`; Quartus Prime Lite
  17.0.2 on the pc386 lock's Yosys netlist.
- The census drives from #1533's game-drive store (`~/fes-drives/pc386`):
  `dpmi` (SHA-256 `bb659032…`), `real` (`b460ca55…`), `bench` (`4fbeb7d9…`).
  The titles are user-supplied (eXoDOS zips and PhilsComputerLab's DOS
  Benchmark Pack). No game bytes are in the repository.

## Census: what the hosts use

`machine_tb --cpu oracle --census` on #1533's game suite
(`sim_fes_pc386.py --suite game --game ID --instruction-census`, each
title's own launch and keys, 20 simulated seconds, 25 for Quake), with
extra census keys for this slice: each far transfer's target type, each
IRET's frame image, the segments an outer return nulls or keeps, each
inner delivery's TSS and SS0, the IOPL-sensitive instructions at CPL 3 with
the bitmap's bit, and MOV to and from CRs and DRs. The extra keys are
scratch; [census-ring3.json](census-ring3.json) has each title's totals.

CENSUS-TABLE

## What the slice builds

The [RTL README](../../../sources/misteross/cores/fes-common/rtl/i386/README.md#protected-mode)
has the detail. In short, following Bochs 3.1's `iret_protected`,
`protected_mode_int`, `fetch_ss_descriptor`, `validate_seg_regs`,
`allow_io`, `check_cs` and the privilege checks of each instruction:

- **CPL** (`cpl`): CS's RPL once a protected-mode load has written CS, 0 in
  real mode and on protected-mode entry. Only a protected-mode CS load
  changes it, a far transfer that flushes everything younger, so decode and
  the staged descriptor's checks read it as it stands. The lockstep compares
  it (`arch_cpl`). The data, SS, far JMP and CALL, RETF and IRET, gate,
  delivery-CS and LAR's-kin checks all take it, and CS's RPL becomes it.
- **The instructions only CPL 0 runs** (HLT, CLTS, MOV to and from CRs and
  DRs, LGDT, LIDT, LMSW, LLDT, LTR) raise #GP(0) at another CPL, and CLI and
  STI at a CPL above IOPL, before any operand, after LOCK's and the form's
  #UD. POPF and IRET load IF only at a CPL up to IOPL, and IOPL only at
  CPL 0. A software interrupt's gate with DPL below CPL is #GP with the IDT
  entry. IRETD's VM bit counts only at CPL 0.
- **A held CS.** A transfer that changes privilege writes CS, SS and ESP at
  its last uop, after every check, so the uop that checks CS has WB keep
  CS's staged descriptor (`hs_*`) and whether privilege changes (`xp`),
  while T0-T3 take the new SS's descriptor and a new temporary, T5, the new
  ESP. Uops for one case or the other (`xc`) become NOPs in D, so IRET and
  the delivery keep one routine each. Nothing architectural is written
  before the last uop that can fault, as the replay rule needs.
- **IRET to an outer privilege** (`piret`): after CS's checks, SS's word
  slot, its descriptor and checks against the new CPL (#GP(sel), #SS(sel)
  for not present), ESP's slot, then the offset against the held limit; the
  flags by the CPL the IRET runs at; CS's and SS's accessed bits; the jump,
  which nulls DS, ES, FS and GS below the new CPL unless they hold
  conforming code (selector 0, every access refused, the rest of the cache
  kept, as Bochs keeps it).
- **A delivery to an inner privilege** (`pvec`, `pint`): for a
  nonconforming CS below CPL, the TSS's ESP and SS for CS's DPL (a 386 or a
  286 TSS; past TR's limit #TS(TR)); SS's descriptor (#TS(sel) past its
  table) and checks (null, RPL, DPL or type #TS(sel); not present #SS(sel));
  the offset; then six slots (five without an error code) probed and
  written on the new stack, through the staged SS's base and checks at T5
  plus each slot (#SS(sel)): the old SS and ESP, EFLAGS, CS, EIP and the
  error code, at the gate's size. A fault on the way leaves SS and ESP as
  they were, as Bochs's `write_new_stack_*` does.
- **#TS** takes the last two fault codes, with T0's selector and with TR's;
  it is contributory and pushes its error code.
- **The I/O permission bitmap** (`iochk`): IN and OUT at a CPL above IOPL
  read the bitmap's base at 66h (#GP(0) unless TR is a 386 TSS of 104 bytes
  or more), the port's word (#GP(0) at or past TR's limit), and check the
  access's bits; the last uop is the access itself.
- **Timing.** The new address sources (the new stack, the TSS, the held
  CS's accessed bit, the transfer's jump) come in after the store-to-load
  check's 12-bit sum, which treats those rare uops as unknown (a load among
  them waits for an older store; a store among them makes a younger load
  wait). A first seal with them on that sum's inputs left D's EIP 0.9 ns
  worse ([Quartus seal](#quartus-seal)).

**Microcode.** `ucode.toml` gains `iochk` and rewrites `pvec`, `pint` and
`piret`; `pretf` keeps its checks under a new name (`rets`), RETF to an
outer privilege still not built. 227 words of 256, from 197; words grow
from 101 bits to 111 (`hold`, `xfer`, `nstk`, `xc`, `xs`, `tss`, `iof`).

**Cost at CPL 0.** Against main the lockstep's CPL-0 runs (before the
generator's CPL-3 blocks) retire the same instructions, and the corpus the
same cycles. A protected-mode IRET and delivery issue their CPL-3 uops as
NOPs: the random programs take 0.3% more cycles and the protected-mode
programs 1.95% more.

## Lockstep

`scripts/test_fes_i386_lockstep.py` at `e8e541d8e`
([report](lockstep-report.json)), and with `--yosys` on the i386 lock's
Yosys `238eac9a4` ([report](lockstep-netlist-report.json)). The reports
are identical but for the netlist's `yosys` field.

| Run | Result |
| --- | --- |
| Corpus (SingleStepTests/80386) | 1,758,699 pass, 0 fail; 3,517,401 retirements |
| Random programs (16 seeds × 2,000) | 32,000 pass, 0 fail; 8,120,110 retirements |
| Protected-mode programs (16 seeds × 500) | 8,000 pass, 0 fail; 426,587 retirements; 17,545 deliveries through the IDT; at CPL 3: 27,841 retirements, 5,785 returns to an outer privilege, 3,768 deliveries to an inner one, 453 IN and OUT through the bitmap |
| Directed cases | 68 pass |
| Unreal-mode cases | 7 pass |
| Protected-mode cases | 143 pass (51 new) |
| No-387 cases | 1,933 pass |
| Memory cases | 27 pass |

Main's run of the same runner fails one program, `random#7#1936`
([#1510](https://github.com/DeanoC/fes/issues/1510)). This slice's
generator draws other programs, and none fails.

**The generator** (`pm_ring3`, new) adds CPL 3 to the protected-mode
blocks. The GDT grows to 28 descriptors (DPL-3 16-bit code at the program's
CS base, a flat 32-bit and a 16-bit DPL-3 stack, DPL-3 data), the TSS gets
a ring-0 stack and an I/O permission bitmap (now and then an SS0 that fails
a check, a 286 TSS, a short limit), a third of the IDT's gates get DPL 3,
and gate 2Fh's handler far-jumps back to CPL 0. A block loads TR, sets IOPL
0-3 by POPF, and IRETs (16 or 32-bit) to CPL 3, now and then with a frame
that fails a check. At CPL 3 it runs CLI, STI, POPF, IN and OUT, INT n,
segment loads, accesses, the instructions only CPL 0 runs, far CALL and
RETF, IRET to CPL 3, LAR's kin and random instructions; injected
interrupts deliver to CPL 0 and IRET back.

**The new directed cases** (`--mode pmode`, `to_r3`): CPL 3 entered by IRET
from a 386 or a 286 TSS: HLT, CLI, STI, LGDT, MOV from CR0 and LTR
(#GP(0)); INT n through DPL-3 386 and 286 gates and back (the stack switch
both ways), and through DPL-0 gates (INT n, INT3, INTO: #GP with the IDT
entry); POPF and POPFD under IOPL 0; the nulled DS, PUSH of the nulled ES
and FS; segment loads by DPL and RPL, conforming code in a data register,
SS by RPL and DPL; LAR and VERR; the I/O bitmap (allowed, denied, a word
across two ports, past TR's limit, a 286 TSS, INS not built); the TSS's
SS0 null, not present, DPL 3, read-only, wrapping a 16-bit stack, past a
small limit, a 32-bit SS0; far CALL and RETF at CPL 3, far JMP to
conforming and DPL-0 code; IRET to CPL 3 of IF and IOPL; from CPL 0, IRET
to CPL 3 with a bad SS (#GP, #SS), a CS whose DPL is not its RPL, a
not-present CS, IRETD, IOPL 3; RETF to CPL 3 (not built); IRET from CPL 3
to RPL 0 (#GP).

**Harness changes.** The bench reads the RTL's CPL. When it hands a step's
fields to Bochs for a divergence that leaves every field to the RTL, a
segment whose cache agrees keeps Bochs's valid bit: the RTL gives valid
from P, which a data segment an outer return nulled keeps set. INS and OUTS
at a CPL above IOPL stop the RTL as not built and are not compared with
Bochs's step.

**test386.asm** (`test_fes_i386_test386.py --cpu rtl`,
[report](test386-rtl-report.json)): as in slice 2's record, POST 00h-06h
pass and POST 08h's set-up enters protected mode with paging, where the
RTL stops as not built (`WHY_PAGING`) after 793,777 retirements. Its
protected-mode part, rings included, waits for Phase 5's paging.

## Quartus seal

`make build-fes-pc386-quartus SEED=1` from a clean checkout of
`445ed9629` ([seal-summary.json](seal-summary.json)), Quartus 17.0.2.
`445ed9629`'s RTL is the branch's; later commits change only the lockstep
bench, the scripts' case count and documents. Package `657245c2…`.

| | This slice | Main (#1522, `c89d6b0c`) |
| --- | ---: | ---: |
| Setup, worst | +0.914 ns | +0.853 ns |
| Hold, worst | +0.082 ns | +0.117 ns |
| ALMs | 33,313 | 33,118 |
| Registers | 30,319 | 29,939 |
| RAM blocks | 265 | 265 |

[stage-slack.json](stage-slack.json) has the worst setup slack into each
register group at the −40 °C corner. D's EIP has +1.38 ns (main +1.95), the
data operands +1.89 (+2.45), AG +2.14 (+2.80), the forwarding tables +1.71
(+2.19); the worst system-clock path is the governor's two-multiplier
chain at +1.20 (+1.69), which this slice does not change. The new
registers (`hs_*`, `t5`, `xp`, the TSS checks) have +5.1 ns or more.

A first seal, `d7d0843a6`, had the new address sources on the
store-to-load check's sum: setup +0.133 ns, D's EIP +1.02. `445ed9629`
took them off it.

## Kit

KIT

## Not in this record

- **CWSDPMI and 32RTM** need paging and task switches (Phase 5); CWSDPMI
  also calls through a 386 call gate and writes DR0-DR3 and DR7.
- **Still not built**, each stopping the CPU with its reason: RETF to an
  outer privilege, call gates, task gates and task switches, INS and OUTS
  at a CPL above IOPL, debug registers. None of the in-scope hosts uses
  them.
- **The governor** charges a transition at its real-mode class's count; a
  386 takes longer for an IRET to an outer privilege and a delivery to an
  inner one.
- **Quake** also needs an x87, which the design's empty socket lacks.
