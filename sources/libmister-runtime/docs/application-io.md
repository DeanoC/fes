# Application I/O

The additive `fes.application` 1.0 ABI uses identity tag 3 and the existing
`fes-gp-v1` programming profile and transport. The authoritative assignments
are generated from mister-packages `packages/abi/fes_application.yaml`.
Existing `fes.simple-game` and `fes.simple-computer` packages retain their
existing requirements, wire identities and startup behavior.

Every admitted application declares required `fes.video.fixed-720p60` 1.0.
It may additionally declare `fes.audio.pcm-s16-stereo-48k`, `fes.gamepad`,
`fes.gamepad.ports`, `fes.keypad.ports`, `fes.media.blob`,
`fes.media.blob-stream` and optional `fes.firmware.blob` 1.0. Stream requires
blob. Supported operational declarations other than firmware must be required;
optionality for those interfaces is expressed by omission. Firmware may be
declared required or optional so BIOS-free titles can share a firmware-capable
package. Unknown or unsupported-version optional declarations are ignored.
Required unknown interfaces, keyboard and persistence are rejected before
programming.

Identity, ABI and build identity are verified before input or execution
commands. Registered live capability bits must exactly match supported declared
interfaces; an undeclared live media endpoint cannot silently change startup.
Capability bits 0 through 3 are gamepad,
video, blob and stream; bit 4 is stereo PCM audio, bit 5 is controller ports,
bit 6 is keypad ports and bit 7 is firmware blob. Opcodes 1/2/3 are
identity/execution/buttons; 4 through 6 use the existing blob codec, 7 through
12 use the existing stream codec, and 15 through 17 are firmware begin/data/commit
for an exact 8192-byte overlay while reset stays held. This map is distinct from
the older game ABI's persistence opcodes.

Video-only activation configures HDMI and releases reset without opening input
or sending keyboard/button words. A declared gamepad uses the existing single
generation-bound input worker. Any admitted application media declaration
keeps execution reset-held at activation, including legacy-size blob-only
applications. Media commit succeeds before release; failures never release a
partial transfer. Legacy computer startup is unchanged.

Application endpoints must accept button updates independently of media
staging, including while reset is held. Identity and buttons must not change
the staged payload. HOLD neutralizes buttons and preserves media staging and
committed readiness; explicit stream Abort provides cancellation. The GP
transport serializes each request across input and media workers. It does not
hold a lock across a complete media transaction, so no long transfer blocks an
input delivery deadline. This interleaving contract is mandatory for endpoints
advertising both gamepad and media.

Existing `load_media` admits up to 16 KiB and remains the launch-time
hold-reset primary bind. Protocol-2 `replace_live_media` fills the same
blob mailbox on an active `fes.simple-computer` generation without
Quiesce/hold-reset; `clear_media` ejects readiness so the next empty
`LOAD ""` is `0/0`. Both require package/generation binding. Tape-loader
busy maps to retryable `busy`. `load_media_stream` retains exact
package/generation binding, observed capacity, bounded snapshots, CRC and
poisoned-mailbox recovery semantics. See [stream media](media-stream.md).
Method names retaining `Computer` are compatibility API names; admission and
verified interfaces determine availability, not a display name or core ID.

Host tests cover admission, tag/capability discovery, no-keyboard startup,
video-only load/Stop/reload without input, media readiness and shared stream
transfer boundaries. No new physical support is claimed. Variable video
timings, keyboard/mouse and persistence layouts are outside this
application ABI slice.

## Logical controller ports

`fes.gamepad.ports` 1.0 supplies exactly two digital ports, numbered 0 and 1,
with the existing eight gamepad bits. It excludes `fes.gamepad` 1.0.
`fes.keypad.ports` 1.0 requires controller ports and adds twelve bits per port:
bits 0–9 are digits 0–9, bit 10 is star and bit 11 is hash. Each interface is
required when present. GP opcode 13 writes a complete digital mask; opcode 14
writes a complete keypad mask. Both use the port as index and acknowledge zero.

The local protocol-2 operation is:

```json
{"protocol":2,"operation":"set_controller","package_id":"<64 lowercase hex>","expected_generation":1,"port":0,"buttons":0,"keypad":0}
```

Every field is required. Masks are bounded to eight/twelve bits and the entire
request is validated before its first exchange. A zero keypad is permitted for
digital-only ports; a nonzero keypad requires the declared live interface.
The existing lifecycle busy boundary serializes each snapshot against media,
Stop and replacement. Package and generation must match the active session.
The two GP writes are sequential, not atomic, and partial delivery failure uses
the ordinary one-shot input-fault cleanup. No retry or separate input lifecycle
is introduced. These packages do not open the legacy virtual-gamepad evdev
worker. FogCast owns physical source assignment and sends neutral snapshots on
disconnect/release. The runtime neutralizes both ports before Start and after
holding execution during Stop/replacement. Existing one-gamepad packages retain
their worker and wire commands unchanged.

## Shared HDMI audio

The required-when-present audio interface adds signed 16-bit stereo samples at
48 kHz. It uses standard I2S with a one-bit delay and 32-bit slots, continuous
3.072 MHz BCLK and 12.288 MHz MCLK. Execution hold emits zero samples by the
next stereo frame after the hold reaches the audio clock domain, retaining
clocks. It requires no additional GP command, host audio stream or mixer.

After verified identity, the runtime disables audio packets before waking or
configuring the ADV7513. It selects I2S0, external 256*Fs MCLK, automatic CTS,
N=6144, PCM channel status and stereo InfoFrame mapping. Only successful link
verification enables audio packets; the core is still held until the ordinary
Start/media-commit release. Setup/link failures never release execution. Stop
powers down the transmitter, then holds the outgoing core; both steps precede
FPGA replacement.
Silent applications explicitly leave audio packets disabled. Legacy launches
restore packet enables and their existing I2S configuration, so launching a
silent application does not suppress subsequent legacy audio.

Register meanings follow the [ADV7513 Programming Guide Rev B](https://www.analog.com/media/en/technical-documentation/user-guides/ADV7513_Programming_Guide.pdf),
sections 4.4.1–4.4.4. The existing 0x4A bit7 is automatic checksum enable;
audio InfoFrame updates use bit5. Software tests cover capability admission,
identity mismatch, silent/audio/legacy transitions, bounded setup failures and
hold/release. Audible HDMI output remains pending exact-artifact kit capture.
