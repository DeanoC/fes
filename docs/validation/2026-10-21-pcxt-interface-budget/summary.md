# PC/XT interface budget preflight, 21 October 2026

The current model cannot establish a physical 100 MHz SDRAM budget, even
before adding board uncertainty. This is a limitation of the available
independent delay envelopes, not a measurement of a malformed clock or proof
that the hardware cannot run at 100 MHz. No placement trial was launched.

FES base: `0221382d7cff85ea5f12a135bcf2e0316ed24d02`.
Compiler evidence: `dee2b36890255a3c9c9b85ceff74e83095d7c9d0`.
The audit reads that immutable compiler commit through Git, never its working
files. `result.json` records the input hashes. The historical host snapshot
is a diagnostic with synthetic IO budgets, not a new producer artifact.

## Forwarded SDRAM clock

The inverted AD20 forwarder uses the controller falling edge for chip rise
and controller rising edge for chip fall. At 100 MHz and 50% duty cycle,
using independent pad intervals and an ideal upstream clock/board:

```text
chip high minimum = 5 ns + fall minimum - rise maximum
chip low minimum  = 5 ns + rise minimum - fall maximum
```

The current model supplies rise `[0, 5.38]` ns and fall `[0, 5.40]` ns.
The audit also computes sensitivity to the retained Quartus AD20 observations,
rounding minima down and maxima up to 10 ps. Those observations are not a
replacement timing model and do not identify the board load.
The 0 pF source is the varying-data AD20 reference. The retained 30 pF
constant-forwarder study reproduces the late envelope, but that does not by
itself validate adopting nonzero early bounds for the complete supported scope.

| Independent envelopes | High minimum (ns) | Low minimum (ns) |
| --- | ---: | ---: |
| Current compiler model | -0.380 | -0.400 |
| Observations at 0 pF only | 2.880 | 2.870 |
| Observations at 30 pF only | 2.220 | 2.200 |
| Pooled observations at 0 and 30 pF | 1.660 | 1.640 |

The RAM Tester reference part, EM63B165 -6, requires at least 2 ns for each
pulse ([Rev. 2.4, Table 11](https://etron.com/wp-content/uploads/2022/04/EM63B165TSBM_Rev-2.4.pdf)).
Its note 10 also requires compensation for slower input transitions; the
table above grants no allowance for slew, uncertainty or route variation.
The reference part has not been established as Kit A's fitted part.

Replacing the zero minima with pooled observed minima alone would therefore
still fail this optimistic check. Choosing only the 30 pF column because it
passes would be invalid: the existing declaration covers 0..30 pF, and the
actual load is unknown. A clock waveform analysis needs audited relationships
between rising and falling delays at the same load and timing corner, plus
the actual routed clock waveform. Independent cross-corner/load extrema lose
that relationship. The next compiler investigation should preserve those
relationships and validate them against same-corner vendor pulse reports.
Do not change the existing minima or acceptance thresholds from this table.

## SDRAM data budget

The existing RAM Tester equations remain the basis for a conditional
EM63B165 -6, CL2 calculation. With chip rising-clock arrival `[Cmin,Cmax]`,
read flight `[Rmin,Rmax]`, write flight `[Dmin,Dmax]` and uncertainty `U`:

```text
input min/max = [Cmin + 2.5 + Rmin - U, Cmax + 6.0 + Rmax + U]
output min/max = [Dmin - Cmax - 0.8 - U, 1.5 + Dmax - Cmin + U]
```

All values are ns. These are external delays relative to the controller
falling edge. The compiler adds the appropriate DQ pad prefix/tail separately;
adding it again to these equations would double-count it. The capture clock
remains related at +5 ns. Chip-select inversion needs its separate propagation
interval. The equations are not executable Kit A constraints while the part,
flights, load, uncertainty and forwarded-clock waveform remain unqualified.

There is another coverage distinction: the snapshot retains active
`SDRAM_DQML`/`SDRAM_DQMH` outputs on AG13/AF13 as unregistered `MISTRAL_OB`
cells with routing-only timing. They are outside the new bidirectional DQ
profile. The controller separately multiplexes actual addon byte masks onto
A11/A12. Establish the selected board's wiring before either constraining the
dedicated outputs as memory mask inputs or declaring them unconnected. RAM
Tester's constant-mask fixture and budget port list cannot settle that choice.
High-Z disable, delayed DQM, chip tHZ/tLZ and bus turnaround remain separate
requirements; passing the digital contention test does not prove them.

## HPS I2C

`fes_pcxt_top.sv` connects HPS `out_clk` and `out_data` directly to SCL/SDA
output enables, with constant-zero data and feedback to the hard IP. There
is no PC/XT fabric launch register. The runtime's Linux I2C implementation
uses `i2c-dev`; its source does not establish a fixed peripheral-clock rate
or controller timing register state for the selected kit.

Intel documents both enables as synchronous to the HPS peripheral clock,
with open-drain external connections ([Cyclone V HPS TRM, section 21.3](https://www.intel.com/content/www/us/en/docs/programmable/683126/21-2/i2c-controller-signal-description.html)).
The retained fit exposes nonzero CELL delays, but its internal launch-source
chain terminates without a constrained clock. The checker confirms this at
all four corners. Neither assigning the CPU clock nor treating the I2C bus
bit rate as the peripheral clock resolves that missing launch relationship.
Required evidence is the selected boot/kernel peripheral-clock and controller
configuration, a validated hard-IP launch/input boundary model, and board
pull-up/load/receiver timing. No synthetic clock or false path was added.

## Reproduction and outcome

```sh
python3 docs/validation/2026-10-21-pcxt-interface-budget/check.py \
  --nextpnr /path/to/DeanoC-nextpnr
```

The checkout must contain the pinned compiler commit; its current branch is
irrelevant. The command recomputes and compares the retained result. Exit zero
means the **blocked preflight** reproduced, not that timing passed.

Kit A remains the first target and Kit B requires separate qualification.
No kit was contacted, no RTL/clock/constraint/toolchain lock or shared contract
changed, and no new compiler binary was built. Zero new routes or vendor fits;
the route allowance remains 1627.843715112 seconds. Physical HPS, SDRAM, HDMI
and reset qualification remain open. The guarded whole-design repair cannot
be admitted from these inputs, so the existing acceptance gate stays intact.
