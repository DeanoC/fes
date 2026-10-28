# fes_i386_cpu RTL

The first-party 386-compatible CPU proposed in
[the i386 CPU design](../../../../docs/i386-cpu-design.md). This directory
holds Phase 1's slices so far: the uop format, the front end from F1 to Q,
and `fes_i386_cpu`, which runs the real-mode instructions `fes_i386_decode`
lists from D to WB, with their memory forms on a data port, fixed uop
sequences, the string instructions and ENTER, I/O on an I/O port, the
microcode ROM, exception delivery, INTR and NMI, and the multiply and divide
unit. There is no cache yet.

| File | Stage | Content |
| --- | --- | --- |
| `fes_i386_pkg.sv` | — | The uop (the Decode table's fields), the prefix record, and `fetched_t`, the instruction Q hands D |
| `fes_i386_fetchbuf.sv` | F1, F2, L1, L2 | Block address and memory port; F2's register; L1's register; L2 writing the 32-byte buffer; each byte's availability for Q |
| `fes_i386_lendec.sv`, `fes_i386_oplen.sv` | L1 | The 16 per-byte decoders: one prefix, the 0Fh escape, the prefix-run flag, and the length classes (opcode and fixed-immediate bytes, ModRM bytes per address size, size-dependent immediates) |
| `fes_i386_lstage.sv` | L2 | Each byte's length, end and next-half bit for every prefix-accumulator variant |
| `fes_i386_qalign.sv` | Q | Next-start select, prefix accumulator and aligner |
| `fes_i386_frontend.sv` | F1-Q | The fetch side and Q together |
| `fes_i386_amode.sv` | DC | The address registers and scale a ModRM and SIB name |
| `fes_i386_dfwd.sv` | D | The forwarding table written a cycle ahead, register file read, index scaling, the late operand and its stalls |
| `fes_i386_agu.sv` | AG | The linear and offset adders: 3:2 rows, one carry chain each, the 16-bit-wrap and address-size select |
| `fes_i386_decode.sv` | DC | The instruction to one uop, its #UD and #GP(0) checks, and LEA's displacement |
| `fes_i386_dops.sv` | D | Data operands: the second forwarding table, pending operands and how far ahead their producers are |
| `fes_i386_shift.sv` | EX | The 64-bit funnel shifter |
| `fes_i386_shflags.sv` | WB | The 386EX's shift CF and OF, from the result and the shifter bits EX keeps |
| `fes_i386_exctl.sv` | M2 | EX's controls, decoded a stage ahead into registered one-hot selects, and its operands cut to size |
| `fes_i386_ex.sv` | EX | The late ALU (one adder, a second chain for OF and SF, ZF from the operands), the result's merge into its register, flags and branch resolution |
| `fes_i386_muldiv.sv` | EX | MUL, IMUL, DIV, IDIV, AAM and AAD over several cycles: four DSP products and a restoring divider, with the 386EX's flags and its divide error |
| `fes_i386_cpu.sv` | F1-WB | The top: front end, DC's decoded buffer, D, AG, M1, M2, EX, WB, the register file, segment registers, reset state and retirement trace |

## Front end

Fetch is sequential from the last redirect; there is no L1I or prediction yet.
The memory port stands for the L1I's way-selected output, as an M10K with
address stall: `mem_data` is the block at the address registered on the last
edge with `mem_load`, and `mem_valid` says that response is available. F2
holds its address and does not advance until `mem_valid`; it requests the next
block on the edge that accepts the current response. Nothing decodes M10K data
in F2 (design rule 7). A redirect flushes F2, L1, L2, the buffer and Q; a
change of CS.D (`code32`) needs one.

Length decode takes two stages. L1 runs the 16 decoders on F2's register and
registers what each byte's instruction is made of: its prefix and run flags,
its opcode and fixed-immediate bytes, its ModRM, SIB and displacement bytes
for each address size, and whether a 2- or 4-byte immediate or offset
follows. The decoders see the previous block's last four bytes and then the
block, so a block's bytes 0-11 are decoded as it passes L1 and bytes 12-15
when the next sequential block does. Those four bytes come from L2's
register, since the previous block may not have reached the buffer yet. L2
adds the classes up for each of the four prefix-accumulator variants (66h
seen, 67h seen), with CS.D and the byte's own 66h or 67h folded in, into each
byte's length, end and next-half bit. L2 writes block and decode into its half
of the buffer once Q has released that half, and in that cycle Q may already
use the block's bytes from L2's register. The second stage costs one cycle
on every redirect: the first instruction issues 5 cycles after a redirect
instead of 4.

The buffer also holds, per byte and variant, whether Q may use the
instruction there: its half is valid, its bytes 12-15 are decoded if it starts
there, and the next half is valid or being filled if it ends past its own
half. It is registered from next-cycle state. A release on that edge is
ignored for the byte's own half, since Q's start has just left it; the next
half's term assumes a release whenever Q is in that half now.

Q keeps a start position and a prefix accumulator. Each cycle it either
consumes one prefix that another prefix follows, or issues the instruction at
start, whose last prefix is part of the per-byte decode. Its loop picks the
stored availability and next start by the accumulator's two flags and then by
start: a select, with no add. Measured in simulation, with D always ready:

- one instruction per cycle on every prefix-free stream, once the buffer is
  full, including streams of 8- to 11-byte instructions;
- an instruction with k prefixes issues max(1, k) cycles after the one before
  it;
- an instruction longer than 15 bytes, or a run of prefixes that reaches a
  16th byte, issues with `too_long` set, for D to raise #GP(0).

Lengths follow the oracle's decoder for every encoding it defines. The i386
raises #UD for the rest, so their length is not architectural, and D decides
#UD. MOV to and from TR6 and TR7 (0F 24, 0F 26), which the design accepts and
the oracle does not, take the CR/DR rule: ModRM is always a register form.

## D's address operands and AG

D decodes the address registers from ModRM and SIB. A SIB index of 100b with a
nonzero scale scales the base, as the 386EX does (Phase 0 divergence
`invalid-sib-scales-base`). D reads the flip-flop register file, takes each
register's youngest in-flight value from the M1, M2, EX or WB result
register, scales the index and registers AG's operands.

D's forwarding selects are worked out a cycle ahead, per GPR. Each cycle a
table records, for every GPR, where its youngest value will be next cycle
among the uops then in M1, M2, EX and WB: in a result register, or only in the
register file. It is written from the destination tags of the uops now in AG,
M1, M2 and EX. Its state as an address operand, late from M1, EX or WB, or
further off, also counts the uop now in D when it leaves D: next cycle that
uop is in AG, younger than everything in the table, and what it writes is
late from M1 if its result is early, else further off. D's cycle applies each
GPR's registered select to the result registers beside the addressing decode
and picks base and index, and whether each stalls, by register number, with
no tag compare. If the stages behind D ever hold, the table must be written
from the held producers.

A value completing in the cycle the uop leaves D arrives late, from the
register it lands in when the uop is in AG: the previous uop's early-ALU
result (M1), load data from M2 (EX) or a late-ALU result from EX (WB). At most
one late value reaches AG, entering its last 3:2 row as a base or an unscaled
index. D stalls the uop when the late value would be a scaled index, when
both operands would be late, or when a value is further off.

AG adds the segment term, D's first-row operand and the displacement in a 3:2
row from registers, then the last-row operand (D's or the late value) in a
second, then one carry chain per sum. With a 16-bit address size every operand
is zero-extended; linear bits 31:16 come from the segment's high half or that
plus one, by the carry that bits 17:16 of the two sums leave once the wrap is
removed, so only the address-size select follows the 32-bit chain.

## D to WB

`fes_i386_cpu` puts DC, D, AG, M1, M2, EX and WB behind the front end, with a
data port that stands for the L1D's output: M1 drives a load's address, the
port registers it on the edge M1 advances, as an M10K does, and the 8 bytes
from the address's dword, as memory held them on that edge, arrive in M2 once
`dmem_valid` says so. A retiring store writes its bytes on the edge it
retires, and the port reads before it writes on the same edge.

**Freeze.** A load in M2 whose bytes have not arrived freezes D to WB: every
stage behind DC keeps its uop, D issues nothing and nothing retires, until
they do. Nothing moves relative to anything else, so the forwarding
distances, AG's late operands, the store merge and D's forwarding tables all
hold as they are. Whether M2 holds a load is a register set as it enters, so
the freeze is one gate on `dmem_valid`; WB's flush waits for it, which makes
a taken branch or a fault in WB wait out a wrong-path load. A multiply or
divide freezes them the same way while it works in EX (`md_busy`, a register
set as the uop enters EX), and so the freeze has one more registered input
and D's issue no new term. So does an I/O access in WB, until the machine
completes it (`io_wait`, set as the uop enters WB and cleared on the
completing edge; see I/O), and an INTR being taken, until the machine returns
its vector (`int_wait`, likewise; see Interrupts). Each wait is one register,
so the freeze stays one LUT of five inputs, which every stage's enable takes.
What WB does with
its uop (flush, redirect, deliver a fault) is decided as the uop enters WB, so
each of those is one gate from the freeze too.

**DC** decodes Q's output register into a two-entry buffer of decoded
instructions: `dc` holds the instruction D issues, and `dc_skid` takes Q's
instruction when `dc` cannot. Q may load its output whenever `dc_skid` is
empty, so D's stall never reaches Q's loop as `d_ready`, and everything D's
issue waits on starts from a register. The decode costs one cycle on every
redirect.

DC decodes the instruction to one uop (`fes_i386_decode`): group 1, INC,
DEC, NEG, NOT, TEST, MOV (to and from a segment register in real mode, and
immediates), LEA, the shifts and rotates, SHLD and SHRD, SETcc, MOVZX and
MOVSX, CBW/CWDE, CWD/CDQ, SAHF, LAHF, the flag set and clear instructions,
NOP, HLT, the near JMP, Jcc, LOOP, LOOPE, LOOPNE and JCXZ, MUL, IMUL (all
three forms), DIV, IDIV, AAM and AAD, BT, BTS, BTR and BTC by an immediate,
BSF and BSR, the stack operations below, and the
first uops of the string instructions and ENTER, with a ModRM memory
operand wherever they take one. A memory operand is a load, a store,
or both in one uop (read-modify-write), and stands for the register it
replaces: no GPR is read or written for it. Its segment is the override, else
SS for a BP-, EBP- or ESP-based address, else DS. LOCK on anything but a
read-modify-write of ADD, OR, ADC, SBB, AND, SUB, XOR, NOT, NEG, INC, DEC,
BTS, BTR or BTC, MOV to CS or to segment register 6 or 7, LEA of a register
and C6h/C7h with reg != 0 raise #UD; over 15 bytes raises #GP(0). Every
other encoding decodes as `FAULT_UNIMPLEMENTED`, which stops the CPU at WB,
so no check can pass over one. An immediate after a ModRM's operand bytes is the instruction's last
bytes, so DC takes it from the length, beside the decode. DC also registers
the address registers and scale a memory ModRM names (`fes_i386_amode`), or
ESP (EBP for LEAVE) for a stack operation.

**Stack operations** are PUSH and POP of a GPR, an immediate (PUSH) or a
segment register, CALL rel, RET and RET imm16, LEAVE and JMP r/m. Each is one
uop with a memory operand at SS:ESP (SS:EBP for LEAVE) at the stack's width
(SS.B), except CALL rel, which is two: the push of the next EIP, then JMP rel
by the displacement the push carries in its unused immediate. A stack
operation also writes ESP: AG has a third
adder for it (ESP less the size for a push, plus it for a pop, plus imm16 for
RET imm16, EBP plus it for LEAVE), and WB writes it beside the main
destination, which wins when it is ESP too (POP ESP). A 32-bit PUSH or POP of
a segment register moves ESP by 4 and transfers the selector's word only, as
the 386EX does. RET and JMP r/m take their target from EX's adder (CS's base
plus the EIP) and redirect from WB like any taken branch; in 16-bit code a
target past FFFFh raises #GP(0) at the branch. ENTER runs on the string
engine (below).

**Fixed sequences.** An instruction of several uops, decoded without the
microcode ROM, keeps its place in `dc` while they issue: as D issues each but
the last, `dc` takes the next from `fes_i386_seq`, so every field D reads is
a register at every step and the uops issue one per cycle. Every field a
sequence rewrites is set outright at every step, from the instruction's
first uop (`dc0`, kept beside `dc`) with every change of the steps before it
applied again, and the per-step controls are reset. No field is kept from
`dc` at some steps and not others, so `dc` loads on two enables from D's
issue: the rewritten fields at every issue, the rest at the instruction's
last. WB
moves EIP only at an instruction's last uop, so a sequence that faults part
way leaves EIP at the instruction. Two temporaries, T0 (16 bits) and T1
(32), are destinations in RS_TEMP: WB writes them and no forwarding sees
them, so a uop reads one at WB, or waits in D until its writers retire.

**Far pointer loads and far transfers** (real mode):
- LES, LDS, LSS, LFS and LGS load the selector's word, past the offset by
  the operand size, into T0, then the offset into the register; WB writes
  the segment from T0 as a second destination (`seg2`), which D's
  segment-busy vector follows. Each part is checked against the limit on its
  own, the selector's offset wrapped by address size, as the 386EX does.
- A far jump is one uop: AG computes its target as the new CS's base,
  selector * 16, plus the offset, and WB writes CS, the selector reaching it
  in `c`. JMP ptr16:16/32 is that alone. JMP m16:16/32 first loads the
  selector into T0 and the offset into T1, and the jump takes them once
  those have retired.
- CALL ptr and CALL m16:16/32 (after the same two loads) read the return
  address's lower slot first, a probe that checks its limit, then push CS
  in an operand-sized slot, zero-extended, and the next EIP, then jump. A
  push that would cross the stack's limit so faults before anything is
  written.
- RETF and RETF imm16 load the offset into T1 and the selector into T0
  without moving ESP; the jump steps ESP at WB, as it flushes every younger
  uop and nothing needs the new ESP sooner.
- In 16-bit code an offset past FFFFh raises #GP(0) at the first uop that
  could write anything: the far jump, or a far call's probe.
- Far transfers are taken branches, with no prediction yet: each redirects
  from WB.

**CR0, CLTS and WAIT.** CR0's MP and TS are all of CR0 the CPU holds,
from `init_cr0`, shown in `arch_cr0`. WAIT is a NOP, or raises #NM when MP
and TS are both set, which DC decodes after every other check, as the
design's [FPU](../../../../docs/i386-cpu-design.md#fpu) section asks. CLTS
clears TS at WB as a system uop that jumps to the next instruction, so
every instruction after it decodes once TS is clear, and no wait in D
follows it. The jump is not cut to 16 bits: a CLTS that ends at FFFFh
leaves EIP 10000h, as any other instruction does. Nothing else writes MP
or TS yet: MOV to CR0, LMSW and the ESC instructions are not built.

**Small forms:**
- XCHG of registers is three XORs that write no flags, each into the
  register the one before read; EX's arithmetic and logic flag selects
  follow the uop's flag-write bits. XCHG with memory loads the operand into
  T1, stores the register, then moves T1 into the register.
- MOV with a direct offset (A0h-A3h) is a memory operand with no base or
  index (`direct`). XLAT's index is AL zero-extended, which D's address
  forwarding masks (`index8`), holding the uop when AL would come late.
  SALC is SBB AL, AL writing no flags.
- CALL r/m near loads its target into T1, pushes the next EIP after checking
  T1 against CS's limit, then jumps to T1. PUSH m loads into T1 and pushes
  it. POP m reads the stack's slot into T1 without moving ESP, stores T1 to
  the operand, whose address registers DC kept beside the stack uop's
  (`m_*`), then pops into nothing to step ESP, so a store that faults leaves
  ESP as the 386EX does. A 32-bit address that reads ESP, as a base or as
  the 386EX's scaled base of an invalid SIB, reads it after the pop, which
  has not happened yet. The store waits for T1, so every older uop has
  retired and ESP is the register file's: D adds the operand size, scaled
  as ESP is, to the store's displacement (`esp_adj`), less 10000h so scaled
  (`esp_wrap`) when a 16-bit stack's SP wraps from its top slot to 0.

**Strings and ENTER** run on the string engine, as the design's string
sequencer describes. While one runs, D keeps copies of the next source and
destination offsets and of the count. Each step's address is a copy, which
the step moves on, so the uops issue from `dc` without waiting on
forwarding, and an offset wraps at its address's width.
- **The first uop** of MOVS, CMPS, STOS, LODS or SCAS is a NOP that loads the
  copies from ESI, EDI and ECX (CX by address size). It waits until they are
  in registers, until no older uop that writes DF is in AG to EX, and until
  neither its segment nor ES is being written. Its registers are fixed, so
  it reads each one's value and pending bit from D's forwarding directly,
  with no select by register number. Then the string runs:
  - one element per count under REP (F2h or F3h), else one;
  - a REP with a count of 0 is a NOP that ends the instruction;
  - each element steps by its bytes, down when DF is set.
- **The first element is built without the count**, which is still on its
  way into the copy when `fes_i386_seq` builds it. The first uop latches
  whether that element ends the instruction (a count of 0, or 1 but for
  CMPS) and whether it is the NOP (0). D then issues it as the last, or as
  the NOP, which writes nothing D tracks. Beside the copy of the count, D
  keeps whether it is 0, 1 or 2, which `fes_i386_seq` reads.
- **The elements.**
  - STOS is a store of eAX at ES:EDI, LODS a load of eAX from seg:ESI, and
    SCAS a compare of eAX with ES:EDI.
  - MOVS is one uop. It loads seg:ESI, then stores the value at ES:EDI, an
    address D computes and checks against ES's limit (`st2`). M1 raises that
    fault after the load's own.
  - CMPS is two: seg:ESI's value as a uop's result, then its compare with
    ES:EDI. The compare takes that result one stage ahead, since D issues
    it in the next cycle: nothing it waits on can change in between.
- **Each element's last uop** steps ESI, EDI and ECX at WB. A fault part way
  leaves them at the elements done, with EIP at the instruction, so no
  forwarding sees the steps. A uop that reads ESI, EDI or ECX waits in D
  while a string element that steps it is in AG to WB. Which of them a uop
  reads is decoded into `dc` (`rd_str`), so that wait is one AND.
- **REPE and REPNE** (F3h and F2h) CMPS and SCAS issue elements ahead of the
  flag result. An element but the last ends the instruction when its
  compare's ZF says so. It does that as a taken branch to the next
  instruction, whose address the first uop keeps, so WB flushes the
  elements behind it.
- **ENTER** uses the same copies.
  - Its first uop is the push of EBP at SS:ESP less the size, ESP unmoved.
    It loads the source copy (EBP less the size), the destination copy (ESP
    less twice the size) and the frame pointer (ESP less the size, its high
    half ESP's on a 16-bit stack).
  - Then come the level's (mod 32) less one copies of a slot below EBP to the
    next push slot, each one uop as a MOVS element, and the push of the
    frame pointer unless the level is 0.
  - The last uop writes EBP from the frame pointer and ESP from the next
    push slot plus the size less imm16, and checks the slot at that ESP, as
    the 386EX does.
  - ESP and EBP change only there, so a fault before it leaves them, with
    the pushes before it written.
- **Rates, without latency:** REP MOVS, STOS, LODS and SCAS issue an element
  a cycle, and REP CMPS one in two. ENTER is 2 uops at level 0, 3 at level 1
  and the level plus 2 above, and a load waits a cycle behind its push of
  EBP and of the frame pointer.

**I/O.** IN and OUT, with an imm8 or DX port, and INS and OUTS make their
access at WB, never speculatively, on the I/O port.
- **The access.** The uop in WB raises `io_req` with `io_port`, `io_size`
  and, for OUT, `io_wdata`: eAX, or OUTS's loaded value. D to WB freeze until
  the machine answers with `io_ack`, IN's data in `io_rdata`, and the uop
  retires on the edge after. A uop that enters WB with a fault makes no
  access, so INS's store and OUTS's load fault first, as on the 386.
- **The data.** IN merges the port's data into eAX by size at WB. INS stores
  it at ES:EDI as a STOS element would; OUTS loads seg:ESI as LODS does.
- **What follows waits.** Nothing younger issues until an I/O instruction
  has retired (`io_busy`, as `t_busy`): no load reads memory an OUT may
  remap, and no uop reads IN's data before the register file holds it. A
  REP INS or OUTS's own elements still issue back to back, each waiting in
  WB for its access; the next instruction's first uop waits.
- **The port and data registers** are used at WB, from the uop's record. EX
  takes one that its producer, one stage ahead, has just computed from the
  WB register into the record, as EX's own operands come.
- Real mode has no I/O permission check, and there is no write buffer yet:
  every older store has written when the access is made.
- **Rates, without latency, with an ack in the cycle the request rises:**
  REP INS and OUTS take 2 cycles an element. An IN or OUT takes 6 more
  than a NOP, mostly the next instruction's wait to issue.

**Interrupts.** INTR and NMI are taken at instruction boundaries, as the
design's commit rules ask, and between a REP string's elements.
- **When.** On the edge an instruction's last uop retires, a delivery's
  included, or a REP string element's (the one that steps ECX,
  `str_cx`), or while the CPU is in HLT.
  - NMI comes first, unless one is being handled: an NMI blocks the next
    until IRET's last uop retires (the microcode word's `iret` bit).
  - INTR is taken if IF is set after the instruction.
  - Neither is taken after MOV SS or POP SS, unless that one follows
    another, nor after an STI that set IF: the shadow, as Bochs keeps it.
    LSS has none. Before a REP string the shadow ends with its first
    element, since Bochs counts an iteration as an instruction.
- **How.** The decision is a register, `int_take`, from that edge.
  - An INTR first asks the machine for its vector: `inta_req` until
    `inta_ack`, with D to WB frozen.
  - Then WB flushes, as for a fault. The uop in WB, the next instruction's,
    neither retires, stores nor makes an I/O access, and EX's running flags
    go back to EFLAGS.
  - DC takes the vector routine's entry with D's EIP the next instruction's,
    so the routine pushes it. An NMI's vector is 2.
  - Between a REP string's elements that EIP is the string's own, since WB
    moves EIP only as an instruction ends. The elements done have stepped
    ESI, EDI and ECX, so IRET returns to the string, which runs on from
    there. The uop in WB, the next element's, is discarded with the rest.
- **HLT** waits for an interrupt now. `trace_halt` is HLT or the stop for
  good, which `trace_unimplemented` tells apart. A HLT that retires with an
  interrupt pending takes it at once.
- **Not built yet:** the debug traps.

**Microcode.** Longer routines come from an M10K ROM
(`fes_i386_ucode_rom`). `scripts/fes_i386_ucode.py` builds it from
`ucode.toml`, with the entry addresses in `fes_i386_ucode_pkg`, checking
every field and routine.
- **Words.** A word is a `ucode_t`: the uop fields a routine sets. The
  operand size, the vector and the memory operand come from the instruction
  in `dc`. A word's memory operand is a stack slot, the vector's entry, or
  the instruction's ModRM operand (`rm`); its registers are GPRs, or the
  instruction's register (`rg`), which DC's first uop carries in src1.
- **Issue.** DC decodes a routine's first uop, and `dc.upc` addresses the
  next word. Each later uop is a `SEQ_UCODE` step of the sequencer, so `dc`'s
  input select keeps its three sources.
  - The ROM's address is a register and its output is registered, so a word
    is there the cycle after `dc` settles. D waits a cycle for each
    (`rom_ok`, `uc_wait`), with `issue` kept off the M10K's pins.
  - Routines are straight sequences. INTO's JNO, a branch that ends the
    instruction only when taken, is the one microbranch, resolved at EX.
- **Stack slots.** A `STK_PEEK` uop reads or writes the slot at ESP plus a
  signed number of operand-sized slots, -8 to 7, without moving ESP. One
  that also writes ESP steps it past eight slots, down from a slot below ESP
  and up from one above.
- **INT's probe.** INT starts with a probe of its three slots. Under SS's
  real-mode limit, FFFFh, a slot faults only by straddling the 64 KiB wrap,
  which one does when they wrap at an odd offset. M1 flags that (`pbad`), and
  M2 raises #SS(0) from the register, before INT writes anything. Under any
  other SS limit INT is unimplemented.
- **The routines:**
  - PUSHA and PUSHAD store each register in its own slot below ESP, from the
    lowest address up, ESP's being the unmoved original, and step ESP at the
    last. Each slot checks its own limit, so a block that wraps the stack
    misaligned stores the slots below the one that straddles and then raises
    #SS(0), ESP unmoved, as the 386EX does.
  - POPA and POPAD load each slot from ESP up the same way, ESP's into T1.
    POPAD on a 16-bit stack takes ESP's high word from it, as the 386EX does.
  - INT n, INT3 and INTO read the vector's entry first, at linear vector × 4,
    as the 386EX does. Then they push FLAGS, clearing IF and TF, CS and IP,
    and jump.
  - IRET reads its three slots, the FLAGS load checking the offset first,
    then jumps, stepping ESP at WB.
  - BOUND loads its upper bound, past the lower by the operand size and
    wrapped by address size, into T1, then compares the register with the
    lower bound, which it loads, and T1 with the register. Each compare
    raises #BR (vector 5) from EX's adder when its first operand is below
    its second, signed, at the operand size, and writes no flag. Both
    bounds load, and check their limits, before either compare, as the
    386EX does: a bound past the limit raises #GP(0) or #SS(0) whatever
    the register. A register operand is #UD.
- **Flags in EX.** PUSHF and POPF are single uops. EX's flag operations give
  FLAGS' image and load the writable real-mode flags in its running copy, as
  INT's and IRET's do.

**Multiply and divide** (`fes_i386_muldiv`) work in EX while D to WB
freeze, from EX's operands, which stay put meanwhile. Only registers lie
between the unit and EX:
- in its first cycle it makes its operands from what EX makes them of
  (EX's src1 and y registers, WB's result, the load, the bypass selects),
  bypassed and at the uop's size, and registers them with the uop's decode,
  so nothing of EX's logic drives it and the DSPs start from registers;
- in its last cycle it writes its result, merged into the destination's old
  value and registered by the cycle before, into the uop's record, which EX
  passes through LEA's select, and a divide error into the record's fault;
- the uop leaves on the cycle after with the unit's flags, which EX's flag
  logic takes off the adder's path.
- **Forms.** MUL and IMUL of AL or eAX by r/m, IMUL r, r/m and r, r/m, imm
  (whose multiplicand is src2 and multiplier imm), DIV and IDIV of AX,
  DX:AX or EDX:EAX by r/m, AAM and AAD. A 16- or 32-bit MUL writes EAX and
  keeps the high half, which a HI uop after it writes into EDX
  (`SEQ_MUL`). A 16- or 32-bit DIV first hands the unit EDX in a LOAD uop,
  which carries the divide's op in imm and reads the memory operand too, so
  a fault on it comes before anything is written; then the divide writes
  EAX and HI the remainder into EDX (`SEQ_DIV`). HI takes the kept half
  into its record as it leaves M2, and EX's lanes merge it.
- **Multiply.** The operands, registered (E0), four 18x18 DSP products of
  the 33-bit operands (E1), their sum as a 3:2 row and one carry chain
  (E2), the result halves and CF/OF (E3), and the undefined flags (E4): 5
  cycles.
  The 386EX multiplies a bit per cycle, right to left, and stops once the
  multiplier's remaining bits are all zeros, or all ones for a negative
  IMUL multiplier; SF, ZF, AF and PF are its last step's, which come out of
  the product shifted right by that step. AAD multiplies AH by imm, then
  adds AL with the ADD's flags.
- **Divide.** Restoring, a bit per cycle, as the 386EX's microcode, whose
  flags are its last step's trial subtract at the operand size. DIV first
  compares the dividend's high half with the divisor; if it is not below,
  the quotient overflows, and the 386EX keeps the subtract and takes one
  step fewer, so the flags a divide error pushes are those steps'. IDIV
  divides a negative dividend's one's complement by the divisor's magnitude,
  adds the one the complement owes and checks the quotient for its sign's
  range. On an overflowing dividend the steps leave a garbage quotient that
  now and then passes, as the 386EX's does (8-bit quotients of 80h).
  Its flags are those of the remainder's sign fix. The unit works 36 cycles
  on DIV r/m32 and 37 on IDIV r/m32, 12 and 13 on a byte (one fewer for a
  DIV that overflows). AAM divides AL by imm.
- **#DE.** A divisor of zero or an overflowing quotient raises `FAULT_DE`
  in EX, delivered at WB as vector 0 like any other fault, with the flags
  the divide changed.
- **The wait.** `md_busy` is set as the uop enters EX and clears in the
  unit's last cycle. A flush waiting in WB cuts it after a cycle, since the
  uop behind it is on the wrong path. The uop in WB waits out a multiply or
  divide too, so its retirement shows the wait.

**D** keeps the instruction's EIP with an incrementer. In 16-bit code EIP is
not cut to 16 bits, as on the 386: an instruction that ends at offset FFFFh
leaves EIP 10000h. Real-mode CS loads leave CS's limit at FFFFh, and an
instruction that starts or runs past it raises #GP(0) at its fetch, before
any fault its decode found; under a larger limit that is unimplemented, as
the general CS-limit check is not built. D applies both, since only D knows
the EIP. D also picks the memory operand's segment limit less the access's
bytes but one, which reset precomputes per segment and size.

The uop's src1 is always its destination's register: a byte or word result
merges into the old value, an implicit source. src2 is the second register
or the immediate; a CL count is a third source. D reads all three from the
register file or the M1, M2, EX and WB result registers (`fes_i386_dops`),
with selects worked out a cycle ahead per GPR as `fes_i386_dfwd` does for
addresses. A data operand never stalls D: if its producer has not computed it,
the operand is pending and records how far ahead the producer is (1-4
stages). No stage behind D holds, so the producer reaches WB when the uop is
that many stages before WB, and the uop takes the WB result register there:
AG, M1 or M2 into its next pipeline register, or EX beside its operand, which
is EX's only forwarding mux. A late result reaches the next uop's ALU with no
bubble. LEA's offset is early when the destination is 32-bit and comes from
AG; a 16-bit LEA merges at EX like any other result. `fes_i386_dfwd` gives
a memory operand's or LEA's address operands and its stalls as before. Both
tables follow ESP from a stack operation as an early result from AG, so a
push or pop right after another takes its address with no wait. ESP read as
data waits in D while a stack operation is in AG (its value reaches the M1
register next cycle), and LEAVE on a 16-bit stack waits until ESP's high
half, which it keeps, is in a register. A
selector read (MOV r/m, Sreg), and a memory operand for its segment's base,
waits in D while an older uop writes that segment register; a register of
the segments in flight makes each wait one select. A load waits one cycle
behind a store, whose data EX computes in the cycle the load would be in M2.
The waits behind the uop in AG (behind a store, a shift, a stack operation
that writes ESP) read what that uop is from bits registered as it enters AG.
A MOVS element's store does not hold the load behind it: its data is its own
load, which is in EX when that load is in M2.

**AG** runs `fes_i386_agu` for a memory operand's linear address and offset,
for LEA's offset, or for a branch's target: CS.base + EIP + length +
displacement in one carry chain, wrapped by operand size.

**M1** checks the segment limit with one compare, the offset against D's
limit, raising #SS(0) for SS and #GP(0) otherwise, and sends a load's address
to the data port. It also takes the load's distance from the stores in EX and
WB, which have not written when the port reads: the one in EX will be in WB
when the load is in M2, and the one in WB writes on the edge the port reads.

**M2** aligns the 8 loaded bytes to the access and takes each byte an older
store writes from that store: a MOVS element's store in EX first (its loaded
value, `pipe_t` ld), then the store in WB, then the one that retired on the
read edge. EX takes the value through its bypass selects, the
ones a WB result uses, so EX's front stays one LUT level.

**EX** (`fes_i386_ex`) takes its selects registered: M2 decodes them from
the uop (`fes_i386_exctl`), so no opcode compare sits on EX's data paths. M2
also prepares the operands: x and y cut to the operation's size with a high
byte moved down, y inverted for a subtract, and an add's operands filled
above its size so that its carry out is the chain's bit 32 at every size. An
operand whose producer is one stage ahead joins x or y from the WB result
register in one LUT level. EX has one adder for ADD, ADC, SUB, SBB, CMP, INC,
DEC, NEG and LOOP's count, with ZF from the operands, and beside it a second
chain whose carry out is the carry into the size's top bit, for OF and SF;
the logic unit; the funnel shifter (`fes_i386_shift`); and the result's
merge into the destination's old value by lane, so WB writes whole
registers. It keeps a running EFLAGS: every uop that passes EX updates it,
so Jcc, SETcc, ADC, SBB and RCL/RCR read it with no forwarding. A shift's
flags reach it a cycle late, from the WB register: WB takes CF and OF from
the result and the shifter bits EX keeps beside it (`fes_i386_shflags`), and
D holds a flag reader one cycle behind a shift. Undefined flags follow the
386EX (the Phase 0 divergence list): logic ops clear AF, shifts by a nonzero
count set it, and the shift OF and wide-count CF rules are in
`fes_i386_shflags`.

**Bit test and scan.** BT, BTS, BTR and BTC by an immediate take the bit's
mask from M2 as y (its complement for BTR), so the logic unit makes BTS's,
BTR's and BTC's result. CF is the operand's bit, selected in EX by an index
M2 registers. The 386EX writes OF as the two bits below it XORed, mod the
size, and leaves SF, ZF, AF and PF. LOCK is valid on BTS, BTR and BTC with
a memory operand. BSF and BSR scan y for its lowest or highest set bit in
EX; a source of 0 sets ZF and keeps the destination. Their other flags are
the 386EX's, which `fes_i386_ex` lists, fitted to every SingleStepTests
state of the six opcodes. BT, BTS, BTR and BTC by a register (0F A3, AB,
B3, BB) are not built: a memory operand's address moves by the register,
which needs an address term AG does not have.

**Branches** are predicted not taken. EX resolves them; a taken one redirects
F1 from WB, where every younger uop is flushed, and clears D's forwarding
tables. A fault, an unimplemented encoding or HLT stops D issuing after it.
At WB an unimplemented encoding stops the CPU with nothing retired, and HLT
retires and stops it. A fault is delivered:
- **Entry.** WB flushes everything younger and restarts the front end and
  DC. A uop that faults before EX changes nothing there, so the delivery
  starts from EX's running flags as they were before it. A divide raises #DE
  in EX itself, after its flags reach that copy, so its delivery pushes them,
  as the 386EX does; BOUND's #BR comes from EX too. The cycle after, DC's skid takes the
  exception's entry (`dc_exc`): a NOP, at length 0, that starts the ROM's
  `vector` routine with the fault's vector, the one INTO enters.
- **The frame.** D's EIP is the faulting instruction's, cut to 16 bits, so
  the routine pushes FLAGS, CS and that IP, and D's CS-limit check passes an
  EIP of 10000h left by an instruction that ends at FFFFh. The front end
  refetches from where it was, and the routine's far jump discards that.
- **A fault in a delivery** stops the CPU, as unimplemented with vector 8:
  double fault and shutdown are not built. In real mode that is a stack that
  straddles the 64 KiB wrap at SP 1, 3 or 5, which faults again however the
  delivery is retried.
- The delivery's last uop retires with `trace_exception`.

**WB** commits in order: the GPR file and segment registers in flip-flops,
a string element's steps of ESI, EDI and ECX, EFLAGS, EIP and stores, the result's low bytes moved to the address's place
in its dword and the next (IN's data for INS), and I/O accesses. A real-mode segment load sets the base to selector * 16 and
keeps the limit and attributes.

**Reset and trace.** `init_*` is the architectural state reset loads,
CR0's MP and TS among it; a machine ties it to the 386 reset state, and the
lockstep harness to a test's state. `trace_*` and `arch_*` show each
retirement.

## Synthesis notes

The i386 RTL has its own toolchain lock, [`toolchain.lock`](toolchain.lock)
here: the DeanoC Yosys fork's main at `238eac9a49`, with nextpnr `bd7a1c0d`
and Mistral `8fcc4cb4`. The timing spikes default to it. The repository's
shared lock stays at Yosys `3a5accbd`, which reads this RTL wrongly, so check
and route the i386 RTL with this lock's Yosys. Each later Yosys fix is a
small bump of this lock, the removal of its one workaround, the `--yosys`
check and a re-measure.

Synthesis runs `proc -norom` first, by choice: it keeps case tables in
logic. Without it the length decoders' 0Fh opcode tables become M10K ROMs
whose registered read takes in F2's byte register, which puts an M10K read in
front of L1's decode, the shape [rule 7](../../../../docs/i386-cpu-design.md#platform-constraints)
keeps out of the pipeline. Measured on the corrected model, F2 fell from
86-91 to 73-77 MHz in the front-end spike. A `rom_style = "logic"` attribute
on the table's case statement does not keep it in LUTs.

Narrow sums (lengths, ends, counts) mark their `+` with `(* no_carry *)`, so
the locked Yosys builds them from LUTs rather than an ALM carry chain
([DeanoC/yosys#44](https://github.com/DeanoC/yosys/pull/44); the attribute
is opt-in and Verilator ignores it).

The locked Yosys still gets some SystemVerilog wrong. This RTL stays inside
what it reads correctly:

- no assignment patterns such as `'{a: ...}` ([#38](https://github.com/DeanoC/yosys/issues/38));
- logic replicated to cut fanout merges back into one copy even on
  `(* keep *)` wires, so each replica needs its own kept input register
  ([#51](https://github.com/DeanoC/yosys/issues/51));
- read two or more fields of one variable-indexed record through a single
  whole-record copy (`at_start = dec[start]`), not as `dec[start].a` and
  `dec[start].b`: the selects share their `start * width` multiply, which
  then maps to a DSP multiplier and a carry chain
  ([#59](https://github.com/DeanoC/yosys/issues/59)). A constant index, as
  in a generate loop, is fine.

`dist` is a SystemVerilog keyword (the constraint operator); Verilator rejects
it as a signal name, so the forwarding modules spell the distance `dist_a` or
`ahead`.

`scripts/test_fes_i386_frontend.py --yosys <yosys>` and
`scripts/test_fes_i386_lockstep.py --yosys <yosys>` run every check on the
netlist Yosys writes from this RTL, which is what the spikes route. Run them
after any RTL change. The front-end runner also synthesizes Q's aligner on its
own and fails if any multiplier or carry cell appears there: Q has no wide sum,
and the front-end spike does not show #59's multiplier.

## Checks

`scripts/test_fes_i386_frontend.py` runs the Verilator checks in
[`sim/i386`](../../sim/i386/README.md#front-end-and-address-checks), and
`scripts/test_fes_i386_lockstep.py` the
[whole-CPU lockstep](../../sim/i386/README.md#whole-cpu-lockstep);
`tests/test_fes_i386_frontend.py` and `tests/test_fes_i386_lockstep.py` run
them from the unit-test suite when Verilator, the pinned oracle and (for the
lockstep) the pinned corpus are present. `scripts/benchmark_fes_i386.py`
routes the timing spikes.
