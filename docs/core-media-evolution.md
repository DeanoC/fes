# Core media capacity and transport

The host library and an FPGA core have different capacity limits. A stored
asset is not necessarily runnable by the selected package.

ZX81 mid-session `.p` tape select/load (mailbox blob while the core stays
running) is designed in [ZX81 tape media](zx81-tape-media.md). That lock
reuses `fes.media.blob` capacity below; it does not widen ZX81 onto
`fes.media.blob-stream` in v1.

## Host storage foundation

The selected FogCast host exposes the supported interpretation of an installed
package through `core-media-capabilities PACKAGE_ID`. This is an offline
`declared-contract` projection, with target compatibility explicitly unknown.
It reports role, raw format, minimum/maximum byte count, interface/version and
transport separately from the host's import policy.

Host import/storage accepts 1 byte through 32 MiB, using the existing general content policy.
CLI snapshots, API ingestion and catalog chunk storage use bounded buffers;
catalog chunks are at most 64 KiB. The host stages an upload before reserving a
database writer and publishes its SHA-addressed object atomically. Existing
inline assets and library identities remain valid. These storage chunks are
not FPGA transfer packets, and 32 MiB is not an advertised cartridge capacity.

The existing `fes.media.blob` 1.0 transport under `fes.simple-computer` 1.0
or `fes.application` 1.0 still
accepts exactly 1..16384 bytes. Larger assets can be retained, but selection and
launch reject them for that contract before programming the FPGA. Unknown
contract versions expose no supported media roles; optional interfaces still
require active runtime support before delivery.

The original host-only storage slice did not change the package format, target
wire, shared ABI, runtime or FPGA. Its legacy SMS package remains limited to
16 KiB; storage capacity alone does not widen that package's contract.

## Versioned larger-media integration

The shared `fes.media.blob-stream` 1.0 wire contract is published in
[mister-packages](../sources/mister-packages/docs/media-stream.md).
Runtime and host software are merged and selected for integration. They
use 32-bit lengths and offsets, ordered 512-byte chunks and CRC32/IEEE, while
keeping legacy blob 1.0 unchanged. The stream contract guarantees 1..32768
bytes; the runtime separately checks the active endpoint's observed capacity.
The concrete SMS target is a 32 KiB fixed map, not general mapper support.
The selected Coleco application uses the same stream contract with two
controller ports and keypads. Its 32 KiB cartridge aperture maps CPU addresses
`0x8000–0xffff`; images above 16 KiB return `0xff` beyond their committed size.
See the [Coleco integration record](coleco-stream-32k.md) for exact revisions
and the distinction between component diagnostics and image validation.

The corrected SMS RTL is merged and its HIP-sealed package is handed off.
Combined-source verification and exact-artifact hardware acceptance are tracked in the
[SMS larger-media integration plan](sms-large-media-plan.md). Implemented
software and published definitions do not establish target acceptance.

### Capability authority

1. Shared definitions identify the media interface/version and semantics.
2. A package describes its requirements and any core-specific capacity, format
   or mapper constraints. A core name never selects these properties.
3. The runtime advertises its implemented driver/transport support separately
   from the active core's observed, negotiated support.
4. FogCast validates the selected asset against the known constraints before
   activation and verifies the resulting active contract before delivery.

Missing advertisement must preserve explicit legacy blob 1.0 semantics or
report unknown/unsupported. It must never mean unlimited capacity. A package
declaration is not proof that an optional interface activated. A maximum size
alone does not prove support for a cartridge's mapper, bank layout or format.
Existing package IDs, archive bytes and seals must not be rewritten merely to
expose the legacy projection.

### Transfer and lifecycle

Larger transfers require an explicitly versioned interface/operation. Do not
widen the old 16-bit media-length/mailbox operation in place or treat the
host's storage-chunk size as a negotiated FPGA chunk size.

The contract and its consumers must enforce:

- Explicit total length and offsets wide enough for the supported media range,
  plus separately bounded chunk length.
- Begin, ordered writes, and commit acknowledgements tied to the admitted
  package/generation and existing kit ownership.
- Exact length/completion checks and a specified integrity check. Host/runtime
  staging verifies the immutable media digest; claiming an FPGA-side digest
  requires that check to exist in the actual endpoint.
- Defined behavior for duplicate, missing, out-of-order and oversized writes;
  no automatic replay after an ambiguous mutation response.
- Reset/quiesce held while media is incomplete, and an explicit abort/timeout
  outcome. An incomplete transfer must never be released as a playable core.
- Bounded Stop/recovery that preserves truthful ownership on cleanup failure.

The old `load_media`/blob 1.0 path continues to work unchanged. Host-to-agent
file staging, the runtime's local operation and the hardware mailbox are
distinct layers; only layers needing new semantics should change.

### Package evolution

Format 2 is closed: adding arbitrary media fields would break its parsers and
seals. If core-specific metadata cannot be represented by the existing versioned
interfaces, introduce an explicit package format revision with coordinated
schema, producer, runtime and host readers. New readers retain format-2 support;
old readers reject the new format clearly. Do not silently reinterpret an
existing field or generate per-core host allowlists.

The current library selects one optional media role. Multiple required assets
(for example BIOS plus cartridge) need an explicit entry schema extension when
a real core requires it; they are not implied by the current `blob` role.

## Ownership and completion gates

| Part | Owner |
| --- | --- |
| Interface semantics, widths and generated definitions | mister-packages |
| Advertised driver/active capabilities, staging and transfer enforcement | libmister-runtime |
| Core storage, banking/mappers, endpoint and sealed package production | misteross; Caster owns SMS |
| Immutable library assets, selection, host/agent APIs and UI | FogCast |
| Compatible pins, integration and exact-artifact acceptance | FES |

Implement the shared/runtime contract with a concrete larger-media core rather
than a speculative transport framework. Before claiming larger-ROM support,
verify boundary sizes, short/long and interrupted transfers, wrong generations,
abort/Stop/relaunch and the actual core's address/bank behavior. Simulation and
software tests are necessary, but real-target acceptance must identify the exact
package/runtime and media digest. Importing a large file is only storage evidence.
