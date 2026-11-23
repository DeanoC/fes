# DE10 netlist identity (recorded, on the fixed Yosys lock)

[`de10_netlist.py`](de10_netlist.py) wrote the DE10 seal's machine netlist
(`pc386_quartus.machine_program`, the SEAL recipe) twice from one checkout path,
`~/fes-scratch/arm-fifo`, on Powerboat on 23 Nov 2026:

| Checkout | Commit | `pc386_system.v` SHA-256 |
|---|---|---|
| #2036's head | `dfacd28e1` | `65cbd41232883f713ef6c120757a1fa95aced1d8047af86d891491a960274daf` |
| This branch (derived form) | `d0ad36901` | `65cbd41232883f713ef6c120757a1fa95aced1d8047af86d891491a960274daf` |

- **Yosys:** the pc386 lock after #2130, commit `d273c1cb774f70d58c890745ac1b7dd106d32a26`
  (binary SHA-256 `0594dfdb…`), with `YOSYS_MAX_THREADS=1`.
- **Result:** `cmp` finds the two files identical. The DE10 pc386 netlist and
  `BUILD_ID` don't move, so the DE10 needs no reseal and no kit run.
