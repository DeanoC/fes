# i386 Phase 5 slice 3: V86 mode, 10 November 2026

This record covers CPU Phase 5's slice 3
([`i386-cpu-design.md`, "Phase 5: V86, paging and task switches"](../../../sources/misteross/docs/i386-cpu-design.md#phase-5-v86-paging-and-task-switches),
tracker [#1548](https://github.com/DeanoC/fes/issues/1548)). `fes_i386_cpu`
now has V86 mode: EFLAGS.VM with CPL 3, real-mode segment loads with V86
caches, its entry by IRETD from CPL 0, deliveries out of it to CPL 0 with
the four extra pushes, its IOPL-sensitive forms, and IN, OUT, INS and OUTS
through the TSS's I/O permission bitmap (INS and OUTS also at a CPL above
IOPL, which were not built before).

**Result** (draft; the seal, the kit runs and the netlist lockstep are
still running):
- **test386.asm** on the RTL now passes POST 21 (V86 mode) and stops at
  POST 22 (task switching), at `int 0x28`, a delivery through a task gate
  (slice 4). Main stopped in POST 21 at an IRETD to V86 mode.
- **Lockstep.** The new directed cases (73 V86 and bitmap cases in
  `FES_I386_MODE=pmode`, 7 V86 cases in `paging`) and the random
  protected-mode programs, which now enter V86 mode, pass against Bochs on
  the RTL. Fourteen one-line mutants of the new RTL each fail directed
  cases ([mutants.txt](mutants.txt)).
- **Speed** (latency bench, [latency-report.json](latency-report.json)): an
  INT from V86 mode through a DPL-3 386 gate and the IRETD back take 166
  clocks; IN through the bitmap takes 26 a time. Every older loop is
  unchanged.

## Design as built

The RTL README's
[V86 mode](../../../sources/misteross/cores/fes-common/rtl/i386/README.md#v86-mode)
has the full description, and the design doc's slice 3 lists where it
departs from the plan. In short:
- **One mode bit.** VM is EFLAGS bit 17 and changes only at a far jump that
  flushes everything after it: the jump into V86 mode sets it (EX) with CPL
  3, a delivery's jump out of it clears it with CPL 0. So DC decodes with
  the committed VM, and a fault part way through a delivery from V86 mode is
  delivered from V86 mode again, as Bochs's is.
- **Decode.** `pm` (PE without VM) picks the descriptor forms: in V86 mode
  segment loads, far JMP and CALL, RETF and IRET take the real-mode forms;
  INT n, INT3, INTO and exceptions take the V86 delivery (`vvec`, `vint`).
  The group 6 forms, LAR, LSL and ARPL are #UD; HLT, CLTS, LMSW, LGDT, LIDT
  and MOV CRn and DRn are #GP(0) by their CPL check.
- **IOPL.** Below IOPL 3, PUSHF, INT n, IRET and LOCK raise #GP(0) in DC
  (CLI and STI by their CPL check); POPF after its pop's own checks, at M1,
  as Bochs pops first. LOCK's #GP(0) follows the 386 manual where Bochs runs
  the instruction: the lockstep lets the RTL's #GP stand (`v86-lock-gp`).
- **Segments.** A real-mode load in V86 mode gives the segment V86's cache,
  limit FFFFh and access rights F3h, CS too (`seg_v86`).
- **IRETD into V86 mode.** `piret` reads EFLAGS', CS's and EIP's slots as
  for any IRET; its CS descriptor word (`vgo`), which waits for T0 and so
  finds T4 settled, becomes a NOP for an IRETD at CPL 0 whose image has VM
  set, as does the word after it, and the ROM goes on at `v86ret`: ESP's
  slot, probes of the four slots below GS's, GS's load, then FS, DS, ES and
  SS, the flags, and the jump, which loads CS, EIP's low word and ESP. A
  protected-mode IRET pays nothing.
- **Deliveries from V86 mode.** Their own routine, which raises
  #GP(selector) for a CS that is conforming or DPL 3 at its hold and for one
  of DPL 1 or 2 once the TSS's stack is read (Bochs's order), pushes GS,
  FS, DS and ES before SS and ESP, gives EFLAGS' image VM set, and nulls the
  four at the jump.
- **IRET in V86 mode at IOPL 3.** The real-mode routine without the offset's
  limit check: Bochs loads IRETD's EIP whole, so an EIP past FFFFh faults at
  the next fetch. A delivery's entry is no longer checked against CS's limit
  (it fetches nothing), so that #GP reaches its handler.
- **INS and OUTS.** Under the bitmap (V86 mode, or a CPL above IOPL) DC
  decodes the string's first uop as the start of `iostr`: DX into T5, the
  bitmap's base, the port's word and its check for the element's size, once
  before a REP's first element, a count of 0 too, as Bochs's `allow_io`;
  its last word becomes the string's first uop.
- **The ROM** holds 334 of its 512 words; its word grows from 117 to 119
  bits (a 5-bit slot, `vgo`, `v86`), within the 6 M10K blocks it had.

## Lockstep

(pending: the full run at the PR's head on the RTL and on the netlist.)

**New directed cases** (`FES_I386_MODE=pmode`, now 273 cases, 73 more):
- Entry and round trips: `v86-int-iretd` (INT 2Ah to an IRETD in 32-bit
  code and back, twice), `v86-int-286-gate`, `v86-int-trap-gate`,
  `v86-int-286-tss`, `v86-int-ss0-big` (a 32-bit SS0), `v86-esp-high` (ESP's
  high half in the frame), `v86-iretd-ss32` (the IRETD from a 32-bit
  stack), `v86-iretd-eip-high` (Bochs keeps EIP's low word),
  `v86-iretd-frame-limit` (GS's slot at 10000h on a 16-bit stack, #SS(0)
  with nothing loaded), `r3-iretd-vm` (VM in the image at CPL 3 is ignored).
- Deliveries' checks: a gate of DPL 0 (#GP(102h)); INT3 through gates of
  DPL 0 and 3 at IOPL 0 (not IOPL-sensitive) and INTO; a conforming CS
  (#GP(60h)), a DPL-3 CS (#GP(70h), also with TR's limit short of CPL 3's
  slot), a DPL-1 CS (#GP(78h) after the TSS's read), a CS not present
  (#NP(58h)), SS0 not present (shutdown); a fault inside a delivery from V86
  mode (`v86-ud-nested-ss`: #UD's EIP slot past an expand-down SS0, whose
  #SS is delivered from V86 mode through a 286 gate).
- IOPL: CLI, STI, PUSHF, PUSHFD, POPF, POPFD, IRET, IRETD and HLT at IOPL 0
  (#GP(0)); PUSHF at SP 1 (#GP before the push); POPF at SP FFFFh and POPFD
  at FFFEh (#SS(0) before the #GP); LOCK at IOPL 0 (#GP(0)) and 3 (runs), an
  invalid LOCK (#UD first); the flags at IOPL 3 (POPF and POPFD leave IOPL
  and VM); IRET and IRETD within V86 mode, and IRETD to an EIP past FFFFh.
- Segments and transfers: `v86-segs` (MOV, LES, LFS, LGS, PUSH and POP of
  segment registers, MOV SS), `v86-far` (far JMP and CALL, RETF).
- #UD (ARPL, LAR, LSL, SLDT, STR, LLDT, VERR) and #GP(0) (LGDT, MOV CR0, MOV
  DR7, LMSW, CLTS); SMSW, SGDT and SIDT run.
- I/O: IN and OUT allowed and refused by the bitmap at IOPL 0 and 3, a word
  across a set bit, a port past TR's limit, a 286 TSS, the TSS's own bytes as
  the bitmap; INSB, REP INSB, OUTSD and REP OUTSW allowed, OUTSB and INSW
  refused, a REP INSB with a count of 0 refused; at CPL 3 (protected mode)
  INSB and OUTSD allowed, OUTSB refused, REP INSW with a count of 0 refused,
  a 286 TSS refused (these replace `r3-insb`, which stopped as not built).

**Paging cases** (`FES_I386_MODE=paging`, now 61, 7 new): #PF from V86 mode
(U/S set in the error code) on a page not present, supervisor or read-only
and at a fetch; user pages read and written; an INT round trip; a GDT on a
supervisor page, which the delivery reads.

**Random programs.** An eighth of the protected-mode blocks' operations now
enter V86 mode (`pm_v86`): an IRETD to the program's own real-mode CS at IOPL
3 mostly (else 0-2), then CLI, STI, PUSHF and POPF of random images, IRET
and IRETD within V86 mode, INT n mostly through a present DPL-3 386 gate (a
round trip), INT3, INTO, IN, OUT, INS and OUTS through the bitmap, segment
loads, data accesses, what V86 mode refuses now and then, and random
instructions; then INT 2Fh back to CPL 0. Injected interrupts are taken in
V86 mode and return to it.

**Harness changes.** `why_e` drops the reasons nothing raises any more (an
IRET to V86 mode, INS and OUTS under the bitmap), so the later reasons'
numbers shift. The lockstep's `v86-lock-gp` rule (LOCK in V86 mode below
IOPL 3, where Bochs does not fault) gives the oracle the RTL's state. The
summary counts V86 retirements, entries, exits, injected interrupts taken
there, its IOPL-sensitive forms at IOPL 3, its I/O and string I/O under the
bitmap.

**A failure that is not this slice's:** a random program at CPL 0 with
paging (`XCHG [BX], ESP` through a code-segment DS on a page not present)
takes #PF on the RTL and #GP(0) on Bochs, on main's RTL too:
[#1644](https://github.com/DeanoC/fes/issues/1644).

## test386.asm

`scripts/test_fes_i386_test386.py --cpu rtl --no-cross-check`
([test386-rtl-report.json](test386-rtl-report.json)): POST codes 00, 01,
02, 03, 04, 05, 06, 08, 09, 20, 21 and 22, no error, then a stop as not
built after 799,276 retirements at 0053h:000E0A18h, `int 0x28` through a
task gate (POST 22, slice 4).

## Seal

(pending.)

## Kit

(pending.)

## Machine sim

(pending: JEMM386 with Wolfenstein 3D and DOOM from boot on the RTL,
Pinball Fantasies from the DOS prompt.)

## What this record does not cover

- **VME and PVI,** Pentium features a 386 does not have.
- **A task switch into a V86 task** and a delivery from V86 mode through a
  task gate (slice 4).
- **Breakpoints and single step** in V86 mode (slice 5).
- **nextpnr.** No route was made (fes#1235).
