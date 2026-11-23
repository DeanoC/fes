# Exploratory arm seed 1 (not counted)

This is the first, shared-RTL form at `6722d4bd3` (`pc386_kbc.sv` edited), before the
DE25-only derivation. The DE25 netlist it compiled has the same FIFO logic.

`board-pair.sh xarm 6722d4bd3` with `SEEDS=""` and `YOSYS_MAX_THREADS=1`, on Powerboat,
22 Nov 2026 15:00-15:45Z, before the fixed Yosys lock. Not part of the recorded pair.

- Machine netlist `55fb8a5373941d40c2928eaaeadbc1b84288e6757fce09f12594c7798b637473`.
- ALMs needed 41,618.8 / 46,800; RAM blocks 168 / 358.
- All five arrays of both FIFOs are MLAB (`machine_at_kbc_{keyboard,mouse}_fifo_*_rtl_0`, Simple Dual Port).
- Worst slack over every clock: setup +1.496, hold 0.000, recovery +7.691, removal +0.125,
  minimum pulse width +0.220.
