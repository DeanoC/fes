# DE25 pixel-qualified HDMI output mask

HDMI RGB, data-enable and sync outputs now use the existing qualified pixel
reset for their masks. The previous direct device-reset gate carried 100 MHz
memory reset/request paths to these pads. Assertion remains asynchronous;
release follows the same pixel edges as readiness and scanout state. The
capture writer and system reset ownership are unchanged.

Implementation and compiled source: `5ac780209a20c28f9a1211489e4298b0d5834f0a`.
Package: `e45793dd0b62bd4cb39cfe00683427fe27f61cacc2ba8785840e9d8c420e2093`.
Functional build ID: `f5b2895bcb642853cbaa1b1d1e7535bb`.

## Verification scope

The fresh Quartus Pro 26.3 build on the 3090 used eight workers under the shared
lock. All ten compile phases pass their unchanged 120 s bounds; total elapsed
time is 542.328 s. The sealed ordinary computer package,
200 source inputs and generated timing/handoff inventory
verify. There are no new timing cuts, clock frequency/phase changes or waivers.

The unbounded actual writer-control induction passes against frozen 79d capture
logic; a deliberately broken capture produces a counterexample. The full
dual-clock actual video pair passes 40,204,224 ticks at identical qualified
reset releases. Actual C ROM/secure-provider and real ZX80/ZX81 RTL models pass
launch, return, held-control, stopped-clock and failure cases. All 73
recorded model source hashes match the current tree. These checks do not prove
physical reset latency, metastability or native boot.

The complete affected suite passes: 1,425 parent cases (two skips), 1,587 FPGA
cases (75 skips), planner checks, generated consistency and the Avalon/native
model. The real new package and retained ZX81 ROM also pass the shared linker
and native preflight under the race detector, with one selection and zero
firmware calls. No SD access or physical driver grant is established.

| Reported multicorner minimum, ns | Previous | Pixel mask |
| --- | ---: | ---: |
| Setup | 1.688 | 1.790 |
| Hold | 0.000 | 0.000 |
| Recovery | 6.599 | 6.557 |
| Removal | 0.198 | 0.065 |
| Minimum pulse width | 0.220 | 0.220 |

The constrained internal TNS remains zero, and hold rounds to 0.000 ns with no
reported margin. This is not external timing qualification or a paired QoR
benchmark. Clock inventory and reset-chain counts remain unchanged (four
system, two pixel). Four unwaived signoff classes remain: missing output delays
(30 ports), missing input delays (two), multiple reset chains (two domains),
and an intra-clock reset false-path finding (one).

## HDMI pad diagnostics

A private routed-netlist copy models the forwarded clock and all 27 RGB/control
pads over four corners. It retains the conservative receiver minima of 1.8 ns
setup and 1.3 ns hold from the
[ADV7513 datasheet](https://www.analog.com/media/en/technical-documentation/data-sheets/ADV7513.pdf).
PCB skew is a symbolic zero reference; real skew and output loading remain
unqualified. The vendor initializer remains at its existing no-delay setting.

All 42 clocks are queried for setup and hold paths in every corner (168 rows).
Only the pixel clock has launch witnesses. There are zero foreign-clock
witnesses, including the previous system-reset sources. Full witnesses and all
108 pad/corner observations are retained. The original QSF, QPF, SOF, timing
report and sealed SDC hashes remain unchanged by the query. Nothing is excluded
by a reset-to-pad false path or a restricted pixel-only source selection.

The worst reference setup is +3.307 ns at data[12]; worst reference hold is
**-0.013 ns at horizontal sync**. Hold therefore still fails even at this
unqualified zero-skew reference. Removing the system-reset path is established;
HDMI timing signoff is not. The fitter needs an encoded receiver/board/load
contract to repair the remaining hold path. No hand-inserted delay, relaxed
receiver minimum or false-path waiver is introduced here.

## Electrical contract still unresolved

All 31 relevant pin locations match the Terasic Rev B manual; the manual's
FPGA_I2C names map to the design's HDMI_I2C names. However, its HDMI pin table
lists the clock as 3.3 V while both the vendor reference QSF and this fitted
design assign 1.1-V on DJ24, bank 2A_T. The fitter reports 0 pF clock load.
This discrepancy predates the overlay. Neither a manual table nor the inherited
assignment establishes the actual board-level clock electrical path or load.
The vendor setting is preserved while that connection is investigated; changing
it to 3.3 V without the bank/board contract would be unjustified.

The manufacturer's archived manual, extracted table/figure, vendor QSF bytes
and actual fitted pin report are hashed in `result.json` and the raw evidence.
No schematic trace length, translator, valid receiver voltage or measured board
skew is inferred from the high-level connectivity diagram. All HDMI references
remain conditional on resolving this electrical contract.

## Hardware boundary and next work

No UART opening, JTAG action, board programming, SD write or flash action took
place. After the user's accidental power interruption, a passive host check
confirms the exact TRWU8CBL USB adapter enumerates; CPU/FPGA running state still
requires requalification. No hardware acceptance result is inferred from USB
enumeration. Historical recovery inputs verify; no restore was attempted.

Next: resolve the clock electrical path, encode legitimate receiver/board/load
constraints so the fitter repairs hold, and complete I2C/IRQ/LED timing plus
remaining reset ownership findings. A matched native chain for this package,
fresh board qualification and bounded ordinary core/ROM/input/return test remain
open. Physical menu/core transitions, media and native networking are still
needed toward full DE10-style behavior.

Raw evidence: `out/nano-pixel-mask-20261107/`. Retention target:
`/home/deano/fes-retained/de25-pixel-mask-20261107-r1`, dependent on the prior
reset-domain retention. No push, PR, merge or lane handoff is implied.
