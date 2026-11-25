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

The run continues through the remainder of the 20-second RTL window.
Its final exit and counters are separate from the demo milestone.
