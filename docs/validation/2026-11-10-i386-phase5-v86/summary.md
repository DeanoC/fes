# i386 Phase 5 slice 3: V86 mode, 10 November 2026

This record covers CPU Phase 5's slice 3
([`i386-cpu-design.md`, "Phase 5: V86, paging and task switches"](../../../sources/misteross/docs/i386-cpu-design.md#phase-5-v86-paging-and-task-switches),
tracker [#1548](https://github.com/DeanoC/fes/issues/1548)). `fes_i386_cpu`
now has V86 mode: EFLAGS.VM with CPL 3, real-mode segment loads with V86
caches, its entry by IRETD from CPL 0, deliveries out of it to CPL 0 with
the four extra pushes, its IOPL-sensitive forms, and IN, OUT, INS and OUTS
through the TSS's I/O permission bitmap (INS and OUTS also at a CPL above
IOPL, which were not built before).

**Result:**
- **test386.asm** on the RTL now passes POST 21 (V86 mode) and stops at
  POST 22 (task switching), at `int 0x28`, a delivery through a task gate
  (slice 4). Main stopped in POST 21 at an IRETD to V86 mode.
- **Lockstep.** Everything passes against Bochs on the RTL and on the
  locked Yosys's netlist, with identical retirement traces, but three random
  programs that fail on main's RTL too ([#1644](https://github.com/DeanoC/fes/issues/1644),
  [#1646](https://github.com/DeanoC/fes/issues/1646)): the full corpus, 73
  more protected-mode cases (V86 mode and the I/O bitmap), 8 more paging
  cases (#PF from V86 mode), and random protected-mode programs that now
  enter V86 mode 7,559 times. Fifteen one-line mutants of the new RTL each
  fail directed cases ([mutants.txt](mutants.txt)).
- **Seal.** (See [Seal](#seal).) A Quartus 17.0.2 fit at seed 1 of
  `7837d3955` met every analysis: setup +0.568 ns, hold +0.075 ns, 36,492
  ALMs (main 36,717) and 268 M10K (main 268). The seal at the PR's head
  (`bd1a9f4b4`, one more term in D after Codex's review) did not fit at seed
  1: routing stopped on congestion.
- **Kit B, max.** DOOM v1.9 `-timedemo demo3` 40.03 fps (main 40.04 the
  same hour), 3DBench, PCPBench and TopBench as main's within a few points.
  Under JEMM386 v5.85, Wolfenstein 3D reaches E1L1 and DOOM plays its demos
  through VCPI; Time Circle, Wolfenstein 3D, Ultima VI, DOOM and U.S. Navy
  Fighters (to its CD-ROM check) run as on main.
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

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of 2,000
random programs, 16 seeds of 500 protected-mode programs, the full corpus) at
`7884ea746`, the PR's RTL, on the RTL and on the netlist from the i386
lock's Yosys (`238eac9a4`):

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 31,999 of 32,000 (7,911,952 retirements) | same |
| Protected-mode programs | 7,998 of 8,000 (415,284 retirements) | same |
| Unbuilt encodings | 49,760, each stopped | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 273, 62, 1,933 and 27, all pass | same |

Every corpus file's and random seed's retirement trace hash is the same on
both ([lockstep-report.json](lockstep-report.json),
[lockstep-netlist-report.json](lockstep-netlist-report.json)). The three
failing programs fail the same way on main's RTL `e835d545d` with this
testbench (its generator now makes other programs, so main's own run never
met them): two XCHG m under paging, #PF on the RTL where Bochs's
read-modify-write check gives #GP(0) for a code-segment DS
([#1644](https://github.com/DeanoC/fes/issues/1644)); and a CALL rel's push
whose #PF ends in shutdown with CR2 0 where Bochs writes the push's address
([#1646](https://github.com/DeanoC/fes/issues/1646)).

**The protected-mode programs** retired 14,200 instructions in V86 mode:
7,559 IRETDs into it, 6,517 deliveries out of it (faults, INT n and 4,013
injected interrupts taken there), 2,728 IOPL-sensitive forms run at IOPL 3,
801 IN and OUT through the bitmap there, and 465 INS and OUTS under the
bitmap in V86 mode or at a CPL above IOPL.

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

**Paging cases** (`FES_I386_MODE=paging`, now 62, 8 new): #PF from V86 mode
(U/S set in the error code) on a page not present, supervisor or read-only
and at a fetch; user pages read and written; an INT round trip; a GDT on a
supervisor page, which the delivery reads; an IRETD in V86 mode to EIP
1xxxxh on a page not present, whose fetch is #GP(0) before any walk
(Codex's review).

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


## test386.asm

`scripts/test_fes_i386_test386.py --cpu rtl --no-cross-check`
([test386-rtl-report.json](test386-rtl-report.json)): POST codes 00, 01,
02, 03, 04, 05, 06, 08, 09, 20, 21 and 22, no error, then a stop as not
built after 799,276 retirements at 0053h:000E0A18h, `int 0x28` through a
task gate (POST 22, slice 4).

## Seal

Quartus 17.0.2 from clean checkouts, with `CACHE_ROOT` and
`QUARTUS_ROOTDIR` as slice 2's record gives them.

| Fit | Commit | Seed | Result | Setup | Hold | ALMs | M10K |
| --- | --- | ---: | --- | ---: | ---: | ---: | ---: |
| a1 | `7837d3955` | 1 | every analysis met (package `7b29b38a…`, the kit's) | +0.568 ns | +0.075 ns | 36,492 | 268 |
| s2 | `bd1a9f4b4` (the PR's RTL) | 1 | **no fit**: routing stopped on congestion while adding hold delay (peak interconnect 62% in X33-44, Y11-22) | | | | |
| d2 | `bd1a9f4b4` | 2 | every analysis met, a diagnostic only (package `48f8cc11…`) | +0.629 ns | +0.080 ns | 36,442 | 268 |

Main's seal (slice 2's) used 36,717 ALMs and 268 M10K. Between a1 and s2
the RTL gained one term in D (`d_ifw` needs `!cs_gp`, from Codex's review)
and comments. The seed-1 seal at the PR's head is the gate, and it fails;
the slice stops there for the coordinator's decision rather than take
another seed as the seal.

## Kit

Runs `m1` and `k1`-`k8` used Kit B, core-only, on 10 November from 13:59 to
15:48 ([kit-scores.json](kit-scores.json)).
- **Packages.** `7b29b38a…`, the fit of `7837d3955` above (build
  `2179950d…`), at max (preset 0), each launch after a SCRUBX scrub. Run
  `m1` is the A/B reference: main's package `3429af98…` (slice 2's final
  seal), the same runner, kit and baseline. The RTL changed after
  `7837d3955` only in D's fetch walk under a fetch past CS's limit (a V86
  IRETD to EIP past FFFFh under paging, which no kit title does) and in
  comments, so the runs were not repeated.
- **Kit.** Baseline image `cd284ca3…` and software `7ddb0bfb`, with no
  install, no image change and no reboot. After each run Kit B was checked
  against its snapshot and logged FREE, and the run's FogCast data trees
  were removed and logged.
- **Disks.** The Phase 4 bench disk, the #1533 game drives `real`
  (`a6977555…`), `pmode` (`b4be6e29…`), `fpu` (`ff322ddc…`) and `dpmi`
  (`9e6ae3a3…`), and the Phase 5 census's JEMM386 drives `real-jemm`
  (`5c0b4b06…`), `pmode-jemm` (`e6d7b613…`) and `xms-jemm` (`1bc8d4af…`),
  from Powerboat. None is committed.

| Benchmark | Main (m1) | This slice (k1) |
| --- | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,377 realtics, 40.04 fps | 3,863 in 3,378, 40.03 fps |
| 3DBench | 76.9 | 76.9 |
| PCPBench | 13.3 | 13.3 |
| TopBench | 307 | 301 |

TopBench varies by a few points between runs of one package (slice 2's
record saw 304-310); its VidMem part read 99 µs and 101 µs.

| Title | Drive | Result |
| --- | --- | --- |
| Wolfenstein 3D under JEMM386 | `real-jemm` | JEMM386 v5.85 loads at boot; a new game, in E1L1 |
| DOOM v1.9 under JEMM386 (VCPI) | `pmode-jemm` | Its title and demos play |
| Master of Orion under JEMM386 | `xms-jemm` | Back at the prompt after "reconfigure hardware options", the setup the census record left to recheck |
| Pinball Fantasies (eXceed) | `real` | eXceed 1.10 runs; its sound setup reaches "Sound configuration complete", where the run's keys stopped |
| U.S. Navy Fighters | `fpu` | Its CD-ROM check, as on main |
| Time Circle (32RTM) | `dpmi` | Its main menu |
| Wolfenstein 3D | `real` | A new game, in E1L1 |
| Ultima VI | `real` | Its introduction plays |
| DOOM v1.9 | `pmode` | Its title and demos play (run `k8`: `k2`'s load of this drive lost the agent's pipe and FogCast's Home room failed its draw budget, so DOOM ran again alone) |

Each title was launched and watched for 100-180 s; this is a recheck, not
#1368's full acceptance.

## Machine sim

(pending: JEMM386 with Wolfenstein 3D and DOOM from boot on the RTL,
Pinball Fantasies from the DOS prompt.)

## What this record does not cover

- **VME and PVI,** Pentium features a 386 does not have.
- **A task switch into a V86 task** and a delivery from V86 mode through a
  task gate (slice 4).
- **Breakpoints and single step** in V86 mode (slice 5).
- **nextpnr.** No route was made (fes#1235).
