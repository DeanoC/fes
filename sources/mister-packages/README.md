# mister-packages

Development lives in the FES repository under `sources/mister-packages`.
The former standalone repository is archived.

This module owns shared board/SoC/MMIO definitions, FES ABI contracts,
programming-profile declarations, format-2 package schemas and conformance
fixtures. Go emitters generate C++14, Go and Verilog consumers. FES runs
`make generate` and `make check-generated` across the tracked modules.

The programming registry contains `fes-gp-v1` for `fes.simple-game`,
`fes.simple-computer` and `fes.application`, plus the diagnostic-only
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
[application I/O](docs/application-io.md), and [stream media](docs/media-stream.md).
