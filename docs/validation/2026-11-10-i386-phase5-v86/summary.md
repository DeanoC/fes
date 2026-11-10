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
- **Seal.** Quartus 17.0.2 at seed 1, from a clean checkout of the PR's
  RTL (`111687d80`), meets every analysis: setup +0.841 ns, hold +0.082
  ns. ALMs 36,398 (main 36,717, -319: fit noise) and 268 M10K (main 268):
  the ROM's wider word still fits its 6 blocks. A seed-1 seal of an earlier
  head did not route; the cause was Codex's first fix, which put CS's limit
  compare in front of a 147-fanout select in D, and its rework restored the
  path ([Seal](#seal)).
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
- **IOPL.** Below IOPL 3, PUSHF, POPF, INT n, IRET and LOCK raise #GP(0) in
  DC, before any operand (CLI and STI by their CPL check). The 386, not
  Bochs, decides the order: its die-extracted microcode
  ([reenigne/x86_microcode](https://github.com/reenigne/x86_microcode),
  `80386/microcode_notes.txt`) enters each in V86 mode at a test of IOPL
  before any bus cycle, `7FB  if (CPL <= IOPL) goto "p POPFd"` with the
  stack read at `803` on the permitted path only, and the same at `7F9`
  (PUSHF), `7FD` (INT ib), `7FF` (IRET) and `7F7` (CLI, STI). Bochs pops
  POPF first, so a stack fault (#SS, #PF) comes before its #GP there, and
  runs a LOCK, where the 386 manual has #GP(0): the lockstep lets the RTL's
  #GP stand for both (`v86-iopl-gp`).
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
`111687d80`, the sealed RTL, on the RTL and on the netlist from the i386
lock's Yosys (`238eac9a4`):

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 31,999 of 32,000 (7,911,952 retirements) | same |
| Protected-mode programs | 7,998 of 8,000 (415,284 retirements) | same |
| Unbuilt encodings | 49,760, each stopped | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 273, 64, 1,933 and 27, all pass | same |

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
  at FFFEh (#GP(0) before the pop, where Bochs gives #SS(0)); LOCK at IOPL 0 (#GP(0)) and 3 (runs), an
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

**Paging cases** (`FES_I386_MODE=paging`, now 64, 10 new): #PF from V86 mode
(U/S set in the error code) on a page not present, supervisor or read-only
and at a fetch; user pages read and written; an INT round trip; a GDT on a
supervisor page, which the delivery reads; an IRETD in V86 mode to EIP
1xxxxh on a page not present, whose fetch is #GP(0) before any walk
(Codex's review); POPF at IOPL 0 with its stack on a page present
(`pg-v86-popf-iopl0`) and not present (`pg-v86-popf-np-iopl0`), each
#GP(0) before the pop as the 386's microcode orders it (Codex's review
raised the second, where Bochs gives #PF).

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
numbers shift. The lockstep's `v86-iopl-gp` rule (LOCK, and POPF and
POPFD, in V86 mode below IOPL 3, where Bochs runs the LOCK or pops first)
gives the oracle the RTL's state. The
summary counts V86 retirements, entries, exits, injected interrupts taken
there, its IOPL-sensitive forms at IOPL 3, its I/O and string I/O under the
bitmap.


## test386.asm

`scripts/test_fes_i386_test386.py --cpu rtl` at
`111687d80` ([test386-rtl-report.json](test386-rtl-report.json)): POST codes 00, 01,
02, 03, 04, 05, 06, 08, 09, 20, 21 and 22, no error, then a stop as not
built after 799,276 retirements at 0053h:000E0A18h, `int 0x28` through a
task gate (POST 22, slice 4).

## Seal

The seal is Quartus 17.0.2, seed 1, from clean checkout `111687d80`
([seal-summary.json](seal-summary.json)), the PR's RTL.
- **Package.** `eca4f692…`, build `be7ae3ef…`, with `core.rbf` `7f7c8097…`.
- **Analyses.** Every one is met: setup +0.841 ns, hold +0.082 ns, recovery
  +7.100 ns, removal +0.816 ns, minimum pulse width +1.122 ns.
- **Clocks.** The system clock is 61.44 MHz.
- **Resources.**

  | Resource | This seal | Main (slice 2's seal `3429af98`) |
  | --- | ---: | ---: |
  | ALMs | 36,398 | 36,717 |
  | Registers | 32,797 | 32,883 |
  | M10K blocks | 268 | 268 |
  | DSP blocks | 38 | 38 |

  The plan estimated 550 ALMs and 3 M10K for this slice. It takes no more
  than the fit noise of about ±200 the area audit measured, and no M10K:
  slice 2 had already grown the ROM to 512 words, and the word's two new
  bits fit the 6 blocks' 120. 5,512 ALMs (13.2%) are left, 2,102 under the
  area audit's working ceiling of 38,500, for slices 4 and 5.

**Diagnostics on the way** (not seals):

| Fit | Commit | Seed | Result | Setup | Hold | ALMs | M10K |
| --- | --- | ---: | --- | ---: | ---: | ---: | ---: |
| a1 ([summary](seal-summary-7837d3955.json)) | `7837d3955` | 1 | met (package `7b29b38a…`, the kit's) | +0.568 ns | +0.075 ns | 36,492 | 268 |
| s2 | `bd1a9f4b4` | 1 | **no fit**: routing stopped on congestion while adding hold delay (peak interconnect 65.4% total, 92.2% vertical; the router's estimate 62% in X33-44, Y11-22) | | | | |
| d2 ([summary](seal-summary-bd1a9f4b4-seed2.json)) | `bd1a9f4b4` | 2 | met | +0.629 ns | +0.080 ns | 36,442 | 268 |
| s3 ([summary](seal-summary-2564ed8e3.json)) | `2564ed8e3` | 1 | met (package `25052de3…`) | +0.536 ns | +0.104 ns | 36,510 | 268 |

`bd1a9f4b4` carried Codex's first fix for a fetch past CS's limit under
paging: `d_ifw && !cs_gp`. Its hold padding was no larger than a1's (1,608
ns against 1,664 ns on the system clock), but the fix put CS's limit compare
(`LessThan99`, four cells from D's EIP) in front of `d_ifw`, the 147-fanout
select that replaces D's uop with the walk, and through
`d_next_instruction` into AG's row and segment term. The worst path from
D's EIP to AG had 2.615 ns of slack on d2, against 3.924 ns on a1. The
sealed RTL keeps `d_ifw` a function of registers and gives the walk uop
`cs_gp`'s #GP(0) as its fault instead; M1 starts no walk for a uop with a
fault, so the behaviour is the same (the case `pg-v86-eip-past-limit-np`
and the mutant `ifw-over-gp` stand). On s3 the worst path from D's EIP to
AG is a plain move into `ag_row`, +10.112 ns; on the seal, +8.002 ns, the
same move (`cpu_d_eip[29]` to `cpu_ag_row[29]`).

s3 was the seal until Codex's review of POPF's order. Its successor,
`111687d80`, moves POPF's and POPFD's #GP(0) in V86 mode below IOPL 3 from
M1 to DC, before the pop, as the 386's microcode orders it
([Design as built](#design-as-built)), and the lockstep's rule lets the
RTL's #GP stand where Bochs pops first.

## Kit

Runs `m1` and `k1`-`k8` used Kit B, core-only, on 10 November from 13:59 to
15:48 ([kit-scores.json](kit-scores.json)).
- **Packages.** `7b29b38a…`, diagnostic fit a1 of `7837d3955` (build
  `2179950d…`), at max (preset 0), each launch after a SCRUBX scrub. Run
  `m1` is the A/B reference: main's package `3429af98…` (slice 2's final
  seal), the same runner, kit and baseline. The sealed RTL
  differs from `7837d3955` in D's fetch walk for a fetch past CS's limit
  (a V86 IRETD to EIP past FFFFh under paging) and in which fault a POPF in
  V86 mode below IOPL 3 takes when its pop would also fault (#GP(0) now,
  as before when the pop does not); a fault ends both, and no kit title
  reaches either. So the runs were not repeated on the seal.
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

`sim_fes_pc386.py --suite game --cpu rtl --limit-seconds 20 --frame-every 2`
(24 seconds for Wolfenstein 3D), with the RTL of `7e09b21a1`. That is
this slice's RTL before the fetch-walk change, which only a V86 IRETD to an
EIP past FFFFh under paging reaches. The JEMM386 drives (the Phase 5
census's) boot on the RTL from reset, so JEMM386's own start-up, its
switch into V86 mode and its paging run on it. Pinball Fantasies and Strike
Commander take over from the oracle at the DOS prompt. The machine hands a
stop to Bochs and does not hand back while VM or paging is on, so each
run's final report says how much ran on the RTL.

| Title | Drive | RTL retirements (with PE) | End |
| --- | --- | ---: | --- |
| Pinball Fantasies (eXceed) | `real` | 1,380,903,279 (1,322,005,645) | The 20 s limit, no stop: eXceed 1.10's sound setup at "Sound configuration complete", as on the kit |
| Master of Orion with JEMM386 | `xms-jemm` | 242,033,888 (65,314,616) | At 7.25 s, back at the prompt after "reconfigure hardware options", no stop, as on the kit |
| Strike Commander (its own V86 manager) | `xms` | 36,680,274 (91,348) | A stop at `66 CF` (IRETD) at 0018h:00003673h, with NT set: a return to a V86 task (slice 4); Bochs ran the other 309,225,981 instructions to the 20 s limit, through its introduction |
| Wolfenstein 3D with JEMM386 | `real-jemm` | 568,184,374 (391,465,102) | The 24 s limit, no stop: a new game in E1L1 (floor 1, 100% health), past the Options menu the census's oracle run reached |
| DOOM v1.9 with JEMM386 (VCPI) | `pmode-jemm` | 790,457,808 (613,738,536) | The 20 s limit, no stop: past `I_StartupTimer`, at its New Game skill menu |

No run handed an instruction to Bochs but Strike Commander's, at its
slice 4 stop.

**The RTL machine suites** with `--cpu rtl`: `a20`, `c2xx` and `wait`
pass. `speaker`, `scroll`, `ems` and `boot` were still running when this
record was written; they are a follow-up after merge (below).

## What this record does not cover

- **VME and PVI,** Pentium features a 386 does not have.
- **A task switch into a V86 task** and a delivery from V86 mode through a
  task gate (slice 4).
- **Breakpoints and single step** in V86 mode (slice 5).
- **nextpnr.** No route was made (fes#1235).
- **The remaining RTL machine suites,** `speaker`, `scroll`, `ems` and
  `boot` with `--cpu rtl`, a follow-up after merge (agreed with Deano).
  They take 1-3 hours each on the RTL, and none was finished when this
  record was written.
