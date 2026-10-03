# Original FES Z80 RTL

Two synthesizable Z80 variants written from published behavior specifications.
The new RTL does not instantiate, translate or include T80, TV80 or an emulator.
SG-1000 and Spectrum select the NMOS variant at native cadence. Spectrum also
has a documented-only 56 MHz development build. Coleco, SMS and ZX81 retain
their existing CPU selections. Each consumer requires its own full-system
timing and hardware qualification.

| Entry point | Behavior | Interface |
| --- | --- | --- |
| `fes_z80_nmos` | Original Zilog NMOS instruction behavior, undocumented operations and flags, original machine-cycle timing | Active-low Z80 strobes; alternating `ce_p`/`ce_n` in one system clock domain |
| `fes_z80_fast` | Zilog UM008011-0816 documented instruction set; unsupported encodings trap | Request/completion transactions; no original T-state padding or physical refresh pulses |

The shared `fes_z80_engine` handles base, CB, ED, DD, FD, DDCB and FDCB
instructions, both register banks, stack, indexed addressing, all interrupt
modes, NMI and HALT. The original `fes_z80_alu` supplies arithmetic and flags.
`fes_z80_bus` converts transactions into pin cycles for the NMOS entry point.
Its optional `FAST` mode is tested independently; the public fast entry point
uses the transaction interface directly.

## NMOS behavior

The NMOS selection includes IXH/IXL/IYH/IYL, SLL, indexed-CB register copies,
ignored/repeated prefixes, ED aliases and unused ED opcodes as NOPs. It models
hidden X/Y flags, CP operand flags, BIT/WZ behavior, Q-dependent SCF/CCF,
block-operation and interrupted-repeat flags, NMOS `OUT (C),0`, and the
maskable-interrupt parity behavior in `LD A,I` and `LD A,R`
(also documented by UM0080 and implemented in the fast variant). Only the low seven
bits of R increment; DDCB/FDCB operations have two M1 fetches.

The NMOS instruction controller uses explicit one-hot state bits to shorten
control paths at the consumer system clock. The fast variant keeps its compact
binary state encoding; this choice does not change instruction or pin timing.

Opcode fetches, operand cycles, I/O and interrupt acknowledge have separate
bus timing. Internal cycles retain their machine-cycle boundaries for DMA;
WAIT stretches the proper bus phase. BUSRQ takes priority over interrupt
recognition, and pending NMI survives DMA. Ordinary interrupt recognition uses
the final T-state rising-edge sample; DMA release uses the live pending lines. HALT continues four-state dummy
fetch/refresh cycles. Warm reset preserves the general/alternate registers,
IX, IY and SP; their cold power-up values are unspecified. Reset clears
PC, I/R, interrupt/control and in-flight state.

IM0 uses an initial acknowledge followed by ordinary memory continuation,
as UM0080 p184 specifies. Block input uses IO4 followed by MEM3, following the
generic I/O waveform with its automatic wait state (UM0080 pp10–11) and the
independent corpus's cycle observations. The INI timing tuple on p298 lists
`(4,5,3,4)` despite its description ordering I/O before memory; it does not
resolve that subcycle discrepancy. Primary physical traces for block-input
subcycles and prefixed/multi-byte IM0 remain qualification requirements.

`ce_p` and `ce_n` must alternate and must never coincide. All storage uses
`posedge clk`; there are no generated clocks. `reset` is an active-high,
system-clock synchronous integration reset. It does not emulate the physical
chip's analog RESET input or its phase-dependent release delay. The enclosing
bus mux must disconnect address/data while `busak_n` is low: FPGA fabric
outputs do not electrically tri-state.

## Fast behavior

A completed transaction advances the engine on `enable && bus_req && bus_ready`
at the rising clock edge. Hold `bus_ready` low until read data is valid or a
write has been accepted. Address, kind and write data remain stable while
stalled. The responder must not repeat a stalled write.

| `bus_kind` | Transaction |
| --- | --- |
| 0 | Opcode fetch |
| 1 | Memory read |
| 2 | Memory write |
| 3 | I/O read |
| 4 | I/O write |
| 5 | Maskable interrupt acknowledge; responder supplies the instruction/vector |
| 6 | NMI acknowledge; returned data is ignored |
| 7 | Internal cycle; NMOS adapter uses `bus_delay` |

The fast core removes timing-only internal cycles and bus extensions. Ordinary
register operations can complete in one enabled clock when the responder can
supply the opcode. Operand bytes and memory/I/O transactions still require
completion. Its byte-register helpers directly access real H/L: documented
indexed byte forms use H/L, while IXH/IXL/IYH/IYL encodings trap. This removes
index-byte muxes without adding instruction clocks. A synchronous RAM must arrange its response latency through
`bus_ready`; tying it high before the RAM's registered data is available is
incorrect. This interface permits the surrounding system to choose its own
memory pipeline instead of inheriting physical Z80 wait states.

Undefined flag results are deterministic zero; documented unaffected flags
are preserved. BIT's undefined S/PV flags are zero. Block-I/O preserves C and
sets N as UM0080 specifies, rather than implementing the NMOS hidden equations.
Unsupported encodings, including undocumented index-byte operations, SLL,
unused ED values and undocumented prefixes, assert sticky `illegal`, assert
`halted`, stop requests and do not retire. Reset recovers the core.

`retired` is a one-system-clock observation pulse and `retire_pc` identifies
its instruction start (the first prefix). Interrupt entry also produces a
completion pulse. Register/debug outputs are observations, not a state-loading
interface. `debug_iff` is `{IFF2,IFF1,EI_delay}`.

## Validation and synthesis

From `sources/misteross`:

```sh
make sim-fes-z80
python3 scripts/sim_fes_z80.py --case nmos
python3 scripts/sim_fes_z80.py --case fast
```

The default offline suite runs exhaustive ALU cases in both modes, independent
instruction regressions, direct bus timing tests and the complete NMOS pin
wrapper. It covers instructions, flags, prefix exceptions, stack/address wrap,
interrupts, refresh, HALT, WAIT, DMA, warm reset and original program results.
Output and build logs live under `build/sim/fes-z80/<case>/`.

The optional external qualification runner consumes JSON test data from
[SingleStepTests/z80](https://github.com/SingleStepTests/z80), fixed at revision
`ebe1875d48f374bcfd4b505d8eb8ee751568b5f7`. It compares both register banks,
PC/SP/I/R, WZ/Q/P, interrupt state, RAM and ordered I/O. Its default is the first
100 cases from each selected opcode file. Downloading requires explicit opt-in:

```sh
python3 scripts/test_fes_z80_vectors.py --download
python3 scripts/test_fes_z80_vectors.py --corpus /absolute/path/to/corpus
python3 scripts/test_fes_z80_vectors.py --pins --corpus /absolute/path/to/corpus
```

The `--pins` mode runs the complete NMOS wrapper and additionally compares
total T states and ordered strobed memory/I/O addresses and data. It checks
M1 count and refresh addresses against published prefix rules, because the
corpus omits M1/RFSH observations. The corpus simplifies memory strobes to one
T state, so it cannot establish strobe widths, half-cycle phases or unstrobed
address behavior. Both modes pass 160,400 cases across all 1,604 opcode files
(100 cases per file); deliberately incorrect durations and read data fail.

The cached receipt records the revision, original Git blob identities and data
hashes; an offline rerun verifies cached hashes. This MIT-licensed corpus is an
independent software oracle, generated from a translated Ares core with fixes.
The design work consumes test data and provenance only, without CPU/generator
implementation sources. These comparisons do not establish complete pin-waveform
or physical-chip equivalence. The default offline suite has no
network or external-data requirement.

### Physical trace replay

`scripts/test_fes_z80_pin_trace.py` replays operator-supplied pin captures through
the complete NMOS wrapper. It compares all eight control outputs at every
rising and falling clock sample, selected/refresh addresses and CPU write data.
The included NOP waveform is an original synthetic expectation from Zilog
UM0080 Figure 5; it exercises the replay path and negative controls, and is
explicitly rejected as physical evidence:

```sh
python3 scripts/test_fes_z80_pin_trace.py --self-test
python3 scripts/test_fes_z80_pin_trace.py --trace /absolute/path/to/capture.json \
  --require-physical --raw-capture /absolute/path/to/original-capture
```

The [synthetic fixture](../../sim/z80/pin-trace-smoke.json) shows the JSON shape.
Use `format: "fes-z80-pin-trace-v1"`, `start: "first-m1-t1-after-reset"`, a positive
`clock_hz`, a settling `sample_delay_ns` less than half a clock, and a
`timestamp_tolerance_ns` less than a quarter of that half-clock interval.
Each sample records `edge_time_ns`, alternating `edge: "rise"`/`"fall"`,
`inputs` and `outputs`. Inputs are levels immediately **before** that edge;
outputs are levels measured **after** it plus the declared settling delay.
Consecutive clock edges and all input/control channels are required; rows are
never filtered. The address may be `null` only outside selected/refresh cycles;
write data is required during CPU writes and `null` otherwise. Supplying an
unstrobed address additionally compares that sample.

A physical `source` records `kind: "physical"`, `manufacturer: "Zilog"`,
`technology: "NMOS"`, chip `part` marking, `capture_tool`, `description`,
`initialization` and SHA256 `capture_sha256` of the original raw capture.
`--raw-capture` verifies those original bytes when provided. Chip identity and
capture provenance are operator supplied; metadata alone cannot prove either.
Hold physical RESET for at least three complete clocks as UM0080 specifies,
then crop to the first post-reset opcode T1 rising edge at PC zero with WAIT,
INT, NMI and BUSRQ inactive. Capture an initialization program that assigns
every general register and flag it later uses; NMOS reset does not initialize
those registers. Keep both edge phases and include refresh intervals.

For normalized CSV, pass metadata JSON without `samples` alongside
`--samples-csv /absolute/path/to/samples.csv`. The required header is:

```text
edge_time_ns,edge,din,wait_n,int_n,nmi_n,busrq_n,a,dout,m1_n,mreq_n,iorq_n,rd_n,wr_n,rfsh_n,halt_n,busak_n
```

Bus/control values are decimal integers or `0x` hexadecimal; empty address/data
cells represent `null`. The self-test writes a JSON/CSV example, logs and source
hashes under `build/sim/fes-z80-pin-trace/`. A replay reports edge, address,
write-data, M1, refresh, interrupt-acknowledge and DMA coverage and fails on any
observed mismatch. It checks settled digital levels, not nanosecond propagation,
electrical high-Z behavior or the physical chip's RESET release latency.
No genuine-chip capture has been run in this lane; physical acceptance remains
unavailable until suitable external captures are supplied.

For a contained Cyclone V timing diagnostic, provide the existing compiler
executables explicitly:

```sh
python3 scripts/benchmark_fes_z80.py --variant fast \
  --yosys /absolute/path/to/yosys \
  --nextpnr /absolute/path/to/nextpnr-mistral \
  --target-mhz 100 --seed 1 --seed 2 --seed 3
```

For the selected 56 MHz fast-core route:

```sh
python3 scripts/benchmark_fes_z80.py --variant fast \
  --yosys /absolute/path/to/yosys \
  --nextpnr /absolute/path/to/nextpnr-mistral \
  --target-mhz 56 --seed 3
```

`--require-target` returns a failure if any selected seed misses the requested
clock, after saving the complete reports. Without it the command records a
diagnostic even when timing misses its target.

56 MHz is 16 times the Spectrum 48K's nominal 3.5 MHz clock. This describes a clock ratio;
shorter instruction cycles and memory latency make program speed workload
dependent. Coleco/Sega's 3.579545 MHz baseline needs 57.272720 MHz for the same
clock ratio. Seed selection is part of the contained recipe, and console
integration needs a fresh timing check.
The benchmark drives the CPU with changing inputs and observes register/bus
state through a small registered signature harness. It targets `5CSEBA6U23I7`
with the native Yosys/nextpnr ALM flow. Its fingerprinted output records exact
sources, tools, options, utilization, critical paths and routed Fmax. These
are diagnostic harness estimates, not a sealed package, console timing,
performance ceiling or hardware acceptance. No bitstream is produced or loaded.

The NMOS one-hot integration changes the shared engine's synthesis graph even
though the fast personality retains its instruction behavior and binary state
codes. Its fast outputs pass all 499 mapped equivalence checks against the
preceding implementation. Fresh timing evidence is required for that graph;
the preceding engine's 56.654 MHz seed-6 result does not qualify it.

With Yosys `0.69+ (1bf1ff3d7)` and nextpnr-mistral
`0.11.1-261-gbdb24661`, router2, the current contained fast envelope reaches
46.221–54.404 MHz across seeds 1–10 at a 56 MHz target. Seed 6 reaches
49.818 MHz. These placements do not pass `--require-target`; the range does
not establish a CPU frequency ceiling. The full Spectrum fast producer uses
its selected system toolchain, memory/socket schedule and placement search,
and refuses sealing unless the actual system closes 56 MHz. Console recipes
qualify the CPU together with their memory and peripheral logic.

Complete physical-chip equivalence, edge-coincident interrupt apertures,
all unstrobed pin traces, firmware/game compatibility and exact-artifact
hardware acceptance remain separate qualification. SG-1000 and Spectrum now
select the native first-party CPU; their package/interface contracts remain
unchanged apart from the core version and functional build identity.

## Behavior sources and provenance

Implementation and tests were written independently from these specifications
and original observations; existing CPU implementation sources were excluded
from the design work:

- [Zilog Z80 CPU User Manual, UM008011-0816](https://www.zilog.com/docs/z80/um0080.pdf): documented instructions, flags and machine cycles.
- [Zilog Z80 product specification, PS0178](https://www.zilog.com/docs/z80/ps0178.pdf): bus pin timing.
- [Sean Young, The Undocumented Z80 Documented, version 0.90](https://datasheets.chipdb.org/Zilog/Z80/z80-documented-0.90.pdf): undocumented instructions, flags and prefix behavior.
- [David Banks, physical NMOS flag measurements](https://github.com/hoglet67/Z80Decoder/wiki/Undocumented-Flags): Q, prefix effects and interrupted block-operation flags.
- [Goran Devic, measured undocumented behavior](https://baltazarstudios.com/webshare/Z80-undocumented-behavior.htm): warm reset and unstrobed bus observations.
- [Zilog 1987 technical manual](https://www.bitsavers.org/components/zilog/z80/1987_Zilog_Z80_Technical_Manual.pdf): NMOS interrupt parity note.

Each source file carries its SPDX license. The instruction engine/ALU/fast
entry point are original MIT-licensed RTL; the NMOS bus/wrapper are original
GPL-2.0-or-later RTL. Existing third-party CPU notices are untouched.
