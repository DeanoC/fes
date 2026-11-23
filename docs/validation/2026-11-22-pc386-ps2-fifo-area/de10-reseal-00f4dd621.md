# Superseded: the DE10 reseal of the shared-RTL form

This records a failed run as it happened. The cut no longer edits the shared
`pc386_kbc.sv`, so the DE10 needs no reseal ([method.md](method.md), "Why a
derivation").

**Run:** the registered producer
`cores/fes-pc386/tools/build_pc386_product.py --with-lockstep --seed 1 --gpu-router HIP`.
- Commit `00f4dd621`, with the shared `pc386_kbc.sv` carrying the one-write-port FIFO.
- A clean detached worktree on Powerboat, 23 Nov 2026, 07:58–08:56Z.
- Quartus Prime Lite 17.0.2, holding the Lite slot `/tmp/fes-quartus-lite-1.lock`.
- The pc386 lock's Yosys `d273c1cb` (after #2130).

**Results:**
- **Lockstep:** the full netlist lockstep passed. Report SHA-256 `2240052800f8630a…`.
- **Quartus:** compiled with 0 errors.
- **Timing:** failed, so the producer stopped with exit 1 and wrote no package.

| Model | Setup | Hold | Recovery | Removal | Min pulse width |
|---|---:|---:|---:|---:|---:|
| Slow 1100mV 100C | +0.011 | +0.254 | +5.642 | +1.171 | +1.122 |
| Slow 1100mV −40C | **−0.135** | +0.204 | +5.841 | +1.129 | +1.122 |
| Fast 1100mV 100C | +6.807 | +0.135 | +10.662 | +0.560 | +1.122 |
| Fast 1100mV −40C | +7.595 | +0.106 | +11.224 | +0.481 | |

**Where it failed:** the setup miss is on `video_clock` (`pll|general[0]` divclk),
with TNS −0.399 ns. In the same model, `system_clock` (`general[0]`), where the
FIFO is, had +0.952 ns and `general[1]` had +10.552 ns.

**What came next (Deano, 23 Nov 2026):** the DE25-only derivation. No other
seed was tried.
