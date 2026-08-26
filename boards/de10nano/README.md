# DE10-Nano constraints and safe volatile loading

These are the minimal constraints for the Terasic DE10-Nano/DE0-Nano-SoC
target used by `010_blinky`. The Terasic [DE10-Nano User
Manual](https://www.terasic.com.tw/cgi-bin/page/archive.pl?Language=English&No=1046)
lists `PIN_V11` as the 50 MHz `FPGA_CLK1_50` input and `PIN_W15` as
`LED[0]`.

The maintained MiSTer [MemTest_MiSTer `sys.tcl` source](https://github.com/MiSTer-devel/MemTest_MiSTer/blob/master/sys/sys.tcl)
independently uses those same assignments for the exact Cyclone V device
`5CSEBA6U23I7`. That source is pin-file provenance; it is not a claim that a
MiSTer Pi or SuperStation One exposes an equivalent FPGA package or pinout.
The target devices in this project are user-attested MiSTer-compatible
DE10-Nano RBF contracts.

`pins.qsf` intentionally contains only the clock and LED locations, with
`3.3-V LVTTL` I/O standards. `clocks.sdc` describes the 20.000 ns input clock.

## Preferred MiSTer Pi ARM/FIFO transport

The user's MiSTer Pi and SuperStation One do not have an onboard USB-Blaster.
The primary `program.py` transport therefore loads an RBF through the
designated MiSTer Pi's ARM-side interface. Supply both endpoint fields
explicitly; this repository has no host or account default:

```text
MISTER_HOST=mister.example MISTER_USER=root \
  make program EXP=010_blinky BUILD=oss
```

`MISTER_HOST` and `MISTER_USER` may instead be passed as `--host` and `--user`
to `scripts/program.py`. They are validated as strict hostname/user values and
are passed to `ssh`/`scp` as array elements. Passwords are never command-line
arguments or logged; an operator may answer the normal SSH prompt.

Before an upload, the script performs read-only SSH checks for an ARM
architecture, the `/dev/MiSTer_cmd` FIFO, a running `Main_MiSTer`/`MiSTer`
process, writable `/tmp`, and an unused deterministic staging name. It stages
only at `/tmp/misteross-<experiment>-<hash-prefix>.rbf`, verifies the remote
SHA-256, then (only without `--dry-run`) writes exactly:

```text
load_core /tmp/misteross-<experiment>-<hash-prefix>.rbf
```

to `/dev/MiSTer_cmd`. `/tmp` is volatile. FogCast may own a custom
`/media/fat/MiSTer`; the procedure never overwrites or replaces that file and
does not write the SD card, flash, or any persistent configuration. The
authenticated [DeanoC/Main_MiSTer `input.cpp` FIFO implementation](https://github.com/DeanoC/Main_MiSTer/blob/fogcast/stage-a-baseline/input.cpp)
defines `/dev/MiSTer_cmd` and exact `load_core PATH` handling, while
[`fpga_io.cpp`](https://github.com/DeanoC/Main_MiSTer/blob/fogcast/stage-a-baseline/fpga_io.cpp)
loads an absolute RBF and restarts the FPGA application. A minimal blinky can
make Main exit because it does not provide the MiSTer framework handshake;
rebooting or power-cycling the unit restores the normal menu. The script never
reboots automatically.

Use `PROGRAM_DRY_RUN=1 make program ...` (or `--dry-run`) to run local and
read-only remote checks without SCP upload or FIFO load. An absent host/user,
ambiguous remote state, missing FIFO/process, staging collision, or hash
mismatch stops before any action.

## Optional external USB-Blaster/JTAG transport

For a real DE10-Nano, an external host can use the optional JTAG path:

```text
PROGRAM_TRANSPORT=jtag PROGRAM_DRY_RUN=1 \
  make program EXP=010_blinky BUILD=oss
```

Power the board through the connector labelled **Power DC Jack** and connect
the host to the connector labelled **USB-Blaster II**. These labels and the
connection points are shown in Figure 2-3 of Terasic's
[DE10-Nano Getting Started Guide](https://www.terasic.com.tw/attachment/archive/1046/Getting_Started_Guide.pdf).
On Linux, first check USB enumeration and permissions, then use the pinned
read-only discovery commands:

```text
openFPGALoader --board de10nano --scan-usb
openFPGALoader --board de10nano --detect
```

`program.py` requires one exact USB-Blaster II VID/PID row (`09fb:6810`) and
one matching Cyclone V JTAG device. If more than one cable is present, pass
the exact discovered identifier with `--cable`; otherwise it stops. The final
operation is only `openFPGALoader ... --write-sram <canonical-top.rbf>`.
There is no flash flag, address, SD-card path, or persistent programming
option. SRAM configuration disappears at power-off, so a power-cycle restores
the board's normal persistent configuration. No programming action is part of
the repository's disconnected tests or dry-run checks.
