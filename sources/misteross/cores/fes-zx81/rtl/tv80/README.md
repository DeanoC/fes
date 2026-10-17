# TV80

Upstream Verilog Z80 kept beside the ZX81 machine. Simulation, the OSS shell,
and the Quartus oracle instantiate `fes_z80_nmos` from `zx81_machine.sv`.
These files are not on either ZX81 producer source list.

Source: https://github.com/hutch31/tv80
Commit: `66a131c38d05ef58b3d8c4f1507a72e6e4aa5d65`
License: MIT (see `LICENSE`)

`t80pa.v` wraps `tv80_core` with Sorgelig T80pa bus timing: `CEN_p`/`CEN_n`
half-cycles, WAIT via CEN gating, and M1 refresh. `TV80_REFRESH` must be
defined. NMI is sampled every clock, matching T80.vhd.
