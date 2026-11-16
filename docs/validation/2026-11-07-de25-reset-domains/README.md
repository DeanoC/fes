# DE25 memory and device reset ownership

The ROM/HPS-DDR producer now derives the peripheral device reset from the
memory sequencer's reset and its early asynchronous request. Both must clear
before devices release. The actual memory reset output asserts on a clock
edge; sharing it alone would lose device containment with a stopped clock.
The owned bridge preserves that containment without another release chain.

Exact implementation/compiled source: `dce9831fd6b190a1b6e973a986ab0fa30ca09204`.
Package: `2b1b597f585bad63b43956b72113af9311c8be535581d101714686d65f2f49a2`.
Functional build ID: `8e1cec97fef788596e276bba530a9a7f`.

The real generated design routes the request through the subsystem boundary
to the bridge. ROM, computer, control, probe, mailbox, PIO and peripheral bus
interfaces share its device reset. Source checks verify the complete memory
controller parameters, OCM reset/request connections, both clocks, all those
device owners and the generated bridge bytes. Changed or absent request
wiring and an independent peripheral asynchronous release reject.

An isolated metadata-only generation control exposed the clocked memory reset
behavior; it is not an accepted device-reset implementation. The correct model
uses the actual generated vendor controller and bridge. All 128 configuration/
PLL reset scenarios pass with stopped-high/low clocks and short pulses. It
checks that the request stays asserted through memory containment, memory reset
has a stable early request, and device release occurs on a clock edge. A
memory-reset-only negative control fails at cycle 77, after a live epoch.
The compiled vendor RTL, board reset source, bridge and complete request
parameters independently match this bounded model. No analog, metastability or
hardware proof is claimed. Vendor simulator style waivers add no timing cuts.

The intermediate generation failure from a missing reset-sink association and
the initial tool-script syntax failure are retained separately. The final
generation passes with `associatedResetSinks` on the owned reset source; it
does not falsely declare the combination a one-to-one direct reset. Each
experiment verifies 343 original configuration/HDL/constraint files unchanged.
See the [Avalon interface specification](https://www.intel.com/programmable/technical-pdfs/683091.pdf).

The fresh Quartus Pro 26.3 build used eight workers on the 3090 host under the
shared lock. All ten normal compile phases pass their unchanged 120 s bounds.
The sealed package and 200 source inputs verify, along
with the generated timing/handoff inventory and four literal obsolete-JTAG SDC
repairs. The actual package and retained ROM also pass the shared linker and
native loader preflight with one selection and zero firmware calls. This does
not establish an SD write, native boot or core-load acceptance.

| Reported multicorner minimum, ns | Previous | Early-request owner |
| --- | ---: | ---: |
| Setup | 1.881 | 1.688 |
| Hold | 0.000 | 0.000 |
| Recovery | 7.259 | 6.599 |
| Removal | 0.084 | 0.198 |
| Minimum pulse width | 0.220 | 0.220 |

Reported TNS is zero and the clock inventory is unchanged. Hold rounds to
0.000 ns, providing no reported margin. System reset chains fall from five to
four; pixel chains remain two. Intra-clock false-path reset findings fall from
two to one. Four signoff categories remain, with zero waivers: missing output
delay (30 ports), missing input delay (two), same-clock reset chains (two
domains), and the one intra-clock reset finding. This is not whole-board timing
qualification or a paired QoR benchmark.

Software validation on the frozen source passes 69 focused DE25 tests (one
default Yosys availability skip; the pinned proof runs in the full suite),
native context/ROM/composition tests, 1,582 parent cases (three skips), 1,587 FPGA
cases (75 skips), generated consistency and the actual Avalon/native model.
The prior capture proof and launch/return/failure models remain exercised.

## HDMI diagnostic and the next repair

Two separate, read-only netlist copies model the forwarded HDMI clock and all
27 data/control pads over four operating corners. They use the ADV7513 receiver
minima of 1.8 ns setup and 1.3 ns hold, with PCB skew held at a symbolic zero
reference. Trace delay and output loading are not qualified. The selected
initializer uses BA=0x60 (no receiver clock delay); see the
[datasheet](https://www.analog.com/media/en/technical-documentation/data-sheets/ADV7513.pdf)
and [programming guide](https://www.analog.com/media/en/technical-documentation/user-guides/adv7513_programming_guide.pdf).

The all-source query has worst setup/hold of -11.013/-1.084 ns. The actual
reported paths originate in the 100 MHz memory reset/request state and reach
HDMI pads through the video's direct `reset_n` output gate. This gives a
specific cross-clock mask path to repair. No clock group or reset-to-output
false path was introduced to hide it.

A second query selects only pixel-clock launches. Those paths have minima
+2.454/+0.065 ns at the same symbolic reference. That restricted result excludes
the failing reset gate and cannot qualify the interface. Its 0.065 ns rounded
hold margin also cannot establish a board skew/load budget. The selected mask
must move to the qualified pixel domain, all-source queries must be repeated,
and the actual external contract still needs completion.

All 216 pad/corner observations and their worst full-path witnesses are retained.
Both queries independently verify the original QSF, QPF, SOF, timing report and
sealed SDC unchanged. Their added constraints exist only in private analysis
copies; neither a successful query nor a narrowed path selection changes the
four open signoff categories or constitutes hardware acceptance.

TRWU8CBL's CPU/FPGA state remains unqualified after the user's power interruption.
No UART opening, JTAG programming, SD write or flash action occurred here. The
historical recovery inputs still verify. A matched native chain for this new
package and a bounded ordinary core/ROM/input/return trial remain necessary,
alongside physical menu/core transitions, media and native networking toward
the requested DE10-style behavior.

Raw evidence: `out/nano-reset-domains-20261107/`; `result.json` binds the exact
receipts and diagnostics. Retention is at
`/home/deano/fes-retained/de25-reset-domains-20261107-r1`, dependent on the prior
shared-reset retention. No PR, push or lane handoff is implied.
