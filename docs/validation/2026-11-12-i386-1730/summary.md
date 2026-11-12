# i386 ICEBP, UMOV and MOV TR4-TR7, 12 November 2026

This record covers [#1730](https://github.com/DeanoC/fes/issues/1730).
#1701 ([record](../2026-11-12-i386-ud-1701/summary.md)) left four
encodings that the 386 defines stopping `fes_i386_cpu` as not built:
- ICEBP (F1h);
- LOADALL386 (0F 07);
- UMOV (0F 10-13);
- MOV to and from TR4-TR7 (0F 24, 0F 26).

This change builds ICEBP, UMOV and MOV TR4-TR7. LOADALL386 is split out to
[#1762](https://github.com/DeanoC/fes/issues/1762) and still stops as not
built.

**Result:**
- **Lockstep.** At `97c61954d`, the RTL and the netlist from the i386
  lock's Yosys pass everything, with identical trace hashes (1,021 of
  1,021): [lockstep-report.json](lockstep-report.json),
  [lockstep-netlist-report.json](lockstep-netlist-report.json). Its RTL and
  testbench are this branch's; later commits change only docs and the
  runner's reporting.
  - The corpus: 1,758,699 states.
  - The random, protected-mode, task-switch, debug and undefined-encoding
    sets, all as on main.
  - The unbuilt sweep: 53,008 encodings.
  - 726 protected-mode cases (on main, 620) and 148 paging cases (on main,
    141).
- **Mutants.** All twelve one-line mutants of the new RTL fail #1730's
  directed cases ([mutants.txt](mutants.txt)).
- **test386.** The RTL passes every POST to FFh, with 79,640,765
  retirements, as on main ([test386-rtl-report.json](test386-rtl-report.json)).
- **Seal.** Quartus 17.0.2, seed 1, on chip-30x0-ai-dev with `--gpu-router
  CUDA`:

  | Commit | Setup | Hold | ALMs | M10K | Summary |
  | --- | --- | --- | --- | --- | --- |
  | `24f645ba8` | +0.781 ns | +0.097 ns | 36,981 | 269 | [seal-summary.json](seal-summary.json) |
  | main `7e08be726`, same host | +0.375 ns | +0.057 ns | 36,694 | 269 | [seal-summary-main.json](seal-summary-main.json) |

  Every analysis met on both. Main's seal is the pc386 Windows session's of
  the same commit, flags and host. This session's own seal of main crashed
  in Quartus's fitter (a segment violation while chip was loaded), not in
  timing.
- **Kit.** Kit B at max, DOOM v1.9 `-timedemo demo3`, 3863 gametics, twice
  per package ([kit.json](kit.json)):
  - this branch: 3378 and 3377 realtics (40.03 and 40.04 fps);
  - main: 3377 and 3376 realtics.

## What the 386 does

Rule (Deano, 10 November): where the RTL and Bochs differ, the original 386
wins. The source is the die-extracted decoder and microcode in
[reenigne/x86_microcode](https://github.com/reenigne/x86_microcode)
`80386/`:
- `decoder23.txt` and `decode2.txt`: the decode;
- `microcode_notes.txt`: the routines.

| Encoding | 386 | Bochs 3.1, CPU level 3 | FES now |
| --- | --- | --- | --- |
| ICEBP (F1h) | Detailed below the table | INT1: interrupt() as a privileged software interrupt (EXT 1, no IOPL or DPL check); leaves GD | INT 1 through INT n's routines as an exception's delivery; GD cleared |
| UMOV (0F 10-13) | decoder23 "UMOV rm,r" and "UMOV r,rm" take MOV's entries (0A, 0B); decode2 28-2F, register and memory, are MOV's (0003, 1013, 1019) | #UD | MOV (88h-8Bh) |
| MOV TR4-TR7 (0F 24, 0F 26) | Detailed below the table | #UD (MOV_RdTd and MOV_TdRd not implemented) | Four registers that keep what is written; #GP(0) above CPL 0 |
| LOADALL386 (0F 07) | 8F6-93B, CPL 0 only | #UD | Stops as not built (#1762) |

**ICEBP on the 386.**
- 93C goes to BREAKPOINT_CMN (943). That routine clears DR7.GD (94C) and
  enters INTERRUPT_X, an exception's delivery (89E). INTERRUPT_HW is set, so
  the gate gets TST_DES_INT_HW, with no DPL check.
- No IOPL check in V86 mode.
- No DR6 bit.
- EIP is not rolled back, so the frame holds the next instruction's EIP: a
  trap.

**MOV TR4-TR7 on the 386.**
- decode2 49 and 4B (MOV TRn, r32, register and memory form) go to 391, and
  5D and 5F (MOV r32, TRn) go to 3B6, for TR4-TR7; TR0-TR3 go to 082B (#UD).
- 391 and 3B6 raise #GP(0) in protected mode above CPL 0, and call no
  GENERAL_DETECT.

### ICEBP

ICEBP decodes as INT3's first uop with vector 1, and runs INT n's routines:
UC_INT in real mode, UC_PINT in protected mode, UC_VINT in V86 mode. Each
of its uops carries `ice`:
- **Gate.** CHK_GATE checks its gate as an exception's (no DPL check against
  CPL).
- **EXT.** A fault in its delivery has EXT set in its error code
  (`wb_nested`), as Bochs's INT1 sets it.
- **GD.** Its first uop clears DR7.GD as it reaches WB, whether it commits or
  faults, as the 386 clears GD before INTERRUPT_X reads the gate.
- **V86.** It has no IOPL check there. Only CDh is IOPL-sensitive.
- **Real mode.** It pushes 16-bit slots whatever the operand size, as
  INT n does.

A delivery's far jump is never predicted, so what follows ICEBP decodes
with GD clear. With TF set, its frame holds TF and no single-step trap
follows, as for INT n on both sides.

**Not established.**
- **EXT.** Whether the 386 sets EXT for ICEBP is not visible in the
  microcode; FES follows Bochs (EXT set).
- **DR7 bit 12.** On the 386 it sends any #DB, ICEBP's included, to an ICE
  (946-949). FES has no ICE and ignores the bit.
- **RF.** ICEBP's frame has RF as INT n's does, which matches Bochs.

### UMOV

DC decodes 0F 10-13 through the one-byte table as 88h-8Bh (`b0m`), and the
length decoder gives them MOV's ModRM length. Outside ICE mode the 386 runs
MOV's own microcode for them; its ICE-space bus cycles need ICE mode, which
FES does not have. LOCK is #UD, as on MOV.

### MOV TR4-TR7

0F 24 and 0F 26 with reg 4-7 run with either ModRM form, the ModRM a
register form whatever its mod (the CRn and DRn rule):
- **#1728's #UD.** #1728 made the memory forms #UD. decode2's 4B and 5F, the
  0Fh memory-form entries, route them to 391 and 3B6 as the register forms,
  so this change runs them.
- **Read.** MOV r32, TRn reads a register through `b_sys` (SYS_RD_TR4-TR7).
- **Write.** MOV TRn, r32 writes through UC_WDR's SYSOP_DR with imm bit 3
  set.
- **Privilege.** Both are #GP(0) above CPL 0 (`priv_op`) and check no GD.
- **TLB test.** TR6's TLB test command is not built: the design's recorded
  deviation, now covering TR4 and TR5 too. What the 386's TR4 and TR5 hold is
  not known.

## Checks

### Lockstep rules (divergences.toml `[[lockstep]]`)

| Rule | What the oracle steps | Counted in the directed cases |
| --- | --- | --- |
| `umov` | The MOV, a REP prefix in the 0Fh's place (same length; neither CPU heeds REP on MOV) | 67 |
| `mov-tr` | At CPL 0, MOV of the r/m register to itself (F3 89 /r), with the testbench's copy of TR4-TR7 giving a read's result or taking a write's value. Above CPL 0 (V86 mode included), MOV r32, CR0 with the same r/m, whose #GP(0) Bochs raises before any operand | 54 |
| `icebp-gd` | ICEBP as written; GD is then cleared on the oracle when its step started INT 1's delivery | 3 |

The runner fails if any rule goes untaken.

### Directed cases

**Protected-mode cases (`FES_I386_MODE=pmode`).** 15 bodies, each run in
real mode, at CPL 0 and 3, and in V86 mode at IOPL 0 and 3:
- ICEBP: plain, prefixed, and after MOV SS.
- UMOV: each of its four forms by register, stores and loads by memory, and
  with prefixes.
- MOV TRn:
  - TR4-TR7 written and read back;
  - TR6's reset value;
  - the memory ModRM forms;
  - with prefixes.

Above CPL 0, the UMOV and MOV TRn bodies end in #GP(0).

Besides those:
- ICEBP with TF set in the four modes where POPF can set it.
- ICEBP with gate 1 not present: #NP(0Bh), at CPL 0 and 3. INT 1 gives
  #NP(0Ah) there.
- ICEBP through a data descriptor: #GP(0Bh).
- ICEBP with GD set: in real mode, protected mode, and with a not-present
  gate.
- UMOV's #GP(0) and #SS(0) at offset FFFFh, at CPL 0 and 3.
- MOV TRn with GD set: no #DB.
- LOCK on ICEBP, on UMOV with memory and on MOV TR6, all #UD. These run with
  #1701's probes, in the five modes and in V86 mode under paging.

**Paging cases (`FES_I386_MODE=paging`).**
- UMOV's store and load to a page that is not present, at CPL 0 and 3: #PF
  with MOV's error code and CR2.

**Unbuilt sweep (`FES_I386_MODE=unbuilt`).**
- `i386_class` classes F1, 0F 10-13 and 0F 24/26 reg 4-7 as defined. These
  are in SCOPE, which leaves 0F 07 the only `stop`.
- Every #1730 encoding still runs with a LOCK prefix as a `ud` line against
  the oracle.

## Runs

- **Commits.** Branch `deano/i386-1730-192351`:
  - `97c61954d`: RTL, testbench and runner;
  - `24f645ba8`: runner reporting and docs;
  - this record.
- **Lockstep.** RTL, then the netlist (`--yosys`, the lock's Yosys
  `0a7927bf`), at `97c61954d` on Powerboat, `--jobs 12`.
- **test386.** `scripts/test_fes_i386_test386.py --cpu rtl` at `24f645ba8`.
- **Mutants.** Built from scratch copies of `97c61954d`'s RTL, each run
  through the pmode and paging cases.
- **Seals.**
  - `scripts/build_fes_pc386_quartus.py --seed 1 --gpu-router CUDA` on
    chip-30x0-ai-dev, at `24f645ba8` (package `eee4bf3c`).
  - Main `7e08be726` (package `27f478e0`) the same way.
- **Kit B, at max.** Core-only on baseline software 7ddb0bfb, image
  179affbb. Runs `b1` (this branch) and `m1` (main). The Phase 4 bench
  disk's DOOM v1.9 `-timedemo demo3` ran twice per package. After each run
  its FogCast data trees were removed and the kit released.
