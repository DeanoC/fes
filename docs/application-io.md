# Composable application I/O 1.0

`fes.application` ABI 1.0, live tag 3, is approved on `fes-gp-v1`.
[The YAML](../packages/abi/fes_application.yaml) owns constants, emitted as
`FesApplication*` and `FES_APPLICATION_*` Verilog macros.
This additive ABI leaves simple-game tag 1, simple-computer tag 2, package
format 2 and their existing packages unchanged. These definitions and synthetic
fixtures do not establish consumer or hardware acceptance.

## Admission

| Interface 1.0 | Capability bit | Meaning |
| --- | --- | --- |
| fes.gamepad | 0 | One normalized eight-button controller |
| fes.video.fixed-720p60 | 1 | Existing fixed 720p60 output contract |
| fes.media.blob | 2 | Sequential bounded media upload |
| fes.media.blob-stream | 3 | Chunked CRC32 upload; requires blob |

V1 runtime admission requires video. Gamepad and media are independently
composable: a video-only autonomous demo is valid, as is video with gamepad
and media. Every implemented registered interface must be declared by the
manifest and advertised in live identity; live capabilities must agree with
the declared registered set. Unknown required interfaces and unsupported
versions fail admission; an unknown optional interface does not grant support.
For this initial contract every recognized, supported operational interface
must be declared required=true; composition is by omission. Recognized supported
optional declarations are rejected, while unknown or unsupported optional
declarations are ignored. Stream requires a required blob declaration. Library
launch with blob requires selected media; development may load held and upload
later. No keyboard, audio, multiple-player,
analog input, persistence or save-layout contract is defined here. A familiar
package name is not evidence of any of these capabilities.

## Mailbox framing and discovery

GPO bit 31 is a request toggle; bits 30:24 opcode, 23:16 index, 15:0 argument.
First write fields with the old toggle, then write the same fields with the
toggle inverted. Wait for GPI ACK bit 23 to equal the new toggle before issuing
another request. GPI bits 31:24 are signature 0xf5, bit 22 is error, bits 21:16
are zero, and bits 15:0 are response. This is the existing FES GP transport;
it does not introduce MMIO addresses or programming recipes.

Discovery starts with Identity (opcode 1), index zero, argument zero. Read
all 16 identity words: 0/1 magic 0x4546/0x3153; 2/3 transport 1/0; 4 ABI tag 3;
5/6 ABI 1/0; 7 capabilities; 8..15 build ID. Each build ID word contains the
next two bytes with the first byte low. Compare identity against the exact
manifest before release or media/input delivery. Stop/reprogram establishes
a fresh identity and clears application state.

## Execution and gamepad

Execution opcode 2 uses index zero: argument zero holds execution reset and
neutralizes buttons; argument one releases execution. Programming starts held,
buttons neutral and media not ready. Hold preserves transfer staging and
readiness; it is not an implicit abort. Release requires no partial transfer
and, if blob is advertised, a successfully committed image. A video-only demo
can release immediately after identity validation. Successful mutations return
zero. Runtime owns programming, containment, timeout recovery and Stop to idle.

Buttons opcode 3 requires gamepad capability, index zero, and an unsigned
eight-bit mask: Up/Down/Left/Right/A/B/Select/Start are bits 0..7. Both press and
release send the full current mask. Writes are valid while held or released.
Higher bits are invalid arguments. These bits have no core-specific keyboard
translation. Missing gamepad means no neutral input transaction is needed.

## Media

Blob opcodes 4/5/6 require blob capability and execution held. Begin uses
index zero and byte length 1..16384, opens a transfer and clears readiness;
it rejects another open transfer. Data index zero supplies a low-byte-first
pair; index one supplies the last odd byte with zero high byte. No data may
overrun the declared length. Commit uses index/argument zero and requires
exactly the declared byte count; success closes staging, sets readiness and
leaves reset held. Invalid requests do not modify state. A replacement Begin
invalidates readiness; failed replacement never makes the old image runnable.
The endpoint owns media interpretation; this byte transport promises no ROM
format, mapper, asset layout or persistence.

Stream opcodes 7..12 and all sequencing, limits, interlocks, CRC32, Abort,
error behavior and timeout rules are exactly the generic contract in
[media-stream.md](media-stream.md), using the FesApplication constant prefix.
Its SMS fixed-map tail subsection is specific to SMS and is not required of
generic applications. Stream is not inferred from blob or payload size.

Errors are invalid opcode=1, index=2, argument=3, state=4. Opcodes whose
capability is absent return invalid opcode. All invalid requests acknowledge
the toggle without changing application state. Lifecycle failures must not
release execution; ambiguous mutating requests must not be retried blindly.

## Evidence and ownership

[Golden exchanges](../testdata/fes-application-v1/exchanges.json) describe two
independent synthetic sessions: video-only and gamepad+video+blob. They are
never-deployed wire examples, not RBFs. Tests check framing, identities, errors
and lifecycle. Existing generic emitters generate C++14, Go and Verilog.
Consumers own their implementations and focused tests; FES selects reviewed
component revisions before integration or exact-artifact hardware acceptance.
