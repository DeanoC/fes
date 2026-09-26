# cpu6502

Verilog NMOS 6502 used by the FES Apple II core.

Source: https://github.com/Arlet/verilog-6502
Commit: `e930327ffecc5062bfce70bbcfba2bbfa6de6e4c`
License: see the notice at the top of each file (use permitted provided the
notice and copyright are retained).

Local changes: `cpu.v` is renamed `cpu6502.v` with module `cpu6502`, and
`ALU.v` is renamed `cpu6502_alu.v` with module `cpu6502_alu`, so the global
Verilog namespace does not collide with other cores, and trailing whitespace
is removed. The logic is unchanged.

The core expects synchronous memory: data for the address presented in one
enabled cycle is consumed on the next enabled cycle. FES drives `RDY` as the
CPU clock enable and holds `DI` stable between enables. Undocumented NMOS
opcodes are not implemented.
