# Media blob stream 1.0

This is the authoritative shared wire contract for `fes.media.blob-stream`
1.0, an extension of `fes.simple-computer` ABI 1.0 (tag 2), not ABI 2.
The YAML constants are in [fes_simple_computer.yaml](../packages/abi/fes_simple_computer.yaml).
Definitions and synthetic fixtures do not establish consumer implementation or
hardware acceptance.

## Admission and compatibility

Live identity capability bit 3 advertises this interface. Existing keyboard,
fixed video and blob 1.0 bits 0..2 and all identity/version words are unchanged.
New SMS packages declare stream 1.0 **required**, alongside the existing required
blob 1.0, keyboard 1.0 and fixed-video 1.0 interfaces. Old runtimes therefore
reject the unknown required interface before activation. This uses existing
format-2 interface fields; no package format or seal interpretation changes.

Every stream 1.0 endpoint accepts every byte length from 1 through 32768.
Info advertises min=1, max in 32768..33554432 and chunk maximum=512 bytes.
The first SMS endpoint advertises max=32768 and uses a fixed map; neither that
advertisement nor this byte transport promises mapper support. Host import
capacity is not endpoint capacity. Validate declared support before activation
and actual advertised limits after identity discovery, before media delivery.
Unsupported/missing advertisement never implies unlimited capacity or fallback
to a widened legacy operation.

### SMS fixed-map tail

For the first SMS endpoint, the cartridge map is 0x0000..0x7fff. After a
successful commit of length N, every address N..0x7fff reads 0xff, including
after replacing a longer image with a shorter one. This must be established
before readiness/Commit acknowledgement; stale bytes from the prior image
must not be visible when execution is released. An endpoint may implement
this by filling storage or masking reads beyond the committed length.
This is an SMS fixed-map rule, not a mapper promise of generic stream 1.0.
Runtime/RTL consumer tests must load a longer non-0xff image, then a shorter
image, and verify the new payload and the entire unused 0xff tail after commit.

## Wire layout

Existing request toggle, ACK, signature and error encoding remain unchanged.
Arguments and responses are u16. All u32 values use low word followed by high
word. Each request is acknowledged before issuing the next. Successful mutating
requests return zero. Info requires argument zero and returns the indexed value.

| Opcode | Name | Index / argument |
| --- | --- | --- |
| 7 | Info | 0 min low, 1 min high, 2 max low, 3 max high, 4 maximum chunk bytes |
| 8 | Begin | 0 total low, 1 total high, 2 expected CRC32 low, 3 expected CRC32 high |
| 9 | Chunk | 0 offset low, 1 offset high, 2 chunk byte length |
| 10 | Data | Index is word ordinal 0..255; argument is payload byte pair |
| 11 | Commit | Index 0, argument 0 |
| 12 | Abort | Index 0, argument 0 |

Begin and Chunk headers are strictly sequential. Their final word atomically
arms the transfer/chunk only after validating the assembled header. A rejected
word does not advance staging. A caller can provide the correct next word or
Abort; it cannot restart a partially staged header by sending index zero.
Info may be read during staging and does not alter it.

Begin requires reset held, no legacy transfer and no active stream transfer.
Its first accepted word clears readiness. Total must be within the advertised
limits. Chunk requires an active transfer, no partial Begin or outstanding
chunk, offset exactly equal to received bytes, and length 1..512 no larger than
`total-received`. Check subtraction bounds before arithmetic; counters and
lengths are at least u32 and must not wrap.

Data requires an armed chunk and exactly its next word ordinal. Low byte goes
first. The last word of an odd-length chunk has high byte zero; that padding is
not stored or included in CRC. A valid word updates received count and CRC
exactly once. After ceil(chunk-length/2) words, the chunk closes and another
Chunk header is required. Extra, duplicate or out-of-order words are rejected.
There is no implicit wrap from ordinal 255 to zero.

Commit requires reset held, active transfer, no partial header/chunk, received
equal to total, and a matching CRC. Success closes the transfer, sets readiness
and leaves reset held. Failure preserves state and readiness remains false.
Execution-release must reject an incomplete or aborted stream; only a complete
committed media image may be released. Hold-reset does not silently discard
transfer state. Stop/reprogram clears staging, transfer and readiness.

Abort is idempotent while reset is held and no legacy transfer is active. It
clears stream headers, active chunk/transfer, counters, CRC state and readiness,
leaving reset held. It does not roll back overwritten memory or make a previous
image playable. Abort while execution is released or a legacy transfer is
active is rejected.

## Errors, legacy interlock and ownership

Use existing errors: invalid opcode=1, invalid index=2, invalid argument=3,
invalid state=4. Unsupported indices and duplicate/out-of-order header/data
indices use 2; invalid lengths, offsets, padding, nonzero control arguments and
CRC mismatch use 3; wrong lifecycle phase or incomplete commit uses 4.
Invalid requests do not mutate payload, staging, counters, CRC, readiness or
reset state (the mailbox still acknowledges the error/toggle).

Legacy opcodes 4..6 retain their original 1..16384-byte semantics. Reject them
with invalid-state during stream header staging or an active stream transfer;
reject stream Begin during a legacy transfer. Shared storage cannot be written
by both paths concurrently. A complete legacy transfer remains usable through
its original lifecycle; stream support must not break standalone legacy loads.

One owned GP/session serializes the entire transaction; high-level generation
is validated before hardware access. No extra wire transaction token is defined.
Do not retry/replay an ambiguous mutation. Quiesce/reset and establish mailbox
synchronization before another transfer; generation alone cannot distinguish a
stale ACK. On timeout or cleanup failure retain truthful ownership and keep reset
held; never release incomplete media.

## CRC32

CRC-32/IEEE is reflected, init `0xffffffff`, reflected polynomial
`0xedb88320`, final XOR `0xffffffff`. Process only payload bytes in order,
least significant bit first. Carry CRC across chunks; no padding or header bytes.
ASCII `123456789` gives `0xcbf43926`; bytes `01 02 03` give `0x55bc801d`.
Expected CRC low/high words for the latter are `0x801d`, `0x55bc`.
CRC is transfer integrity, not a replacement for immutable media SHA-256 identity.

## Fixtures and evidence

[testdata/fes-media-stream-v1/exchanges.json](../testdata/fes-media-stream-v1/exchanges.json)
contains synthetic compact exchanges, error/state cases, CRC vectors and size
boundaries. The original legacy exchange file is unchanged. Shared Go tests
validate constants, wire encoding, a test-only state interpretation and CRCs;
runtime and RTL consumers must exercise these vectors against their own
implementations. Required consumer tests include 32768-byte success, advertised
limit and transport overflow rejection, all 256 word ordinals of a full chunk,
interruption/Abort/Stop/relaunch and exact-artifact physical SMS execution.
