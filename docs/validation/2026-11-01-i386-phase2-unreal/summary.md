# i386 Phase 2, first slice: protected-mode entry, descriptor loads and unreal mode, 1 November 2026

Issue [#1370](https://github.com/DeanoC/fes/issues/1370), the CPU half of
[#1369](https://github.com/DeanoC/fes/issues/1369), part of
[#1267](https://github.com/DeanoC/fes/issues/1267) (pc386 M5) and the i386
design's Phase 2. The kit runs of Ultima VII and Wolfenstein 3D with XMS
need #1335 and #1363 on main and stay with #1369
([Not in this record](#not-in-this-record)).

**Result:**
- `fes_i386_cpu` now enters and leaves protected mode at CPL 0. It has MOV
  to and from CR0, CR2 and CR3, LMSW and SMSW, LGDT, SGDT, LIDT and SIDT.
  It loads DS, ES, FS, GS and SS from the GDT with their checks, error codes
  and accessed bit, and far JMPs to a code descriptor. Data segments keep
  their protected-mode cache in real mode (unreal mode); a real-mode CS load
  takes the 386 values ([What the slice builds](#what-the-slice-builds)).
- The RTL and the i386 lock's Yosys netlist match Bochs on every existing
  lockstep run: 1,758,699 corpus states, 32,000 random programs, the
  directed and memory cases. They also match on 8,000 new random programs
  dense with protected-mode blocks, 417,342 retirements of them with PE set,
  and on five directed unreal-mode cases ([Lockstep](#lockstep)).
- The SingleStepTests vector runner and test386 give the same results as
  main's adapter, byte for byte ([Oracle runners](#oracle-runners)).
- FreeDOS boots with HIMEMX and no `/MAX=0` on the RTL CPU in the machine sim
  and in `system_tb`. HIMEMX enters protected mode on the RTL to set up
  unreal mode. MEM, run from the prompt and from AUTOEXEC.BAT, reports
  15,360K of XMS, and FreeCOM's XMS swap runs it, with no stop and no oracle
  step ([Machine](#freedos-with-himemx-on-the-rtl-cpu)). One `system_tb` run
  met a bench gap in its DDR model after every check had passed; it is
  filed as #1372.
- Quartus 17.0.2 seals `fes.pc386` from a clean checkout at 61.44 MHz with
  every analysis met: setup +0.409 ns (the pixel clock; the system clock
  has +2.603 ns and 73.1 MHz), hold +0.064 ns, 26,436 ALMs, about 2,190
  more than main's last seal ([Seal](#quartus-seal)).

## Inputs

- FES branch `deano/i386-phase2-unreal`, from main `58edb6b3b`; main
  `97668042d` merged in as `c4a1c23a4` (#1290 and #1367, neither touching
  i386 or pc386 sources).
- RTL revision `c4a1c23a4`: the seal, the RTL lockstep and the standard
  system suite. The HIMEMX machine and `system_tb` runs are at `39581a878`,
  which adds two counters to the machine bench and changes no RTL. The
  netlist lockstep and a data-point seal are at `309d87186`, whose i386 and
  pc386 sources differ from `c4a1c23a4` only in three comments. Later
  commits change only this record.
- Oracle: Bochs 3.1 at CPU level 3 (the `fes-i386-oracle` build), corpus
  SingleStepTests/80386 `459d49fbe628`, test386.asm `cfd052d1e64d`.
- Verilator 5.032 on Powerboat. The netlist lockstep used the i386 lock's
  Yosys `238eac9a4`. The seal used Quartus Prime Lite 17.0.2 with the
  netlist from the pc386 lock's Yosys `0a7927bf`.

## What the slice builds

The [RTL README](../../../sources/misteross/cores/fes-common/rtl/i386/README.md#protected-mode)
has the detail. In short:

- **Control and table registers.** MOV r32, CRn and SMSW are an ALU move of
  the register. MOV CRn, r32, LMSW, LGDT and LIDT end in a system uop that
  writes at WB and jumps to the next instruction, so DC decodes what follows
  under the new CR0. CR0 with PG and PE set stops the CPU as not built
  (paging is Phase 5); PG with PE clear is #GP(0). Real-mode interrupts read
  their vector through IDTR.
- **Segment loads.** MOV Sreg, POP Sreg and LDS/LES/LFS/LGS/LSS in protected
  mode run microcode (`pseg`, `ppop`, `plptr`): the descriptor's two dwords
  into T2 and T3, the checks from Bochs's `load_seg_reg` with #GP, #SS and
  #NP and their error codes, the accessed-bit store, then the cache write. A
  null selector loads a null cache. TI=1 reads the LDT at the reset LDTR
  (base 0, limit FFFFh), since LLDT is not built.
- **Far JMP** at CPL 0 to a code descriptor (`pjmp`, `pjmpm`), with
  `jump_protected`'s checks. A TSS, task gate or call gate stops the CPU as
  not built.
- **Access checks** in every mode against the cached limit and type, as
  Bochs's `read_virtual_checks` and `write_virtual_checks` make them:
  expand-down, G-scaled limits, not-present, execute-only and read-only
  segments. Each load works these out once (`seg_chk_t`).
- **Leaving protected mode.** Clearing PE makes CS a writable data segment,
  as Bochs does; the real-mode far JMP after it sets CS's limit to FFFFh and
  its access rights to 93h (the design's 386 rule). DS, ES, FS, GS and SS
  keep the limit and type a protected-mode load gave them.
- **Not built** (each stops the CPU with `trace_unimplemented`, and
  `trace_pm_stop` with the vector and error code where PE is set):
  exception and interrupt delivery with PE set; far CALL, RETF, IRET, INT n,
  INT3 and INTO in protected mode; gates, privilege transitions, LLDT, LTR
  and the rest of group 6, LAR, LSL, VERR, VERW, ARPL, task switches, debug
  registers, V86 and paging.

The design doc's [segment-cache contract](../../../sources/misteross/docs/i386-cpu-design.md#architecture)
and Phase 2 row now say this.

Nothing HIMEMX, Voodoo or FreeCOM runs needed more than the issue's scope.
From its code, Ultima VII's `MAINMENU.EXE` builds a GDT with a flat 4 GiB
data descriptor (18h), a 64 KiB data descriptor with B clear for SS (20h)
and a 16-bit code descriptor of limit FFFFh (38h). Its IDT holds gates
(selector 28h), which the CPU never reads, because Voodoo runs with
interrupts off while PE is set. An oracle-CPU machine run
(`machine_tb --cpu oracle --census`) of a scratch Black Gate disk with
HIMEMX's full XMS ran Voodoo's protected-mode entry. With PE set it ran only
MOV CR0, MOV Sreg, JMP FAR m16:16, CLI, JMP short, MOV r16 imm16, DEC and
32-bit XOR, all built. The game then stopped short on conventional memory
(it asks for 561,144 bytes). The kit disk for #1369's second PR has to leave
that much free.

## Lockstep

`scripts/test_fes_i386_lockstep.py` at `c4a1c23a4`
([report](lockstep-report.json)), and with `--yosys` on the i386 lock's
Yosys `238eac9a4` at `309d87186` ([report](lockstep-netlist-report.json)).
The two reports are identical but for the netlist's `yosys` field:

| Run | Result |
| --- | --- |
| Corpus (SingleStepTests/80386, 941 files) | 1,758,699 pass, 0 fail; 3,517,401 retirements compared |
| Random programs (16 seeds × 2,000) | 32,000 pass, 0 fail; 8,718,916 retirements |
| Protected-mode programs (16 seeds × 500, new) | 8,000 pass, 0 fail; 675,887 retirements, 417,342 of them (with the random programs') with PE set; 1,269 real-mode CS loads under the 386 rule |
| Unbuilt encodings | 53,344 outside SCOPE, all stopped as not built |
| Directed cases | 40 pass |
| Unreal-mode cases (new) | 5 pass |
| Memory cases (DMA, FLUSH) | 26 pass |

The sequential-gap check still finds every retirement one cycle after the
last.

**The protected-mode programs** (seeds 1001-1016, `--pm-programs`) mix the
random programs' real-mode code with blocks that build a 16-entry GDT of
random descriptors (null, code and data of both sizes, expand-down, G set or
clear, not present, DPLs and RPLs, TSSes and gates), LGDT it, set PE, load
segments with MOV, POP and LDS-style instructions, far JMP between code
descriptors and back to real mode, and run memory, string and stack
instructions through what they loaded. 11,765 runs ended in an exception or
interrupt with PE set, which stops the CPU as not built. The harness then
compares the RTL's vector and error code with Bochs's first event:

| Vector | Stops |
| --- | --- |
| #GP (13) | 9,697 |
| #SS (12) | 995 |
| INTR | 529 |
| #NP (11) | 376 |
| NMI (2) | 112 |
| #UD (6) | 55 |
| #DE (0) | 1 |

**The unreal-mode cases** (`--mode unreal`): from real mode with a GDT of
a 64 KiB 16-bit code descriptor, a 4 GiB data descriptor (G), a 64 KiB data
descriptor and a 4 GiB 16-bit code descriptor, each enters protected mode,
far-jumps to code, loads DS, ES, FS, GS and SS, clears PE, far-jumps to real
mode, reloads the segments with real-mode values and then:
- `unreal`: all five loaded from the 4 GiB descriptor read or write at
  2 MiB through 32-bit addresses with no fault, and the RTL's caches hold
  limit FFFFFFFFh;
- `normal-gs`: GS from the 64 KiB descriptor faults #GP(0) on a word at
  GS:FFFFh;
- `normal-ss`: SS from it faults #SS(0) on a word at SS:FFFFh;
- `cs-rule`: with the 4 GiB code descriptor, CS still reads at 10000h with
  PE clear; after the real-mode far JMP a word at CS:FFFFh faults #GP(0);
- `cs-rule-hlt`: the RTL's CS after that jump is limit FFFFh, access rights
  93h, D/B and G clear.

Bochs 3.1 keeps CS's cache on a real-mode CS load. The lockstep gives the
oracle the RTL's CS limit and access rights after each such load
(`cs_rule`): 1,269 in the protected-mode programs, 2 in these cases. This is
the design's known limit, not a divergence ([Known limits](#known-limits)).

## Oracle runners

The adapter gained the CPL (`State::cpl`, Bochs's CS RPL), so the
oracle-only runners were rerun against main's adapter at `97668042d`:
- `test_fes_i386_vectors.py`: 1,758,700 tests, 1,437,596 pass and 321,103
  fail on both. The fails are the divergence list's known cases. The two
  `report.json` files are byte-identical.
- `test_fes_i386_test386.py`: POST codes to FFh, 44,926 EE lines with none
  differing from the reference, final RAM SHA-256 `44597188…` on both; the
  output is identical.

## FreeDOS with HIMEMX on the RTL CPU

Scratch disks, built in a worktree of #1363's branch (`freedos-games.img`
and `freedos.img` under its `build/sim/fes-pc386`) and not committed:
- **Games image**: #1363's games image with its FDCONFIG.SYS line changed to
  `DEVICE=C:\FDOS\BIN\HIMEMX.EXE /VERBOSE` (no `/MAX=0`), SHA-256
  `6aece8ad…`. AUTOEXEC.BAT runs FESENV, CTMOUSE and VER.
- **Boot fixture with HIMEMX**: the system bench's FreeDOS fixture with
  #1363's HIMEMX 3.38 as `C:\HIMEMX.EXE`, FDCONFIG.SYS `DOS=HIGH` and
  `DEVICE=C:\HIMEMX.EXE /VERBOSE`, and the games image's MEM.EXE run from
  AUTOEXEC.BAT after VER, SHA-256 `9815e3b6…`.

COMMAND.COM on both is FreeCOM 0.86 built with XMS swap (`XMS_Swap`): it
copies itself to XMS before each external program and back after, so each
external program means XMS moves. HIMEMX moves in unreal mode: its first
move faults #GP at DS's 64 KiB limit, and its INT 0Dh handler enters
protected mode, loads DS and ES from a 4 GiB descriptor and returns to real
mode.

| Run | Result |
| --- | --- |
| Machine sim, RTL CPU (`machine_tb --cpu rtl`), games image, `mem` typed at the prompt, at `39581a878` | Pass ([report](machine-himem-report.json)): the prompt at 2.40 s and MEM's output by 3.65 s, with 15,360K of XMS, 159K used and 15,201K free. All 98,807,423 instructions retired on the RTL: 0 oracle steps, 0 stops. The RTL entered protected mode once (`pe_entries`), and 6 retirements left PE set (`pe_retired`): HIMEMX's INT 0Dh handler loading its 4 GiB limits. FESENV and CTMOUSE ran before the prompt, through FreeCOM's swap |
| `system_tb` (`pc386_system`, RTL CPU, no oracle: a stop ends the run), boot fixture with HIMEMX and MEM, `--seed 1`, at `39581a878` | Pass ([report](system-himem-report.json)): `C:\>` at 2.85 s after 47,423,516 instructions. HIMEMX reports `XMS 3.00 80386 4G`, and MEM shows 15,360K of XMS, 159K used and 15,201K free. Every check passes: the dirty map, the save, the record and its round trip |
| `sim_fes_pc386.py --suite system`, the bench's own fixture (no HIMEMX), at `c4a1c23a4` | Pass ([report](system-report.json)): `C:\>` at 2.75 s |

The bench's write test puts `MD C:\M5` in AUTOEXEC.BAT's PATH line, so on
the HIMEMX disk it runs MD, VER and MEM.

The first `system_tb` run of that disk, at `c4a1c23a4` with the default
seed, booted the same way and passed every check through the record. At
the round trip's release, 3 million clocks after the prompt, the bench
stopped on its DDR model's port-2 check: `a write burst cut short, then
another begun`. The bench holds a port by not driving it, where
`fes_hps_ddr_guard` on a kit finishes a started write burst. A hold that
lands inside a VGA capture burst leaves the model's burst open, and the
capture's first burst after the next release fails the check. This is a
bench gap outside the CPU; the guest's timing decides whether a run meets
it. It is filed as
[#1372](https://github.com/DeanoC/fes/issues/1372). Seed 1 changes only the
DDR ports' timing draws, and with it the run passes.

## Quartus seal

`make build-fes-pc386-quartus` at seed 1 from a clean checkout of
`c4a1c23a4`: [`seal-summary.json`](seal-summary.json). Against main's last
seal ([computer 1.1](../2026-10-31-pc386-m5-computer11/summary.md#quartus-seal),
`e6de1a468`):

| | Seal, `c4a1c23a4` | `309d87186` (data point) | Main's last seal, `e6de1a468` |
| --- | --- | --- | --- |
| Build ID, package | `0e6955d4…`, `6e0a663a…` | `aeb21d95…`, `529057f9…` | `d355dd17…`, `6823af43…` |
| System Fmax, slow corners (61.44 MHz) | 73.14-73.25 MHz, setup slack +2.603 ns | 70.20-72.14 MHz | 70.86-71.66 MHz |
| Pixel Fmax (74.27 MHz) | 76.60-77.01 MHz | 86.02-86.95 MHz | 83.03-84.13 MHz |
| Setup, hold slack | +0.409 (pixel), +0.064 ns | +1.839, +0.105 ns | +1.420, +0.113 ns |
| Recovery, removal, pulse width | +7.922, +0.396, +1.122 ns | +6.893, +0.270, +1.122 ns | +7.851, +0.460, +1.122 ns |
| ALMs | 26,436 of 41,910 (63%) | 26,421 | 24,246 |
| Registers | 24,176 | 24,136 | 22,252 |
| RAM blocks | 256 of 553 | 256 | 254 |
| DSP blocks | 29 of 112 | 29 | 29 |
| RBF | 3,557,656 bytes, SHA-256 `840e7c0e…` | 3,563,752 bytes | 3,492,112 bytes |

The checkout was clean (`git status` empty before the build). Every
analysis passes, with no illegal or unconstrained clocks. The worst setup
path is now the pixel clock's, at +0.409 ns in the slow -40 C corner; the
system clock has +2.603 ns. `309d87186` has the same RTL but for three
comments and the main merge, which touch no pc386 source; the comments move
Yosys's source-line attributes, so its fit is a second placement of the same
logic. Its pixel clock had 86 MHz: the drop to 77 MHz is placement, not
logic, and the pixel domain has no CPU logic.

Against main's last seal, protected mode costs about 2,190 ALMs (9%),
1,924 registers and 2 RAM blocks. The new registers are the descriptor
staging (`ds_*`), each segment's low bound and type flags for the access
checks, T2 and T3, CR2, CR3, GDTR and IDTR with their check limits, and the
new uop fields through the pipeline. The seals of #1335 and #1364 are on
their branches, not on main.

## Yosys workarounds

The pc386 sealer's Yosys elaboration rejected two constructs the first
version used, which Verilator accepts. Both also fail on the i386 lock's
Yosys. The RTL avoids them; each has a DeanoC fork issue with a repro on the
locked binary:
- [DeanoC/yosys#72](https://github.com/DeanoC/yosys/issues/72): a cast of a
  non-constant expression as a `case` selector (`case (sys_rd_e'(sys_idx))`)
  is `Static cast with non constant expression!`. The RTL cases on the vector
  with size-cast labels, and `fault_vector` takes `logic [3:0]`.
- [DeanoC/yosys#73](https://github.com/DeanoC/yosys/issues/73):
  `{a[i], b[i]} <= d` is `incompatible mix of lookahead and non-lookahead
  IDs` once `a[i]` is a variable-index target elsewhere in the block. The
  segment-cache write is one statement per field.

## Known limits

The first five are in [`sim/i386/README.md`](../../../sources/misteross/cores/fes-common/sim/i386/README.md#known-limits);
the last two are how the lockstep handles the reset alias and a stop:
- A real-mode CS load: the RTL sets limit FFFFh and access rights 93h (the
  386 rule), where Bochs 3.1 keeps the cache; the lockstep adopts the RTL's
  values (`cs_rule`), and the unreal-mode cases check the rule.
- Bochs skips the limit check of a flat segment (base 0, limit FFFFFFFFh),
  so an access wrapping past 4 GiB faults only on the RTL. The generated
  programs keep clear of it.
- Bochs reads CR0's bits 30-5 as ones and forces ET (its 387); the lockstep
  compares PE, MP, EM, TS and PG.
- LLDT is not built: a TI=1 selector reads the reset LDTR's table.
- Bochs keeps CPL as CS's RPL; the adapter carries it (`State::cpl`).
- The FFFF0000h reset alias: the CPU's physical map and the lockstep's oracle
  bus both alias FFFF0000h and up to F0000h.
- Anything the CPU stops on as not built (above) ends a lockstep run as a
  compared stop, not a pass of that instruction.

## Not in this record

- **The kit.** Serpent Isle at `max`, The Black Gate and Wolfenstein 3D with
  XMS on a kit. The games profile keeps HIMEMX's `/MAX=0` until #1363 is on
  main, and nothing on a kit can drive Ultima VII's menus until #1335's
  keyboard and mouse are there. Both come in #1369's second PR, from a seal
  of that tree; The Black Gate's 386-preset check waits for the governor
  (#1364).
- Protected-mode test386 in RTL, gates, privilege, delivery through the IDT
  and the rest of Phase 2's exit.
