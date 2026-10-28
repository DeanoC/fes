# DE25-Nano FPGA bring-up

This standalone FPGA diagnostic targets the Terasic DE25-Nano with
`A5EB013BB23BE4SCS`. It counts upward on the eight active-low user LEDs once
every half second, using the 50 MHz `CLOCK0_50` input. Clock and LED pins use
the `1.1-V` I/O standard. The Reset Release IP holds the counter reset until
the FPGA finishes initialization; reset then releases through two registers
in the 50 MHz domain. The HPS, SDRAM, LPDDR4, HDMI and expansion headers are
not instantiated.

The separate [HPS mailbox diagnostic](hps/README.md) extends the pinned vendor
GHRD with a small bridge-relative register block and a transaction simulation.
It preserves the vendor HPS/LPDDR4 configuration and tracks its own generation,
compiler and firmware-compatibility evidence.

Run from this directory:

```sh
make sim
make test
make project
make build QUARTUS_ROOTDIR=/absolute/path/to/quartus-pro-26.3
```

`project` needs only Python and copies a self-contained project into a fresh
directory beneath `../../build/diagnostic/de25nano/`. `OUTPUT=/new/directory`
selects an explicit new destination. Existing outputs are never overwritten.
`build` requires an explicit Quartus Prime **26.3.0 Pro Edition** installation
with Agilex 5 device support and the required authorization. Device, pin and
configuration settings follow Terasic's rev. B golden project. Compilation
uses two CPU workers. It does not use the DE10-Nano's Quartus Lite
17.0.2 oracle helper or the Cyclone V OSS toolchain. The project preserves
the golden project's active-serial x4 scheme and 125 MHz `OSC_CLK_1`
initialization clock. It generates the Reset Release IP with the same
installation's `quartus_ipgenerate` before compiling.

Each build retains copied sources, `inputs.json`, `quartus-version.txt`,
`ip-generation.log`, `compile.log` and Quartus output reports. A successful compile adds
`build.json` and `output_files/de25nano_blinky.sof`. The SOF is a standalone
JTAG SRAM diagnostic, not an FES core package or an image payload. No command
programs hardware or writes flash. Compile success does not establish timing
or hardware acceptance; the clock is constrained at 50 MHz, while LED outputs
have no external synchronous timing contract and remain visible as
unconstrained I/O in TimeQuest.

Pins and device identity come from the official [Terasic rev. B resource
package v1.0.0](https://dl2.terasic.com/resources/de25-nano/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip),
`Demonstration/FPGA/Golden_top/golden_top.qsf`. The downloaded ZIP SHA-256 is
`f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66`;
the golden QSF SHA-256 is
`de1431e72627743c7fa3ba7d6b8a833c54a28b9a3c0bfa76292126175bcea105`.
The manuals list an `A5EB013BB23BE4SR1` device, while the golden project and
current vendor BSP specify `A5EB013BB23BE4SCS`; this project targets the latter
explicitly. An R1-marked board needs its own device qualification. The rev. A
and rev. B manual clock/LED pin tables agree; no memory support is implied.

`reset_release.ip` derives from the same resource package's
`Demonstration/FPGA/Board_Info_RTL/reset_release.ip`, retaining its license
notice. Its device and IP version are updated to `A5EB013BB23BE4SCS` and
`19.4.9`, matching that example's Quartus 25.1.1 generation report and the
component supplied with Quartus 26.3. The
[Agilex 5 configuration guide](https://docs.altera.com/r/docs/813773/26.1/device-configuration-user-guide-agilextm-5-fpgas-and-socs/understanding-the-reset-release-ip-requirement)
requires this IP because fabric sectors enter user mode at different times.
The Verilator bench uses a separate controllable `reset_release_model.v`;
it checks delayed initialization, asynchronous reset assertion, synchronous
release and the unchanged hardware divider. That functional model does not
prove configuration behavior on a physical FPGA.

The [2026-10-28 compiler record](../../docs/validation/2026-10-28-de25-quartus-26.3/README.md)
retains a successful Quartus 26.3.0 Build 114 diagnostic compile. Required
50 MHz fabric timing passes; LED output timing, electrical qualification and
MTBF remain open, and overall design closure is not established. The record
identifies the two retained warnings and exact SOF digest.

The next hardware check is a manual JTAG SRAM load of that exact SOF on an
identified SCS board. Verify the eight LEDs count at half-second intervals and
retain the board/device identity. This check does not establish Linux boot,
display, memory or runtime support.
