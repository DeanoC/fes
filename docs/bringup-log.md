# Host survey — 2026-08-26

Initial non-mutating survey on the development host:

| Capability | Result |
| --- | --- |
| CMake | present |
| Ninja | present |
| Python | present |
| Git | present |
| FPGA tools (Quartus, Yosys, nextpnr-mistral, Mistral, openFPGALoader) | not detected |
| USB-Blaster | not detected |

This log records exact tool identities, command syntax, device support, cable
visibility, artifact hashes, and hardware observations as they are established.
The initial host survey does not install packages or alter the machine.

## Evidence policy

Bootstrap and diagnostics are idempotent and non-mutating unless an explicit
build action is requested. Before wrappers rely on command syntax, the pinned
tools' version/help output will be captured here. A hardware result records the
experiment, tool versions, artifact hash, and observed LED cadence.
