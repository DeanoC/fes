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

The EU uses alternating operand-read and execute/writeback phases at the
unchanged 100 MHz fabric clock. Four 16-bit registers separate the logic and
ADD operand muxes from the ALU. The dispatch byte is captured on the read edge
before the BIU can pop the queue. Architectural registers, flags, PC, stack,
address markers and jump flushing advance only on execute edges. This gives a
maximum of 50 million microwords per second; it is not a 50 MHz clock domain.

The unchanged 4096-word image is split into four byte-wide block ROMs by
`cpu_roms` in `scripts/sim_fes_pcxt.py`. Local ROM addresses capture the same
next-PC value on the same execute edge as the PC. The word and one-hot control
outputs also advance only on execute edges, keeping one immutable microword
through both phases. Reset initializes these outputs from the generated
reset-entry word, including a one-cycle warm reset. The sequencer retains its two-microword lead and flush
behavior. No instruction word changes.

The intervening operand-phase system edge reads the ROMs, so address setup
and the ROM-to-word/control paths each have one full 10 ns system cycle. The
former +90-degree ROM clock, its private hierarchy ports and its PLL output
are removed. SDRAM capture retains its separate +180-degree clock. There are
no multicycle exceptions; every path retains its physical-clock constraints.

The BIU continues every fabric cycle. A private EU-to-BIU strobe-valid signal
masks the held command's queue-pop, segment-load and timing-counter actions to
one read edge per microword. Persistent request/data fields remain available
on both phases. Completion synchronizers and sticky capture also run every
fabric cycle, including the operand phase.

The same generator emits `mcl86_facts.vh` from every control word. Separate ADD
operand muxes retain only sources used by ADD words; other operations use the
full muxes. The adder sum and carry flags are consumed only for ADD, so this
specialization covers arbitrary guest firmware and register values. A reserved
jump-source hold is retained automatically if the image uses it. Exhaustive
table checks and symbolic operand/jump-condition checks cover these boundaries. ADD controls use independent rising-edge register processes,
preserved through synthesis, to separate their physical fanout from the full
logic-operation muxes. The copies capture the same ROM stage on the same enabled edge;
symbolic read-alignment checks include both copies. DMA arbitration evaluates
all four circular priority masks in parallel; whole-DMA sequential equivalence
checks its state and interface against the original indexed priority loop.

Only the full logic operand-1 mux uses `pcxt_logic_operand1_mux` and the
post-synthesis `synth/logic_operand1_map.v` map. Six parallel masked groups
feed one OR level; the measured BIU-return and immediate groups use fast
inputs. ADD-specific muxes remain independently inferred. The generic helper
and mapped LUT truth tables are equivalent for every data/select vector,
including non-one-hot selects; structural checks require two data levels.

The native map also disconnects provably zero product inputs using tool-supplied
constant masks; exhaustive partial-constant proofs cover those transformations.



`tests/test_pcxt_cpu_selection.py` reconstructs all 4096 control words from
the byte images and checks ROM read alignment across both phases. Symbolic
checks cover local addresses under arbitrary reset/advance, the original
PC/stack/stall equations with execute-enable holds, operand mappings and jump
conditions. `tests/test_pcxt_eu_pipeline.py` checks the actual operand registers,
ALU and every intermediate carry for arbitrary inputs/resets, ROM word/control
holds, strobe masking and full-rate sticky completion. These are compositional
proofs, not whole-CPU sequential equivalence or silicon timing conformance.

FES selects `is8086=0`, `fake286_flags=0`, cycle timing enabled, byte-wide
accesses and no coprocessor wait. The nominal 4.772727 MHz pin clock remains
21/440 of the 100 MHz fabric rate and is sampled as data. The BIU's timing
counter still counts fabric clocks. Changing EU retirement latency can add
guest instruction delays even though those clocks remain unchanged.

`python3 scripts/sim_fes_pcxt.py --suite cpu-timing` measures NOP, dependent
ADD/ADC, branch-loop and REP-copy workloads, with result checks and zero/31/173
fabric-cycle storage waits. It reports both fabric and guest-clock intervals;
these are throughput measurements, not acceptance limits for 8088 cycle timing.
The ordinary CPU and assembled-system suites cover instruction behavior,
interrupts, waits, DMA/refresh and video with this execution schedule.

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
