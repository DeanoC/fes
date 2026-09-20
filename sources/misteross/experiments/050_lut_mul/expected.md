# Expected behavior

The FPGA exposes an eight-by-eight unsigned product on the HPS general-purpose
interface. There is no LED or other external pin. A host diagnostic writes the
operands through GPO and reads one product byte through GPI; a blank display
is not a failure.

After configuration the high 16 GPI bits stay `0xD510`. The low 8 bits are the
registered selected product byte, initially zero. Bits `[9:8]` count fabric
clocks so a host can see the core is running without an LED.

GPO fields are:

- bits `[7:0]`: left operand
- bits `[15:8]`: right operand
- bit `16`: high-byte select

The sixteen-bit product is `left * right`, implemented in logic cells. One
clock later GPI low bits show `product[7:0]` when bit 16 is clear, or
`product[15:8]` when bit 16 is set. Changing one operand changes the product;
`0xFF * 0xFF` is `0xFE01`.

Linux peeks and pokes the Cyclone V FPGA-manager GPO/GPI pair (`0xFF706010` /
`0xFF706014`, `h2f_gp` / `f2h_gp`). Those are the same wires native development
load uses for MiSTer SPI identity after programming. This experiment does not
implement that SPI probe. It does not use the mailbox HELLO/START/DATA
transcript. DSP blocks stay unused.
