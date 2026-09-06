# Expected behavior

The FPGA exposes an eight-by-eight unsigned DSP product and two writeable
tables on the HPS general-purpose interface: a 32-by-8 lab table and a
256-by-8 block table. There is no LED. A host diagnostic peeks and pokes
GPO/GPI; a blank display is not a failure.

After configuration the high 16 GPI bits stay `0xD810`. The low 8 bits are the
selected view, initially the product low byte (zero). Bits `[9:8]` count fabric
clocks so a host can see the core is running without an LED.

GPO fields are:

- bits `[7:0]`: left operand or table index
- bits `[15:8]`: right operand or write data
- bit `16`: product high-byte select, or table write enable
- bits `[18:17]`: view (`00` product, `01` lab table, `10` block table)

Product view does not write either table. A table write is one clock with bit
16 set in that view. Matching read data appears after a later clock with bit 16
clear. Writes to one table do not change the other. The lab table uses only the
low 5 index bits. The sixteen-bit product is `left * right`; `0xFF * 0xFF` is
`0xFE01`.

Linux peeks and pokes the Cyclone V FPGA-manager GPO/GPI pair (`0xFF706010` /
`0xFF706014`, `h2f_gp` / `f2h_gp`). Those are the same wires native development
load uses for MiSTer SPI identity after programming. This experiment does not
implement that SPI probe. It does not use the mailbox HELLO/START/DATA
transcript.
