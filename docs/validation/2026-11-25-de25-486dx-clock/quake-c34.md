# C0 = 34 Quake demo

G5 passed its [first rendered demo frame gate](../2026-11-24-i486dx-s1/method.md#g5-quake-on-the-rtl)
on the 92.156863 MHz host RTL bench built at
`3df878912962ede8557166d56c93ff8896869943`. The 387 was enabled.
Quake ran past the earlier FNINIT stall and reported playing
`DEMO1.DEM` on the console at 14.00 simulated seconds. The 16.00 s
capture shows the rendered Necropolis scene with the HUD and weapon.

The DX RTL took over at cycle 423,927,563. The retained run log shows
no CPU stop, shutdown or reset after handover through the demo capture.
At the first profile sample after the capture, 16.100073 s, the RTL had
retired 357,605,587 instructions, including 178,787,508 in protected
mode. The [receipt](sim-c34.json) names the retained frames, log prefix
and sampled profile counters by digest.

The [replay-input comparison](sim-c34-e255d4e3-match.json) binds this
result to the exercised inputs of the repaired C34 candidate
`e255d4e3a09fd29ad2cfeed465bf49a15e1c17ce`.

## Full RTL window

The same game process completed the full 20-second RTL window on
26 November at 00:22:26 Sofia time. It exited 0 at 24.600 simulated
seconds and cycle 2,267,064,843. The RTL retired 537,748,026 instructions,
including 358,817,314 in protected mode, with zero oracle fallback steps.
The log reports zero VGA scanout, presenter and plane underflows and no
CPU stop or reset. The final frame shows the running demo.

The [complete receipt](sim-c34.json) verifies 105 artifacts, including
the unchanged first-frame snapshot. The [d18 replay comparison](sim-c34-d18a5a45-match.json)
still matches after the full window, including the game disk copies. It
applies the completed run to the exercised inputs of
`d18a5a45b91236abc4efac7607c84e97735eab07`.

The [structural proof comparison](sim-c34-b6310cd4-match.json) verifies
the same inputs for `b6310cd4cb7f9eb9f7c5cb4e1462ebb9708024cc`, including
the derived CPU, bench, Quake command and disk image. The completed run
applies to this fitter candidate through the exercised inputs.

The original launch wrapper classified 3DBench's scheduled `END limit`
exit as a failure. The complete receipt uses the corrected analyzer on the
retained raw logs. The original Quake process result records its own
successful exit.
