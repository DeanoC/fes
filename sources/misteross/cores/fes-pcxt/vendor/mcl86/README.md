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
module reads both images on the same clock/address, preserving the single-clock
read latency and the EU's two-word sequencer lead. No instruction word changes.

`tests/test_pcxt_cpu_selection.py` symbolically checks every ROM address with
registered read alignment and compares the real operand wiring/selection against
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
