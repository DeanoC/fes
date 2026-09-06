# Expected behavior

The FPGA exposes a 32-by-8 writeable table on the HPS general-purpose
interface. There is no LED or other external pin. A host diagnostic reads
and writes the table through GPO/GPI; a blank display is not a failure.

After configuration the high 16 GPI bits stay `0xD410`. The low 8 bits are
the registered read data, initially zero. Bits `[9:8]` count fabric clocks so
a host can see the core is running without an LED.

GPO fields are:

- bits `[4:0]`: table index
- bits `[15:8]`: write data
- bit `16`: write enable

A write is one clock with bit 16 set. The matching read data appears on GPI
after a later clock with bit 16 clear and the same index. Writes to one index
do not change another. The simulation checks three indexes, a later read in
a different order, and a replacement write.

Production uses the same 5-bit index. Linux can poke this map through the HPS
GP registers; it does not use the mailbox HELLO/START/DATA transcript.
