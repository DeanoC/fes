# Expected behavior

The FPGA exposes three eight-by-eight unsigned products on the HPS
general-purpose interface, packed into one physical DSP block. There is no LED
or other external pin. A host diagnostic writes the operands and lane through
GPO and reads one product byte through GPI; a blank display is not a failure.

After configuration the high 16 GPI bits stay `0xD611`. The low 8 bits are the
registered selected product byte, initially zero. Bits `[9:8]` count fabric
clocks so a host can see the core is running without an LED.

GPO fields are:

- bits `[7:0]`: left operand
- bits `[15:8]`: right operand
- bit `16`: high-byte select
- bits `[18:17]`: lane (`0` = `left * right`, `1` = `left * ~right`,
  `2` = `left * (right ^ 8'h01)`)

The selected sixteen-bit product is implemented in a DSP block. One clock later
GPI low bits show `product[7:0]` when bit 16 is clear, or `product[15:8]` when
bit 16 is set. Distinguishing examples:

- `0x0A` and `0x0C` yield `0x0078` / `0x097E` / `0x0082`
- `0x12` and `0x34` yield `0x03A8` / `0x0E46` / `0x03BA`
- `0xFF` and `0xFF` yield `0xFE01` / `0` / `0xFD02`

Linux peeks and pokes the Cyclone V FPGA-manager GPO/GPI pair (`0xFF706010` /
`0xFF706014`, `h2f_gp` / `f2h_gp`). Those are the same wires native development
load uses for MiSTer SPI identity after programming. This experiment does not
implement that SPI probe. It does not use the mailbox HELLO/START/DATA
transcript. Yosys emits three `MISTRAL_MUL9X9` cells. nextpnr-mistral places
them on z-lanes 0/1/2 of one physical DSP site, with RESULT slices
`0:17` / `18:35` / `37:54` (physical `RESULT.36` is unused). The kit probe
checks all three lanes. M10K, MLAB, and PLL stay unused. Quartus comparison
is not implemented.

`make sim EXP=410_dsp_triple` checks the three products through the HPS model.
`make oss EXP=410_dsp_triple` produces compressed `build/oss/410_dsp_triple/top.rbf`.
