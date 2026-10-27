# i386 lockstep oracle, corpus runners and RTL checks

Phase 0 of the i386 CPU design (DeanoC/fes#1068): an external emulator as the
lockstep oracle, checked against two corpora, with a divergence list naming the
deciding case for every disagreement. Phase 1 adds Verilator checks of the
[front-end, D and AG RTL](../../rtl/i386/README.md), the front end against the
oracle, the [whole-CPU lockstep](#whole-cpu-lockstep) of `fes_i386_cpu`
against the oracle, and the routed [timing spikes](#timing-spikes).

Everything here is test-only. The oracle, the corpora and the tools that build
them are fetched into the ignored `build/` directory and never enter a package
or an RTL source list.

## Oracle

The oracle is the [Bochs](https://github.com/bochs-emu/Bochs) CPU at revision
`30cfd566ecc8deb33e56e801e32b444a7041b6d2` (tag `REL_3_1_FINAL`,
LGPL-2.1-or-later), configured as a 386DX:

| Option | Why |
| --- | --- |
| `--enable-cpu-level=3` | 386 instruction set, CR0 reserved bits read as 1, no CPUID, BSWAP/XADD/CMPXCHG/INVD/WBINVD/INVLPG raise #UD, EFLAGS.AC not writable |
| `--enable-fpu` | WAIT and ESC take the architected EM, MP and TS exceptions. Without the FPU Bochs raises #UD for WAIT and never faults on ESC. The FES CPU's empty-socket data results (all-ones stores) are not oracle-checked; no corpus case depends on them |
| `--enable-x86-debugger` | DR0-DR3, DR6 and DR7 breakpoints |

`scripts/fes_i386_oracle.py` fetches that revision only when a runner is given
`--download`, refuses a checkout that is not the clean pinned commit, configures
it with the options above and builds the CPU, FPU and softfloat libraries plus
Bochs' parameter tree. Nothing else of Bochs is compiled.

### Choice

The design allowed MAME's i386 core or Bochs at CPU level 3, whichever passes
more of the corpora. Both were built behind the same adapter interface and run
against both corpora:

| Corpus | Bochs 3.1 (level 3) | MAME 0.289 `i386_device` |
| --- | --- | --- |
| SingleStepTests/80386 states passing (of 1,758,699) | 1,433,608 | 1,213,134 |
| test386.asm | reaches POST FFh; 9 divergence groups; POST EEh text identical to the reference | stops after POST 20h (hangs in the ring-3 tests); 12 failed checks before that; no POST E0h, EEh or FFh |

Bochs passes 220,474 more SingleStepTests states and is the only one that
finishes test386. MAME matches the 386EX where Bochs does not on 68,555 states,
almost all AF after shifts (63,622). Bochs matches where MAME does not on
289,029, including no #GP for real-mode limit violations such as a word access
at offset FFFFh (89,415, which the design requires), no #SS on stack wrap
(15,017), AND/OR/XOR/TEST leaving AF unchanged (100,073), a missing #UD for an
invalid LOCK (16,278), wrong RCL/RCR results (13,663) and MOV CS without #UD.
The MAME build used for this comparison (MAME's own sources against a stub
device framework, with data breakpoints stubbed) is not kept; nothing in the
corpora enables DR7.

### Adapter

[`i386_oracle.h`](i386_oracle.h) is backend-neutral; [`bochs_oracle.cpp`](bochs_oracle.cpp)
implements it and supplies the platform objects the Bochs CPU links against.

- `step()` either takes the event pending at the instruction boundary (a debug
  trap, NMI or INTR) and stops at the handler's first instruction, or executes
  one instruction. A faulting instruction delivers its exception within the
  step and does not retire. A REP string instruction retires in one step
  unless a trap is pending between iterations. The result lists the events
  (exception, external interrupt, NMI, software interrupt), the linear and
  physical address and size of every write, and whether the CPU is halted or
  shut down.
- `get_state()` / `set_state()` cover the GPRs, EFLAGS, EIP, the six segment
  registers with their hidden caches, LDTR, TR, GDTR, IDTR, CR0, CR2, CR3 and
  DR0-DR3, DR6, DR7.
- Memory and I/O are served by a `Bus`. `page()` hands the oracle 4 KiB-aligned
  host pages ([`PageMemory`](i386_oracle.h) allocates them); instruction fetch
  needs them. Other memory goes through `read()` and `write()`. INTR is a level
  sampled at instruction boundaries and acknowledged through
  `interrupt_acknowledge()`; NMI is an edge, blocked until IRET.
- `set_a20()` masks physical bit 20. `invalidate()` tells the oracle the
  harness changed memory behind it.
- `decode()` returns Bochs' decode of one instruction without executing it:
  whether it ends within 15 bytes, its length, and whether the configured CPU
  raises #UD for it. It touches no CPU state.

Bochs pauses a REP string instruction whenever it stores near code it has
decoded; the adapter continues it within the step. A REP that overwrites its
own encoding is re-decoded by Bochs, which is one of the listed divergences.

## Corpora

### SingleStepTests/80386

`scripts/test_fes_i386_vectors.py` runs
[SingleStepTests/80386](https://github.com/SingleStepTests/80386) at revision
`459d49fbe6280e9ed46fee887b58dacd9cb880ab`: 1,758,700 real-mode instruction
states captured from an 80386EX (MIT; data public domain). It reads only the
README, LICENSE, `revocation_list.txt` and the MOO files, verifies every file
against its pinned Git blob, and skips revoked tests.

Each test loads the initial registers and RAM, steps the oracle until it
executes the test's terminating HLT, and compares architectural state only: the
20 MOO registers (EFLAGS bits 0-17), every final RAM byte, writes to bytes the
corpus did not list, and the exception or interrupt taken. Bus cycles are not
compared. Undefined flags are compared; the corpus decides them.

```sh
python3 scripts/test_fes_i386_vectors.py --download
python3 scripts/test_fes_i386_vectors.py --opcode D3.4 --opcode 6661
```

`FES_I386_TRACE=1` makes the testbench print every step. A run writes
`build/sim/fes-i386-vectors/report.json` with the failures grouped by file and
failing key.

### test386.asm

`scripts/test_fes_i386_test386.py` assembles
[test386.asm](https://github.com/barotto/test386.asm) at revision
`cfd052d1e64d5375dea5a681c1eadeed64ceda2c` (GPL-3.0-or-later, test input only)
with NASM 2.16.03 (BSD-2-Clause; the release tarball is hash-checked and built
in `build/`). The ROM is the 128 KiB version with every configuration EQU set:
`TEST_UNDEF=1`, `CPU_FAMILY=3`, POST codes on port 190h, text on port E9h, no
LPT or COM output.

[`test386_tb.cpp`](test386_tb.cpp) maps the ROM at E0000h and FFFE0000h with RAM
below it, boots from reset, records each POST code, keeps the POST EEh text and
writes the final 640 KiB of RAM to `build/sim/fes-i386-test386/test386-dump.bin`.
The runner compares the text with `test386-EE-reference.txt`.

test386 halts at its first failed check, which would hide every later test, so
the testbench steps over exactly the checks the divergence list names, by the
linear address of the failing branch or faulting instruction. Checks are mapped
through the NASM listing; a check in code test386 copies to RAM is named by its
address and instruction bytes instead.

```sh
python3 scripts/test_fes_i386_test386.py --download
```

### Cross-check

The test386 runner also checks test386's i386 undefined-behaviour cases
against SingleStepTests states with the same inputs: the TEST_UNDEF flag cases
(AAA, AAS, AAD, AAM, DAA, DAS, SHL, SHR, BT*, RCL, RCR), PUSH Sreg with a 32-bit
operand, POPAD with a 16-bit stack and LEA with SIB index 100b. It needs the
SingleStepTests corpus; `--no-cross-check` skips it.

## Divergence list

[`divergences.toml`](divergences.toml) has one entry per behaviour where the
oracle and a corpus disagree, or the corpora disagree with each other. Each
entry names its deciding case, says what each side does, and which side the
FES CPU follows. The runners fail on any failure no entry covers and on any
entry that no longer matches a failure.

| Section | Entries | Covers |
| --- | --- | --- |
| `[[vectors]]` | 21 | all 325,091 failing SingleStepTests states |
| `[[test386]]` | 9 | every failed test386 check (655 stepped over in one run) |
| `[[corpora]]` | 0 | the corpora agree on all 8,695 overlapping states |

Most entries are i386 undefined flags (BT* OF, MUL/IMUL/DIV flags, BSF/BSR,
BCD adjusts, shift AF/OF/CF), where Bochs follows later Intel CPUs. The rest are
real-mode details Bochs does not model: SIB index 100b with a scale, POPAD's
ESP high word, the order of a real-mode interrupt's vector read and pushes,
partial PUSHAD/POPA transfers that hit the stack limit, LOCK #UD before the
15-byte limit, ARPL without a write, and a REP overwriting itself. One entry
follows the oracle: the 386EX answers port 22h from an on-chip register, which
is not CPU behaviour.

## Known limits

Behaviour the design specifies that no corpus covers yet, where the oracle
differs. Directed tests in later phases decide these, not the oracle:

- Bochs models a 387: MOV CR0 forces ET to 1, ESC instructions return 387
  results and FERR is not routed. The FES CPU models an empty socket.
- MOV to or from TR6 and TR7 (0F 24, 0F 26) raises #UD in Bochs; the design
  accepts them.
- LOADALL is not modelled.
- Bochs below CPU level 6 keeps the 0F 38 and 0F 3A three-byte escapes in its
  decoder table without an opcode table and dereferences null decoding them.
  `decode()` reports both as #UD without calling the decoder; `step()` on
  either still crashes Bochs. No corpus case reaches them.
- SingleStepTests has no protected-mode, V86 or paging set yet, so those paths
  are checked only by test386.

## Front-end and address checks

`scripts/test_fes_i386_frontend.py` builds three Verilator testbenches and
writes `build/sim/fes-i386-frontend/report.json`:

| Testbench | Scope | Coverage |
| --- | --- | --- |
| [`lendec_tb.cpp`](lendec_tb.cpp) | The per-byte length decode, L1's 16 decoders feeding L2's sums through [`lendec_tb_top.sv`](lendec_tb_top.sv) | Exhaustive: no prefix or each of the eleven, every one- and two-byte opcode, every ModRM and every SIB byte, in 16- and 32-bit default size, against `decode()`. Each byte's length for all four prefix-accumulator variants, against the oracle's decode behind the variant's extra 66h and 67h; each variant's end and next-half bit. Up to four cases share an evaluation and rotate through all 16 decoders. Plus directed checks of prefix runs and of MOV TR6/TR7 |
| [`frontend_tb.cpp`](frontend_tb.cpp) | F1 to Q through [`frontend_tb_top.sv`](frontend_tb_top.sv) | Sampled: random bytes (with and without D stalls), prefix-heavy bytes, long prefix runs, and generated defined instructions with 0-4 prefixes whose issue cycles are checked exactly, as is each redirect's first issue. Then random bytes, instructions and long instructions again with each fetch block delayed 0-12 cycles and garbage on the port until it arrives, untimed. A directed 32-bit case delays a fetch response after Q releases the first half; a 10-byte MOV crosses back into it. The check requires Q to wait for the response and then match Bochs. |
| [`dag_tb.cpp`](dag_tb.cpp) | D's address operands and AG through [`dag_tb_top.sv`](dag_tb_top.sv), cycle by cycle | Every memory ModRM, every SIB and both address sizes as a uop stream whose producers move through AG, M1, M2, EX and WB, most writing a register one of the next uops reads: each cycle D must stall exactly when the late-operand rule says, and AG must otherwise give the architectural linear address and offset; a stalled uop is checked again until it leaves. Then each kind of result into the next uop's base, unscaled index and scaled index, timed against the design's latencies. Needs no oracle |

A case is compared where the oracle defines the encoding; for LOCK, the
encoding without LOCK, since LOCK's validity is D's check. An instruction the
oracle cannot fit in 15 bytes must come out `too_long`.

Fetch latency is what reaches Q's next-half availability term: without it L2
always holds the next block when Q leaves a half. Each `*-fetch-latency`
stream counts the cycles in which Q left a half that L2 held no block for
(`l2_empty_release`, read from the RTL's internals, so a run on Yosys's
netlist reports zero). The memory model is not an L1I: the L1I slice's real
misses still need the check repeated with them.

The bench's `mem_valid` qualifies the response for the block at the registered
memory address. The directed case withholds block 2 after block 1 has filled
the other half, so the L2 pipeline empties while Q releases the first half.
Q jumps from a far CALL at byte 15 to a 10-byte MOV at byte 23. The MOV's
first bytes are in half 1 and its immediate crosses into the released half 0;
the test verifies that it does not issue until block 2 arrives.

```sh
python3 scripts/test_fes_i386_frontend.py --download
python3 scripts/test_fes_i386_frontend.py --bench frontend --instructions 50000 --seed 7
python3 scripts/test_fes_i386_frontend.py --yosys /abs/path/to/i386-locked/yosys
```

`--yosys` runs every check on the netlist that Yosys writes from the RTL
instead of the RTL itself. The spikes are built from Yosys's reading of the
SystemVerilog, and Yosys has misread constructs without an error. Use the
Yosys of the i386 lock ([`rtl/i386/toolchain.lock`](../../rtl/i386/toolchain.lock));
the repository lock's older Yosys misreads the RTL's per-byte records.

## Whole-CPU lockstep

[`lockstep_tb.cpp`](lockstep_tb.cpp) runs `fes_i386_cpu` beside the oracle.
The oracle steps one instruction and the RTL runs until it retires one; the
two architectural states are then compared: EIP, the eight GPRs, EFLAGS bits
0-17 and the six segment registers with their hidden caches. Memory writes
are compared as each side reports them: each instruction must write the same
bytes, with the same values, on both sides. An instruction that faults in
the oracle must fault in the RTL with the same vector, nothing of it
retired. Both deliver it, and the delivery is compared as a retirement, its
frame included. A fault in a delivery, which the oracle shows as a second
fault or a shutdown, must stop the RTL as not built (vector 8), with nothing
of the instruction retired and nothing of the delivery written. What the
instruction wrote before its fault may stand: ENTER's pushes, or a REP
string's elements with their steps of ESI, EDI and ECX. Each byte the RTL
wrote must be one the oracle wrote before its delivery began (each oracle
event counts the writes before it), with the oracle's value unless the
oracle's partial frame wrote it again. The RTL may write less: its far CALL
and INT check their slots before they push, as the 386EX does. HLT ends a
run on both sides. Every byte either side writes is
cleared after the run.

The RTL starts each run from the state its `init_*` ports load at reset. Its
memory model serves the fetch port with a random 0-12 cycle delay per block
and garbage on the port until the block arrives
(`FES_I386_LATENCY=1`, the default), and the data port the same per load:
a load's bytes are memory as it was on the edge M1 sent the address, read
before the store retiring on that edge writes, with garbage on the port until
they arrive. The random runs report the cycles spent waiting for them. The
model's `Latency` port class is what later slices' MMIO and I/O ports take.

Where the oracle and SingleStepTests disagree, [`divergences.toml`](divergences.toml)
says which side the FES CPU follows. For an entry that follows the corpus,
each retirement it applies to leaves the fields it names out of the
comparison with the oracle, and the oracle takes the RTL's values so both run
on from the same state, memory included; the testbench works out each
entry's opcode stem and `when` predicates from the instruction, as the
vectors runner does. An entry that leaves every field to the corpus also
decides whether the instruction faults and with which vector, and whether
the RTL halts after it: the RTL's outcome stands. The corpus itself then checks those values.

`scripts/test_fes_i386_lockstep.py` drives it two ways:

| Source | What runs | Checked |
| --- | --- | --- |
| SingleStepTests/80386 | Every state of the instruction forms the CPU executes (the runner's `SCOPE` table), from its initial state to its HLT | Every retirement against the oracle, then the RTL's final state against the corpus's, undefined flags and RAM included |
| Random programs | Register, immediate and 16-bit-addressed memory forms of the same instructions with segment, REP, 66h and 67h prefixes (and now and then a LOCK), forward branches, bounded LOOPs, backward jumps, pushes and pops, CALLs to subroutines that return with RET or RET imm16 (two deep, their bodies leaving ESP, SS and the return address alone), far CALLs returning with RETF or RETF imm16, through a pointer in the instruction or one stored just before, far JMPs, PUSH BP / MOV BP, SP / LEAVE frames, the far pointer loads, XCHG, MOV with a direct offset, XLAT, SALC, PUSH and POP r/m, CALL r/m near through a register or a stored pointer (until a far transfer moves CS), PUSHA and POPA, PUSHF/POPF pairs, interrupt blocks (SS above the vector table, the vector's entry stored as the handler's CS:IP, INT n, INT3 or INTO, and a handler that IRETs), the string instructions with REP, REPE and REPNE (SI, DI and a CX of 0-9 set first, now and then CLD or STD; a quarter REP MOVS or CMPS over two seeded words at SI, overlapping within 4 bytes, with ES = DS set by PUSH DS / POP ES just before), and ENTER / LEAVE frames at any nesting level, from random register states and zeroed memory. A far target is a CS:IP for the target's linear address, so the program's state is chosen before its layout | Every retirement against the oracle; a run stops at its HLT, a delivered exception, 2,000 retirements or a store into its own code, which needs self-modifying-code detection the RTL does not have yet. Two seeds run again without latency and must retire the same trace (a hash of every retirement's state) |
| Directed faults | 66 E8 with a 32-bit target past FFFFh, a 32-bit RET to 10000h, and JMP EBX and CALL EBX to 10000h, with the #GP entry at a HLT; ENTER whose check of its final slot, at SP FFFFh, raises #SS; INT 20h at SP 1, the 66 E8 case at SP 3, and ENTER 0, 2 at SP 5 (its frame pointer's push straddles the wrap), whose deliveries fault | Bochs throughout; #GP(0) or #SS(0) delivered with the faulting CS:IP pushed, then the handler's HLT followed by 32 cycles with no fetch request, D issue or data read. The nested cases stop the RTL with nothing of the delivery written |

The latency-free reruns also give the cycles between retirements: in
sequence, after an instruction with more than one prefix, after a taken
branch, after a shift or rotate, and after a store. These check D's holds
cycle-exactly, or the run fails: an instruction that consumes the flags
(ADC, SBB, Jcc, SETcc, RCL, RCR, LAHF, CMC, LOOPE, LOOPNE) must retire two
cycles after a shift and anything else one; an instruction that reads memory
must retire at least two cycles after a store, and any other instruction
without a memory operand one; a push or pop right after another must retire
one cycle behind it, as its ESP is early from AG, unless it is a pop after a
push (a load after a store) or follows a segment load. CALL, PUSH and POP
of r/m, RETF, PUSHA, POPA, INT and IRET (several uops), LEAVE, PUSH and POP
of (E)SP, which read ESP as data, and anything after POP (E)SP or POPA,
which load ESP, are left out of that check. XCHG of registers, three uops,
has a bucket of its own, and a store counts for the store holds only as an
instruction's last uop (not XCHG's with memory, nor POP m's, nor a string's
or ENTER's). ENTER is left out of the stack hold as the later instruction.
Anything within five instructions after a string has a bucket of its own:
one that reads ESI, EDI or ECX waits in D until the string's last element
retires.

A fault or a software interrupt (INT n, INT3, INTO) is the corpus's
exception: the run goes on into the handler, to its HLT, and the corpus's
final state, the pushed frame included, is compared. A random program stops
once it has delivered an exception, after the delivery is compared.

Each later slice adds its instruction forms to `SCOPE`, its ports to the
memory model, and its instructions to the random generator; the comparison
and the divergence handling stay.

```sh
python3 scripts/test_fes_i386_lockstep.py --download
python3 scripts/test_fes_i386_lockstep.py --opcode 00 --opcode D3.4 --random-programs 0
python3 scripts/test_fes_i386_lockstep.py --yosys /abs/path/to/i386-locked/yosys
```

`--yosys` simulates the netlist the i386 lock's Yosys writes from the CPU
(after `proc -norom; flatten; opt -purge`) instead of the RTL.

## Timing spikes

[`spike/`](spike) holds five routed spikes, each a block inside a
stimulus and signature envelope with one clock, KEY0 and LED0 on pads:

| Spike | Block |
| --- | --- |
| `spike_frontend.sv` | F1, F2 behind a 16 KiB 4-way L1I model (M10K tags and data, compares against a registered ITLB page, way select with the hit signals replicated per two bytes), L1, L2, the buffer and Q |
| `spike_dag.sv` | D's address operands (addressing decode, register file, the forwarding table, scaling, segment select) and AG, with the producers moving through AG-EX registers; D's opcode decode, not built, is stimulus |
| `spike_m2.sv` | The M2 stage of a 16 KiB 4-way L1D: M10K tag and data reads, four tag compares against a registered page, way select |
| `spike_cpu.sv` | `fes_i386_cpu`, F1 to WB, behind `spike_frontend`'s L1I model, with a registered fetch-valid bit and the reset state from the stimulus |
| `spike_rom.sv` | A registered case-table ROM, synthesized without `proc -norom` so it maps to an M10K: checks DeanoC/yosys#40's fix, since the i386 RTL keeps its own tables in logic |

`scripts/benchmark_fes_i386.py` synthesizes them with the selected lock's
authenticated Yosys, by default the i386 lock's (M10K and DSP allowed, no LUT
RAM), and routes each seed
with nextpnr's GPU router through `gpu-run`, CPU fallback rejected. Timing is
allowed to fail so a miss is measured; it writes `summary.json` and
`runs.jsonl` with each seed's fmax, critical path, a figure per pipeline
stage (the worst setup slack among the registers the stage writes) and the
envelope's worst endpoints. Each JSONL row joins the route to the lock, tool,
source, synthesis, logic-depth, and cell-count identities from `summary.json`;
`--version` supplies its run label. `cells_by_stage` counts the labeled FF and
M10K cells. Each synthesis also records every stage's deepest logic path
before placement: LUT levels, carry cells and the named nets on it.

```sh
FES_TOOLCHAIN_CACHE_ROOT=/abs/out/cache/misteross-toolchains \
  FES_TOOLCHAIN_LOCKFILE="$PWD/cores/fes-common/rtl/i386/toolchain.lock" \
  FES_TOOLCHAIN_GPU_ROUTER=HIP FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' \
  scripts/bootstrap.sh   # provisions the i386 lock's toolchain once
python3 scripts/benchmark_fes_i386.py --cache-root /abs/out/cache/misteross-toolchains \
    --version "phase 1 whole CPU"
```

## Tests

`tests/test_fes_i386_oracle.py` covers MOO parsing, failure classification, the
divergence-list schema, listing parsing and test386 case extraction without
network access; its oracle smoke test runs when the pinned Bochs checkout is
already in `build/`. `tests/test_fes_i386_frontend.py` covers the runner and
the spike harness's report reading; it lints the spikes and runs the D and AG
check when Verilator is installed, runs the oracle-backed front-end checks when
the pinned Bochs checkout is present too, and runs the D and AG check on
Yosys's netlist when `FES_I386_YOSYS` names a Yosys.
`tests/test_fes_i386_lockstep.py` covers the lockstep runner's scope,
divergence and output handling, and runs a short lockstep when Verilator, the
pinned Bochs checkout and the pinned corpus are present (and on Yosys's
netlist when `FES_I386_YOSYS` names a Yosys).
