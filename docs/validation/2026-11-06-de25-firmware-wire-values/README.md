# Early EL3 wire values bound to the real cold producer

The first guarded-OCRAM board trial stopped after SPL's final ATF descriptor,
with no vendor secure startup or native prompt. Source investigation found a
guaranteed incompatibility: the C producer encodes `FES_NANO_FABRIC_ABSENT` as
one, while the assembly and its hand-packed QEMU control both required zero.
The physical trace does not identify the first failing instruction; SPL's
final RAM exclusion and the EL3 guard both follow the last emitted descriptor.
The stopped image is retained and must not be replayed.

Implementation `7477f6dc2` changes the guard to require the actual absent value.
All thirteen assembly wire constants now have C static assertions in the
selected firmware translation unit: magic/version/sizes, flags, both cookies,
history, fabric, producer inventory and the reset mask. The named constants
supply the assembly comparisons. Deliberate drift of every constant fails C
compilation. No schema, public service, ownership grant, memory range, ROM
frame, deadline or poll bound changes.

A shared fixture now executes the actual `fes_nano_cold_begin`,
`fes_nano_cold_finish` and checksum implementation with injected register
callbacks. It emits the completed C struct as bytes. Host tests check both
supported core identities and the wire layout. These are host C functions;
the following ARM guard executes separately in QEMU, with Nano register reads
modeled in RAM.

The original guard rejects that genuine producer record in an explicit ARM
regression. The corrected guard accepts it. Another 254 fresh QEMU boots cover
a valid producer control and all 224 individual record-byte corruptions, plus
a second valid control and 28 binding/live-state/replay/layout cases. The added
cases reject CRC-correct fabric zero, fabric present and a real producer record
for the other core. These results supersede the earlier hand-packed controls
for producer compatibility. They do not establish hardware memory ownership
or boot acceptance.

All ten required changed-input integration steps passed on implementation
`7477f6dc2`. This includes 1,507 parent cases with two environment skips,
1,581 producer cases with 75 tool/fixture skips, generated-consumer consistency
and the native firmware/FPGA integrations. The focused selection, SPL and cold
producer tests passed. The first local generated-header compile failure is
retained; it caught a missing string separator before the firmware build.

The real BL31 cross-build passed all thirteen C static assertions. Its ELF and
flat binary agree. The actual 852-byte early guard compares the fabric field
with one, retains canonical reset/power addresses and a permanent rejection
loop, and has no stack frame or calls. Its only early store remains the
initialized DDR latch. Vendor initialization follows the guard. The default
DDR-only ELF gate still rejects this selected OCRAM image.

The matched SPL/U-Boot build passed, with a sealed 8,097,792-byte FIT under the
unchanged 8 MiB limit. Actual final-branch disassembly confirms the live SPL
exclusion and both full-width cookies, with no success-path stack writes or
calls before the branch. File-only Quartus composition ran under the shared
lock and independently verified the unchanged 1,658,880-byte sealed FPGA core
payload. The rebuilt cold chain is
`8c6e45d693f69417b356289e06cc4c224e97ed2879825964ead33270bd4adc58`.

The [receipt](result.json) binds exact source, test, ELF, branch, allocation and
composition evidence under `out/nano-firmware-wire-fix-20261106`. The native
application/entry remain the byte-exact retained
`31d716957759c3dede3330877dac2ce7bcd2d643` artifacts; their source hashes and
retained inventory were checked, and they were not rebuilt or attributed to
this firmware commit.

No board operation followed the preceding physical stop. This corrected image
has zero hardware attempts. Prepare a fresh bounded procedure for this exact
chain, with new live kit admission and the existing deadlines; do not replay
the stopped input. The earlier claim/spare card remain retained, the factory
card and other kit are untouched, and CPU/FPGA halt is not independently proved.
Native boot, ROM completion/Run/return, ordinary package admission and full
DE10-equivalent core loading remain unverified for this image.
