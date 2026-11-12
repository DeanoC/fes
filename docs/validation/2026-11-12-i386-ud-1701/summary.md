# i386 #UD for every encoding the 386 rejects, 12 November 2026

This record covers [#1701](https://github.com/DeanoC/fes/issues/1701).
`fes_i386_cpu` decoded every encoding it did not list as
`FAULT_UNIMPLEMENTED`, which stops the CPU. Windows for Workgroups 3.11
stops there twice on main's RTL: at WIN386's `0F A6` probe in real mode,
and at KRNL386's `0F FF` at CPL 3 under paging
([early-Windows plan](../2026-11-11-pc386-windows-plan/summary.md)). A 386
raises #UD (vector 6, a fault with no error code) for both.

**Result:**
- **What changed.** Every encoding the 386's decoder rejects now raises #UD,
  in real mode, V86 mode and protected mode at any CPL, before any operand
  access and before a LOCK's or IOPL's check. The change is in
  `fes_i386_decode.sv`: the 0Fh map's default arm, FE /2-/7, FF /7,
  0F BA /0-/3 and MOV of TR0-TR3. The other rejections (LOCK, MOV to CS,
  the memory-only forms with a register, C6h/C7h and 8Fh with reg not 0,
  group 6 /6 /7, group 7 /5 /7, CR1 and CR4-CR7) already raised #UD.
- **Still not built.** The 386 defines four encodings this build does not run:
  ICEBP (F1h), LOADALL (0F 07), UMOV (0F 10-0F 13) and MOV to and from
  TR4-TR7. They still stop as not built (below).
- **Lockstep.** The RTL and the netlist from the i386 lock's Yosys pass with
  identical trace hashes. This run has #1705 merged in:
  [lockstep-report.json](lockstep-report.json),
  [lockstep-netlist-report.json](lockstep-netlist-report.json). Without it,
  random programs fail only at #1704 (main's open NMI-after-IRET bug); see
  [lockstep-without-1705.json](lockstep-without-1705.json).
- **New checks.**
  - An exhaustive sweep of every opcode and following byte: 52,496 encodings.
  - 210 directed protected-mode cases and 58 paging cases.
  - 4,000 random programs with undefined encodings in every mode, 232 of
    them ending in a #UD compared against Bochs.
- **Mutants.** All seven one-line mutants of the new decode fail
  ([mutants.txt](mutants.txt)).
- **test386** on the RTL passes every POST to FFh (79,640,765 retirements,
  as on main): [test386-rtl-report.json](test386-rtl-report.json).
- **Seal.** Quartus 17.0.2, seed 1, on chip-30x0-ai-dev with `--gpu-router
  CUDA`, at `27decb7ad`: every analysis met, setup +0.519 ns, hold
  +0.101 ns, 36,216 ALMs, 269 M10K ([seal-summary.json](seal-summary.json)).
  Main `78b7ee4b7` on the same host: setup +0.268 ns, hold +0.100 ns, 36,350
  ALMs, 269 M10K ([seal-summary-main.json](seal-summary-main.json)).
- **Kit.** Kit B at max: DOOM v1.9 `-timedemo demo3` timed 3863 gametics in
  3376 realtics (40.05 fps) on this branch's package, twice; main's, run the
  same way right after, 3373 (40.08 fps), twice. −0.09%, within the spread
  between packages slice 5 saw on this kit (40.06 and 40.07)
  ([kit.json](kit.json)).
- **Windows.** WFW_LINE

## Which encodings the 386 rejects

Rule (Deano, 10 November): where the RTL and Bochs differ, the original 386
wins. The source here is the 386's own decoder: the die-extracted decode
ROMs in [reenigne/x86_microcode](https://github.com/reenigne/x86_microcode)
`80386/`.
- `decoder23.txt` maps each opcode, with the 0Fh flag and a protected-mode
  bit, to an entry. ModRM-dependent rows add the reg field and the
  register/memory form.
- `decode2.txt` holds the per-reg entries.
- Entry `082B` is the illegal one: `microcode_notes.txt` 82B is
  `#UD: goto FAULT`, with 82C `SIGMA = 6`.

Read row by row, the ROM gives:

| Encoding | 386 decoder | Bochs 3.1, CPU level 3 | FES before | FES now |
| --- | --- | --- | --- | --- |
| Every one-byte opcode | defined (D6h SALC and F1h ICEBP included) | defined | built but F1h | unchanged |
| 0F 04, 05, 08-0F, 14-1F, 25, 27-7F, A2, AA, AE, B0, B1, B8, B9, C0-FF | #UD (082B) | #UD, but 0F 0D (NOP Ev) | stop | #UD |
| 0F A6, 0F A7 (XBTS, IBTS) | #UD for every reg (`decode2` 91-97) | #UD | stop | #UD |
| 0F 07 (LOADALL386) | 8F6, CPL 0 only | #UD | stop | stop (not built) |
| 0F 10-13 (UMOV) | MOV's entries | #UD | stop | stop (not built) |
| 0F 24, 0F 26 (MOV TRn) | TR0-TR3 #UD; TR4-TR7 391/3B6, CPL 0 | #UD | stop | TR0-TR3 #UD; TR4-TR7 stop |
| F1h (ICEBP) | 93C, INT 1 through BREAKPOINT_CMN | INT1 | stop | stop (not built) |
| FE /2-/7, FF /7, 0F BA /0-/3 | #UD | #UD | stop | #UD |
| 0F 01 /7 by memory | #UD | INVLPG | #UD | #UD |
| 0F 38, 0F 3A | #UD | its step dereferences null | stop | #UD |

The 386 manual documents only TR6 and TR7. The ROM routes TR4-TR7 to the
same routine and TR0-TR3 to #UD.

**XBTS and IBTS.** They were documented on the early (B-step) 386 and
removed later. The extracted die's decoder still lists them, but
`decode2.txt` sends every one of their reg fields to 082B. So this die, like
every later stepping, raises #UD, and so does Bochs. WIN386's probe expects
that.

**Divergences.** Two are recorded in `divergences.toml` as `[[lockstep]]`
entries, the 386 followed:
- `ud-0f0d`: Bochs runs 0F 0D as NOP Ev.
- `ud-invlpg`: Bochs runs 0F 01 /7 with a memory operand as INVLPG, the
  486's instruction.

The runner reads their `encodings`. It checks that the RTL alone raises #UD,
and that Bochs's decode still runs them, or the entry no longer matches.
Bochs's 0F 38 and 0F 3A crash is an oracle limitation, not a divergence. The
sim README records it.

**Not built, with follow-ups.** No software the census or Windows uses runs
them:
- ICEBP: Bochs runs it as INT 1 too, so it can be lockstepped once built.
- LOADALL386 at CPL 0.
- UMOV, which runs as MOV on a 386 outside ICE mode.
- MOV to and from TR4-TR7: the design already names TR6/TR7 as accepting
  MOV.

### Edges checked

- **CS's limit.** An undefined opcode whose next byte lies past CS's limit
  was probed against Bochs (scratch cases, not kept). Where the RTL's length
  decoder gives the encoding a ModRM (FE, 0F BA, 0F B9, 0F 24), both raise
  #GP(0) for the fetch. Where it does not (0F FF, 0F A6, 0F 0B, 0F 05,
  0F C8), both raise #UD. The RTL and Bochs agree, so nothing changed.
  - For XBTS the 386 ROM decides after the ModRM. A real 386 may therefore
    fetch that byte first and raise #GP(0) at such a limit, where FES and
    Bochs raise #UD. No source settles the 386's prefetch order here, and
    no software is known to depend on it.
- **Memory operands.** A rejected encoding with a memory operand on a page
  not present raises #UD with no walk and no #PF (paging cases
  `pg-ud-r0-*`, `pg-ud-r3-*`).
- **Over 15 bytes.** #UD keeps its priority over the 15-byte #GP(0), as LOCK's
  already did (divergence `lock-ud-before-length-gp`, the 386EX's order).
  The random programs stay under 15 bytes.

## Checks

### The unbuilt sweep (`FES_I386_MODE=unbuilt`)

The runner classes every one-byte and 0Fh opcode, with every following byte,
by the ROM tables above (`i386_class`):

| Kind | Encodings | Check |
| --- | --- | --- |
| `ud` | 49,912 | #UD in lockstep with Bochs in real mode from 1000:0100: the delivery, the pushed IP and SP, the handler's HLT |
| `ud-rtl` | 512 (0F 38, 0F 3A) | #UD on the RTL alone, first event, nothing retired |
| `ud-rtl-runs` | 280 (0F 0D, 0F 01 /7 by memory) | as `ud-rtl`, and Bochs's decode must still run them |
| `stop` | 1,792 (F1, 0F 07, 0F 10-13, 0F 24/26 reg 4-7) | FAULT_UNIMPLEMENTED before anything retires |

All 52,496 pass. The real-mode-only #UDs of group 6, LAR, LSL and ARPL are
classed `ud` too. The runner fails if the 386 defines an encoding that SCOPE
leaves out and the list does not name.

### Directed cases

- **Protected-mode cases.** 42 probes, each run in real mode, at CPL 0, at
  CPL 3, and in V86 mode at IOPL 0 and at IOPL 3 (210 cases; 350 → 560):
  - undefined 0Fh opcodes, XBTS and IBTS by register and memory;
  - FE /2 and /7 by memory, FF /3, /5 and /7 by register;
  - LES, LDS, LEA, BOUND, LSS and LFS of a register;
  - group 7 /5, /7, SGDT and LIDT by register; group 6 /6;
  - 0F BA /0 and /3 by memory;
  - CR1, CR4, TR0 and TR3;
  - 8F /1, C6 /1, C7 /7 by memory, MOV of segment register 6;
  - LOCK on 0F FF and on FE /2 by memory;
  - a prefixed XBTS and REP 0F 0B.
- **Paging cases.** Eight 32-bit memory-form probes at CPL 0 and 3 with the
  operand's page not present, plus the 42 probes in V86 mode under paging
  (58 cases; 77 → 135).

### Random programs (`--undefined-programs`, seeds 7001-7016)

Protected-mode programs (30 blocks per hundred items) in which, from the
generator's side stream, an encoding the 386 rejects now and then stands in
for an instruction. The program ends at its #UD, which is compared as a
delivery. 16 × 250 programs. The #UDs came from real mode, both CPLs and
V86 mode, alongside the programs' own faults.

## Runs

- **Commits.** Branch `deano/i386-ud-1701`: `6269cb133`, `fb20aa113`,
  `27decb7ad`.
- **Lockstep.**
  - RTL and netlist at the scratch merge `35b2cc488`: this branch plus
    #1705's `95506e54d`. Only `PMODE_CASES` needed a hand merge (352 + 210).
    The scratch merge is not pushed.
  - Without #1705 (`fb20aa113`): corpus, directed cases and sweep pass; the
    random sets fail only at #1704.
- **test386** at `fb20aa113`.
- **Mutants** of `fb20aa113`'s decode.
- **Seals** on chip-30x0-ai-dev with `--gpu-router CUDA`, seed 1:
  - this branch at `27decb7ad`;
  - main at `78b7ee4b7`, the comparison.
- **Kit B, at max** (core-only on baseline software 7ddb0bfb, restored and
  released). The Phase 4 bench disk's DOOM v1.9 `-timedemo demo3` runs twice
  per package: run `u1` this branch, run `m1` main.
- **Windows.** The `wfw-386` launch on the `win-msdos` drive. The machine
  bench was built on Powerboat and run on chip-30x0-ai-dev, with
  `--cpu rtl --rtl-at-prompt 1`, as the plan record's `wfw-rtl`. No Windows
  bytes, frames or files are stored here.
