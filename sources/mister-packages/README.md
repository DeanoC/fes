# mister-packages

Development lives in the FES repository under `sources/mister-packages`.
The former standalone repository is archived.

This module owns shared board/SoC/MMIO definitions, FES ABI contracts,
programming-profile declarations, format-2/3/4 package schemas and conformance
fixtures. Go emitters generate C++14, Go and Verilog consumers. FES runs
`make generate` and `make check-generated` across the tracked modules.

The programming registry contains `fes-gp-v1` for `fes.simple-game`,
`fes.simple-computer`, `fes.application` and `fes.computer`, plus the diagnostic-only
`development-contained-v1`. Conventional game profile/source pins and the
MiSTer programming/ABI pair are retired. Format-2 structural fixtures may
still describe arbitrary ABIs; syntax validity does not confer activation
support. Historical oracle records remain provenance.

The runtime owns physical programming and compatibility admission; FogCast
owns library/session context; misteross owns source builds and RBF provenance.
Shared persistence layouts and wire contracts remain here.

Run `make test` with Python `jsonschema` available and Go installed.
Use `make fixtures` to regenerate canonical fixtures and `make check-fixtures`
to verify them. See [schema](docs/schema.md),
[application I/O](docs/application-io.md), [home-computer I/O](docs/computer-io.md)
and [stream media](docs/media-stream.md). The simple-computer
[session display](docs/session-display.md) shares the fixed DDR framebuffer
layout while preserving machine execution and existing ABI 1.0 packages.

Atari ST adds the read-only 720 KiB unit-0 media interface to the existing
computer ABI and a separate internal [68000 expansion fabric contract](packages/fabric/fes_fabric_atari_st_bus.yaml).
Neither changes GP framing or establishes hardware acceptance.

[Timed video parts](docs/video-parts.md) define an internal FPGA fabric socket
using a separate `kind: fabric` package and the existing Verilog constant
emitter. Its fixed 720p proof adds no GP ABI or runtime capability assignment.

The computer contract also defines opt-in relative mouse and writable ST disk
capture. [Computer I/O](docs/computer-io.md) owns the GP framing/capabilities;
[media data](docs/media-data.md) owns the durable complete-image record and
patterned independent fixture. Existing read-only computer endpoints retain
their earlier capability set and reject the added opcodes.

Required `fes.media.atari-st-floppy-geometry` 1.0 adds capability 10 for bounded
80–82 track, one/two-head, nine/ten-sector raw ST disks with 512-byte sectors.
Legacy 720 KiB behavior and durable record bytes remain unchanged; nonlegacy
saved records use layout 1.1. See [computer I/O](docs/computer-io.md).
