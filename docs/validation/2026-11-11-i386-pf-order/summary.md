# i386 fault order under paging: XCHG m and CALL rel, 11 November 2026

This record covers two RTL fixes found by Phase 5 slice 3's lockstep
(tracker [#1548](https://github.com/DeanoC/fes/issues/1548)). Both
failures were already present on main before slice 3:
- XCHG m's load was checked against its segment as a read
  ([#1644](https://github.com/DeanoC/fes/issues/1644)).
- A CALL rel push that faults under paging never raised its #PF
  ([#1646](https://github.com/DeanoC/fes/issues/1646)).

**Result:**
- **Both issue repros pass.** The seeds in the issues pass vacuously on this
  branch's testbench, because #1651's generator change, merged with main
  into slice 3, draws other programs from them. Both seeds fail on main's
  RTL and pass on this RTL when run on a testbench built from slice 3's
  `lockstep_tb.cpp` before that merge (`111687d80`):
  - `random#1004#129` (PM 30): `@f98.vector d e`, then pass.
  - `random#14#622` (PM 2): `@c26.cr2 6cc4 0`, then pass.
- **Directed cases.** Five new paging cases (64 → 69); one-line mutants of
  the three changes fail them ([mutants.txt](mutants.txt)).
- **Lockstep.** Everything passes on the RTL and on the netlist from the
  i386 lock's Yosys, with identical trace hashes, except
  `random#1010#118`. That is [#1663](https://github.com/DeanoC/fes/issues/1663),
  the known failure slice 3 left, and it fails the same way.
- **test386** on the RTL reaches POST 22 and stops at `int 0x28`, a
  delivery through a task gate (slice 4), as on main.
- **Seal.** Quartus 17.0.2 seed 1 from a clean checkout of `5445dae5d`
  meets every analysis: setup +0.673 ns, hold +0.085 ns. It uses 36,139
  ALMs (main 36,398, −259: fit noise) and 268 M10K (main 268).

## Causes and changes

### #1644: XCHG m

`SEQ_XCHGM`'s first uop loads the operand into T1, and its second stores
the register there. D checked the load against the segment as a read
(`seg_rbad`). The paging slice already translated that load as a write
(`wx`). So through a DS its store cannot write, but that reads (a readable
code segment, read-only data), the load got as far as its page walk. A
page not present then raised #PF (error 2) before the store's #GP(0).

Bochs reads XCHG's operand with `read_RMW_virtual_*`, which checks the
segment for a write first. The load now takes the store's write check
(`xchg_ld`, the same `seg_wbad[mseg]` term), so #GP(0) (#SS(0) for SS)
comes before any walk. `wx` reuses `xchg_ld`.

**The 386.** Evidence from the die-extracted microcode
([reenigne/x86_microcode](https://github.com/reenigne/x86_microcode),
`80386/microcode_notes.txt`) is suggestive, not conclusive:
- XCHG rm,r's memory form reads at `0B1` with a bus operation of its own,
  `rd` (0x06) rather than `RD`. `fields.txt` takes `rd` to assert LOCK#,
  and it is also the read of the TSS busy bit's locked update at `78E`.
- The notes do not decode whether that cycle checks the segment as a
  write.
- The `9` on its entry point (90B1h) marks the LOCK-legal forms (ALU m but
  CMP, INC/DEC/NOT/NEG m, BTS/BTR/BTC m, XCHG m), not a write check:
  shifts, also read-modify-write, carry `1`.

The change makes the load's segment check agree with the page check the
RTL already made for it, as Bochs's does.

**ARPL m does not share the order on the 386.** `ARPL mw` (16AAh) reads
with a plain `RD_W` at `6AA` and writes `WR_R` at `6B0` only on the path
that raises the RPL. `ARPL_FAILED` (`6B3`) clears ZF with no bus cycle.
Its segment check stays a read's, as divergence `arpl-no-write-no-fault`
already has it. The RTL still translates its load as a write under paging
(`wx`), which follows Bochs, not the microcode. That is
[#1665](https://github.com/DeanoC/fes/issues/1665) and is not changed
here.

### #1646: CALL rel's push

The issue saw CR2 stay 0 at the shutdown. The cause was not the CR2 write
(`exc_take` and `wb_shut` write it for any #PF). The RTL never raised the
#PF:
- M1 checks CALL rel's target against CS's limit on its push (`tchk`). A
  target past the limit set the uop's fault to #GP(0) there.
- M1 starts no page walk for a uop with a fault, so the push's walk, which
  would have refused the page, never ran.
- The #GP(0)'s delivery then failed through the program's IDT to a
  shutdown with CR2 0. In the repro CS's limit was C2Ch and the target
  C30h.
- Bochs pushes first: #PF (error 2, CR2 6CC4h), then #GP(73h), #DF,
  #GP(43h) and shutdown.

The 386 pushes before it checks too. Near CALL's microcode writes the
return address (`07E OPR_W = EIP; WR`) before `DESCOD` takes the target,
and CALL rel's 075–078 do the same.

The push now translates as a `dfl` uop does. That mechanism already let
a st2 store's later segment fault yield to its load's #PF. M1 sets `dfl`
for a push whose target is past the limit (not for a far transfer's
`cs_late`), so M1 asks the walker, and M2's #PF replaces the #GP(0).

The walker runs only for the oldest uop, and otherwise replays it.
M2 honoured that replay only for a uop with no fault, so a `dfl` uop
behind older work took its later fault instead. The repro's push was such
a uop. A `dfl` uop now replays too, and the instruction comes back alone
as the oldest and walks. This also covers st2 uops, which had the same
gap.

**What it leaves to #1510.** CALL's order is otherwise unchanged. A push
that does not fault still writes nothing before its #GP(0), and the 386
and Bochs write the slot first. That is #1510's change after #1645, so
the present-page case of a CALL past CS's limit under paging belongs with
it.

## Directed cases

`FES_I386_MODE=paging`, now 69 cases. The paging GDT gains two
descriptors: 50h, 16-bit code at DPL 3 (base 0, limit FFFFh), and 58h,
flat read-only data.
- `pg-xchg-code-ds-np`: DS 18h (readable code), `XCHG [10004h], EAX` on a
  page not present: #GP(0), error 0.
- `pg-xchg-ro-ds-np`: DS 58h, `XCHG [10004h], BL`: #GP(0).
- `pg-xchg-np`: flat writable DS: #PF, error 2 (W/R set), CR2 10004h.
- `pg-r3-call-past-limit-np`: at CPL 3, a far jump to 53h, ESP 10008h,
  `CALL rel32` to 20000h past the limit, the push's page not present: #PF
  delivered on the TSS's stack, error 6, CR2 10004h.
- `pg-r3-call-past-limit-ro`: the same with the page read-only at user
  level: #PF, error 7.

The mutants are in [mutants.txt](mutants.txt): each of the three RTL
changes reverted, and XCHG m's load translated as a read. Each fails only
its own cases.

## Lockstep

`scripts/test_fes_i386_lockstep.py --jobs 16` ran at `88b17a34b`, the RTL
and testbench of the PR head `5445dae5d`. The main merge that follows
changes nothing under `sources/misteross`. It ran on the RTL and on the netlist from the i386 lock's Yosys (slot
`44e245aa…`), with the lock's Verilator 5.054:

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 (7,977,455 retirements) | same |
| Protected-mode programs | 7,999 of 8,000 (432,655 retirements) | same |
| Unbuilt encodings | 49,760, each stopped | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 275, 69, 1,933 and 27, all pass | same |

The one failure, on both, is `random#1010#118` (#1663: Bochs nests a
handler's first-fetch #PF inside the delivery's step), as on slice 3. Every
trace hash is the same on the RTL and the netlist: the 941 corpus files,
16 random seeds, 16 protected-mode seeds and each directed set
([lockstep-report.json](lockstep-report.json),
[lockstep-netlist-report.json](lockstep-netlist-report.json)). The
retirement counts equal slice 3's merged run, so the current generator
draws none of the changed paths. The directed cases and the pre-merge
testbench's repros are what exercise them.

## test386

`scripts/test_fes_i386_test386.py --cpu rtl`: POST 00-09, 20, 21, 22, and
the stop at `int 0x28` (E0A18h, a task gate) with 799,276 retirements and
no errors, as on main ([test386-rtl-report.json](test386-rtl-report.json)).

## Seal

Quartus 17.0.2 Lite, seed 1, from a clean checkout of `5445dae5d`
([seal-summary.json](seal-summary.json)):
- **Package.** `e46879fd…`, build `ab8a8204…`, `core.rbf` `4be3b63f…`.
- **Analyses.** All met: setup +0.673 ns, hold +0.085 ns, recovery +6.253
  ns, removal +0.274 ns, minimum pulse width +1.122 ns. Clocks: system
  61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz.
- **Resources.**

  | Resource | This seal | Main (slice 3's seal `111687d80`) |
  | --- | ---: | ---: |
  | ALMs | 36,139 | 36,398 |
  | Registers | 32,531 | 32,797 |
  | M10K blocks | 268 | 268 |
  | DSP blocks | 38 | 38 |

An earlier seed-1 build of `0ecbf9c34` (the RTL change alone) finished
Quartus with every analysis met (setup +0.736 ns, hold +0.095 ns). It
published nothing, because the sealer refused it: "source identity changed
during build", the checkout having moved under it. The seal above was then
built from its own clean worktree.

No kit run: the seal shows no significant change in timing or area.
