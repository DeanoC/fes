# cpu6502

Verilog NMOS 6502 used by FES Apple II, C64 and the development 1541.

Source: https://github.com/Arlet/verilog-6502
Commit: `e930327ffecc5062bfce70bbcfba2bbfa6de6e4c`
License: see the notice at the top of each file (use permitted provided the
notice and copyright are retained).

Local changes: `cpu.v` is renamed `cpu6502.v` with module `cpu6502`, and
`ALU.v` is renamed `cpu6502_alu.v` with module `cpu6502_alu`, so the global
Verilog namespace does not collide with other cores, and trailing whitespace
is removed. FES adds an optional active-low `SO` input with `ENABLE_SO=1`.
A falling edge sets V, including between CPU enables; a held low level does
not retrigger after CLV. In this mode instruction flag writes occur only on
an enabled CPU cycle and retain priority over a simultaneous pin edge.
The original V-update logic remains selected by default. Apple II and the
C64 motherboard tie SO high; only the development drive enables it.
`make sim-fes-c64-so` checks BVC polling, status pushes, CLV under a held-low
pin and the disabled mode. NMOS subphase sampling/inhibit windows are not
modeled yet.

The core expects synchronous memory: data for the address presented in one
enabled cycle is consumed on the next enabled cycle. FES drives `RDY` as the
CPU clock enable and holds `DI` stable between enables. Undocumented NMOS
opcodes remain incomplete. The stable SAX family (`$83`, `$87`, `$8F`, `$97`)
is implemented in all four addressing modes. It stores `A & X` without changing
registers or flags, including zero-page index and indirect-pointer wrapping.
`make sim-fes-c64-sax` checks the writes, enabled cycle counts and register/flag
preservation, including CPU enable gaps.
