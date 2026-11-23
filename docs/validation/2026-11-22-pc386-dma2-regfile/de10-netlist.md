# DE10 netlist identity (recorded, on the fixed Yosys lock)

[`de10_netlist.py`](de10_netlist.py) wrote the DE10 seal's machine netlist
(`pc386_quartus.machine_program`, the SEAL recipe) twice from one checkout path,
`~/fes-scratch/arm-dma2`, on Powerboat, 23 Nov 2026:

| Checkout | Commit | `pc386_system.v` SHA-256 |
|---|---|---|
| #2036's head | `dfacd28e1` | `65cbd41232883f713ef6c120757a1fa95aced1d8047af86d891491a960274daf` |
| This branch | `f27f23f97` | `65cbd41232883f713ef6c120757a1fa95aced1d8047af86d891491a960274daf` |

- **Yosys:** the pc386 lock after #2130, commit `d273c1cb774f70d58c890745ac1b7dd106d32a26`
  (binary SHA-256 `0594dfdb…`), `YOSYS_MAX_THREADS=1`.
- **Result:** `cmp` finds the two files identical.
- **Why:** this cut changes only DE25 files (`boards/de25nano/pc386/`). The DE10
  `pc386_system` and `fes_dma8237` sources are untouched, so the DE10 pc386
  netlist and its `BUILD_ID` don't move, and the DE10 seal needs no rerun.

An earlier run at the local baseline `d9961acfb` (af58ebed2 merged with main
`856ef5c64`) against `3fee8b9c6` gave the same digest on both sides. #2036's
later head only changed DE25 shell files.
