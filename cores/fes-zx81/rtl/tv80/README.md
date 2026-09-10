# TV80

Verilog Z80 used by FES ZX81 simulation and later OSS synthesis.

Source: https://github.com/hutch31/tv80
Commit: `66a131c38d05ef58b3d8c4f1507a72e6e4aa5d65`
License: MIT (see `LICENSE`)

`t80pa.v` wraps `tv80_core` with a T80pa-style pinout. `CEN_p` is the T-state
enable. `CEN_n` is unused.
