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
arguments or logged; an operator may answer the normal SSH prompt. The bounded
SSH options use `BatchMode=no`, `ConnectTimeout=10`, one connection attempt,
and `StrictHostKeyChecking=yes`. Use an SSH key/agent when repeated prompts
are undesirable; no password or persistent ControlMaster socket is stored by
this procedure.

Before an upload, the script performs one consolidated read-only SSH preflight
for an ARM architecture, root-owned `/dev/MiSTer_cmd` FIFO metadata, exactly
one root-owned `Main_MiSTer`/`MiSTer` PID, that process's authenticated
executable identity, a matching open FIFO inode, and writable `/tmp`. Every
non-dry action also requires the explicit operator attestation
`--expected-board misterpi` (or `PROGRAM_EXPECTED_BOARD=misterpi`) and the
expected Main executable SHA-256 (`--expected-main-sha256` or
`MISTER_EXPECTED_MAIN_SHA256`). A dry run may report the discovered Main hash,
but does not authenticate it for an action.

The stage directory is a fresh cryptographically unpredictable
`/tmp/misteross-<32-hex>/`, created atomically with mode `0700`; an existing
name, race, symlink, non-root owner, wrong type/mode/link count, or failed
metadata check stops before upload or FIFO dispatch. The RBF is uploaded as
`artifact.rbf` inside that private directory and its exact remote SHA-256 and
metadata are verified before (only without `--dry-run`) writing:

```text
load_core /tmp/misteross-<32-hex>/artifact.rbf
```

to `/dev/MiSTer_cmd`. The script prints the exact shell-escaped action and
reports only `load request dispatched; outcome unverified`; it never claims
that the FPGA loaded successfully. Hardware observation and post-load status
belong to Task 10. The volatile directory is intentionally left in place for
operator recovery and disappears on reboot/power-cycle; it is never a
persistent programming path. FogCast may own a custom `/media/fat/MiSTer`, so
the procedure never overwrites or replaces it and does not write the SD card,
flash, or persistent configuration.

The cited Main source is pinned to the immutable
[DeanoC/Main_MiSTer commit `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d` `input.cpp`](https://github.com/DeanoC/Main_MiSTer/blob/d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d/input.cpp),
which defines `/dev/MiSTer_cmd` and exact `load_core PATH` handling; its
[`fpga_io.cpp`](https://github.com/DeanoC/Main_MiSTer/blob/d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d/fpga_io.cpp)
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
one matching Cyclone V JTAG device. `--cable usb-blasterII` names only the
interface type. If more than one exact cable row is discovered, pass the
physical index captured from that scan with `--cable-index`; an absent or
out-of-range index stops. Every non-dry JTAG action also requires the explicit
operator attestation `--expected-board de10nano` (or
`PROGRAM_EXPECTED_BOARD=de10nano`). The loader consumes a private immutable
local snapshot of the validated RBF, so replacing the canonical file during
discovery cannot change the bytes selected for programming. The final
operation is only `openFPGALoader ... --write-sram <private-snapshot.rbf>`.
There is no flash flag, address, SD-card path, or persistent programming
option. SRAM configuration disappears at power-off, so a power-cycle restores
the board's normal persistent configuration. No programming action is part of
the repository's disconnected tests or dry-run checks.
