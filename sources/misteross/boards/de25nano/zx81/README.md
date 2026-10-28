# DE25-Nano ZX81 diagnostic

This standalone Quartus Pro 26.3 lane builds an FPGA-only ZX81 BASIC machine
for the DE25-Nano revision-B `A5EB013BB23BE4SCS`. It uses the existing
`zx81_machine`, first-party NMOS Z80, 16 KiB internal RAM and 720p renderer.
The repository's ZX81 firmware initializes the ROM. KEY0 resets the machine;
KEY1 supplies ENTER through the real keyboard matrix. Other keys are released.
HDMI audio pins remain low.

Run from this directory:

```sh
make test
make sim
make project VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
make build QUARTUS_ROOTDIR=/absolute/altera_pro/26.3 \
  VENDOR_ARCHIVE=/absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
```

The builder uses the same SHA-256 locked Terasic archive, SCS HDMI pins,
Reset Release IP and unchanged ADV7513 initializer as the
[display diagnostic](../display/README.md). Two native Agilex PLLs generate
74.25 MHz for HDMI and 52.224 MHz for the machine. The existing rational
clock-enable scheduler then supplies the 6.5 MHz ULA and 3.25 MHz CPU phases.
The targets are not rounded to a different machine speed.

Preparation derives board-local copies of `zx81_dpram` and
`zx81_video_720p`, replacing their guarded Cyclone V/M10K selections with
Agilex 5/M20K. It records original and derived hashes and retains the RAM's
one-cycle read and same-port new-data semantics. Shared ZX81 RTL and the
DE10 package recipes are unchanged.

`project` prepares a fresh private output. `build` requires clean committed
source, regenerates IP and compiles with two CPU workers. `OUTPUT=/new/path`
selects an explicit destination; existing destinations are refused. Retained
inputs, commands, generated SDC and report hashes bind the resulting
`project/output_files/de25nano_zx81.sof` to its producer. No command programs
hardware or writes flash.

The board simulation uses the real machine, CPU, firmware and video RTL with
controllable digital PLL/configuration/I2C boundaries. It does not model the
physical PLL, RAM primitive, I2C transfers or HDMI link. The existing capture
buffer has asynchronous write/read clocks and no complete-frame ownership;
startup, read/write collisions and visible tearing remain physical
qualification gaps. External HDMI timing and the vendor's contradictory
DJ24 clock IO-standard documentation retain the same open gates as the
display diagnostic. Timing reports must be assessed separately from compile
success.

This is a JTAG SRAM diagnostic, not a `fes.zx81` package. It has no HPS,
LPDDR4, runtime transport, USB keyboard, tape delivery, expansion socket,
session display or audio source. It does not preserve the Linux boot design
or qualify a pairing with Terasic boot firmware. Initial physical testing
requires the identified SCS board, this exact SOF and an HDMI capture; Linux
serial/DHCP/SSH bring-up remains a separate target.
