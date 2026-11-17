# Bounded vendor cold-startup checkpoints

The latest physical run accepted the final SPL exclusion and early EL3 guard,
then stopped before vendor startup completed. This change adds 15 bounded
checkpoints: entry plus before/after reset handling, CPU-data initialization,
RW cache invalidation, NOBITS cache invalidation, BSS clearing, stack selection
and BL31 setup. It locates a subsequent stop without replacing those operations.
This record covers software and compiled artifacts; the new image has no board
boot evidence yet.

Implementation `7e90d783e38408c7ec7fdcab892cd59e4ca30ccd` generates private
copies of the pinned vendor macros for the selected cold entry. The vendor
header and warm entry stay unchanged. Removing only diagnostics and private
names reproduces both original macro bodies exactly. The existing first-entry
guard remains 1,428 bytes, with no stack accesses or function calls. The 15 new
UART expansions are each 120 bytes, use the canonical UART data/status addresses,
and retain the 384-byte message and 100,000-poll wait bounds. Arguments and
retained registers survive the diagnostics; the reviewed original vendor calls
remain in their original order. Diagnostics make no candidate-RAM accesses.

Twelve actual AArch64 QEMU cases execute the copied vendor macros with injected
platform callees. They cover completion, stops inside each of seven callees,
all-ones/busy UART status, the register contract of all 15 messages, and the
original warm macro with no diagnostics. The harness checks cache/BSS arguments,
stack selection and retained boot arguments. Its platform functions and RAM
layout are models; these cases do not establish Nano OCRAM accessibility or
vendor callee behavior on hardware. Initial harness builds lacked a platform
include and used oversized compare immediates; their failed inputs and logs
remain in the archive alongside the corrected harness.

Seventy-six focused host tests pass, including missing, duplicate, unknown and
out-of-order markers. The explicit selected decoder requires all 15 markers
before accepting U-Boot, with setup-after following both EL3 capture observations.
A partial guard/vendor trace remains useful failure evidence and cannot grant
boot, ownership or a firmware service. The changed-input integration checks and
actual BL31/SPL/U-Boot builds pass; exact result counts and hashes are recorded
in `result.json`. The composed image retains the sealed FPGA payload unchanged.

The native application and compressed entry are reused byte for byte from
`31d716957759c3dede3330877dac2ce7bcd2d643`; all 2,098 selected source hashes
match. The private firmware stack/BSS reservation and native CPU exclusion are
unchanged. The prior physical guard acceptance does not prove writable OCRAM,
vendor initialization, native startup or a completed ROM transfer. The one-second
ROM limit, 16-byte frames, 64-poll limit and existing operator bounds remain intact.

The next step is one fresh source-frozen bounded trial on TRWU8CBL. It must
revalidate the exact claim, adapter, programming link, firmware chain and upload
readbacks, require the new trace before native input, and stop at its first failed
gate. The previously consumed diagnostic image is not replayed. The factory card
and other kit remain untouched; no factory restoration is required by the user.
