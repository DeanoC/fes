# FES PC/XT development

This development slice runs an original diagnostic ROM on an 8088 CPU through
an eight-slot virtual ISA backplane. The [machine design](../../docs/architecture.md#proposed-fes-ibm-pcxt-core)
targets an 8088/8086 PC/XT with IBM graphics and extensions to the existing
linkable-card mechanism. This slice has no motherboard chipset, graphics,
disk controller, BIOS, board top, physical socket map or package producer.
It cannot boot DOS or PC/XT games yet. Conventional RAM and ROM are testbench
models; no physical SDRAM controller is connected.

## Implemented path

`pcxt_cpu.sv` selects the pinned [MCL86 import](vendor/mcl86/README.md) in 8088
mode, with a four-byte prefetch queue, original reserved FLAGS behavior and
byte-wide external transfers. The fabric clock must be 100 MHz; the synchronous
fractional pin clock is nominally 4.772727 MHz. `pcxt_cpu_bus.sv` translates its
address/data phases to byte transactions and holds READY/data until sampled.
It propagates LOCK and stops on a latched fabric fault until reset. There is no
8086 selector, turbo setting, 80186 extension or fake-286 FLAGS mode in FES.
DMA ownership and HOLD/HLDA remain unimplemented.

`pcxt_isa_bridge.sv` latches a byte transaction, collects registered card decode
responses, waits for READY and sends one commit. `pcxt_isa_boundary.sv` supplies
one request and one response register, matching the existing linkable-slot
pipeline model. `pcxt_isa_endpoint.sv` separates device preparation from side
effects and acknowledges a commit exactly once. Vacant slots read as `FF`;
conflicting or stale card responses fault before commit. IRQ and DRQ remain
live while the CPU is waiting.

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
and missing decode acknowledgements and a claimant disappearing during a wait. A deterministic randomized memory test
uses an independent scoreboard. Reset is swept across 60 positions in a delayed
transfer to check pending operations and response draining.

The CPU suite assembles the original `sim/diagnostic.S` with GNU binutils
(`as` and `ld`, x86 target, `.arch i8086`). It starts at physical `FFFF0` and
checks integer arithmetic, multiply/divide, CALL/RET, REP word strings at odd
addresses, segment and 20-bit address wrap, LOCK, native FLAGS, ISA word I/O,
port-FFFF wrap, partial decode, option memory and unmapped reads. Probe counters
check exactly-once side effects with 160 fabric clocks of card wait. Software
INT/IRET, two INTA cycles, HLT wakeup and NMI use a testbench vector source;
this is not an implemented 8259. Port 80h is a testbench progress sink.
Seven complete runs vary memory latency/backpressure and recover from reset
during an outstanding fetch and an ISA conflict. No BIOS calls are intercepted.

Verilator lint is fatal; `sim/cpu.vlt` scopes width and unused-wire exceptions
to imported CPU files. Run either suite alone with
`python3 scripts/sim_fes_pcxt.py --suite isa` or `--suite cpu`.
Outputs live under `build/sim/fes-pcxt/{isa,cpu}/`. The parent affected-test planner and
CI simulation matrix include this simulation-only family. Passing these tests
is host evidence only. Physical socket placement/routing, exact-shell archives,
firmware composition and hardware acceptance remain required before a linkable
PC/XT package can be published. The existing closed linker layouts are unchanged.

The optional diagnostic synthesis command uses the same authenticated toolchain
as the existing Coleco producer:

```sh
make synth-fes-pcxt-isa CACHE_ROOT=/absolute/misteross-toolchains
python3 scripts/synth_fes_pcxt.py --suite cpu --cache-root /absolute/misteross-toolchains
```

It synthesizes the simulation fixture (two probe cards, eight portable slots,
fault-injection controls) and writes `build/synth/fes-pcxt/synth.json`,
`yosys.log` and `diagnostic.json` with source hashes and compiler identity.
The CPU variant includes the CPU and external memory ports, writing the same
files beneath `build/synth/fes-pcxt/cpu/`. It does not synthesize the testbench
RAM or diagnostic system ROM. The cell counts describe these fixtures only.
These commands perform no placement,
routing, package sealing or deployment.
