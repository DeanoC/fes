# i386 lockstep oracle and corpus runners

Phase 0 of the i386 CPU design (DeanoC/fes#1068): an external emulator as the
lockstep oracle, checked against two corpora, with a divergence list naming the
deciding case for every disagreement. There is no CPU RTL yet.

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

| Corpus | Bochs 3.1 (level 3) | MAME 0.289 i386 |
| --- | --- | --- |
| SingleStepTests/80386 states passing (of 1,758,699) | 1,433,608 | _pending_ |
| test386.asm | reaches POST FFh with 9 divergence groups; POST EEh text identical to the reference | _pending_ |

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
linear address of the failing branch or faulting instruction.

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
- SingleStepTests has no protected-mode, V86 or paging set yet, so those paths
  are checked only by test386.

## Tests

`tests/test_fes_i386_oracle.py` covers MOO parsing, failure classification, the
divergence-list schema, listing parsing and test386 case extraction without
network access; its oracle smoke test runs when the pinned Bochs checkout is
already in `build/`.
