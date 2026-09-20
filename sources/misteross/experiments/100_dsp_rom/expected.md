# Expected behavior

The FPGA multiplies an eight-bit HPS operand by one byte from an initialized
256-by-8 block table. There is no LED. A host diagnostic peeks and pokes
GPO/GPI; a blank display is not a failure.

After configuration the high 16 GPI bits stay `0xD910`. The low 8 bits are the
selected product byte, initially zero. Bits `[9:8]` count fabric clocks so a
host can see the core is running without an LED.

GPO fields are:

- bits `[7:0]`: left operand
- bits `[15:8]`: table index
- bit `16`: product high-byte select

The table byte at index `i` is `i XOR 8'hA5`. The sixteen-bit product is
`left * table[index]`; `0x0C * table[0]` is `0x0C * 0xA5 = 0x07BC`, and
`0xFF * table[0xFF]` is `0xFF * 0x5A = 0x59A6`. The table read is registered,
then the product is registered, so a new index is visible in GPI two clocks
later. Changing only the high-byte select needs one clock.

Linux peeks and pokes the Cyclone V FPGA-manager GPO/GPI pair (`0xFF706010` /
`0xFF706014`, `h2f_gp` / `f2h_gp`). Those are the same wires native development
load uses for MiSTer SPI identity after programming. This experiment does not
implement that SPI probe. It does not use the mailbox HELLO/START/DATA
transcript.
