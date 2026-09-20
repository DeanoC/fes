# Expected behavior

The FPGA exposes two writeable tables on the HPS general-purpose interface: a
32-by-8 lab table and a 256-by-8 block table. There is no LED. A host
diagnostic reads and writes through GPO/GPI; a blank display is not a failure.

After configuration the high 16 GPI bits stay `0xD710`. The low 8 bits are the
registered read data, initially zero. Bits `[9:8]` count fabric clocks so a
host can see the core is running without an LED.

GPO fields are:

- bits `[7:0]`: table index
- bits `[15:8]`: write data
- bit `16`: write enable
- bit `17`: bank select (`0` lab table, `1` block table)

A write is one clock with bit 16 set. The matching read data appears on GPI
after a later clock with bit 16 clear and the same bank and index. Writes to
one bank do not change the other. The lab table uses only the low 5 index bits.

Linux peeks and pokes the Cyclone V FPGA-manager GPO/GPI pair (`0xFF706010` /
`0xFF706014`, `h2f_gp` / `f2h_gp`). Those are the same wires native development
load uses for MiSTer SPI identity after programming. This experiment does not
implement that SPI probe. It does not use the mailbox HELLO/START/DATA
transcript.
