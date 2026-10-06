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

Before the full-span scan, an independent bounded byte lane tests preservation
at 64 distinct addresses: all four banks, rows 0/1/0800/1000, and columns 0–7.
Low rows use columns 0–3 and high rows use columns 4–7. Every
address runs both byte orders. Each case writes and reads `A55A` with BE11,
then writes `3CC7` with BE10 and `E169` with BE01. Upper-first reads must be
`3C5A`, then `3C69`; lower-first reads must be `A569`, then `3C69`. A final
`F00F` write with BE00 must leave `3C69`. The inactive payload byte deliberately
differs from stored memory. Each write has its own full-word readback.

The 128 cases contain 512 read checks. Request gaps are 0, 32, 1024 or 2048
fabric clocks, covering refresh between requests at all supported rates. The
controller latches byte enables with each request, establishes active-high
DQM two fabric clocks before WRITE, retains it through WRITE, and clears both
masks for reads. The MiSTer addon shares chip DQML/DQMH with A11/A12:
the controller preserves all row bits during ACTIVATE, then drives the masks
on A11/A12 during the RCD setup, column command and write hold. Separate
SDRAM_DQML/DQMH top-level signals are logical mirrors; they do not reach the
chip masks on this addon. RAMTEST enables an additional command/address
register stage, packed into output cells in native and Quartus builds. Atari
ST leaves `IO_OUTPUT_REGISTERS` disabled and retains the direct controller
outputs and their existing timing. See the
[addon schematic](https://github.com/MiSTer-devel/Hardware_MiSTer/blob/master/releases/sdram_xsds_3.0.pdf)
and [reference controller](https://github.com/MiSTer-devel/GBA_MiSTer/blob/master/rtl/sdram.sv).

The `BYTE` row has its own RUN/PASS/FAIL/STOP/NACK status and completed case/read
counts. Two receipt rows retain the first failure's halfword address, readback
step (1/3/5/7), requested BE, original write payload, expected word and actual
word. A missing acknowledgement records the outstanding step as NACK. Failure
stops the preflight and prevents the full-span scan and overall SDRAM PASS.
The gamepad stop and execution hold also stop/reset the preflight.

`make sim-fes-ramtest` checks all three controller rate/CAS profiles, refresh
adjacency, timeout and stop handling. The integrated RTL simulation then checks
the complete SDRAM/HPS scans and five injected physical-mask fault controls:
ignored, swapped, lower inhibited, upper inhibited and both inhibited. Each
fault must retain its exact readback and display a red failure. The behavioral
memory covers address bit 17, so the selected high columns do not alias. These
are digital simulation checks, not electrical timing or physical acceptance.
Set `RAMTEST_CAPTURE_DIR` to an existing directory when running the simulation
to retain settled correct-mask and ignored-mask HDMI frames as PPM files.

The SDRAM scan writes the whole addon and reads it back for six patterns:
`0000`, `FFFF`, `5555`, `AAAA`, the address mixed with its high half, and
the inverse. The HDMI text shows the pattern, the live address, and the live
expect/got values. The full error count stays on screen, with the first
mismatch address and the data that was read there, and the most recent
mismatch address. The table below it keeps the six pattern counts from
each finished rate. That column's sum matches the error count from the
scan that produced it, including a mismatch on the last word.

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

Both OSS rates use the shared `toolchain.lock` HIP compiler slot. Its Yosys declares every
fpga2sdram port, so all three ports reach the netlist; the pinned blackbox
before it listed only command port 2 and data port 3. The OSS recipe checks
that the synthesized fpga2sdram cell carries the generated layout constants.
Each rate seals a package into `build/fes-ramtest-100/` or
`build/fes-ramtest-130/`; its timing report covers the memory, capture and
74.25 MHz video domains. FES registers and ships the OSS 100 MHz variant as a standard catalog
utility. Direct recipe invocation defaults to `--memory-mhz 100`;
`--memory-mhz 130` requests the separate higher-rate build. The default
retains timing signoff; the open 130 MHz timing work does not block shipment.

The build ID hashes the source revision, so each commit synthesises a slightly
different netlist. The seal therefore routes `PLACER_SEEDS` in turn and keeps
the first seed that meets every clock at analogue signoff.
`qor-ranking.json` lists the seeds it tried, and `build-summary.json`
records the winning seed.

For the opt-in 130 MHz compiler timing diagnostic, provision its separate
lock and run the paired seed-2 routes:

```sh
make toolchain-fes-ramtest-timing
make diagnose-fes-ramtest-timing GPU_DEVICE=1
```

The diagnostic pins merged Yosys [#17](https://github.com/DeanoC/yosys/pull/17)
and nextpnr [#93](https://github.com/DeanoC/nextpnr/pull/93),
[#95](https://github.com/DeanoC/nextpnr/pull/95) and the asynchronous-clear
correctness fix [#94](https://github.com/DeanoC/nextpnr/pull/94). It first routes
with up to four generic local enable copies. It then uses that timing report
to try local LUT remap candidate 0 with up to eight LAB groups, routing the
same synthesis and BUILD_ID again. Both attempts retain their own outputs in
`build/fes-ramtest-timing-130/{qor-search,remap-search}/`; `ranking.json`
records which result was selected and why the remap was selected, rejected,
or had no qualifying candidate.

Both completed routes must prove a live HIP backend, final signoff and all
three clock rows; CPU fallback or incomplete routing fails the diagnostic.
The remap wins only with improved memory Fmax, no regression in the other
clock maxima and no final reported hold violations. A completed HIP route is
reported even below 130 MHz; `passing` records whether all three frequency
constraints were met. The hold screen covers final nextpnr warnings, not the
full package evidence or hardware acceptance. The diagnostic RBF is not a
sealed package. The normal 100 and 130 MHz package builds keep their existing
lock and signoff gate. The historical 116.918 MHz route
also used two opt-in,
cell-name-specific probes. Those probes no longer match the current RTL and
remain disabled here; 116.918 MHz is not a result for the current source.

The [merged-compiler measurement](../../docs/validation/2026-09-29-ramtest-merged-compilers.md)
reaches 109.951 MHz memory with generic remapping, from a 106.157 MHz baseline.
Pixel changes from 94.500 to 94.357 MHz; both exceed 74.25 MHz, but the conservative
selector retains the baseline because pixel Fmax decreased. This is a measured
tradeoff, not recovered 130 MHz closure or hardware acceptance.

The [fixed-input controls](../../docs/validation/2026-09-29-ramtest-fixed-input.md)
isolate the lower baseline to the changed embedded BUILD_ID: holding the synthesized
design fixed reproduces the same timing with old and merged nextpnr. Compiler
comparisons therefore need fixed synthesis inputs as well as a fixed seed. The
DDR0 first-error enable exposed by remapping has a verified Boolean opportunity
for a shallower reduction, but no further routed gain has been demonstrated.

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

`make sim-fes-ramtest` checks that the pattern table matches the total,
including a mismatch on the final word, then the byte-lane bench above,
then short spans of the same patterns against behavioral SDRAM and
fpga2sdram models. The DDR model
refuses commands at random and while 14 reads are pending, returns read
data late, and flags a command outside the window or a burst left
incomplete. The test checks
identity and the video, gamepad and HPS DDR capability bits, execution
release, a green status glyph, the `ADDR` signature at three addresses per
port, holds that land inside a write burst and during reads, and a button
stop. The OSS 100 MHz package is registered in FES and included in the
factory image and standard catalog. The 130 MHz build remains explicit.

The RTL is GPL-2.0-or-later. The license text and corresponding-source notice
are in [the FES release notices](../../../../image/licenses/fes.ramtest/SOURCE.md).
Image assembly installs COPYING and a notice generated from the sealed RBF’s
exact source revision in the adjacent `core-notices/fes.ramtest/<package-id>/`
directory, outside the closed package.

Packages before `fes.memory.hps-ddr` tested the SDRAM addon only. Their HPS
scan addressed DDR inside Linux memory and failed on the first write under a
boot bitstream without the layout. They do not declare the interface, so the
runtime keeps their FPGA ports in reset.
