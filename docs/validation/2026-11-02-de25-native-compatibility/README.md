# Native Go package compatibility and AArch64 admission checks

Tested source: `89bed50551ede17feec935b0e23991f1e0b6209f`, based on
`c7eb502d1092ad9f8a864174346c556b170d44d4`. The main implementation is
`5627a70175e52ea9c33a7dd38f5d4410d948a637`; the tested follow-up repairs the
diagnostic's complete package-identity expectation.

This is host and bare-metal AArch64 QEMU evidence. No Nano board, FPGA program,
SD-card write, Quartus invocation or physical runtime admission occurred.

## Behavior

The native `runtimeadmission.Compatibility` checker validates the caller's exact
retained archive identity, an explicitly supplied target/profile contract, and
the existing C++ runtime's semantics for all four FES ABI families. It accepts
no per-package allow decisions or policy JSON. It copies its platform rows and
rejects an unconfigured checker, diagnostic-only profiles, unknown profile/ABI
pairings, and unsupported target, ABI or interface declarations.

The port preserves optional-interface rules as well as required capabilities:
an invalid optional persistence declaration still rejects. It also checks
persistence transport/layout pairing, reset-held linked-cartridge mailboxes,
firmware-only computer ROM linking, menu/session-display/DDR/shared-plane
dependencies, controller/keypad dependencies, Coleco video sockets, media-unit
and expansion-bus uniqueness, writable tape/floppy dependencies, and Spectrum
disk/tape/DDR pairing. Computer ABI 1.1 remains refused, matching the implemented
C++ driver rather than inferring driver support from newer shared constants.

The checker owns no lifecycle and supplies no physical activation method.
`NewDE10Compatibility` uses the existing generated DE10 registry for parity
checks and rejects Nano targets. A future Nano integration must supply its
qualified platform contract, boot proof, live identity, containment, memory,
input and display services. The existing single lifecycle and guarded native
firmware hooks remain the transition owners.

The native `admission` diagnostic now checks each retained package with Go,
compares its result with the actual C++ oracle, and only then exercises the
existing model lifecycle. Linked cases check compatibility before linking.
The runner validates the new native result field and explicit completion
marker. Its trusted model policy remains separate and still grants no physical
programming permission.

## Paired contract evidence

[Paired verification](compatibility-result-verification.json) records **657**
unique archive cases: 105 simple-game, 128 simple-computer, 184 application and
240 computer cases. All match the C++ oracle under the Go race detector.
Each family includes a verified positive baseline and interface rejections.

| C++ and Go result | Cases |
| --- | ---: |
| Compatible | 245 |
| Unsupported ABI | 24 |
| Unsupported interface | 376 |
| Unsupported/diagnostic programming profile | 8 |
| Unsupported target | 4 |

The cases use shared synthetic format-2/3/4 vectors, including a structurally
valid Coleco 8 KiB BIOS plus 16 KiB cartridge map. They change interface versions
and requirement flags, remove mandatory interfaces, exercise ROM roles, and
combine dependent interfaces. None is a runnable bitstream. The verifier checks
all 504 selected repository source hashes, source stability, archive identities,
the actual oracle executable and every recorded command log. The identical
oracle SHA-256 in this check and the native run is
`99a17326ed30f8109f5c61ce896cfd86dfb64b3dd9fc5e78d45719640b8b2306`.

The affected-check planner schedules this parity test for changes to either
implementation, shared ABI/profile definitions or retained archive code. Its
regression independently checks the Go, C++ and shared-definition input paths.
The existing emitter supplies four additional Go consumers; 43 generated
consumers and 37 fixture copies match. No YAML value, schema, wire constant,
programming-profile declaration or C++ production construction changed.

## Native execution

[Native verification](native-result-verification.json) checks the actual
5,870,312-byte AArch64 ELF, which has no interpreter, its TamaGo compiler, all
1,559 selected source hashes and the current committed checker/diagnostic bytes.
EL1 execution in QEMU completed **17** retained-package decisions and **three**
retained ROM-link cases. Every native compatibility code matches its C++ result.
The model retains its owner on rejection and checks complete package identity,
including target, programming profile and programmed digest, after release.

The native ELF SHA-256 is
`bd0586481fa46cc256be50ff7291c1f71b90d2f040f60d03a69cf04eb7688cdc`.
[Raw native console](native-console.log) and
[native results](native-admission-results.json) preserve the observations.
The FAT32 image has identical pre/post SHA-256
`233ddb9c4f08705131ddf15d4859775a5e1f7b45435398f29b0374886ca831ba`;
the ROM-source mutation checks restored their bytes. QEMU was explicitly reaped.
This is Linux-free native execution of contract checks, not Nano hardware proof.

## Selected checks and retained failures

All **12** [selected software groups](software-result.json) passed on the tested
source. The 1,308-ID parent selection used three upfront intact-module partitions,
each capped at 300 seconds, with a 900-second outer cap. Host tests, the C++
runtime suite, shared schema/emitter tests, native firmware/FPGA integration and
the paired checker passed. FPGA software ran 1,577 tests with 75 skips within
its unchanged 840-second cap. The [coverage summary](coverage-summary.json)
records parent skip reasons and exact scope; omitted coverage is not qualified.
All jobs had disk guards, heartbeats and finite bounds.

The first paired run stopped because its TOML writer omitted the required empty
`interfaces = []` declaration when testing missing application video. That
fixture-writing failure is retained; the corrected corpus passes. The initial
`make check` rejected the intentionally uncommitted shared-module README; the
committed-source consistency check passes. A host preflight then exposed the
older diagnostic's partial package-identity expectation. It now compares the
complete binding, and both host and native admission tests pass. Original logs
remain retained; no failure was waived or turned into a pass.

## Retention and next integration

[Source retention](source-retention.json) records a complete source bundle with
no prerequisites, independently restored without shared objects and checked by
full Git integrity verification. Source, raw checks, paired cases, compiler
receipts, native ELF/FAT32 image and retained failures are archived under
`/home/deano/fes-retained/de25-native-compatibility-20261102/` and remain available
under `out/nano-native-admission-20261102/`.

Next is the qualified Nano platform/boot contract and normal runtime activation
of compatible retained packages through the real hooks. The previously verified
two same-package hardware loads remain separate evidence on their original
producer; this native compatibility step does not expand that hardware claim.
Arbitrary Nano package switching, Tenfoot physical launch/return, native
USB/video/network acceptance and active DDR remain open. The full goal stays
active. No PR push, handoff or merge occurred; ownership remains `lane:deano`.
Every future 3090 Quartus job must hold the existing shared exclusive
`/tmp/lander-quartus-3090.lock` for the entire job.
