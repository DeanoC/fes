# G5: Quake on the DX's RTL with the x87 fix

**Pass.** Quake runs on the DX's RTL past the old stall (`00a7:0005256c`,
`DB E3`) with no CPU stop, through its demo, to the end of its 20-second RTL
window.

## The run

Powerboat, harness [`quake/i486dxs1_quake.sh`](quake/i486dxs1_quake.sh):
- **The tree:** a scratch merge of this PR's head `494479998` with the x87
  fix's **first version** (`started || take`;
  [`quake/patch-applied.txt`](quake/patch-applied.txt)). #2195's final fix
  adds the `!unbuilt` term for the apply-edge race Codex found, which this
  run's wrong-path stall was not.
- **The bench:** `machine_tb` built by `sim_fes_pc386.py --suite cpu-profile
  --cpu-core i486dx` (binary `d628835a…`).
- **The command:** round 2's command line (census `bench` drive, `quakes`,
  `quake +timedemo demo1 +exec mode0.cfg -nosound -nocdaudio`) with `--x87 1
  --rtl-at-prompt 1 --rtl-for 20`, frames every 1.0 s.
- **The x87 wiring:** this PR's `machine_tb` wires the RTL CPU's x87 unit to
  the machine (`x87_present`, F0h/F1h, IRQ 13).

## Result ([`quake/quake-lines.txt`](quake/quake-lines.txt))

- **Handover:** `RTL-BEGIN 460018150`, at the DOS prompt. The run ended at
  `END rtl-window … seconds=24.600`, exit 0.
- **No `STOP` line:** no stall, no unimplemented form, no reset.
- **The RTL's counters:** 579,354,541 instructions retired; 0 oracle steps;
  387,146,229 in protected mode (1,187 PE entries); 0 VGA underflows.
- **Frames:**
  - [t0013](quake/t0013.png) (13 s): the console shows "Playing demo from
    DEMO1.DEM";
  - [t0015](quake/t0015.png): the status bar is up as the console retracts;
  - [t0017](quake/t0017.png) and [final](quake/final.png) (24.6 s): the
    demo's 3D view in play.

Round 2's bring-up stalled at about 9.8 simulated seconds. This run passes
that point and goes on to 24.6 s.

## What this does not establish

- **The fix's final version on Quake.** This run used the first version. The
  final version is lockstep-tested (#2195).
- **Quake's CPI.** No window was measured (G4 is DOOM's and 3DBench's).
- **x87 state across the handover.** Quake's own FNINIT starts the unit after
  the handover, so none is carried.
