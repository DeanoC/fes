# Application I/O

The additive `fes.application` 1.0 ABI uses identity tag 3 and the existing
`fes-gp-v1` programming profile and transport. The authoritative assignments
are generated from mister-packages `packages/abi/fes_application.yaml`.
Existing `fes.simple-game` and `fes.simple-computer` packages retain their
existing requirements, wire identities and startup behavior.

Every admitted application declares required `fes.video.fixed-720p60` 1.0.
It may additionally declare `fes.gamepad`, `fes.media.blob` and
`fes.media.blob-stream` 1.0. Stream requires blob. Supported operational
declarations must be required; optionality is expressed by omission. Unknown
or unsupported-version optional declarations are ignored. Required unknown
interfaces, keyboard, persistence and audio are rejected before programming.

Identity, ABI and build identity are verified before input or execution
commands. Registered live capability bits must exactly match supported declared
interfaces; an undeclared live media endpoint cannot silently change startup.
Capability bits 0 through 3 are gamepad,
video, blob and stream. Opcodes 1/2/3 are identity/execution/buttons; 4 through
6 use the existing blob codec and 7 through 12 use the existing stream codec.
This map is distinct from the older game ABI's persistence opcodes.

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

Existing `load_media` admits up to 16 KiB; `load_media_stream` retains exact
package/generation binding, observed capacity, bounded snapshots, CRC and
poisoned-mailbox recovery semantics. See [stream media](media-stream.md).
Method names retaining `Computer` are compatibility API names; admission and
verified interfaces determine availability, not a display name or core ID.

Host tests cover admission, tag/capability discovery, no-keyboard startup,
video-only load/Stop/reload without input, media readiness and shared stream
transfer boundaries. No new physical support is claimed. Audio, variable video
timings, keyboard/mouse, multiplayer and persistence layouts are outside this
application ABI slice.
