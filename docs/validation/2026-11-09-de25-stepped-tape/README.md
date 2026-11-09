# Bounded initial-tape steps

The original one-second whole-upload policy rejects legitimate loading on
TRWU8CBL: the current917-byte fixture cannot complete. This changes that
software policy rather than making valid media fit an arbitrary whole-file
limit. The preceding consumed image and its failed trace remain preserved.

The version-2 secure service has one preparation call, one call per512-byte
chunk and one final commit/identity/fetch verification. Every call keeps its
one-second bound and every CSR keeps fresh ownership/CPU/reset/USB checks.
Every step closes its supplier frame; the source/descriptor are revalidated
when the next call resumes. Total elapsed firmware time, including gaps, is
bounded by ceil(bytes/512)+2 seconds, at most34s for16375bytes. The64-poll
ACK bound remains. Callbacks must still return; these cooperative checks
cannot preempt a hardware access or SMC that never returns.

Pending replies carry verified progress and the next ordinal, without a final
GPO/CRC receipt. Repeated/foreign ordinals do no supplier I/O. Cancellation
consumes the next ordinal and invokes the existing retirement/containment
path. The native caller yields between steps and retains uncertain memory.
The core remains held until the final complete receipt passes.

The Go media budget is ceil(bytes/512)+4 platform seconds, including publication
and final observations. Ordinary Go GP exchanges retain an independent
one-second ACK deadline inside that media budget. The native runtime retains
one second for control/fetch progress, excluding the separately bounded tape
upload. Tape-enabled fetch observations report both durations and the
selected byte count; strict serial validators enforce the size-derived bound.
The private service magic, tag and schema major are2; mixed old/new caller and
firmware paths reject. The FPGA GP/media protocol is unchanged.

Focused C tests cover odd/chunk-edge/maximum images, valid transfers exceeding
one second, gaps, total expiry, cancellation, mutation, stale calls and late
clock regression. The Go/actual-C tests use one persistent C process across
firmware calls. Native unit tests accept a valid two-second tape through launch
and return, keeping control time separate. Strong cold/provider tests exercise
ordinary and MMU builds, including cancellation/source mutation after the
first accepted step and the real containment callback. The final Go check ran
without test caching because it compiles external C inputs.

This is software evidence only. All11 required current integration checks
pass on source b8d788c067384b91f919a46bdcc295b25bbeb9a8. Unchanged FPGA-only
unit evidence is reused with exact source/membership/catalog and original
prefix/suffix receipts rechecked. The shared tape definition and generated
consumers were tested fresh. The matched SDK/entry/BL31/SPL/FIT/SOF
build now passes; a fresh bounded board trial remains. No ordinary
core-loading or physical display/input acceptance is claimed. The separate
direct-store MMU observer prototype remains unselected. Old host-data cleanup
stays paused at the user's request.

The remote contract check first hit a copied Python3.13 binary extension on
Python3.12; the same committed contracts pass in the working local environment.
The native build preparation also needed the original ROM input and complete
offline module-cache metadata. Both missing-input failures are retained, and
the unchanged toolchain and restored inputs are used for a fresh output.

The first full boot package on protocol source b8d788c0 exceeded its8MiB
upload/seal bound by2728bytes; it was stopped before sealing/programming.
Source964657270 instead uses the existing frontend build-tag constant at
dispatch and rejects mismatched frontend link flags. Accepted profile choices
are unchanged. The actual Go function table confirms that the unused
`main.servePrograms` diagnostic frontend is removed while
`main.serveProgramTenfoot` remains. The SDK ELF shrinks212992bytes.

The new matched candidate is `1059bb64e5cac38b`: SDK, entry, BL31, sealed
SPL/FIT and paired FPGA composition pass. The FIT is8309400bytes, leaving
79208bytes under the unchanged8388608-byte bound. Current source/file hashes
and matched metadata are checked; FPGA payload bytes remain unchanged. The
private mount build preserves canonical source paths without symlink
admission. The shared Quartus lock was held as deano for composition.

The11 full integration checks apply to protocol source b8d788c0. The only
subsequent code change is the frontend selection repair, checked by its32 flag
classes, focused selection/source tests, actual ARM64 compilation/function
tables and rebuilt matched chain. Previous physical evidence does not qualify
this candidate. It has no prior programming reservation and has not run on a
board. The board test must use fresh single-use operations and independently
verify device cleanup. Ordinary package admission, physical display/input and
full DE10 appliance equivalence remain open.
