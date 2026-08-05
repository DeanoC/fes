# FogCast ideas

This is a project to make an emulation/MiSTer software runner, currently a proof of concept.

## POC3 candidates

- Build a small Mac-first UI over the local host API.
- Add explicit execution capabilities, starting with `fpga_native`.
- Add host-only emulator adapters without video streaming through MiSTer.
- Add launch/stop/status events and progress reporting.
- Preserve privacy boundaries: do not expose NAS paths, ROM filenames, cache digests, or credentials.

## Out of scope for the current POC3 slice

- Arbitrary additional systems.
- Wireless controller support.
- Save-state synchronization.
- Internet access or remote administration.
- Video streaming through the MiSTer.
