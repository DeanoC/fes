# DE10-Nano target

The included constraints target the Cyclone V SoC FPGA
`5CSEBA6U23I7`. `pins.qsf` assigns the 50 MHz input clock and the LED used by
`010_blinky`; `clocks.sdc` declares the 20 ns clock period.

The pin assignments are cross-checked against the Terasic DE10-Nano manual
(https://www.terasic.com.tw/cgi-bin/page/archive.pl?Language=English&No=1046)
and MiSTer's MemTest constraints
(https://github.com/MiSTer-devel/MemTest_MiSTer/blob/master/sys/sys.tcl).

Normal `sim`, `oss`, `oracle`, and `compare` targets only produce local files.
They never program hardware.

The designated native kit loads an already-built RBF through FogCast
`POST /api/v1/session/development-rbf`. That kit has no `/dev/MiSTer_cmd`.
`make program` is a separate Main-FIFO or JTAG diagnostic for a conventional
MiSTer or DE10-Nano. Neither path writes flash, HPS storage, or the SD card.

Start with a dry run:

```sh
PROGRAM_DRY_RUN=1 MISTER_HOST=misterpi MISTER_USER=root \
  make program EXP=010_blinky BUILD=oss
```

For JTAG discovery on a real DE10-Nano:

```sh
openFPGALoader --board de10nano --scan-usb
openFPGALoader --board de10nano --detect
```

The MiSTer Pi is disposable development hardware. A minimal experiment does
not implement the MiSTer SPI identity the native runtime probes after a
development load; FogCast Stop restores idle through the development reboot
handshake.
