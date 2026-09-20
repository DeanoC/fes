# Expected behavior

The reduced simulation overrides `ADDR_BITS` to 4, so the stored table has
sixteen bytes. Each byte is `index XOR 8'hA5`. The LED follows bit 0 of the
registered read data, so it updates one rising edge after the address that
selected that byte.

After reset, address and data are zero and `LED[0]` is low. The first rising
edge presents table index 0 (`8'hA5`, LED high). The second rising edge
presents index 1 (`8'hA4`, LED low). The 17-edge test covers the sixteen
entries and the wrap back to index 0.

With the production default `ADDR_BITS=8` the same XOR pattern occupies 256
bytes. The expected values describe the FPGA pin level; this file does not
infer a visual LED polarity for any other board.
