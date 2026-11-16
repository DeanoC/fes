# DE25 secure source-context diagnostic, 2026-11-01

The top TRWU8CBL passed one bounded native source-context read on the sealed
HPS-first FPGA diagnostic. This is a hardware diagnostic, not runtime core-load
acceptance or measured/signed boot. Tested source: `9743c0819eb34e3ebcce81f286eb2210a37be22b` (tree
`efd56affec12b9cba8a2cd18460cf776b78c2f5a`).
The original factory card stayed outside the board; the spare diagnostic card
and continuing claim remain in use under the user's retained-diagnostic policy.

## Observed result

One qualified recovery boot downloaded the 5,214,820-byte native FIT,
verified its RAM hash, overwrote only `FES/UPLOAD.BIN` and independently read it
back from SD. The protected boot files and recovery manifest stayed unchanged.
A subsequent matched whole-SOF volatile JTAG boot verified that FIT before
parsing, initialized 1 GiB DDR and reached the matched manual loader. One
`fesboot` entered EL2 cleanup and EL1 native Go. Both owned bridge observations
retained the same SPL owner, generation 1 and flags `0xdf`.

The native reader bracketed the two secure context halves with identical owned
Snapshots and printed the exact compiled context digest, package and build IDs.
The composed FPGA core payload stayed byte-identical to the sealed package.
No bridge opening, CPU FPGA MMIO, runtime SDM configuration, native SD access,
core activation or secondary CPU release occurred. Both JTAG programming phases
used the exact top cable at 6 MHz; the original 15 MHz clock was restored and
read back. USB permissions were restored, and the UART was closed.

| Identity | Value |
| --- | --- |
| Native boot | `d0584135f55f2ad9` |
| HPS source context | `94da3557f175eae05144d70937c3afceda20c6c9f9ca9d25155535da3e0814fc` |
| Package | `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63` |
| FPGA build | `e25bb65a1d14a2bbbddf4c770ea033e5` |
| Composed SOF | `616ee321fbef264be244f3725ad79905707b237a8968b64241ed1d4f4db44910` |
| Native FIT | `b1f0cfe34c1aba102e7d2bf7589b6862a7df0cf14eba4581ec69b3f71e573eda` |
| Native phase one | `6217a4c1c2e537261c5d9518adde57a8896e34e52baa4e2e65ff1cab6fce3f86` |
| Unchanged core | `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c` |
| HPS I/O | `df9589f2122209af6d453bee8ba0dd18f3192fa556b0b6e245cd3cb1a8fe4b1f` |

## Software and composition checks

All 14 planned software phase checks completed on the fixed validation source
above, including parent, shared-contract, firmware/native, RTL and generated
consumers. The repaired outer run reached its 900-second cap during the final
FPGA test phase. The preceding 13 phases had returned zero: the final planned
child was observed under the unchanged sequential fail-stop executor, with its
argv, working directory and source head recorded. The unfinished final phase
then passed independently under its own bound. The raw capped run remains
nonpassing evidence; the phase-completion packet retains the separate basis
for each completed check. The final FPGA suite passed with 1,576 cases and 75
skips in 404.7 seconds; the parent phase passed 1,237 tests.

The native SDK/entry/SPL/BL31 artifacts were built on `6e8102da0`.
Only `scripts/test_policy.py` and its focused test changed afterwards; all 253
selected embedded source digests remain identical, and preflight verifies that
exact two-file difference. Native build and validation revisions are recorded
separately; the existing binaries are not described as rebuilt on the policy
commit.

An initial broader run, planned at `4f8743b96`, reached its 900-second outer cap
while running unrelated deployed video/media regressions; it is retained as
nonpassing evidence, with no hardware attempt. The private bridge channel
emits only embedded C/Go constants. Its test selection now matches those
consumers, while keeping firmware, RTL, native lifecycle and contract checks.
Mixed deployed inputs and neighboring unknown definitions still select their
extended suites. The fixed-head completed phases above supply passing evidence.

The actual sealed package passed simulated CPU/TF-A/RTL and native controller/
tenfoot lifecycle checks. That simulation grants no physical admission; the
existing runtime oracle still reports `unsupported_target`. The actual native
command and encompassing wrapper passed 15 and 21 host-injected success/failure
fixtures respectively. Missing/stale contexts, failed read halves, mismatched
ownership and incomplete/reordered serial traces reject.

The first entry attempt lacked the extracted cross-toolchain library path;
its failure is retained. The first BL31 attempt exposed an import collision
between FPGA and firmware verifier modules. FPGA context verification now runs
in a bounded isolated process, and its imported validator sources are included
in the source seal. Those candidates were never used on the board.

## Evidence and remaining gate

[Native serial](native-console.log), [recovery serial](recovery-console.log),
[upload serial](upload-console.log), [native receipt](native.json),
[upload/readback](upload.json), [outer cleanup receipt](experiment.json),
[independent postcheck](postcheck.json),
[composition](composition.json), [source context](hps-context.json),
[software](software.json) and [sealed-package model](sealed-model.json) retain
these exact boundaries. [File hashes](files.json) cover the copied raw evidence.

The 136,514,882-byte build archive and independent member verification live at
`/home/deano/fes-retained/de25-hps-context-20261101`; its SHA-256 is
`0e8def8491a8ef4eccc56c677b59ecd35bacb15e2ffd78612e5b3b9ee4a5df43`.
The native-source incremental bundle requires `cc47b1211` from the preceding
[retained HPS/native pair](../2026-11-01-de25-native-hps-pair/README.md).
A second test-selection bundle extends native source `6e8102da0` to the
validation revision. Both exact source trees were restored using the archive's shallow
boundaries and verified prerequisite objects. Trial/software logs and helpers
are retained alongside the build archive after the test.

This establishes a secure-firmware/native source-context roundtrip while
bridges are closed. It does not measure the live HPS I/O state or qualify a CPU
FPGA aperture. `HPSContextVerifier` deliberately does not implement
`BootHandoffVerifier`; neither this digest nor a package supplied by a caller
confers programming authority. Next is a separately bounded physical CPU CSR
probe through qualified control-only ownership, followed by actual SDM,
master-stop/drain and DMA/cache ownership qualification before core launch.
