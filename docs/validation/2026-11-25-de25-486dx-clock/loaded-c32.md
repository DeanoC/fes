# 486DX presenter LOADED interval

At 97.916667 MHz, the original five-cycle interval leaves 0.660 ns for the
flag route. The first two fitted measurements were 3.331 ns and 3.184 ns.
Both fits passed ordinary timing and sealing. The first-miss results and
independent route checks are in [the fit record](fits.md).

The derived DX presenter holds a completed row until eight machine
clocks have elapsed since the preceding report. While it holds the
row, it keeps the DDR request inactive and the row, epoch and payload
stable. The report updates the payload and flips the flag on the same
edge. The flag crosses three pixel synchronizer stages. The payload
has a 10 ns timing bound and the flag route is capped at 3.9 ns.
The rung collector verifies that the fitted machine
contains the digitally reviewed derivation before using the eight-cycle
interval in its budget.

The digital tests used the presenter and RAM model captured from
`27a9104b408c3c3384988b3b063606ec1559144f`. The input presenter SHA-256 is
`aed6df2bcb71cf1d975d5591d20d3c35258a25839743f088f9a29bc028a251b3`.
The derived presenter SHA-256 is
`7437e4b680112d6d8e5fb7ecf5a0a65f92ba937148b9bfbdfe1550703bdf8167`.
Reversing the four anchored edits reproduces the captured source.

| Digital check | Result |
| --- | --- |
| Original phase and route-delay model | Five-clock minimum. 13 of 64 cases lost a coherent report. |
| Derived phase and route-delay model | Eight-clock minimum. All 64 cases received coherent reports. |
| Reset during a completed hold | The held row was canceled. Four restarted reports arrived in order. |
| New epoch during a completed hold | Old rows 1–2 and new rows 1–4 were published and received in order. |
| C32 machine period, 640×480 | 720 raster deadlines met, zero underflows, 83.93 µs minimum margin. |
| C24 machine period, 960×720 | 720 raster deadlines met, zero underflows, 61.06 µs minimum margin. |

The lifecycle bench uses the normal public RTL ports and checks the
timer, DDR counters and report at each machine edge. The raster runs
exercise one frame each with periodic acceptance and return stalls in
the digital DDR model. The phase sweep separately models flag and
payload delays. An early harness assertion sampled the final pixel
counter before the receiver monitor updated it. The corrected bench
passed with identical DUT bytes.

The source, fixtures, logs and re-run commands are retained in
`/data/projects/de25-486dx-clock-2263/loaded-cooldown-evidence`.
The SHA-256 of its `evidence.sha256` inventory is
`1519a2e2fbd84002b7e8499410cb8052e33cc0f1a29fb5910b3770cb519f0108`.

The pinned Yosys frontend elaborated the exact same presenter bytes
through the registered board component preparation in the C32 scratch
checkout. Its script selected the derived presenter once, and the
netlist carries the LOADED gap register and flag. The placement-free
`loaded-rearm-proof/witness.json` SHA-256 is
`8fc3b82c0006f885bca95602eaabfdd5d3f2eecd57ed2b836697c3b83a1b58bb`.

```sh
sha256sum -c evidence.sha256
python3 derive_loaded.py
bash run-phase64.sh
bash run-lifecycle.sh
```

Digital results cover the re-arm interval and the exercised DDR model.
Fitted route and CDC checks establish the clock qualification.
