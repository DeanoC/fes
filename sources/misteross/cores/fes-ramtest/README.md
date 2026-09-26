# fes.ramtest

Utility core for the MiSTer GPIO SDRAM addon and the FPGA-to-HPS DDR bridge.
The host mailbox is `fes.application` 1.0, with the fixed 720p picture and the
gamepad interface. There is no memory opcode. After execution release, each
path writes a span and reads it back for six patterns: `0000`, `FFFF`,
`5555`, `AAAA`, the address mixed with its high half, and the inverse.
The HDMI text shows the pattern, the live address, and the live expect/got
values. The full error count stays on screen, with the first mismatch address
and the data that was read there, and the most recent mismatch address.

The SDRAM clock pin uses a DDR output and rises on the fabric falling edge.
The Quartus diagnostic builds run at one selected rate for the entire scan:

```sh
QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus make build-fes-ramtest-quartus
RAMTEST_MHZ=100 QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus make build-fes-ramtest-quartus
```

The first command builds 130 MHz into `build/fes-ramtest-quartus/`; the second
builds 100 MHz into `build/fes-ramtest-quartus-100/`. Each directory contains
an RBF, timing report and loadable `comparison.fcore`. The bitstreams use
different build identities. Both select the frequency directly from the PLL,
pack SDRAM command/address registers into the output cells, and capture read
data on a shifted clock in the input cells. The 130 MHz path also pipelines
the captured data before the controller. The controller uses CAS latency 3
at 130 MHz and CAS latency 2 at 100 MHz. A gamepad button stops a scan.

The 50 MHz OSS build remains a separate path via `make build-fes-ramtest`.
`make build-fes-ramtest-100` seals a 100 MHz OSS package into
`build/fes-ramtest-100/` using the pinned HIP nextpnr toolchain. The OSS packer
does not support DDR input registers on bidirectional DQ pads, so that build
uses a phase-shifted 100 MHz clock and a fabric input register. Its timing
report covers the 100 MHz controller, capture, 50 MHz HPS and 74.25 MHz video
domains. Package `58d29d3a99d04988083b04216726662ef6eae7d7af2c804fe3324e14fe721c83`
passed all six full-span SDRAM patterns with zero errors in two hardware runs.
`make toolchain-fes-ramtest-130` provisions `toolchains/ramtest-130.lock`:
Yosys `1bf1ff3d`, Mistral `7ed06e21`, and nextpnr main with the calibrated
Mistral placement delay prediction (DeanoC/nextpnr#86). Then
`make build-fes-ramtest-130` seals its package with seed 2. Without that
prediction the same netlist misses the 130 MHz memory clock on seed 2
(128.16 MHz); with it, 31 of 32 seeds tried close every domain. An earlier
unsealed seed-2 route on nextpnr `50a2832e` passed six full-span SDRAM
patterns with zero errors on the designated kit. That test does not qualify
the current toolchain or bitstream; repeat hardware acceptance on the new
sealed package.

The Quartus timing report
covers internal setup paths but does not constrain external SDRAM I/O timing,
so the full-memory hardware scan is the acceptance evidence for these rates.
The HPS span is 256K steps starting at byte address `0x01000000`
in the same command field the bridge probe used. It does not walk all of
system RAM. A missing acknowledge stops that path, which is what a contained
HPS bridge does. A data mismatch is counted and the scan continues.

A `fes.gamepad` button stops both scans and the status line says `STOP`.
The host maps a keyboard onto those buttons (arrows, Enter, Space, and the
letter keys it already forwards). Escape and Backspace leave the session in
the host and are not delivered to the core.

A `fes-gp-v1` package load releases the HPS bridges after user mode. A raw
development RBF stays on the contained profile, so the HPS path fails until
the bridges are released. The SDRAM addon does not need that release.

`make sim-fes-ramtest` runs a short span of the same patterns against
behavioral memory. It checks identity, the video and gamepad capability
bits, execution release, a green status glyph, and a button stop. The
sealed core keeps the full spans above. `make build-fes-ramtest` seals an
RBF. The package is not registered and is not in the factory image.
