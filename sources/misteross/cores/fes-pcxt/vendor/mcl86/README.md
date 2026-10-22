# MCL86 CPU import

This development CPU is Ted Fried's MCL86, as maintained in
[PCXT_MiSTer](https://github.com/spark2k06/PCXT_MiSTer/tree/af4f8a98ae562af0e3f9b86cc3a59673f999d0f6/rtl/8088),
commit `af4f8a98ae562af0e3f9b86cc3a59673f999d0f6`.
Only the execution unit, maximum-mode bus unit, adder, microcode ROM and CPU
wrapper are imported. No MiSTer platform, BIOS or game image is included.

The RTL files carry MIT notices for Ted Fried and Marcus Andrade. The upstream
repository's [GPL-3.0 license](LICENSE) is retained for the unmarked microcode
image. Preserve both the file notices and repository license when distributing
this import. This is MCL86 microcode, separate from PC system firmware.

[source.json](source.json) records upstream paths and SHA-256 hashes. A local
status fix drives previously undriven status bits `system_signals[15:14,10]` to zero.
The manifest records upstream and patched hashes for each changed file. Other
copied files retain upstream bytes except the timing changes described below.
Simulation and diagnostic synthesis verify these hashes before building. The microcode is imported as the upstream
4096-word image; this snapshot contains no matching assembler source. Do not
substitute the older MicroCoreLabs listing: it differs from this image.

The unchanged microcode is split into four byte-wide block ROMs. `cpu_roms`
in `scripts/sim_fes_pcxt.py` copies the raw image and emits
`mcl86_ucode_byte0.mem` through `mcl86_ucode_byte3.mem`. Each lane captures a
local address from the EU's next-PC value on the same system edge as the real
PC. The next-value equations preserve the original reset, jumps, stall and
call-stack behavior. Four kept register processes separate ROM address loads
from the PC's other consumers without another cycle.

The board reads those ROMs on a dedicated +90-degree 100 MHz clock and captures
the word and decoded one-hot controls on the next system edge. SDRAM capture
retains its separate +180-degree clock. The original registered-read latency
and two-word sequencer lead are preserved. Address setup has 2.5 ns and ROM
read/output has 7.5 ns; both require timing qualification. No instruction word
changes.

The same generator emits `mcl86_facts.vh` from every control word. Separate ADD
operand muxes retain only sources used by ADD words; other operations use the
full muxes. The adder sum and carry flags are consumed only for ADD, so this
specialization covers arbitrary guest firmware and register values. A reserved
jump-source hold is retained automatically if the image uses it. Exhaustive
table checks and symbolic operand/jump-condition checks cover these boundaries. ADD controls use independent rising-edge register processes,
preserved through synthesis, to separate their physical fanout from the full
logic-operation muxes. The copies capture the same ROM stage on the same edge;
symbolic read-alignment checks include both copies. DMA arbitration evaluates
all four circular priority masks in parallel; whole-DMA sequential equivalence
checks its state and interface against the original indexed priority loop.

The full logic operand-1 mux uses `pcxt_logic_operand1_mux` and the
post-synthesis `synth/logic_operand1_map.v` map. Six parallel masked groups
feed one OR level; the measured BIU-return and immediate groups use fast inputs.
ADD operand 0 has a separate `pcxt_add_operand0_mux` and
`synth/add_operand0_map.v`: the current mask leaves twelve products per bit,
implemented as four LUT6 groups and an OR LUT4. ADD operand 1 remains inferred.
Both maps preserve arbitrary data and selector vectors, including zero and
multiple simultaneous selects, without changing cycles or sequential state.
Structural checks require two data levels; complete routed timing remains open.

The native maps disconnect provably zero products using tool-supplied constant
masks; exhaustive partial-constant proofs cover those transformations.



`tests/test_pcxt_cpu_selection.py` reconstructs every control word from the four
byte images, then cuts only those immutable lookups and checks the actual
read/output stages at 90/180/270 degrees for arbitrary registered-address
payloads. An unbounded invariant checks all four local addresses after reset,
including later resets. A sequential equivalence check covers the 66 original
PC/call-stack/stall bits with arbitrary control and bus inputs; operand and
jump checks cover the real bindings for all input values. These are compositional
CPU proofs, not a monolithic whole-CPU equivalence result. All four byte files
must be beside the executable/netlist; the normal helpers generate them.
These are CPU control ROMs, separate from the guest firmware loader.

FES fixes the wrapper to `is8086=0`, `fake286_flags=0`, cycle timing enabled,
byte-wide accesses and no coprocessor wait. Its 100 MHz fabric clock generates
a nominal 4.772727 MHz pin clock by toggling at 21/220 of the fabric frequency.
The pin clock is sampled as data; all state uses the fabric clock. The imported
EU timing table also assumes 100 MHz. Changing that frequency requires a new
timing qualification, not just changing the pin-clock divider.

The bus unit has a four-byte 8088 prefetch queue and serializes word accesses.
Its empty flag compares the low four pointer bits directly, equivalent to
testing their four-bit difference for zero. An all-input SAT check proves
that replacement; the occupancy subtraction remains for the other flags.
FES connects its multiplexed bus through `pcxt_cpu_bus.sv`, holds READY low until
the byte transaction finishes, and preserves read data through the following
passive status phase. LOCK is propagated. INTA transactions are distinct from
memory and I/O. A fabric fault latches a diagnostic stop until reset.

Current evidence is the original FES diagnostic ROM, bus regression and locked
Cyclone V synthesis. These do not establish exhaustive instruction/undefined
flag correctness, silicon timing equivalence, self-modifying-code behavior,
interruptible REP, or game compatibility. HOLD/HLDA and DMA ownership arbitration
are not implemented. The upstream optional 8086 and fake-286 paths are never
selected by this FES wrapper; 8086 mode needs a separately qualified integration.
