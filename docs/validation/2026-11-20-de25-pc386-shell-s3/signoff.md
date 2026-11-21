# Slice 3: the pc386 board's signoff findings

Deano chose (21 November 2026) to admit the pc386 board's design-assistant
findings by reviewed bases, as the Spectrum's are admitted. His conditions:

- fix the board's own crossings first;
- write one basis per finding group, naming its SDC constraint and its
  synchroniser or handshake;
- change no shared DE10 RTL, and widen no rule or count.

Every fit below is one seed (seed 1) of `boards/de25nano/pc386/build.py build`,
Quartus Pro 26.3, on chip-30x0-ai-dev. Timing passes at every corner in each
of them; the worst slack is hold, 0.000 ns.

## Before and after

| Rule | j (before) | k | l | m | n | o | p (after) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| CDC-50001 | 90 | 72 | 67 | 72 | 72 | 72 | **67** |
| CDC-50002 | 3 | 6 | 6 | 6 | 6 | 6 | **6** |
| CDC-50007 | 1 | – | – | – | – | – | – |
| CDC-50102 | 1 | 1 | 1 | 1 | 1 | 1 | **1** |
| TMC-20023 | – | 1 | – | – | – | – | – |
| LNT-30010 | 2 | 3 | 2 | 2 | 2 | 2 | **3** |
| FLP-10000 | 54 | 54 | 54 | 54 | 54 | 54 | **54** |
| TMC-20602 | 24 | 22 | 24 | 24 | 24 | 24 | **23** |
| RDC-50003 | 3 | 3 | 3 | 3 | 3 | 3 | **3** |
| TMC-20011 | 2 | 2 | 2 | 2 | 2 | 2 | **2** |
| TMC-20012 | 5 | 5 | 5 | 5 | 5 | 5 | **5** |
| Failed rules | 10 | 10 | 9 | 9 | 9 | 9 | **9** |

| Fit | Commit | What changed |
| --- | --- | --- |
| j | `bc0a124ba` | Before: the board as it was, before main's merge (main moves pc386_system's netlist) |
| k | `ababd53f7` | Base copied in the machine clock; fault record copied at held-rise; presenter synchronisers named on the board |
| l | `d6991ad41` | Fault copy register enable-only; presenter toggles' false path to the stage-0 register |
| m | `36e38230b` | Fault record by request and acknowledge (the held-rise copy was unsafe); toggles back to the D pin |
| n | `3cdd2071c` | Fault copy enabled from its synchroniser alone |
| o | `2b6380401` | Fault acknowledge its own launch register |
| p | `ed95c3d21` | Fault record launched from a register that only copies it |

## What the board fixes removed

- **The grant base** (CDC-50001 ×20 in j): the arbiter read `base_lo` from the
  peripheral clock. The base is now copied in the machine clock when the
  granted synchroniser rises. `report_asynch_cdc` classes it a compliant
  CE-type bus.
- **The fault record** (CDC-50007 in j): five per-bit synchronisers with no bus
  constraint, so the record could cross torn. k's copy at held-rise cleared
  the rule, but it was unsafe: the guards keep presenting commands after the
  machine reads as held, and a faulted port never drains. From m on, the record
  crosses by request and acknowledge and follows the record live.
  `window_control_tb` checks that the launched copy never changes while a
  request is open. With the acknowledge gate removed, that check fails.

  The design assistant accepted that handshake only in p's form. m to o show it
  flagging the copy (CDC-50001 ×5) while the bundle launched from a register
  whose enable came from the returning acknowledge. A calibrated proxy
  isolated the cause: `window_control` alone, two clocks, the board's SDC;
  l's form clean and o's flagged, as on the board. The proxy then showed p's
  form clean.

Each board crossing is named in `pc386-clocks.sdc` against its synchroniser.
The two bundles carry net, data, skew, maximum and minimum delay bounds, with
endpoint counts.

## What remains, and its bases

`scanout/machine_findings.py` is the engine; `pc386/signoff_bases.py` is the
table (in pc386's closure only). On p, called directly on the fit, every row
of these six rules matches exactly one basis:

| Rule | Rows | Bases |
| --- | ---: | --- |
| CDC-50001 | 67 | presenter capture 20, fetch 35, loaded 12 |
| CDC-50002 | 6 | capture buffer 2, toggles to pixel 2, toggle to machine 1, pixel reset 1 |
| CDC-50102 | 1 | status counts (pc386_snapshot) |
| LNT-30010 | 3 | BIU reset 2 (with a fitter duplicate), machine reset 1 |
| FLP-10000 | 54 | small RAMs in the machine netlist, the guards, the arbiter and the bridge FIFO |
| TMC-20602 | 23 | machine-netlist control registers |

Each crossing basis quotes its constraint lines from the compiled SDC.

- **Synchronisers.** Each names its synchroniser. The record's
  `source_inputs` must carry the pinned digests of the shared RTL the argument
  rests on: `pc386_presenter.sv`, `pc386_vga_scanout.sv`, `pc386_snapshot.sv`
  and `pc386_machine.sv`.
- **Toggle basis.** It also requires the compiled QSF to carry the board's
  synchroniser assignments.
- **Toggle and pixel-reset findings.** These state what STA shows on the fit:
  the crossing paths are cut, and the only timed path into each stage 0 is the
  receiving clock's own reset.
- **Loaded-row report.** Its stability is derived from the fetch state machine:
  at least five machine clocks (81.4 ns) between flips, against 50.4 ns to the
  copy.

## Open

- **RDC-50003** (3 rows): the window bridge's two DC FIFOs (Platform Designer
  `mm_ccb`) put their own reset synchronisers in the machine and 100 MHz
  domains, beside the board's. The HDMI output-reset row is the one the
  Spectrum's review admits. No bridge parameter removes the FIFO
  synchronisers. Whether to admit them or replace the bridge is Deano's
  decision; the gate refuses here until then.
- **TMC-20011/TMC-20012**: admitted by the Spectrum's existing input and audio
  reviews, which collect on the pc386 topology.
- **A dry run, not a package.** The record here is the producer's own, with
  stub tool identities. The signoff and repeat fits run once RDC-50003 is
  settled and main is merged.
