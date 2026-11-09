# i386 Phase 5 slice 2: debug registers and call gates, 9 November 2026

This record covers CPU Phase 5's slice 2
([`i386-cpu-design.md`, "Phase 5: V86, paging and task switches"](../../../sources/misteross/docs/i386-cpu-design.md#phase-5-v86-paging-and-task-switches),
tracker [#1548](https://github.com/DeanoC/fes/issues/1548)). `fes_i386_cpu`
now has the debug register file (MOV to and from DR0-DR7, DR6's and DR7's
fixed bits, DR7.GD's #DB), far CALL through 286 and 386 call gates to the
same and to an inner privilege (the TSS's stack and the parameter copy),
and RETF to an outer privilege.

**Result:**
- **test386.asm** on the RTL now passes POST 20 (ring 3, through
  `call RING0_GATE`) and stops in POST 21 at an IRETD to V86 mode (slice
  3). Main stopped at POST 20's call gate.
- **Lockstep.** Everything passes against Bochs, on the RTL and on the
  locked Yosys's netlist, with identical retirement traces: the full
  corpus, 32,000 random programs, 8,000 protected-mode programs (1,180 MOV
  DRn, 369 far CALLs to an inner privilege, 289 through a gate to the same
  privilege, 1,031 RETFs to an outer privilege) and 51 new directed cases.
  Seven one-line mutants of the new RTL each fail directed cases.
- **Seal.** Quartus 17.0.2 at seed 1 meets every analysis at 61.44 MHz:
  setup +0.645 ns (main +0.396), hold +0.104 ns. ALMs grow from 36,225 to
  36,683 (+458; 87.5%), so seed 1 still fits. The microcode ROM grows to 512
  words: 3 more M10K blocks (268).
- **Kit A, max.** U.S. Navy Fighters, newly unlocked, runs its extender
  (IRET to CPL 3, call gate back) and stops at its CD-ROM check: "Could not
  find US Navy Fighters CD." DOOM v1.9 `-timedemo demo3` 40.03 fps, and the
  benchmarks match main's seal run on the same kit and baseline the same
  hour. Time Circle reaches its menu; Wolfenstein 3D, Ultima VI and DOOM
  start and play.
- **Machine sim.** U.S. Navy Fighters runs on the RTL from the DOS prompt
  to the same CD-ROM message, with no oracle step: 78.5M instructions on
  the RTL, 318 protected-mode entries.

## Design as built

The RTL README's
[Protected mode](../../../sources/misteross/cores/fes-common/rtl/i386/README.md#protected-mode)
has the full description, and the design doc's slice 2 lists where it
departs from the plan. In short:
- **The brief's premise.** Phase 2's third slice (#1552) built no call gate
  at all: any far CALL or JMP through one stopped as not built. So this
  slice builds call gates to the same privilege as well as to an inner one.
  Far JMP through a call gate stays not built (no census title uses one),
  as do far JMP and CALL to a TSS or task gate (slice 4).
- **Debug registers.** DR0-DR3, DR6's writable bits (15-12 and 3-0; the rest
  read as ones, as Bochs's 386 keeps them) and DR7 (bit 10 always set). MOV
  r32, DRn reads them in D; MOV DRn, r32 writes T1 at WB and jumps to the
  next instruction. DR4 and DR5 are DR6 and DR7. Both moves are CPL 0 only
  (#GP(0)); with DR7.GD set either raises #DB first (after LOCK's #UD and the
  15-byte #GP), as Bochs's `MOV_DdRd` checks GD before CPL. Taking that #DB
  sets DR6.BD, clears B0-B3, bit 12 and BT, keeps BS, and clears GD. No
  breakpoint is armed: DR7's enables do nothing until slice 5.
- **Far CALL** in protected mode is one routine per form (`pcall`,
  `pcallm`). Its descriptor check holds the descriptor and says whether it
  is a call gate (`cg`); conditional uops (`xc`: direct, gate, gsame, xp,
  same) then run the direct, same-privilege-gate and inner-gate forms. The
  direct form and RETF to the same privilege end at their own jumps
  (`xend`), so they skip the other forms' uops.
- **Inner call gates.** The GATE uop splits the gate (T2, T3) into its
  selector, offset, size and parameter count; the CS checks are a
  delivery's (`CHK_INTCS`) and hold xp for a nonconforming CS below CPL;
  the new stack comes from the TSS with a delivery's SS checks. The stores
  follow Bochs's `call_gate`: the old SS, the old ESP (SP from a 16-bit
  stack), the parameters from the old stack's highest down, CS and the
  return EIP (IP from 16-bit code), then the offset's check against the
  new CS's limit, so a fault there leaves the stores written, as Bochs's
  are.
- **The parameter copy** is two ROM words, a load into T4 from the old
  stack and its store to the new one. `fes_i386_seq` steps the ROM address
  back from the store to the load until it has copied the gate's count
  (0-31), with the slots worked out as each word is expanded, off D's
  paths; at a count of 0 both words are NOPs.
- **RETF to an outer privilege** reads SS's and ESP's slots past imm16
  (`pimm`; a 16-bit stack's slot past 64 KiB is #SS(0), as Bochs reads it
  without the wrap), checks SS against the RPL as an outer IRET does, and
  loads SS, ESP (T5 plus imm16) and CS, nulling the data segments as an
  outer IRET does.
- **A dropped uop writes nothing.** A conditional uop its case drops was a
  NOP with its memory, register and checks cleared; it now also clears its
  hold, transfer, segment and ESP writes. With conditional jumps this
  mattered: the first run of RETF's reordered routine performed a dropped
  jump's transfer.
- **The ROM** grows from 256 to 512 words (277 used), 3 more M10K blocks,
  and its word from 111 to 117 bits. Slice 4's task switches needed the
  growth anyway.

**Cost.** A protected-mode far CALL and RETF pair at the same privilege
takes 10 more clocks than on main (the `callf-retf` case: 497 against 487;
`r3-callf` 901 against 891), from the hold and the offset's check on
RETF's path and the call-gate test on CALL's. IRET and deliveries are
unchanged (`r0-iretd-r3`, `r3-int-iretd` equal).

## Lockstep

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of 2,000
random programs, 16 seeds of 500 protected-mode programs, the full corpus) at
`9887e0543`, the sealed checkout, on the RTL and on the netlist from the i386
lock's Yosys (`238eac9a4`):

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 (8,102,938 retirements) | same |
| Protected-mode programs | 8,000 of 8,000 (462,989 retirements) | same |
| Unbuilt encodings | 49,760, each stopped | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 198, 54, 1,933 and 27, all pass | same |

Every corpus file's and random seed's retirement trace hash is the same on
both ([lockstep-report.json](lockstep-report.json),
[lockstep-netlist-report.json](lockstep-netlist-report.json)).

**The protected-mode programs** retired, with PE set: 1,180 MOV to or from
DRn, 369 far CALLs to an inner privilege, 289 far CALLs through a gate at
the same privilege, 1,031 RETFs to an outer privilege, 5,109 returns to an
outer privilege in all (IRET too), and 3,117 deliveries to an inner one.
The generator's call gate (descriptor A0h) is a 286 or 386 gate with 0-4
parameters (now and then up to 31), DPL 3 mostly, to a stub in 16- or
32-bit code at CPL 0 that RETFs past them; now and then not present, with a
null or data CS, or an offset past its CS's limit. Ring 3 is entered by
RETF a quarter of the time (by RETF imm16 now and then) and calls the gate
from there; CPL 0 calls it directly and through memory. MOV DRn reads any
register and writes random values, DR7's with no enable set and GD now and
then; at CPL 3 it raises #GP(0).

**Harness changes.** `MOV DRn` (0F 21, 0F 23) joins the lockstep's scope.
The state compare covers DR0-DR3, DR6 and DR7 (the oracle already held
them). The corpus lists DR6 and DR7 as 0; its states now take the 386's
fixed bits (DR6 FFFF0FF0h, DR7 400h), since no corpus instruction touches
them. `why_e` drops the reasons nothing raises any more (an inner
delivery, an outer return), so the reason numbers after them shift.

**New directed cases** (`FES_I386_MODE=pmode`, 198 cases, 51 new):
- RETF to CPL 3: `r0-retf-r3`, `r0-retf-r3-imm`, `r0-retfd-r3`,
  `r0-retfd-r3-imm`; SS null (#GP(0)), RPL not the new CPL (#GP(68h)), not
  present (#SS(28h)); `r0-retf-r3-ds-null` (DS nulled, ES at DPL 3 kept);
  `r0-retf-r3-wrap` (the SS slot past a 16-bit stack's top, #SS(0));
  `r3-retf-outer-rpl` (RPL below CPL, #GP(08h)).
- Call gates from CPL 3 to CPL 0, with the RETF back: 286 and 386 gates
  with 0 parameters (`r3-cg16-n0`, `r3-cg32-n0`), 2 and 3
  (`r3-cg16-n2`, `r3-cg32-n3`) and 31 (`r3-cg32-n31`); a 386 gate to
  16-bit code and a 286 one to 32-bit code (`r3-cg32-to16`,
  `r3-cg16-to32`); on a 32-bit SS0 (`r3-cg32-ss32`); through a 286 TSS
  (`r3-cg16-286tss`, `r3-cg32-286tss`); to DPL 1 on SS1 (`r3-cg16-dpl1`);
  a 386 gate from a 16-bit stack whose ESP has a high half
  (`r3-cg32-sp16`, which pushes SP).
- Call gates to the same privilege: from CPL 3 to conforming code
  (`r3-cg16-conforming`); from CPL 0 (`r0-cg32-same`, `r0-cgm32-same`
  through memory); SP 2, where the gate's pushes wrap and the RETF's read
  does not (`r0-cg16-same-wrap`, #SS(0)); the offset past the limit after
  the pushes (`r0-cg-same-off-past`).
- Each fault: the gate's DPL below CPL (`r3-cg-dpl0`) or RPL (`r0-cg-rpl`),
  #GP(88h); not present, #NP(88h); its CS null (#GP(0)), data (#GP(10h)),
  not present (#NP(58h)), DPL above CPL (`r0-cg-cs-dpl3`, #GP(70h)); a
  gate to DPL 1 whose SS1 is null (#TS(0)), not present (#SS(28h)), RPL 3
  (#TS(68h)), DPL 0 (#TS(48h)) or whose stack runs past its limit
  (#SS(80h)); SS0 null, whose #TS's own delivery shuts down
  (`r3-cg-ss0-null`); a parameter past the old stack's top
  (`r3-cg-param-ss`, #SS(0), after the old SS and ESP are written); the
  offset past the limit (`r3-cg-off-past`, #GP(0) after the stores).
- Debug registers: every DRn at CPL 0 (`dr-moves`) and in real mode
  (`real-dr-moves`), DR4 and DR5 included; DR6's writable bits
  (`dr6-bits`); GD's #DB on a read and a write (`dr-gd-read`,
  `dr-gd-write`), in real mode through the IVT (`real-dr-gd`), at CPL 3
  ahead of the #GP (`r3-dr-gd`), after an IRETD that set RF (`dr-gd-rf`:
  the frame's RF is clear); LOCK's #UD ahead of GD (`dr-gd-lock`); #GP(0)
  at CPL 3 (`r3-mov-dr7`, `r3-mov-dr0-write`).

**The cases catch broken RTL.** Seven mutants, each a one-line change,
each fail directed cases ([mutants.txt](mutants.txt)): parameters copied in
the wrong order (6 cases), SP not zero-extended (`r3-cg32-sp16`), DR6's
low bits from the wrong nibble (`dr6-bits`), GD after the CPL check
(`r3-dr-gd`), RF set in #DB's frame (4), RETF's SS slot without imm16 (11),
a not-present gate taken (`r3-cg-np`).

## test386.asm

`scripts/test_fes_i386_test386.py --cpu rtl` at `9887e0543`
([test386-rtl-report.json](test386-rtl-report.json)): POST codes 00, 01,
02, 03, 04, 05, 06, 08, 09, 20 and 21, no error, then a stop as not built
after 796,814 retirements at 00D0h:2D53h, an IRETD to V86 mode (POST 21,
slice 3). POST 20 runs its ring-3 code through `call RING0_GATE`.

## Seal

The seal is Quartus 17.0.2, seed 1, from clean checkout `9887e0543`
([seal-summary.json](seal-summary.json)).
- **Package.** `0d17486e…`, build `cf2d9203…`, with `core.rbf` `cad673cb…`.
- **Analyses.** Every one is met:
  - setup +0.645 ns;
  - hold +0.104 ns;
  - recovery +5.084 ns;
  - removal +0.417 ns;
  - minimum pulse width +1.122 ns.
- **Clocks.** The system clock is 61.44 MHz.
- **Resources.**

  | Resource | This seal | Main (#1587's seal `79c5009c`) |
  | --- | ---: | ---: |
  | ALMs | 36,683 | 36,225 |
  | Registers | 32,874 | 32,843 |
  | M10K blocks | 268 | 265 |
  | DSP blocks | 38 | 38 |

  The plan estimated about 350 ALMs for the debug registers and inner call
  gates; the slice takes 458, within the fit noise of about ±200 the area
  audit measured. 5,227 ALMs (12.5%) are left, 1,817 under the area
  audit's working ceiling of 38,500, for slices 3-5, which have about
  1,550 of the audit's 2,000 estimate for slices 2-5 left.

[stage-slack.json](stage-slack.json) gives each register group's worst setup
slack at the slow −40 °C corner. The worst path is D's load-forwarding
register (`dfwd_ld_ex`) into a D register Yosys left unnamed, the same
family as main's worst (into `r_dst`, +0.396), which this seal moves to
+1.518. D's EIP (+1.227) and `dc0` (+1.636) keep their margins.

## Kit

Runs `k1`, `m1`, `k2` and `k3` used Kit A, core-only, on 9 November from
17:37 to 18:28 ([kit-scores.json](kit-scores.json)).
- **Package.** `0d17486e…`, the seal above, at max (preset 0), each launch
  after a SCRUBX scrub. Run `m1` is the A/B reference: main's #1587 seal
  `79c5009c…`, the same runner, kit and baseline.
- **Kit.** Baseline image `cd284ca3…` and software `66316149…` (the kits'
  9 November baseline), with no install, no image change and no reboot.
  After each run Kit A was checked against its snapshot and logged FREE,
  and the run's FogCast data trees were removed and logged.
- **Disks.** The Phase 4 bench disk (`fe81c9c4…`) and the #1533 game drives
  `dpmi` (`48ab2972…`), `real` (`719dfedb…`), `pmode` (`75d909ca…`) and
  `fpu` (`152c83df…`), from Powerboat by SHA-256. None is committed.

| Benchmark | Main (#1587's seal, m1) | This slice (k1) |
| --- | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,378 realtics, 40.03 fps | 3,863 in 3,378, 40.03 fps |
| Pack DOOM, maximum detail | 2,134 in 2,062, 36.2 fps | 2,134 in 2,062, 36.2 fps |
| Pack DOOM, minimum detail | — | 2,134 in 748, 99.9 fps |
| 3DBench | 76.9 | 76.9 |
| PCPBench | — | 13.3 |
| TopBench | 306 | 304 |

#1587's own record had demo3 at 40.05 fps (3,376 realtics) on the kits'
earlier baseline; today both packages give 3,378. TopBench varies by a few
points between runs of one package (main read 309 and 310 in earlier
records); its parts differ by 1-3 µs either way here.

| Title | Drive | Result |
| --- | --- | --- |
| U.S. Navy Fighters, newly unlocked | `fpu` | Past its extender (IRET to CPL 3, a call gate back to CPL 0, MOV DRn; the machine sim below runs it on the RTL), the title stops at its CD-ROM check: "Could not find US Navy Fighters CD. Please ensure that the US Navy Fighters CD is in the drive, and that you have loaded the MSCDEX driver." |
| Time Circle (32RTM) | `dpmi` | Its main menu (1 PLAYER, 2 PLAYERS, setup, EXIT) |
| Wolfenstein 3D | `real` | A new game, in E1M1 |
| Ultima VI | `real` | Its introduction plays |
| DOOM v1.9 | `pmode` | Its title and demos play |

Each title was launched and watched for 100-180 s; this is a recheck that
the slice leaves them running, not #1368's full acceptance.

## Machine sim

`sim_fes_pc386.py --suite game --game ID --cpu rtl --rtl-at-prompt
--limit-seconds 20` with this branch's RTL: the RTL takes over at the DOS
prompt.

| Title | Instructions on the RTL | With PE set | Protected-mode entries | Handed to Bochs | Ends |
| --- | ---: | ---: | ---: | ---: | --- |
| U.S. Navy Fighters ([report](machine-usnf-rtl-report.json)) | 78,491,184 | 568,281 | 318 | 0 | At the prompt after its CD-ROM message, 7.45 simulated seconds |

Outstanding: Jump 'n Bump and Angband (CWSDPMI), whose first stop on main's
RTL was a MOV DRn, are running on this RTL; their rows follow in a later
commit. Time Circle is not affected by this slice (slice 1's record and
the kit run above cover it); its 20-second RTL run was stopped at 4
simulated seconds to free Powerboat for them.

## Changes after the seal

The seal, lockstep and test386 runs are from `9887e0543`. After it:
- **Main merged in** (`770dbb1e8`): no CPU file changed; the KF8253 fix
  (#1549) changes the machine's PIT, under the pc386 reseal exemption.
- **The governor's costs for MOV DRn** (`d718e66ff`): CI's check that
  every built encoding has a governor class found 0F 21 and 0F 23 without
  one. They take the 386's 22 clocks (DR0-DR3) and 14 (DR6, DR7) and the
  i486's 9 and 10; the governor's ROM changes only at the presets with a
  table, not at max, which every run above used.

## What this record does not cover

- **Far JMP through a call gate** stays not built; the random programs do
  not jump through their gate.
- **A DR7 write does not flush the TLBs**, where Bochs's does (to arm its
  breakpoints). A 386's does not either. Only software that changes a page
  table without flushing and then writes DR7 could see the difference; the
  lockstep has none.
- **Breakpoints, single step, the T bit** (slice 5).
- **A call gate's EIP past FFFFh in 16-bit code** (IP stored, `zx`): no
  directed case runs 16-bit code past 64 KiB into a 386 gate.
- **nextpnr.** No route was made (fes#1235: Quartus at its best is the i386
  reference until the CPU works).

## Commands

From `sources/misteross`:

```sh
python3 scripts/test_fes_i386_lockstep.py --jobs 12
python3 scripts/test_fes_i386_lockstep.py --jobs 12 --yosys <i386 lock's yosys 238eac9a4>
FES_I386_MODE=pmode build/sim/fes-i386-lockstep/rtl/Vfes_i386_cpu   # the protected-mode cases alone
python3 scripts/test_fes_i386_test386.py --cpu rtl --no-cross-check
python3 scripts/build_fes_pc386_quartus.py --root "$PWD" --cache-root "$CACHE_ROOT" --seed 1
python3 scripts/sim_fes_pc386.py --suite game --game usnf --cpu rtl --rtl-at-prompt --limit-seconds 20
```

The seal ran with `CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains`
and `QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus`. Both locks'
toolchain slots had been pruned from that cache and were rebuilt with
`scripts/bootstrap.sh` (the pc386 lock's with `FES_TOOLCHAIN_GPU_ROUTER=HIP`,
the producer's key). The kit runs used scratch runners on Powerboat
(`~/fes-scratch/p5dr/kit`), which are not committed.
