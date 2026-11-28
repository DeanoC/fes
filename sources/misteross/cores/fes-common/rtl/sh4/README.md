# FES SH-4 integer RTL

The first slice is an independently written, single-issue SH-4 integer core
targeting the DE25-Nano's Agilex 5. It separates fetch, decode and operand
capture, execution, and retirement with registers. Only one instruction is in
flight in the execution pipeline. A separate fetch stage looks up the next
halfword while the current instruction executes. Sequential register instructions
with warm cache hits and direct addressing retire every three clocks.

The 8 KiB direct-mapped instruction cache uses 256 lines of 32 bytes. Its data
and tags use synchronous M20K RAM. A miss fills one line with eight ordered
32-bit reads. Data accesses take priority between refill beats. Fetch redirects
discard the prefetched instruction and drain any abandoned response. CPU stores
discard the prefetched instruction and invalidate the cache index of the write,
including corresponding instruction addresses in aliases of that index. P2 and
P4 instruction reads bypass the cache and issue only when the CPU needs the
instruction.

The 16 KiB direct-mapped data cache uses 512 lines of 32 bytes, synchronous
M20K data and tag arrays, and a separate valid bit per line. Loads allocate on
a miss. Stores write through to the external bus with the original byte
strobes. On a hit they update the selected cache bytes, and a store miss
writes without allocating. An uncached or differently tagged store invalidates
the matching index to cover address aliases. P2 and P4 data accesses bypass
the cache. The execution pipeline waits for a load result or for the external
bus to accept the store. Data traffic and instruction refills share the
registered, single-outstanding bus.

The MMU adds a fully associative 64-entry unified TLB and a four-entry
instruction TLB. Instruction misses search the UTLB and fill the ITLB using
the SH-4 LRUI state. Operand translations use the UTLB. The lookup pipeline
registers the match vector and reduces it in eight banks. Entries support
1 KiB, 4 KiB, 64 KiB and 1 MiB pages, ASID and shared-page matching, supervisor
single-virtual mode, permissions, the dirty bit and cacheability.

Longword accesses to the P4 control registers at `0xff000000` through
`0xff000010` and `0xff000034` expose PTEH, PTEL, TTB, TEA, MMUCR and PTEA.
`LDTLB` installs an entry at MMUCR.URC and MMUCR.TI clears both TLBs. P0/U0
and P3 translate when MMUCR.AT is set. Translated accesses use physical cache
addresses and the entry's C bit. Translated instruction lookups wait for the
architectural fetch PC to avoid speculative TLB changes. Direct-address
fetches still overlap execution.

The [Renesas SH-4 software manual](https://www.renesas.com/en/document/mas/sh-4-software-manual?r=1055166)
defines instruction encodings and behavior. The implemented slice includes:

- Register and immediate MOV and ADD, SUB, ADDC, SUBC, AND, OR, XOR, NOT, TST,
  integer compares, single-bit logical and arithmetic shifts, and T-bit access.
- Byte, word, and longword MOV through a register, including predecrement
  stores and postincrement loads, base-plus-displacement forms, PC-relative
  literal loads, and MOVA.
- BT, BF, BT/S, BF/S, BRA, BSR, JMP, JSR, RTS, and NOP. The branch decision and
  target are captured before the delay slot executes.
- LDTLB and register forms of LDC/STC for SR.MD and SR.T.

FPU, the remaining integer and privileged state, exception entry and RTE,
interrupts, copy-back mode, software cache controls, raw TLB arrays, area-7
MMU register aliases, board buses and PTEA PCMCIA behavior remain outside this
slice. The core exposes sticky diagnostic faults until reset.

Codes 1–4 indicate an unsupported or privileged opcode, a prohibited delay
slot, an unaligned fetch, and an unaligned data access. Codes 5–9 indicate a TLB
miss, multiple hit, protection violation, initial page write, and address
error. `fault_pc` identifies the offending instruction or attempted fetch.
TLB faults capture the virtual address in TEA and its page number in PTEH. General registers have
no defined reset values. Software must initialize them before reading them.

## Memory contract

`mem_valid && mem_ready` accepts one aligned byte-addressed request.
`mem_wstrb == 0` selects a read. A write completes when accepted, with one strobe
per little-endian byte lane. A read returns exactly one `mem_rvalid` pulse at
least one cycle after acceptance. The core holds its request while stalled and
has at most one outstanding read. Already accepted instruction reads can
complete after a redirect or diagnostic fault. Cacheable executable memory
must permit speculation. All cacheable memory must permit whole-line fills.
External writers must reset the core before execution resumes to invalidate
both caches and flush outstanding reads in the surrounding memory interface.
The bus carries translated physical addresses for mapped accesses and retains
architectural addresses for direct accesses, including the default reset
address `0xa0000000`.

The diagnostic top resets to address zero and exposes a host-side word loader
while reset is asserted. It uses four synchronous byte banks for a 2 KiB
unified memory. In this top, out-of-range reads return zero and out-of-range
stores are discarded.

## Validation

From the repository root:

```sh
make -C sources/misteross sim-fes-sh4
make -C sources/misteross/cores/fes-common/rtl/sh4 fit QUARTUS_ROOTDIR=/data/altera_pro/26.3/quartus
```

The Verilator test runs hand-encoded integer code against the synchronous RAM
and a memory model that stalls requests and delays read responses. It checks
warm-hit retirement cadence, cache conflicts, abandoned fills, instruction
stores, uncached fetches, and fault retirement and restart. The operand cache
test checks warm hits, partial writes, conflict refills, an uncached alias
read and write, and a cold store that leaves the line unallocated. MMU checks
cover all page sizes, ITLB replacement and hits, ASID/shared-page rules,
faults, and guest code that programs the TLB and accesses remapped code and
data.

The Quartus Pro build uses the shared builder lease and leaves reports under
`out/sh4/fit/output_files`. It targets
the DE25-Nano part at 200 MHz. The clock is the only physical port in the
virtual-pin project. The fit and timing reports cover internal logic and RAM.
Pinout, programming and on-board behavior require a board-level design.
