# Experimental initial tape handoff

[`fes.fabric.embedded.tape`](../packages/fabric/fes_fabric_embedded_tape.yaml)
defines the private channel between native publication and a secure initial-tape
service. It generates C and Go constants. Call `0xc2000304` is an unused private
experiment number, not a vendor allocation. The current Nano SiP dispatcher,
strong platform supplier and board runtime do not select it.

The trusted platform supplies the fixed 8 MiB arena at `0x94800000`, with the
descriptor in its final page at `0x94fff000`. The payload begins at the arena
base and contains 1–16375 bytes. A completed configuration lease is required
before publication; a tape lease cannot be overwritten until successful
completion. A subsequent tape requires a new completed configuration and a
newer generation under the same boot owner. Uncertain outcomes retain memory
and retire the arena. These software rules supply no physical reservation.

The 128-byte little-endian descriptor is:

| Offset | Bytes | Meaning |
| --- | --- | --- |
| 0 | 4 | Magic `0x31505446` |
| 4 | 4 | Version 1 |
| 8 | 4 | Header size 128 |
| 12 | 4 | Payload byte count |
| 16 | 8 | Boot owner |
| 24 | 8 | Active bridge generation |
| 32 | 4 | Acknowledged initial Hold GPO |
| 36 | 4 | Payload IEEE CRC-32 |
| 40 | 16 | Build ID in manifest byte order |
| 56 | 48 | Payload SHA-384 |
| 104 | 24 | Zero, reserved |

The request registers are `[fid, Load, owner, cookie, generation, 0, 0]`, with
cookie `0x46455354`. They carry no address, timeout or selected identity.
Decoding FID/cookie under the secure lifecycle lock remains the dispatcher's
obligation. The service reads the fixed header twice under fresh ownership
checks, compares it to private accepted program identity and verifies the
fixed source digest before calling the real held-transfer engine.

Reply registers contain status, `(tag << 32) | bytes`, generation and
`(crc << 32) | finalGPO`, with tag `0x46545031`. Bytes, generation and GPO/CRC
are published only on complete success; failure keeps only the tag. No failed
or successful attempt can be replayed. Wrong operation/owner/generation does
not access the supplier or consume a fresh eligible service object.

The original one-second budget covers header capture, digest verification,
transfer and final ownership proof. The engine receives both the service's
start time and its last observed timer sample, preserving monotonic checks
across that boundary. A request cannot supply either timer value.

Portable tests join the actual Go arena/descriptor codec to the actual C
service and engine with an injected endpoint, ownership and digest supplier.
Separate captured-RTL tests cover the engine and native launch/return protocol.
Neither establishes the missing strong supplier, physical timing or board
acceptance.
