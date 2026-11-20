# Slice 3 spike: one-seed fit of the pc386 window shell

**Diagnostic, one seed, provisional.** This is not the controlled pair the
plan fixes (#2030, "The shell's cost"), and it is not a signoff. It answers
two questions early:

- Does the three-master fabric survive Platform Designer generation?
- What does the shell cost, roughly?

## What was built

These parts are committed under `boards/de25nano/pc386/`:

- `derive.py`, the plane derivation;
- `fes_de25_hps_window.v`, the window adapter with its guards, rebase and
  bounds;
- `fes_de25_pc386_standard.v`, the component wrapper;
- `fes_de25_pc386_standard_hw.tcl`, the component definition.

These parts were scratch, made by post-processing a prepared Spectrum board
project (`spectrum/build.py project`):

- the component swapped in;
- three `mm_ccb` bridges (128/64/64-bit, MAX_BURST_SIZE 128) from the
  machine clock to the 100 MHz side, each routed into
  `f2sdram_adapter.axi4_sub` beside the scanout's;
- the board SDRAM conduit removed;
- the grant tied to valid with base 0;
- the machine PLL set to 61.437908 MHz;
- an SDC that sources only the shared board constraints;
- `clocks.sdc`'s "FIFO peer reset ingress changed" check demoted to a
  warning, because the shared FIFO reset repair (`shared_resets.py`) is
  pinned to the stock two bridges and was not applied.

The machine is the Yosys SEAL netlist of the derived `pc386_system`:
elaborated by the pc386 lock's Yosys at main `8a162c536`'s guest rates,
5,796,013 bytes, M10K renamed to M20K by `fit_i386_device.prepare_netlist`.
`X87_PRESENT` and `X87_BACKEND` are 0.

The run used Quartus Pro 26.3 on chip-30x0-ai-dev, seed 1 (the project
default). The reference is the same prepared Spectrum project, compiled
unchanged apart from the same SDC demotion.

## Results

| | pc386 spike | Spectrum reference |
| --- | ---: | ---: |
| ALMs needed [=A-B+C], whole design | **45,227** (96.6%) | 10,167 (21.7%) |
| Machine component (`subsys_periph/fes_scanout`) | 35,126 | 5,733 |
| ... the machine alone (`system`, pc386 only) | 33,098 | — |
| ... session display | 1,304 | (inside the 5,733) |
| ... window adapter (`window`) | 642 | — |
| ... transport and GP crossing | 72 | (inside) |
| Outside the component | 10,101 | 4,434 |
| ... F2SDRAM interconnect (`soc_inst/mm_interconnect_0`) | **6,273** | 770 |
| ... the three window `mm_ccb` bridges | 271 | — |
| ... HPS, on-chip memory, lightweight interconnect | 895 + 541 + 473 | 934 + 558 + 486 |
| LABs touched | 4,639 / 4,680 | — |
| DSP | 24 | — |

**The shell costs about 12,100 ALMs** at this seed: 45,227 − 33,098. That
is against the 651 virtual-pin ALMs that stood in for it in every DE25 fit
so far. It has two parts:

- **About 5,800, the DE25 base shell that every machine carries.** That
  is the 4,434 outside the Spectrum's component, plus the session display
  and the transport inside it. The Spectrum does not apportion its own
  display, so the split is approximate.
- **About 6,300, the window.** Of that:
  - about 5,500 is the F2SDRAM interconnect's growth from 770 to 6,273.
    Platform Designer joins four masters onto one 256-bit AXI port, with a
    pipeline stage of about 240 ALMs per mux and width adapters for the
    64-bit ports;
  - 642 is the adapter;
  - 271 is the bridges.

**Timing.** Within each clock, every path passes:

- the machine clock at 61.437908 MHz has +4.702 ns of setup slack;
- the pixel clock has +4.976 ns.

Every failing path is a machine↔pixel, machine↔100 MHz or pixel↔100 MHz
crossing that the spike's SDC does not name ("Asynchronous, Timed
Unsafe"): worst −8.376 ns, 71 + 120 + 74 + 33 endpoints. The slice
names each of them against its synchronizer, as the DE10 and DE25
Spectrum/ST shells do. Nothing is cut that is not a real asynchronous
crossing with a synchronizer behind it.

## What this means for D2

A rough sum: the identity-on machine (#1959: 42,943–43,010 ALMs with 651
virtual-pin ALMs) plus this shell comes to about 54,400 ALMs, against 46,800.
The 387-on machine would not fit with the shell as built. That sum is
uncontrolled: different trees, front-end paths and seeds, and the derived
machine drops the plane. The order of magnitude still stands.

The largest single item is the interconnect's 5,500 ALMs. That is fabric
shape, not logic the machine needs. The candidates, all unmeasured:

- Arbitrate the three ports into one master inside the component, so that
  F2SDRAM sees two masters, as the stock shell does.
- Set Platform Designer's interconnect pipelining to fewer stages.
- Present the window at the adapter's 256-bit width, so no width adapter is
  inserted.

## Not established

- No controlled pair. The envelope must be fitted around these netlist
  bytes, reused by hash, for the shell cost to be a measurement.
- One seed.
- The spike's SDC demotion and missing FIFO reset repair.
- The grant tied off. Its adders fold.
- No 4 KiB burst splitting in the adapter. Platform Designer's translator
  handles the AXI side.
