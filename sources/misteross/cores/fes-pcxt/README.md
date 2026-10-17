# FES PC/XT development

This development slice runs an original diagnostic ROM on an 8088 CPU through
an eight-slot virtual ISA backplane. The [machine design](../../docs/architecture.md#proposed-fes-ibm-pcxt-core)
targets an 8088/8086 PC/XT with IBM graphics and extensions to the existing
linkable-card mechanism. The motherboard slice adds memory decoding, an 8259A
PIC and an 8253 PIT. It has no PPI/keyboard, DMA controller, graphics, disk
controller, BIOS, board top, physical socket map or package producer.
It cannot boot DOS or PC/XT games yet. Conventional RAM and ROM are testbench
models; no physical SDRAM controller is connected.

## Implemented path

`pcxt_cpu.sv` selects the pinned [MCL86 import](vendor/mcl86/README.md) in 8088
mode, with a four-byte prefetch queue, original reserved FLAGS behavior and
byte-wide external transfers. The fabric clock must be 100 MHz; the synchronous
fractional pin clock is nominally 4.772727 MHz. `pcxt_cpu_bus.sv` translates its
address/data phases to byte transactions and holds READY/data until sampled.
It propagates LOCK, reports both interrupt-acknowledge bytes as locked, and
stops on a latched fabric fault until reset. `halted` stays asserted from the
halt bus cycle, including later prefetches, until the next non-fetch bus
status. There is no
8086 selector, turbo setting, 80186 extension or fake-286 FLAGS mode in FES.
DMA ownership and HOLD/HLDA remain unimplemented.

`pcxt_motherboard.sv` latches one CPU byte request and routes it to conventional
RAM (`00000–9FFFF`), read-only system ROM (`F0000–FFFFF`), a local device or ISA.
ROM writes complete without reaching storage. Video/option-memory apertures
remain on ISA; vacant slots return `FF`. Storage has VALID/READY admission and
one DONE response, with stable address/data until completion. Reset cancels
pending work and must also reset the downstream storage and ISA participants.
This is a storage interface, not an SDRAM implementation.

The MIT-licensed [KF8259](vendor/kf8259/README.md) and
[KF8253](vendor/kf8253/README.md) imports are pinned to the same upstream commit
as the CPU. PIC ports `20–3F` use A0; PIT ports `40–5F` use A1:0. Both ignore
A15:10, matching the XT motherboard decode. INTA bypasses I/O decode and drives
the real PIC's two-pulse sequence. IRQ0 comes from PIT channel 0, IRQ1 from the
future keyboard input, and IRQ2–7 from ISA. The timer clock divides CPU guest
clock enables by four (nominal 1.193182 MHz); it continues through CPU waits and
HLT. All counter pins are synchronous data, not generated RTL clock domains.
Channels 0/1 have gates tied high; channel 2 has an external gate and all three
outputs are exposed. Refresh DMA and port-61h speaker control are not connected.
Local reads/writes have one setup, eight active and nine release fabric clocks;
CS/address remain stable through release so imported bus-edge detectors retire
side effects before DONE. These timings are development integration choices,
not a claim of original motherboard wait-state equivalence.

`pcxt_isa_bridge.sv` latches a byte transaction, collects registered card decode
responses, waits for READY and sends one commit. `pcxt_isa_boundary.sv` supplies
one request and one response register, matching the existing linkable-slot
pipeline model. `pcxt_isa_endpoint.sv` separates device preparation from side
effects and acknowledges a commit exactly once. Vacant slots read as `FF`;
conflicting or stale card responses fault before commit. An owner that
withdraws after COMMIT also faults and drains, without a second commit. IRQ
and DRQ remain live while the CPU is waiting.

The [shared fabric contract](../../../mister-packages/docs/pcxt-isa.md) defines
the wire fields and timing. It is emitted from mister-packages, not duplicated
in the core. The portable boundaries have no BEL reservations; they do not
establish CRAM containment or admit PC/XT cards into the production linker.

`expansions/pcxt_isa_probe.sv` is an original diagnostic card with eight I/O
registers, 16 bytes of scratch memory and a DMA loopback byte. Its configurable
I/O mask exercises partial-address aliases. Default mappings are ports
`300–307` (ten-bit decode), memory `C8000–C800F`, IRQ5 and DMA2.

| Register offset | Read | Write |
| --- | --- | --- |
| 0 | Data byte | Replace data byte |
| 1 | Flags | Bit 0 raises IRQ; bit 1 raises DRQ |
| 2 | Read count before this read | Ignored |
| 3 | Write count | Ignored |
| 4 | Fabric wait cycles (0–255) | Set wait cycles for subsequent transfers |
| 5 | DMA byte | Set DMA byte |
| 6 | Committed DMA-byte count | Ignored |
| 7 | DMA terminal-count count | Ignored |

All I/O writes increment the write counter, including writes to read-only
registers; all I/O reads increment the read counter at commit. Counters wrap
at 256. Memory transfers and DMA use separate state. Terminal count is counted
once per committed DMA byte that carries it. The card is a test device, not a
historical ISA adapter.

## Validation

From `sources/misteross`:

```sh
make sim-fes-pcxt
```

Verilator runs the bridge, all eight boundaries and real probe endpoints in
slots 1 and 8. The C++ driver checks I/O aliases, full 16-bit I/O decode, AEN,
memory, unmapped accesses, delayed READY, held VALID, IRQ/DRQ, DMA direction and
terminal count. Fault injection checks duplicate claims, stale transaction tags
and missing decode acknowledgements, a claimant disappearing during a wait, and
the selected owner disappearing after COMMIT. A deterministic randomized memory test
uses an independent scoreboard. Reset is swept across 60 positions in a delayed
transfer. Each position checks that a commit already pending at the card does
not pulse on the reset edge, and that reset clears pending operations without
a phantom completion.

The CPU suite assembles the original `sim/diagnostic.S` with GNU binutils
(`as` and `ld`, x86 target, `.arch i8086`). It starts at physical `FFFF0` and
checks integer arithmetic, multiply/divide, CALL/RET, REP word strings at odd
addresses, segment and 20-bit address wrap, LOCK, native FLAGS, ISA word I/O,
port-FFFF wrap, partial decode, option memory and unmapped reads. Probe counters
check exactly-once side effects with 160 fabric clocks of card wait. Software
INT/IRET, two locked INTA cycles, a halt that stays asserted until wakeup, and
NMI use a testbench vector source;
the CPU-only fixture deliberately retains this independent stimulus. Port 80h
is a progress sink in that fixture only.
Seven complete runs vary memory latency/backpressure and recover from reset
during an outstanding fetch and an ISA conflict. No BIOS calls are intercepted.

The motherboard bus suite checks RAM/ROM boundaries, ROM write suppression,
ISA holes and option memory, decode aliases, PIC masking/IRR/ISR, priority,
specific/non-specific EOI, rotation, poll, automatic EOI, level/edge requests,
spurious IRQ7 and real ISA IRQ5 delivery. Timer tests cover all six modes:
mode-0 gating, latched two-byte reads, zero reload (65536), BCD borrow,
mode-2/3 frequency and odd/even duty cycles, mode-1 one-shot and mode-4/5
strobes. Channel 1 counts and channel 0 continues during a long storage wait.
Reset tests cancel an outstanding storage write and sweep all local bus phases.
These are bounded integration tests; cascade, special mask mode, all counter
reload/gate corner cases and exact original-silicon timing are not qualified.

The integrated machine suite boots the original `sim/motherboard.S` ROM on the
8088. It programs PIC/PIT, wakes from HLT for three timer IRQ0 handlers, then
services ISA IRQ5 and clears its source. The testbench supplies only RAM/ROM,
checks both locked INTA cycles and their vectors, and observes success in RAM.
Three runs vary storage latency/backpressure; no host-supplied vectors or I/O
shortcuts participate in this fixture.

Verilator lint is fatal. `sim/cpu.vlt` and `sim/chipset.vlt` scope exceptions to
imported files and the fixtures' shared reset (asynchronous in imported chips,
synchronous in FES RTL). Run a suite alone with
`python3 scripts/sim_fes_pcxt.py --suite isa`, `--suite cpu`,
`--suite motherboard` or `--suite machine`.
Outputs live under `build/sim/fes-pcxt/{isa,cpu,motherboard,machine}/`. The parent affected-test planner and
CI simulation matrix include this simulation-only family. Passing these tests
is host evidence only. Physical socket placement/routing, exact-shell archives,
firmware composition and hardware acceptance remain required before a linkable
PC/XT package can be published. The existing closed linker layouts are unchanged.

The optional diagnostic synthesis command uses the same authenticated toolchain
as the existing Coleco producer:

```sh
make synth-fes-pcxt-isa CACHE_ROOT=/absolute/misteross-toolchains
python3 scripts/synth_fes_pcxt.py --suite cpu --cache-root /absolute/misteross-toolchains
python3 scripts/synth_fes_pcxt.py --suite machine --cache-root /absolute/misteross-toolchains
```

It synthesizes the simulation fixture (two probe cards, eight portable slots,
fault-injection controls) and writes `build/synth/fes-pcxt/synth.json`,
`yosys.log` and `diagnostic.json` with source hashes and compiler identity.
The CPU variant includes the CPU and external memory ports, writing the same
files beneath `build/synth/fes-pcxt/cpu/`. The machine variant adds the
motherboard/PIC/PIT under `build/synth/fes-pcxt/machine/`; locked Yosys
`5391eeb1e` maps that fixture to 1,993 FFs, 2,729 LUT/arithmetic cells and 20
M10Ks. Neither variant synthesizes the testbench RAM or diagnostic system ROM.
The cell counts describe these fixtures only.
These commands perform no placement,
routing, package sealing or deployment.
