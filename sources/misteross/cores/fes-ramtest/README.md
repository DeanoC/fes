# fes.ramtest

Utility core for the MiSTer GPIO SDRAM addon and the FPGA-to-HPS DDR bridge.
The host mailbox is `fes.application` 1.0. The picture is fixed 720p. There is
no memory opcode: after execution release, the core writes and reads four
halfwords on each path and paints the result.

The top bar is the SDRAM addon. The bottom bar is HPS DDR. Green is pass, red
is fail, and amber is still running or still held in application reset.
A `fes-gp-v1` package load releases the HPS bridges after user mode. A raw
development RBF stays on the contained profile, so that HPS bar fails until
the bridges are released. The SDRAM addon does not need that release.

`make sim-fes-ramtest` runs the march against behavioral memory and checks
identity, the video capability bit, execution release, and a green bar.
`make build-fes-ramtest` seals an RBF. The package is not registered and is
not in the factory image.
