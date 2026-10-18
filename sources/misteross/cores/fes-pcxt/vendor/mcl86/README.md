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

Local timing changes register predecoded operand controls in a separate ROM and
use parallel selection instead of binary operand mux trees. `cpu_roms` in
`scripts/sim_fes_pcxt.py` copies the original `mcl86_ucode.mem` unchanged and
derives `mcl86_operand_select.mem` from its two four-bit select fields. The ROM
module reads both block-ROM images on the dedicated 180-degree clock and captures them
in local rising-edge output registers. The EU address is exclusively a
rising-edge register, so the original rising-edge read latency and two-word
sequencer lead are preserved. Both half-cycle paths need routed qualification.
No instruction word changes.

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

Only the full logic operand-1 mux uses `pcxt_logic_operand1_mux` and the
post-synthesis `synth/logic_operand1_map.v` map. Six parallel masked groups
feed one OR level; the measured BIU-return and immediate groups use fast
inputs. ADD-specific muxes remain independently inferred. The generic helper
and mapped LUT truth tables are equivalent for every data/select vector,
including non-one-hot selects; structural checks require two data levels.



`tests/test_pcxt_cpu_selection.py` exhaustively checks all 4096 generated ROM
rows, then cuts the immutable lookup and symbolically checks the actual
two-phase read/output stages for arbitrary 32-bit payload transitions at
registered addresses, against the original rising-edge ROM. It also compares the real operand wiring/selection against
the imported equations for all register values and select fields. The ROM-copy
and selector files must both be beside the executable/netlist build; the normal
simulation and synthesis helpers generate them. These are CPU control ROMs,
separate from the guest BIOS/firmware loader.

FES fixes the wrapper to `is8086=0`, `fake286_flags=0`, cycle timing enabled,
byte-wide accesses and no coprocessor wait. Its 100 MHz fabric clock generates
a nominal 4.772727 MHz pin clock by toggling at 21/220 of the fabric frequency.
The pin clock is sampled as data; all state uses the fabric clock. The imported
EU timing table also assumes 100 MHz. Changing that frequency requires a new
timing qualification, not just changing the pin-clock divider.

The bus unit has a four-byte 8088 prefetch queue and serializes word accesses.
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
