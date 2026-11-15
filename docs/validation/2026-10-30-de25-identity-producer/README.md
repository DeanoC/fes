# Embedded control identity producer prototype

Misteross-owned exploratory work in the FES worktree, based on `8d85018f4`.
The producer implementation is `c682d7fb9` and the host-tool selection
follow-up is `99f892413`, and the source-identity mismatch test is
`ee893f749`; the result also includes this validation record.
Evidence is host simulation and native Go tests. No board, shared kit, GPU,
FPGA synthesis/place-and-route or Powerboat process was used.

## Scope

The standalone producer hashes its explicit source and tool inputs in the
existing canonical format-2 build-input record, calls the existing
`build_identity` function, and supplies that 128-bit value to a top-level
simulation module. The top passes it to the FPGA identity/drain endpoint as
`BUILD_ID`; an explicit experimental control-ABI value is passed separately.

The producer emits a Verilated host executable, build-input record and
simulation-only descriptor. The descriptor binds the record and executable
bytes to the build ID. This is not a FES package, RBF, production seal or
DE25 bitstream. The existing package exporter still accepts the DE10 target;
this prototype does not relax that admission or register a Nano profile.

## Validation

- Two committed-source builds at `ee893f749` changed only their explicit
  control ABI; their functional build IDs and live ABI observations differed.
  Source and tool inputs matched between them. Each live endpoint returned
  every build-ID word and completed a held-address, partial-burst and
  final-drain check. The canonical input records and descriptors are retained
  alongside this note; the executable remains in the local `out/` directory.
- The native Go adapter read the producer descriptor, checked a synthetic model
  admission binding against the actual produced Verilator model, rejected
  wrong build/ABI values, and enabled, disabled and drained the reader. This
  ran under Go race detection; it used a synthetic payload, not a playable core.
- Changed build ID, ABI, descriptor, build record and executable were rejected.
  A self-consistent altered source record and descriptor, with the ABI unchanged,
  were also rejected by the live compiled model. Hashes detect drift; they do not authenticate untrusted code.
- A second committed-source run passed with TamaGo cross-compilation variables,
  an invalid inherited make flag and symlinked simulator/compiler paths. A
  controlled-path test also checked that a second compiler in a simulator
  directory cannot displace the selected compiler.
- The prototype lives outside the source roots used by existing video-part
  producers; those roots did not change. No shipped core consumes this top.
- `make test-changed`, `make check` and `make deadcode` passed at `c682d7fb9`.
  The only subsequent source changes were to producer tool selection and its
  verifier; the complete producer integration and `make check` passed again at
  `ee893f749`. The adjacent receipt identifies the commands, source snapshots,
  artifact hashes and local log paths for each run.

## Remaining work

A board producer still needs an Agilex-capable synthesis/implementation flow,
verified Nano pin/clock/reset and CSR/DDR integration, and real package/profile
admission. Its final bitstream, timing and hardware behavior must be qualified
separately. No physical claim follows from this host simulation.
