# FogCast ideas

FogCast is a calm game-playing experience for a TV: a host selector over NAS
and local libraries chooses FPGA-native games, host-emulated games, or other
machines on the LAN. The MiSTer should feel like a simple cast target rather
than a second library-management UI.

The immediate product path is the working direct FPGA launch flow described in
the [canonical architecture](docs/ARCHITECTURE.md), followed by development
RBF loading through that same target command path. Low-latency input, capture,
remote media, and additional hosts are useful extensions, not prerequisites
for that path.
