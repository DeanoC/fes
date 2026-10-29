# Shared scanout copier and Nano boot display contract

FES-owned exploratory native-base work on `codex/de25-embedded-os`, based on
`32c16adfd5` (full base identity in the receipt). The result is the commit
containing this record. Evidence is host software/fault tests, native AArch64
QEMU output and Nano cross-compilation. No physical board, shared kit, FPGA
programming, GPU or Powerboat process was used. Production Linux components,
shared ABI definitions and generated consumers are unchanged.

## Implemented path

QEMU RAMFB and Nano now use one RGBA-to-XRGB8888 CPU copier. A private Go frame
is copied into a separate permanently reserved window, with bounded four-row
yield points, explicit publication and serialized callers. It handles subimage
origins and distinct strides without changing row padding. Write/publication
failure poisons the surface and retains the entire window. No future access
through that surface is accepted. Invalid source arguments reject before writes.
The old RAMFB copy loop was removed; QEMU's fw_cfg configuration remains separate.

The optional Nano adapter maps an explicit 4 MiB boot-owned HPS DDR window,
requires fixed 1280x720/5120-byte XRGB8888 geometry and allows one open attempt.
Schema 4 admits the declaration only outside Go RAM, firmware reservations and
Ethernet DMA, with fixed boot lifetime and declared firmware artifact digest.
Disabled display is null/false. Superseded profiles reject. The shared boot mapper
and memory barrier now serve network and display without resetting their pools.

This is an initial **preinitialized firmware scanout** path, not an Agilex port
of the DE10 menu ABI. That ABI's fixed DDR layout cannot be inferred onto Nano.
The adapter programs no FPGA/display registers, validates no live firmware ID,
and has no vblank, physical drain, release or safe reconfiguration operation.
Firmware must already provide the reader, HDMI timing/PHY and DDR reachability,
and keep that image/address/geometry immutable for the boot. Its declared digest
is provenance input, not observed identity. No compatible Nano bitstream or
bootable image is supplied. This change alone establishes no physical Nano UI.

Tenfoot uses the optional provider through the existing board interface. Its
successful copy completes only the software model's preparation. A presenter
error returns before `FinishFrame`, leaving the model grant held. Close must
refuse that outstanding owner rather than converting an unknown publication
into a successful completion. None of these model checks authorizes physical
FPGA replacement; the boot scanout pool stays pinned after application exit.

## Checks and receipts

- Entire module: `GOWORK=off go -C platform/embedded/tamago test -race ./...` passed.
- Scanout tests: channel order, padded strides, nonzero-origin subimage, source
  independence, window bounds, bounded chunks, serialized concurrent frames,
  invalid geometry and retained ownership after partial write/publication error.
- Fixture-backed modelbackend suite: passed with `FES_MODEL_FIXTURES` recorded
  in the receipt. It includes an actual failed scanout publication followed by
  refusal to close the held model frame. Host-only cleanup confirms that this
  fake sink has no asynchronous access; it is not a physical drain observation.
- Embedded/profile Python tests: passed, including rejection of DE10 addresses,
  Go/firmware/network overlap, malformed geometry/provenance/lifetime and stale
  schemas. Disabled display remains explicit.
- Native QEMU `display`: actual RAMFB color bars and serial-requested inverted
  bars were captured through QMP. Every RGB pixel in both captures matched the
  independently generated expected pattern. No disk/network/USB is involved.
- Native QEMU `play`: all eleven actual tenfoot launch/return/failure cases passed
  through the shared presenter, including stale grants and model cleanup before
  QMP shutdown. FAT32 hash unchanged. These are software FPGA endpoints.
- Nano `probe`, `display`, `tenfoot`: AArch64 cross-builds passed with a synthetic
  enabled display profile; a probe with display disabled also cross-built.
  ELF/layout and QEMU-device exclusion checks passed. The exclusion check now
  matches exact controller package identities, so `cmd/display` is not mistaken
  for the separate QEMU `display` implementation.
- `make test-changed` against the base above: passed parent regressions and
  generated-consumer consistency. `make deadcode` and whitespace checks passed.

The [compact receipt](evidence.json) binds final selected sources, compiler/ELF
identities, test/fixture inputs, full local receipts/logs and screenshot hashes.
Logs, captures and binaries remain under the named worktree `out/` paths; no
hardware acceptance or portable release is attached.

See the [scanout/firmware contract](../../../platform/embedded/tamago/scanout/README.md)
for exact schema fields and supported behavior. First integration requires a
matching, independently qualified Nano firmware artifact and memory handoff.
After serial boot, run `display`: inspect the first frame and send serial `n`
for the second. Validate actual DDR reads, bounds and HDMI output before enabling
tenfoot. Physical programming/identity/containment and generation-bound display
quiescence remain unfinished prerequisites for a real launch/return cycle.
