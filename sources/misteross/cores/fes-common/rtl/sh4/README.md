# FES SH-4 RTL

The first slice is an independently written, single-issue SH-4 integer core
targeting the DE25-Nano's Agilex 5. It separates fetch, decode and operand
capture, execution, and retirement with registers. Only one instruction is in
flight in the execution pipeline. A separate fetch stage looks up the next
halfword while the current instruction executes. Sequential register instructions
with warm cache hits and direct addressing retire every three clocks.

The 8 KiB direct-mapped instruction cache uses 256 lines of 32 bytes. Its data
and tags use synchronous M20K RAM. A miss fills one line with eight ordered
32-bit reads. Data accesses take priority between refill beats. Fetch redirects
discard the prefetched instruction and drain any abandoned response. External
writes accepted from the CPU, including copy-back write-backs, discard the
prefetched instruction and invalidate the corresponding cache index.
Software generating code in copy-back memory must first write back the data. P2 and
P4 instruction reads bypass the cache and issue only when the CPU needs the
instruction.

The 16 KiB direct-mapped data cache uses 512 lines of 32 bytes with synchronous
M20K data and tags and separate valid and dirty bits. Loads allocate on a miss.
Write-through stores update resident bytes on a hit and write to the external
bus without allocating on a miss. Copy-back stores allocate a line, update the
selected bytes and mark it dirty. A dirty victim writes all eight words to its
original address before the replacement refill. P2 and P4 bypass the cache.
An uncached alias access preserves dirty data by writing back the matching
line first. TAS.B also purges a matching clean line before the locked read.

The P4 CCR at `0xff00001c` controls cache enables, P1 copy-back mode, and
write-through for direct P0/P3 accesses. For translated pages, PTEL.WT
selects the write mode. CCR.ICI invalidates the instruction cache and CCR.OCI
discards the operand cache's valid and dirty state. Both command bits read as
zero. The core resets with caches disabled. The diagnostic sets its reset
parameter to enable both caches in write-through mode for its SRAM tests.
Data traffic, cache write-backs and instruction refills share the registered
single-outstanding bus.

OCBI discards a matching line, OCBWB writes a dirty line back and leaves it
resident, and OCBP writes back and invalidates. OCBI checks store permission,
while OCBWB and OCBP check load permission. MOVCA.L allocates a copy-back line
without a fill read. It initializes the other words to zero on allocation.
Software must treat those words as undefined, as on SH-4 hardware.

The MMU adds a fully associative 64-entry unified TLB and a four-entry
instruction TLB. Instruction misses search the UTLB and fill the ITLB using
the SH-4 LRUI state. Operand translations use the UTLB. The lookup pipeline
registers the match vector and reduces it in eight banks. Entries support
1 KiB, 4 KiB, 64 KiB and 1 MiB pages, ASID and shared-page matching, supervisor
single-virtual mode, permissions, the dirty bit and cacheability.

Longword accesses to the P4 control registers at `0xff000000` through
`0xff000010` and `0xff000034` expose PTEH, PTEL, TTB, TEA, MMUCR and PTEA.
`LDTLB` installs an entry at MMUCR.URC and MMUCR.TI clears both TLBs. P0/U0
and P3 translate when MMUCR.AT is set. The caches use virtual indexes,
physical tags, and the entry's C bit. Translated instruction lookups wait for the
architectural fetch PC to avoid speculative TLB changes. Direct-address
fetches still overlap execution.

Privileged longword accesses can read and write the ITLB address and data
arrays at `0xf2000000` and `0xf3000000`, and the UTLB arrays at
`0xf6000000` and `0xf7000000`. Data array 2 starts at `0xf3800000` or
`0xf7800000` and holds the page's SA and TC bits. LDTLB saves PTEA in the
entry, and an ITLB refill copies those bits with the mapping. An associative
UTLB address write uses PTEH.ASID and the supplied VPN, updates the matching
valid and dirty bits, and updates any matching ITLB entry. A multiple-hit
exception prevents the update. Indexed accesses and associative writes use
the translation pipeline's match and reduction stages.

The [Renesas SH-4 software manual](https://www.renesas.com/en/document/mas/sh-4-software-manual?r=1055166)
defines instruction encodings and behavior. The implemented slice includes:

- Register and immediate MOV and ADD, SUB, ADDC, SUBC, AND, OR, XOR, NOT, TST,
  integer compares, shifts and rotates, T-bit and S-bit access, extensions,
  SWAP, XTRCT, DT, and the DIV0U/DIV0S/DIV1 division steps.
- MUL.L, MULS.W, MULU.W, DMULS.L and DMULU.L, with pipelined partial products.
  MAC.W and MAC.L share the multiplier and use separate translated operand
  reads. Both postincrements and the accumulator update retire together.
  CLRMAC and register and stack transfers expose MACH and MACL.
- Byte, word, and longword MOV through a register, including predecrement
  stores and postincrement loads, base-plus-displacement forms, PC-relative
  literal loads, indexed and GBR-relative forms, and MOVA.
- TST.B, AND.B, XOR.B and OR.B with GBR-relative byte addressing, and
  TAS.B with a locked byte read and write. TAS.B checks store permission
  and the dirty bit before touching memory, then uses the uncached path.
- BT, BF, BT/S, BF/S, BRA, BSR, BRAF, BSRF, JMP, JSR, RTS, and NOP. The branch decision and
  target are captured before the delay slot executes.
- LDTLB, TRAPA, RTE, SLEEP, and register and stack forms of LDC/STC for SR,
  GBR, VBR, SSR, SPC, DBR and the inactive R0–R7 bank. STC also reads and
  stacks SGR. STS/LDS support PR register and stack transfers.

## Cache index and RAM modes

CCR.IIX substitutes effective-address bit 25 for bit 12 of the
instruction cache index. CCR.OIX substitutes bit 25 for bit 13 of the
operand cache index. Both modes retain physical tags. The P4 cache
arrays address raw cache entries in either mode.

With CCR.ORA and CCR.OCE set, operand cache entries 128–255 and 384–511
become two 4 KiB RAM banks. Data accesses at `0x7c000000–0x7fffffff`
use those banks without translation or external bus activity. Bank
selection uses effective-address bit 13 in normal index mode and bit
25 in OC index mode. Bits 11–0 select a byte within the bank. Normal
cache accesses use the remaining 8 KiB. Byte enables apply to RAM
writes, and an aligned paired transfer reads or writes both words in
the bank. Instruction fetches from this range raise an address error
while RAM mode is enabled.

## Cache arrays

Privileged longword accesses can read and write instruction cache tags
and words at `0xf0000000` and `0xf1000000`, and operand cache tags and
words at `0xf4000000` and `0xf5000000`. Indexed tag writes set the physical
tag and valid bit, plus the dirty bit for the operand cache. An operand
retag writes back a valid dirty victim before replacing the tag.
Associative writes leave a mismatched or invalid line alone. With
translation enabled they compare the supplied virtual tag through the
ITLB for instructions or UTLB for operands. Translation misses leave
the line intact. Software is responsible for maintaining coherence
between distinct virtual indexes that alias the same physical address.

## Floating-point execution

FPSCR selects the FR and XF banks, controls pair size for FMOV, and holds
the architectural mode and exception fields. The transfer path supports
FPUL and FPSCR register and stack moves, the FPUL/FR moves FLDS and FSTS,
FRCHG, FSCHG, FLDI0, FLDI1, FABS and FNEG, and all single and paired FMOV
addressing forms. A paired memory transfer uses two ordered longwords
through the normal operand path. It checks eight-byte alignment and
translation before touching memory and retires its destination or
address update after both words complete. The little-endian interface
carries the low word of a pair first.

The SR.FD gate distinguishes a general FPU-disable exception (0x800)
from a delay-slot FPU-disable exception (0x820). The handler may clear FD
in SSR and use RTE to retry.

FADD, FSUB, FMUL, FDIV, and FSQRT share classification, rounding, and
result packing for single and double precision. FMUL uses registered
18-bit DSP partial products and carry propagation between their columns.
FDIV and FSQRT use a restoring digit step with a registered trial
subtraction. Single precision generates 26 quotient or root bits, and
double precision generates 55, followed by a sticky bit from the remainder.

FCMP/EQ and FCMP/GT write SR.T. FLOAT converts a signed integer in FPUL
to the selected floating-point precision. FTRC truncates to a signed
integer in FPUL. FCNVSD widens a single-precision value in FPUL into a
double register, and FCNVDS rounds a double into a single in FPUL.
These instructions share the arithmetic unit's classification, rounding,
or alignment stages.

FMAC uses the single-precision multiply datapath, aligns the full product
with the old FRn value, and rounds once after addition. FIPR and FTRV
share a four-term dot-product datapath. It retains 28 significand bits
per product and aligns the products to 30 bits before summing them.
FIPR writes the last lane of its destination vector. FTRV reads the
column-major matrix from XF and buffers all four output lanes until
FPSCR exception checks pass. The vector operations report inexact
because they use finite-width intermediate results.

FSRRA reuses the digit engine to compute a reciprocal square root. It
retains 30 root digits before division, then rounds the reciprocal once.
FSCA uses the low 16 bits of FPUL as a fraction of a revolution. It
interpolates one quarter-wave table for both sine and cosine and commits
the register pair together. The high 27 bits of each ROM word hold
the sample rounded from `2^26 * sin(pi * i / 2048)`. The low 17 bits hold
the difference to the next rounded sample.

The arithmetic supports nearest-even and toward-zero rounding, FPSCR
denormal handling, and the SH-4 NaN encodings. An arithmetic exception
updates FPSCR cause and sticky flags and enters VBR + 0x100 with EXPEVT
0x120 before changing the destination. The SH-4's O/U/I
software-completion traps also apply to normal operands with an exact
result. FSQRT and FLOAT use the inexact enable for this behavior.
FMAC, FIPR, and FTRV check the O/U/I enables for all operands. FTRV
also checks the invalid-operation enable. FSCA reports inexact for
every input. FSRRA reports inexact for positive normal inputs.

## Prefetch and store queues

Outside the queue window, PREF fills a cacheable 32-byte operand line. It
ignores disabled or uncached accesses and suppressed translation faults
leave TEA and PTEH unchanged.

Aligned longword stores to `0xe0000000` through `0xe3ffffff` populate the
two 32-byte store queues. The address selects a queue at bit 5 and a word
at bits 4–2. PREF in that window publishes the selected queue to one
aligned external line. With translation disabled, QACR0 and QACR1 at
`0xff000038` and `0xff00003c` supply the destination's upper address bits.
With translation enabled, the UTLB supplies the destination and checks
store permission for a queue write and read permission for queue PREF.
MMUCR.SQMD can restrict queue access to supervisor mode.

The core waits for the queue transfer to complete before retiring PREF.
The shared-bus interface marks its eight ordered longword writes with
`mem_sq`, sampled with an accepted request. No CPU instruction read
interleaves the marked writes. An adapter can use the marker and beat
address to assemble a native 32-byte transaction. Software must initialize
the queue contents before publishing them.

## On-chip register aliases

Uncached TLB mappings into physical addresses `0x1f000000–0x1fffffff`
reach the corresponding control register addresses in P4. The TLB
controls access to the mapped page, including user-mode access.
The alias and direct P4 paths share the CCR, QACR, event and MMU
registers. Other mapped control-register requests reach the bus at
their canonical P4 addresses. An invalid alias access records its
original virtual address in TEA.

## Exception entry and return

TLB misses enter at VBR + 0x400. Address errors, illegal instructions, TLB
protection and initial-write exceptions, and TRAPA enter at VBR + 0x100. Entry
saves SR, the restart PC and R15 in SSR, SPC and SGR, sets SR.MD, SR.RB and
SR.BL, and records EXPEVT. Delay-slot faults save the branch PC for replay.
TRAPA saves the next PC and makes its immediate available through TRA. The
longword P4 registers are TRA at `0xff000020` and EXPEVT at `0xff000024`.

Privileged SR.RB selects the second bank of R0–R7. RTE restores SR from SSR
and jumps to SPC after executing its delay slot. The slot is fetched with
the original privilege and executes with the restored SR and register bank.
The guest TLB handler can read TEA and PTEH, load PTEL, execute LDTLB, then
return and retry the faulting access.

Maskable interrupts enter at VBR + 0x600 between instructions. A request
must exceed SR.IMASK. SR.BL masks requests during execution and ordinary
sleep. Standby wake can enter with SR.BL set. Interrupt entry saves the next
PC in SPC, saves SR and R15 in SSR and SGR, switches to the privileged register
bank, and exposes the event in INTEVT at P4 address `0xff000028`. Delayed
branches retire their slots before interrupts can enter. NMI latches a rising
edge, takes priority over the maskable request, and uses event code 0x1c0.
SLEEP suspends instruction issue until an eligible interrupt arrives.

The interrupt controller presents a synchronous selected priority
in `irq_level` and event code in `irq_event` until the CPU pulses `irq_ack`.
Level zero means idle. The synchronous `nmi` input is edge-triggered, and
`nmi_while_blocked` lets the controller permit NMI while SR.BL is set.

Board bus signaling, PTEA PCMCIA behavior and instruction cases outside
the supported sets still need integration.
A synchronous fault with SR.BL set reports EXPEVT 0x020, restarts the CPU
at the reset vector and leaves the peripherals running. Instruction and operand TLB multiple hits
restart the CPU and apply a manual reset to the peripherals. They report
EXPEVT 0x140 and preserve TEA and PTEH for the boot code. The core waits for
accepted bus transactions before restarting. The SH-4 manual leaves
exceptions in an RTE delay slot undefined. This core restarts in that case.

With `STOP_ON_RESET_EXCEPTION` enabled for simulation, terminal fault codes 1–4 indicate an unsupported or privileged opcode, a
prohibited delay slot, an unaligned fetch, and an unaligned data access.
Codes 5–10 indicate a TLB miss, multiple hit, protection violation, initial
page write, address error, and TRAPA. `fault_pc` records the instruction or
attempted fetch at a diagnostic stop. TLB faults capture the virtual address
in TEA and its page number in PTEH. General registers have no defined reset
values. Software must initialize them before reading them.

## User break controller

BARA/BARB, BAMRA/BAMRB, BBRA/BBRB, BDRB, BDMRB and BRCR occupy
the UBC P4 window at `0xff200000`. BASRA and BASRB hold the ASID
comparands. A and B can act independently or as an A-then-B sequence.
Channel B can also compare the operand data. The controller reports
match flags in BRCR and uses EXPEVT `0x1e0` on exception entry.
BRCR can select DBR as the destination.

Pre-execution breaks use the architectural PC and can stop a delayed
branch before its slot runs. Post-execution breaks allow retirement
before saving the next PC in SPC. Operand matching observes the cache
and external data path using the guest virtual address. It also sees
cache maintenance addresses. Some internally handled CPU register and
store-queue accesses bypass the operand comparator. The normal
instruction path overlaps fetch when neither channel is configured.

## Bus state controller

The `fes_sh7091` wrapper routes BSC accesses to on-chip registers and
forwards other requests to its external memory port. BCR and WCR
registers expose the configured widths and timing fields. The refresh
timer counts the supplied `bus_tick` enables, supports keyed writes
and sticky interrupt flags, and exposes compare and overflow requests.
Writes to the SDMR apertures emit area, mode/precharge, and address
signals to the board memory controller. The wrapper's external
memory port uses a request/response handshake. The board memory
controller drives SDRAM wire timing.

The wrapper also handles the three TMU channels and INTC priority
registers. TMU counts down from the programmed value and reloads on
underflow. It accepts prescaled peripheral clocks, RTC ticks, and
external clock edges. Channel 2 captures its count and signals an
interrupt and a DMA request when configured. The priority encoder
combines timer and refresh interrupts with the external encoded
request. Acknowledgement returns to the external device only for
an external interrupt.

The INTC control register selects the external interrupt mode and NMI
edge. The board supplies the inverted NMI pin as `nmi` and the inverse
of the four IRL pins as `irq_level`. In encoded mode, the board also
supplies the event code. Independent mode supplies the SH7091 IRL levels
and events. ICR reports the pin level, supports MAI masking and clears
NMIB on NMI entry. Changing the edge selection inhibits detection for
six `bus_tick` enables. The board provides synchronized and filtered inputs.

## General-purpose I/O

The GPIO block provides the PCTRA, PDTRA, PCTRB, PDTRB and GPIOIC
registers at the BSC's P4 addresses. BCR2.PORTEN enables the twenty
GPIO pins. Inputs are synchronized and sampled on `bus_tick`.
The wrapper exposes the output latch, output enables and pull-up
enables for the board to apply to the shared pins. BCR2, pin direction
and the interrupt mask survive a manual reset. Data latches retain
their values during reset.

The sixteen port A inputs can generate a shared active-low level
interrupt. The handler reads PDTRA to identify the inputs. IPRC
sets its priority and INTEVT reports `0x620`.

## Clock and standby control

FRQCR exposes the requested clock ratios, PLL settings and clock output
enable. The core keeps one FPGA fabric clock. The wrapper exports the
programmed control and a change pulse for the board clock-enable and
PLL logic. The reset value is parameterized.

STBCR selects standby on SLEEP and controls module stops. STBCR2 selects
deep sleep, which defers new DMA units until wake. Ordinary sleep leaves
the peripherals running. The wrapper exports processor status, standby
mode, and the peripheral high-impedance and pull-up requests for the
board pins. Module stops gate the register ports and the relevant
peripheral activity. SCI state initializes when stopped and its output
pins hold their previous levels.

The RTC stays active in standby. NMI, RTC interrupts and eligible
external interrupts can start a watchdog stabilization count. The CPU
takes the wake interrupt only after the count wraps. This wrap neither
requests a reset nor sets an overflow flag. The board supplies the
watchdog and oscillator clock enables, and implements the physical
clock and pin changes.

## Watchdog timer

The watchdog timer supports the keyed WTCNT and WTCSR writes, eight
prescaler settings and interval interrupt event `0x560`. The board
supplies the divider input as synchronous `watchdog_tick` enables.
Watchdog overflow resets the core and its peripherals and publishes the
selected reset kind on `watchdog_reset` for the board. The external bus
responder flushes outstanding responses when either reset output is set.
The timer and its overflow state survive that internal reset. The reset handler reads
EXPEVT `0x020` following a manual watchdog reset. Physical oscillator and PLL control and reset of external devices are
outside this wrapper. See the watchdog and interrupt controller chapters
of the [Renesas SH7750-series hardware manual](https://www.renesas.com/en/document/mah/sh7750-sh7750s-sh7750r-group-users-manual-hardware?r=1055161).

## Real-time clock

The RTC takes a synchronous 32.768 kHz enable on `rtc_tick`. It supplies
the TMU and TCLK output from the oscillator divider and exposes the BCD
calendar, alarm and control registers through P4. Alarm, periodic and
carry requests use the RTC priority in IPRA. The clock continues counting
through processor reset when its oscillator enable is active. Divider reset and 30-second adjustment are available
through RCR2. The board supplies oscillator startup and persistence
across FPGA reconfiguration.

## Serial interfaces

SCI exposes the SH7091 byte registers at `0xffe00000`. It has separate
transmit and receive data registers and supports asynchronous framing,
multiprocessor reception, and internally or externally clocked
synchronous transfers. The SCI pin interface exposes RxD, TxD and SCK
with separate output enables. SCI smart-card mode remains outside the wrapper.

SCIF exposes its byte and word registers at `0xffe80000`. It has
16-byte transmit and receive FIFOs, programmable thresholds, an
asynchronous baud generator, modem flow control and loopback. The
external clock input is a synchronous enable at 16 times the bit
rate. The board implements the external pin directions.

Both blocks feed INTC and can request byte DMA independently of their
interrupt enables. The DMAC supports their transmit and receive
resource selectors, and routes the serial beat to the local register
port. A simultaneous CPU access to the same peripheral waits for that
beat. The external memory half of the transfer uses the normal bus.

## DMA controller

The four-channel DMAC exposes SAR, DAR, DMATCR, CHCR and DMAOR through
the SH7091 P4 register window. It supports dual-address memory copies, single-
and dual-address DREQ transfers on channels 0 and 1, and TMU2 capture requests.
The capture value can supply the data for a 32-bit transfer. DREQ inputs are
active low, with an active-high acknowledgement when the DMAC starts a unit.

The DMA engine buffers each 1-, 2-, 4-, 8- or 32-byte transfer unit and
moves it through the shared 32-bit port. Completion and address errors
feed INTC. DMA accesses physical memory and software manages cache
coherency around DMA buffers.

With DMAOR.DDT set, the board can submit a 64-bit DTR command on
`ddt_valid && ddt_ready` with `ddt_kind=0`. Kind 1 requests another unit
on the previous DTR channel, and kind 2 requests a unit directly on channel 2.
DTR commands configure channel 0 from the request fields, with support for
continuous edge bursts, per-unit requests, and transfer termination.
Channels 1–3 use their CPU-programmed registers and can queue four external
requests apiece. A board bridge can use the direct channel-2 request for
Dreamcast display-list, YUV or texture transfers.

Single-address transfers use a separate device port. A beat transfers on
`device_valid && device_ready` with data packed in the low-order bytes.
`device_write` selects the memory-to-device direction. `device_channel`
identifies the active DMA channel, `device_mask` marks the bytes in the beat,
and `device_last` marks the end of the transfer unit. The board bridge
supplies physical DBREQ/BAVL/TR/TDACK and DREQ pin timing. The Holly register
block that generates Dreamcast DMA requests remains to be integrated.

## Memory contract

`mem_valid && mem_ready` accepts one aligned byte-addressed request.
`mem_wstrb == 0` selects a read, with `mem_rmask` marking the requested byte
lanes. Cache fills and instruction reads request a whole word. A write
completes when accepted, with one strobe per little-endian byte lane.
A read returns exactly one `mem_rvalid` pulse at
least one cycle after acceptance. The core holds its request while stalled and
has at most one outstanding read. Already accepted instruction reads can
complete after a redirect or diagnostic fault. Cacheable executable memory
must permit speculation. All cacheable memory must permit whole-line fills.
External writers must reset the core before execution resumes to invalidate
both caches and flush outstanding reads in the surrounding memory interface.
The bus carries translated physical addresses for mapped accesses and retains
architectural addresses for direct accesses, including the default reset
address `0xa0000000`.

For TAS.B, `mem_lock` stays high from the first byte read request through
acceptance of the matching byte write. A shared-bus arbiter must preserve
exclusive ownership from acceptance of the locked read until the locked
write is accepted. The core defers intervening instruction reads. Any dirty TAS.B line is
written back before the lock begins. The
SH7091 arbiter preserves TAS ownership and keeps all eight store-queue
writes together. The diagnostic RAM has no read side effects.

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
data. The exception tests execute an in-guest refill handler for a data read,
an instruction fetch, and a store in a branch delay slot. They check restart
through RTE, the restored register bank, and the general TRAPA vector.
Additional guest programs exercise signed and unsigned division and
multiplication, flag updates, indexed moves, and nested calls that save PR
on the stack. MAC tests cover both saturation widths, shared-base
postincrement, and a TLB miss on the second operand followed by RTE.
Control transfer tests cover stack round trips, user-mode GBR transfers,
privilege violations and a faulting SR load. They also check that an
LDC.L changing SR.RB increments the original bank's address register.
Byte-operation tests cover lane selection, preservation of neighboring bytes,
T-bit behavior, lock duration, and a TAS.B retry after a TLB miss. Read-only
and initial-write faults must precede any TAS.B bus request. Copy-back tests
cover partial-byte store allocation, eviction to the victim's address, PTEL.WT
selection, TAS.B on a dirty P2 alias, and dirty-line discard through CCR.OCI.
Line-operation tests cover permission checks on misses, preserving or
discarding resident lines, and MOVCA.L allocation without refill traffic.
Store-queue tests cover QACR selection, UTLB translation, ordered bursts,
privilege and initial-write faults, and PREF misses both inside and outside
the queue window. TLB array tests cover indexed data and attributes, ITLB
refill, associative invalidation and refresh, ASID matching, and multiple
matches. A guest also enables translation using mappings installed through
the P4 arrays. Cache-array regressions exercise injected instruction
words, associative invalidation, translated operand tags, and dirty
write-back on an operand retag. Index-mode tests cover instruction and operand
indexes and raw P4 entries. Operand RAM tests cover mirrored banks,
byte masks, paired transfers, concurrent use of the cache half, and
access with the MMU enabled. Floating-point transfer tests cover both
banks, single and paired moves, endian order, alignment, system-register
stack transfers, a TLB retry, and FPU-disable exception return. Arithmetic tests
cover signed cancellation, normalization, ties, overflow, subnormal results,
NaNs, divide by zero, and precise arithmetic traps with delay-slot replay.
Comparisons and conversions cover signed zero, NaNs, integer saturation,
narrowing and integer rounding, both precisions, and preservation of SR.T
on an invalid-operation trap. FMAC tests distinguish fused rounding from
separate multiply and add. Vector tests check the matrix bank, lane
destination and preservation of the full destination on an FTRV trap.
FSCA and FSRRA regressions exercise guest decode, the inexact trap,
signed zero, infinity, and denormal inputs.
Interrupt tests cover priority masking, delay-slot completion, RTE,
SLEEP and wakeup, and NMI priority and block behavior. BSC checks cover
reset straps, access widths, masked fields, refresh keys and flags,
and the address carried by an SDRAM mode write. TMU checks cover
underflow and reload, capture and DMA request acknowledgement, and
priority tie handling. A guest program wakes from SLEEP on a TMU0
interrupt, clears the source in its handler, and resumes through RTE.
DMA checks cover a stalled read, a 32-byte unit, byte lane steering, register
writeback, sticky flag acknowledgement, an alignment error, a capture
transfer and an external request. Demand DMA tests check DTR setup,
repeat and halt commands, queued requests and device backpressure. A guest
programs the Dreamcast channel-2 register values and sends two 32-byte units
to the device port. The shared-bus tests check that DMA
respects TAS and store-queue ownership. A guest configures a DMA copy,
wakes on the completion interrupt, and reads the destination after RTE.
ICR regressions cover both NMI edges, masking and independent IRL input.
A guest uses NMIB to accept NMI with SR.BL set and reads the cleared NMIB.
Watchdog checks cover keyed accesses, divider timing, interrupt priority,
and a guest that restarts at the reset vector and reads WOVF and EXPEVT.
RTC checks cover all three interrupt sources, leap and ordinary calendar
rollover, time adjustment and reset retention. A guest wakes on an RTC
periodic interrupt and resumes through RTE. Serial tests cover FIFO
loopback, multiprocessor filtering, SCI clock modes, and interrupt
priority. Guest programs send data from RAM and receive it back into
RAM through separate TX and RX DMA channels on each serial port.
GPIO tests cover register widths, pin direction, pull-ups and reset
retention. A guest enables BCR2.PORTEN, wakes on a port A interrupt,
reads the triggered input and resumes through RTE.
CPG regressions check FRQCR writes, module-stop access, standby wake
after watchdog stabilization and direct NMI wake from deep sleep.
The DMA test checks that a paused controller defers a pending transfer.
Guest UBC tests check before and after instruction breaks, delayed
branches, a DBR vector, and a store watched by address and data.
Focused tests check address and ASID masks, sequential A/B conditions
and discarding a post-execution match when the instruction is abandoned.

The Quartus Pro build uses the shared builder lease and leaves reports under
`out/sh4/fit/output_files`. It targets
the DE25-Nano part at 200 MHz. The clock is the only physical port in the
virtual-pin project. The fit and timing reports cover internal logic and RAM.
Pinout, programming and on-board behavior require a board-level design.
