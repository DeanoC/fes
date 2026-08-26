# DE10-Nano constraints

These are the minimal constraints for the Terasic DE10-Nano/DE0-Nano-SoC
target used by `010_blinky`. The Terasic [DE10-Nano User
Manual](https://www.terasic.com.tw/cgi-bin/page/archive.pl?Language=English&No=1046)
lists `PIN_V11` as the 50 MHz `FPGA_CLK1_50` input and `PIN_W15` as
`LED[0]`.

The maintained MiSTer [MemTest_MiSTer `sys.tcl` source](https://github.com/MiSTer-devel/MemTest_MiSTer/blob/master/sys/sys.tcl)
independently uses those same assignments for the exact Cyclone V device
`5CSEBA6U23I7`. That independent source check is provenance for this
DE10-Nano constraint file; it is not a claim that MiSTer Pi or SuperStation
One exposes an equivalent FPGA package or pinout.

`pins.qsf` intentionally contains only the clock and LED locations, with
`3.3-V LVTTL` I/O standards. `clocks.sdc` describes the 20.000 ns input clock.
No hardware programming or board deployment is part of this experiment.
