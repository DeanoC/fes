# fes.ramtest

Utility core for the MiSTer GPIO SDRAM addon and the HPS DDR3 on the SoC.
The host mailbox is `fes.application` 1.0, with the fixed 720p picture, the
gamepad and `fes.memory.hps-ddr` 1.0. There is no memory opcode. After
execution release both memories are tested at the memory clock, 100 or
130 MHz. A `fes.gamepad` button stops every scan and the status says `STOP`.
The host maps a keyboard onto those buttons (arrows, Enter, Space, and the
letter keys it already forwards). Escape and Backspace leave the session in
the host and are not delivered to the core.

## SDRAM addon

The SDRAM scan writes the whole addon and reads it back for six patterns:
`0000`, `FFFF`, `5555`, `AAAA`, the address mixed with its high half, and
the inverse. The HDMI text shows the pattern, the live address, and the live
expect/got values. The full error count stays on screen, with the first
mismatch address and the data that was read there, and the most recent
mismatch address. The table below it keeps the six pattern counts.

The SDRAM clock pin uses a DDR output and rises on the fabric falling edge.
Quartus selects the frequency directly from the PLL, packs SDRAM
command/address registers into the output cells, and captures read data on a
shifted clock in the input cells; the 130 MHz path also pipelines the
captured data before the controller. The controller uses CAS latency 3 at
130 MHz and CAS latency 2 at 100 MHz. The OSS packer does not support DDR
input registers on bidirectional DQ pads, so the OSS builds use a
phase-shifted capture clock and a fabric input register.

## HPS DDR

The three `fes.memory.hps-ddr` ports scan the core DDR window
`0x30000000-0x3fffffff` at the same time, on the memory clock:

| Port | Width | Span |
| --- | --- | --- |
| 0 | 128-bit | `0x30000000-0x37ffffff` |
| 1 | 64-bit | `0x38000000-0x3bffffff` |
| 2 | 64-bit | `0x3c000000-0x3fffffff` |

The DDR below `0x30000000` is kept from Linux too, but the kernel's
`MiSTer_fb` console framebuffer sits at `0x22000000`; its cursor overwrote
test data there on the kit. The tester stays inside the interface window.

Each port writes its span and reads it back for seven patterns. Every
64-bit lane at byte address `a` holds a value of `a` alone:

| Pattern | Lane value | Burst |
| --- | --- | --- |
| `ZERO` | all zeros | 128 |
| `ONES` | all ones | 64 |
| `CHCK` | `5555...` or `AAAA...` by lane | 1 |
| `WALK` | one bit, selected by `a[8:3]` | 16 |
| `INVR` | `{a, ~a}` | 64 |
| `BYTE` | one byte of `{~a, a}` written into `INVR`, byte `a[5:3]` | 32 |
| `ADDR` | `{~a, a}` | 128 |

A mismatch is counted and the scan continues. Each port row shows the
pattern, the current address, its error count and `RUN`, `PASS`, `FAIL`,
`STOP` or `NACK`. The rows below show the first and last failing address
with the first failing pattern, and every data bit that has failed. The
`MB/S` row is each port's throughput for its last write and read pass. No
progress for 1,000,000 cycles is `NACK`, which ends that port's scan: the
controller is not accepting commands.

The ports go through `cores/fes-common/rtl/fes_hps_ddr.v`, which drives the
shared layout and registers the HPS boundary. An execution hold finishes any
write burst already started, with byte enables cleared, and drains read data
before the scan restarts. The controller cannot recover a burst stopped
midway.

The controller only accepts these commands when the boot splash carries the
same port layout; see [the splash](../fes-splash/README.md). The runtime
releases the FPGA ports only after identity and the SDR mirror registers
prove the layout. A development RBF load leaves them in reset.

A finished scan leaves `ADDR` in the whole window, so Linux can check the
address mapping. Each 32-bit word at byte `a` reads `a` and the word at
`a + 4` reads `~a`:

```sh
busybox devmem 0x30000000 32   # 0x30000000
busybox devmem 0x30000004 32   # 0xCFFFFFFF
busybox devmem 0x3FFFFFF8 32   # 0x3FFFFFF8
busybox devmem 0x3FFFFFFC 32   # 0xC0000007
```

## Builds

The [2026-09-27 timing investigation](../../docs/validation/2026-09-27-ramtest-timing.md)
compares Quartus and OSS on matched source inputs, including FSM encoding and
routing controls. It explains the observed gap without changing this core's RTL.
The [initialized-FSM follow-up](../../docs/validation/2026-09-27-ramtest-fsm-init.md)
records the Yosys candidate, equivalence checks and unchanged-source experiment.
The [FSM isolation and routing follow-up](../../docs/validation/2026-09-27-ramtest-fsm-routing.md)
records the GPU convergence fix and the remaining timeout/control-path limit.
The [placement-option follow-up](../../docs/validation/2026-09-27-ramtest-placer-options.md)
corrects the earlier effective exponent, verifies preserved defaults, and measures
the combined FSM/placement improvement; the 130 MHz target remains unresolved.
The [corrected routed comparison](../../docs/validation/2026-09-28-ramtest-observation-validity.md)
retains 116.918 MHz memory and 76.959 MHz pixel as the strongest retained
experimental stack. The later [fixed-placement state-copy trial](../../docs/validation/2026-09-28-ramtest-feedback-local-copy.md)
does not improve it. These are host-only compiler probes; neither selects a
production toolchain or qualifies a 130 MHz RBF. The [validation index](../../docs/README.md#dated-records)
lists the full investigation.

```sh
make toolchain-fes-ramtest
make build-fes-ramtest-100
make build-fes-ramtest-130
```

Both OSS rates use `toolchains/ramtest.lock`. Its Yosys declares every
fpga2sdram port, so all three ports reach the netlist; the pinned blackbox
before it listed only command port 2 and data port 3. The OSS recipe checks
that the synthesized fpga2sdram cell carries the generated layout constants.
Each rate seals a package into `build/fes-ramtest-100/` or
`build/fes-ramtest-130/`; its timing report covers the memory, capture and
74.25 MHz video domains. Direct recipe invocation requires `--memory-mhz 100`
or `--memory-mhz 130`.

The build ID hashes the source revision, so each commit synthesises a slightly
different netlist. The seal therefore routes `PLACER_SEEDS` in turn and keeps
the first seed that meets every clock at analogue signoff.
`qor-ranking.json` lists the seeds it tried, and `build-summary.json`
records the winning seed.

For the opt-in 130 MHz compiler timing diagnostic, provision its separate
lock and run one seed-2 route:

```sh
make toolchain-fes-ramtest-timing
make diagnose-fes-ramtest-timing GPU_DEVICE=1
```

The diagnostic pins Yosys [#17](https://github.com/DeanoC/yosys/pull/17)
and nextpnr [#93](https://github.com/DeanoC/nextpnr/pull/93), requests up to
four generic local enable copies, then writes
`build/fes-ramtest-timing-130/ranking.json` plus synthesis, route and timing
outputs. The route log must prove a live HIP backend; CPU fallback is rejected.
A completed HIP route is reported even below 130 MHz; `passing` records
whether all three clock-frequency constraints were met. It does not check the
full package evidence or hold timing. The diagnostic RBF is not a sealed
package or hardware acceptance. The normal 100 and 130 MHz package builds
keep their existing lock and signoff gate. The historical 116.918 MHz route
also used two opt-in,
cell-name-specific probes. Those probes no longer match the current RTL and
remain disabled here; 116.918 MHz is not a result for the current source.

The Quartus 17.0.2 diagnostic compiles the same RTL:

```sh
QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus make build-fes-ramtest-quartus
RAMTEST_MHZ=100 QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus make build-fes-ramtest-quartus
```

The first builds 130 MHz into `build/fes-ramtest-quartus/`; the second builds
100 MHz into `build/fes-ramtest-quartus-100/`. Each directory contains an
RBF, timing report and loadable `comparison.fcore`. The bitstreams use
different build identities. The Quartus timing report covers internal setup
paths, including the fpga2sdram ports, but does not constrain external SDRAM
I/O timing, so the full-memory hardware scan is the acceptance evidence for
these rates.

`make sim-fes-ramtest` runs short spans of the same patterns against
behavioral SDRAM and fpga2sdram models. The DDR model refuses commands at
random and while 14 reads are pending, returns read data late, and flags a
command outside the window or a burst left incomplete. The test checks
identity and the video, gamepad and HPS DDR capability bits, execution
release, a green status glyph, the `ADDR` signature at three addresses per
port, holds that land inside a write burst and during reads, and a button
stop. The package is not registered and is not in the factory image.

Packages before `fes.memory.hps-ddr` tested the SDRAM addon only. Their HPS
scan addressed DDR inside Linux memory and failed on the first write under a
boot bitstream without the layout. They do not declare the interface, so the
runtime keeps their FPGA ports in reset.
