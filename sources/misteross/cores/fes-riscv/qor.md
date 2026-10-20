# fes.riscv QoR on the DE10-Nano

CPU-only size and routed clock for `fes_rv32_cpu`, measured with the locked
Yosys/nextpnr-mistral/Mistral flow on `5CSEBA6U23I7`, next to five other FPGA
RISC-V cores in the same harness. This is not a sealed package, not a kit
result, and not the HDMI system number.

The sealed `fes.riscv` system (CPU, 32 KiB RAM, framebuffer, raster, mailbox)
is a different design. Its host seal closed 74.25 MHz at 75.18 MHz on an older
toolchain pin, with 2,829 `MISTRAL_COMB`, 761 `MISTRAL_FF` and 54 `MISTRAL_M10K`,
video included. Do not quote that fmax as the CPU fmax, or this table as the
package.

The table was measured on FES `8f5c5b598`, before the model-generated ALU
on main (`322e38353`). That ALU still computes `sum` as `a + b` or `a - b`,
so the critical-path description below still names the same assignment.
The production package built from that ALU is the full system, not this
harness. Its first HIP route was 79.051 MHz, 2,755 ALUTs and 761
flip-flops. See the CPU README. Do not mix that package figure into this
table.

## Method

Toolchain used for this historical measurement: Yosys `5391eeb1`, Mistral `8fcc4cb4`,
nextpnr `1656e473`. Synthesis was `synth_intel_alm` with lutram and DSP
allowed, so each core could infer a register file. None used a DSP or an
MLAB. The sealed producer still passes `-nolutram -nodsp`; that flag was not
mixed into this comparison.

Route: device `5CSEBA6U23I7`, HeAP, timing weight 10, criticality exponent 2,
seed 1, HIP GPU router on the RX 7900 XTX, `--freq 200` with timing allowed
to fail. Every core missed 200 MHz. The clock below is `timing.json` achieved
fmax after routing. One seed only. On this device a different seed moves fmax
by several MHz, so clocks within about 5 MHz are the same band.

Harness, identical except for the bus the core already has: one clock on
`PIN_V11`, one registered observation pin on `PIN_W15`, reset and interrupt
from an LFSR rather than pads, and a 16 KiB one-cycle byte-write RAM. Core
M10K counts exclude that RAM. A live second port became a second copy of the
RAM (16 blocks per port; a 4096-deep M10K is 2 bits wide), not one true
dual-port block. That copy is harness, not core.

Upstream pins: PicoRV32 `ef203c2`, SERV `f200eb2`, DarkRISCV `74590ce`,
LiteX VexRiscv Verilog `642ecfe`.

## Results

| Core | Config | COMB | FF | Core M10K | Routed fmax |
| --- | --- | ---: | ---: | ---: | ---: |
| SERV | RV32I, bit-serial, CSR, no M/C | 255 | 202 | 1 | 114.7 MHz |
| VexRiscv Min | RV32I, pipelined, no cache, no multiplier | 1,090 | 798 | 2 | 82.7 MHz |
| PicoRV32 | RV32I, multicycle, IRQ and timer, iterative shift, no M/C | 1,621 | 720 | 2 | 114.8 MHz |
| VexRiscv Lite | RV32I, pipelined, instruction cache, no multiplier in the netlist | 1,854 | 1,048 | 5 | 75.2 MHz |
| FES `fes_rv32_cpu` | RV32I, Zicsr, Zicntr, multicycle, barrel shifter, three interrupts | 1,887 | 442 | 2 | 79.7 MHz |
| DarkRISCV | RV32I, 3-stage, CSR, interrupt, no M | 1,912 | 737 | 2 | 83.6 MHz |

The part has 83,820 `MISTRAL_COMB` (41,910 ALMs) and 553 M10Ks. The FES CPU
is 1,887 COMB cells, about 2.3% of the device. Its two core M10Ks are the
register file. Its 442 flip-flops are the lowest of the word-wide cores for
that reason. VexRiscv Lite's five M10Ks are the register file plus three
instruction-cache blocks; it is not a bare CPU.

FES, VexRiscv Min and DarkRISCV (79.7, 82.7, 83.6 MHz) are one band on this
seed. PicoRV32 and SERV at 115 MHz are actually faster clocks. VexRiscv Lite
at 75.2 MHz is the cache clock of a larger configuration, not evidence that
a pipeline is slower.

## Clock is not throughput

| Core | Cycles, from that core's own description | Rough instructions/s at this fmax |
| --- | --- | --- |
| FES | ALU, branch or jump: 3. Load or store: 5. Average near 4 | about 20–27 million |
| PicoRV32 | Average about 4 with a one-cycle memory. ALU 3, load 5, shifts 4–14 | about 29 million, shifts slower than FES |
| VexRiscv Min, DarkRISCV | Pipelined. Near 1 cycle per ALU instruction when memory ack is one cycle | on the order of the fmax, several times FES |
| SERV | Bit-serial, at least 32 cycles per instruction | about 3.6 million |

PicoRV32's higher clock does not make it a much faster computer than FES.
SERV's matching clock makes it the slowest computer in the table. VexRiscv
Min is the result that matters: half the logic of FES, the same clock band,
and a pipeline.

## What to change

The product clock is 74.25 MHz, shared with the raster. This harness has
about 7% margin at 79.7 MHz on seed 1. The original seal was tighter, at
75.18 MHz. The qualified-ALU package on main was 79.051 MHz, close to this
harness. A new `BUILD_ID` still moves placement. The useful timing work is
margin at 74.25, not a core clock above the pixel clock. A second clock
domain is not justified by these numbers.

The routed critical path, from `timing.json` on seed 1, is inside the CPU.
It starts at the register-file read, passes `is_op`, then a long carry chain
whose cell is named `csr.wdata` and whose source line is `sum` in
`fes_rv32_alu`, then enters the `access_addr` carry chain, then control
logic, and ends at `mepc`'s enable. Not `mtval`. The carry chain is in the
same cycle as the M10K clock-to-out.

That series is in the netlist, not in the RTL. `access_addr` is
`rs1 + access_imm`. It does not read `alu_result`. `pc_plus_offset` is a
third adder, `pc + imm`. Registering `sum` does not cut the address adder.
Do not choose the register boundary from the cell names alone.

1. Register each adder's own result at the end of EXEC, before the
   trap, CSR and PC mux: `alu_result`, `access_addr` and `pc_plus_offset`.
   FETCH has already captured the operands, so the extra cycle fits EXEC
   rather than requiring a pipeline. ALU, branch, jump and memory
   instructions go from 3 cycles toward 4, and loads from 5 toward 6.
   Re-measure before any other timing change, and do not combine it with a
   shifter rewrite. If the new path no longer walks both carry chains, the
   series link was the mapper joining adders that the RTL keeps apart. If
   one adder remains critical, that adder is the one to cut further.
   PicoRV32's two-cycle ALU is a related trade, not this cut: that core
   routed at 115 MHz with the ALU split from the address add.

2. Leave the barrel shifter alone unless that cut is not enough. The three
   shifts are computed on every cycle and muxed with `sum`, so they widen
   the ALU cone, but the worst path above is the carry chains. An iterative
   shift would raise fmax further and make shifts take as many cycles as
   the shift amount. That is a worse computer for a problem the result
   registers should address first.

3. Do not pipeline this core to close 74.25 MHz. VexRiscv Min shows what a
   pipeline buys: similar fmax, half the COMB, much higher instruction rate.
   That is the right shape if the goal becomes more instructions per frame.
   It is a new CPU, and it does not by itself create pixel-clock margin.
   The multicycle core is already in the pipelined cores' clock band and
   behind them on work per clock. How to overlap fetch on this CPU, using
   the free port of the firmware RAM, is [pipeline.md](pipeline.md).

4. Do not shrink toward SERV, and do not pull the register file out of
   M10K. Area is not the limit on this part. The low flip-flop count is the
   M10K register file. Replacing it with flip-flops would grow the core
   toward PicoRV32's 720 FFs and is likely to hurt the path that already
   starts at the register-file output.

5. Do not add the M extension as a combinational multiply. The adder is
   already the critical cone, and the sealed recipe forbids DSP. If M is
   wanted later, it has to be iterative and off this path.

Not worth doing for speed or size: another placer-seed search (the producer
already tries seeds 1–8), allowing LUTRAM (nothing here mapped to an MLAB),
or treating the sealed 75.18 MHz figure as the CPU result.
