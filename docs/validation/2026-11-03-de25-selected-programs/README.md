# Native selected programs with a fixed boot anchor

The native loader can now select eligible packages from SD without compiling
each selected package into the boot image. Implementation and matched native
build: `068495f0cf1d6011c31447a63bde4b70e7056702`, based on
`00a8245ee`. This is a software and cross-build checkpoint. No board operation,
SD write, FPGA compilation or 3090 job occurs in this step. Ordinary Nano
admission and native Tenfoot hardware launch remain open.

## Selected program and immutable platform

`ProgramLoader` is constructed from a board-compiled source record tied to the
original `HPSContextVerifier` digest. The [platform record](program-platform.json)
contains device, HPS I/O/PFG identity, full HPS/EMIF parameter fingerprint,
bootloader handoff, master inventory and the current ABI/profile association.
The exporter rechecks every HPS/EMIF parameter against the fixed reviewed
recipe, including the disabled coherent port and exact generated defaults.
The existing complete master-inventory and sealed-project checks still apply.

The SD selection record names package and metadata paths plus their expected
identities/digests. `Store.Retain` closes both files and owns private archive
and metadata bytes. `ProgramLoader.Preflight` checks the real archive's
package/build/target/ABI, required interfaces and declared board compatibility
before SD containment. Rejection leaves healthy storage active, with no
firmware call or reservation. Digests are derived from the actual retained
payload. Addresses, bridge geometry, stop policy and computer capabilities
remain compiled platform facts. Uploaded metadata cannot add an HPS master.

A single-use plan binds the selected identity and private maps into publication,
configuration, live computer identity, opening and failure retirement. All
ownership and return checks continue to use the immutable boot anchor. The
selected program cannot replace it, replay a completion or open through a
copied loader. This is integrity and compatibility assertion checking, not
package authentication, measured boot or a normal programming-profile grant.
The current compiled ABI/profile still describes the contained diagnostic shell.

## Native frontend and software evidence

The opt-in `--selected-programs --computer-control` mode reads
`FES/CORESEL.JSN`. A two-load test also needs `--computer-reload
--computer-storage-return` and two explicit choices. Each request supplies
`path`, `package_id`, `metadata_path` and `metadata_sha256`. The builder exports
`PROGRAM.JSN` and `CORESEL.JSN` examples for its sealed package. The example
contains that package twice; different packages are exercised in software.
The selection list is external to the boot image, with no compiled package list.

All [eight affected scopes](software-summary.json), 1,342 exact selected parent
tests and generated-consumer consistency checks pass. Native SD lease/PIO,
firmware/RTL, model launch/return and model Tenfoot integration pass. The new
joined lifecycle test executes three different canonical synthetic packages,
core IDs, build IDs and payload sizes (128/192/64 bytes), checking the actual
published request bytes while boot identity remains unchanged. These are not
physical FPGA packages. Failure cases cover source loss, publication, write,
completion ordering, opening, wrong live identity, Hold and closure; uncertain
ownership stays retained and cannot replay. Archive ABI/profile/interface
mismatches, incompatible or incomplete metadata, bounded selection-file failures
and private metadata retention also pass.

A separate explicit test consumes the actual sealed producer export and the
1,649,664-byte retained FPGA package. It passes native preflight without a
firmware call. The [linked SDK audit](selected-sdk-proof.json) reads the actual
Go string variables from ELF symbols: selected mode, reload and storage return
are enabled and the compiled platform bytes match. The choice and metadata
files are absent from the ELF. The [source audit](selected-inputs.json) checks
118 selected Git blobs and 1,298 pinned module/SDK inputs.

## Matched artifacts and remaining physical work

SDK, entry, BL31, U-Boot and local PFG composition pass. The linked vendor-startup,
relocation, contained-console, SPL/BL31 seal, CPU mapping and per-load provider
checks pass. PFG recomposes the existing sealed FPGA source
`c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`; its phase-two payload is unchanged.
The [build summary](software-and-build-summary.json) records exact artifact
sizes and hashes. Candidate native boot ID: `145213088b9b4dbb`.

The candidate is under `out/nano-selected-program-20261103/`; source, candidate
and software evidence are retained under
`/home/deano/fes-retained/de25-selected-program-20261103/` with a verified frozen
input dependency. It has not been loaded on TRWU8CBL. The last independently
verified board state remains the
[per-load firmware test](../2026-11-03-de25-program-binding/README.md) on source
`d9ddb6ce4`, boot `e3a3b7d0063c37ce`, parked after return generation 11 with SD
contained and data held. Its spare and exclusive claim are retained, and the
factory card is safe. That evidence applies to the earlier image.

The sealed FPGA package contains the ZX81 machine and ROM. The preceding board
observation establishes fetch activity and controlled return, not usable ZX81
input/video acceptance. Next, extend bounded network delivery/readback to the
selection and metadata files, test this selected frontend on its matched image,
and qualify different eligible physical packages. Native runtime/Tenfoot
binding, normal-profile admission, ROM linking and the remaining memory,
USB/input, video and native-network qualification still need work. The full
DE10-equivalent goal stays active. Ownership remains `lane:deano`; no PR handoff
or merge is made.
