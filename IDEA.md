# FogCast ideas

FogCast is a project to make an emulation and MiSTer software runner. The host
provides a friendly selector over NAS and other libraries; a MiSTer can play a
game directly through its FPGA when appropriate, or present a game running on a
host or another PC.

The aim is a single, calm game-playing experience on the TV: FPGA titles and
host-emulated titles should share discovery and launch intent, while the target
handles local mechanisms and presents the result. Low-latency input is an
important long-term goal, so a local controller can eventually work naturally
with the selected experience.

My use case is a MiSTer connected to the main TV: play FPGA ROMs directly, or
play a game that does not run on the MiSTer in the same setup. Another use is
playing through the MiSTer locally.

The MiSTer portion should feel integrated and approachable rather than exposing
a collection of separate settings and core menus. Long term, FogCast could
unify a large retro collection and machines across a LAN, including other
PC/Mac/Linux hosts, different FPGA architectures, and eventually real hardware
through capture and USB input converters. Accepting some lag where appropriate
may make that convenience worthwhile.

This is product vision, not an implementation or acceptance commitment. The
current appliance/runtime direction is [the canonical architecture](docs/ARCHITECTURE.md),
and its staged work is [the active roadmap](docs/ROADMAP.md). POC6 remains
historical evidence for one retained testbed, not proof that this vision is
complete.
