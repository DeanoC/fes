# DE25 Quartus and AArch64 composition proof, 2026-10-29

The diagnostic builds a real Quartus PR base and two alternative personas for
`A5EB013BB23BE4SCS`, then assembles their opaque bytes into deterministic
deployment bundles using native and emulated AArch64 executables. The output is
base plus one persona. No monolithic RBF linker, programming backend or
production admission is implemented.

Base: `d1bca43f02ef935d2d44e88ad745378f98407140`.
Compiled producer: `4048ee2727239b0bb93364dc62ae538440e45163`.
Committed standalone verifier: `18be76d2603f374e655a07f273b8a3ce7d0b4e8b`.
All target source hashes and the proof-driver hash in [proof.json](proof.json)
match that verifier commit. Later documentation does not relabel the compiled
producer. The [operator guide](../../../boards/de25nano/link-proof/README.md)
describes the maintained recipe.

## Actual compiler and memory evidence

Quartus Pro 26.3.0 Build 114 SC Pro Edition compiled the exact SCS device,
seed 1, with two CPU workers. Base completed with zero errors/four warnings;
both personas completed with zero errors/five warnings. Both implementation
revisions import the same final root QDB. Its SHA-256 remains
`e42d7fd2e8763798e261d60f24d65b6b4b138a7d1cae81910a02c590e45b3b41`.

The [build receipt](build.json) binds tools, source closure, generated inputs,
reports, timing and artifacts. [inputs.json](inputs.json) preserves the original
producer identity. Vendor-decoded POF0 region `0x01000000` and hash
`2a56e609a04548caa70944b6bd009959d07510ad054368496597da868fc9fb19`
match across base/A/B; all three integrity checks pass. `pfg --compare` reports
absent unsigned owner-key fields, so its success is not additional pairing
evidence. The descriptor labels the actual check `pfg-info-pof0-match`.

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| Base RBF | 991232 | `f11c06ce9d1d84bb049ef4261e1609ee81d40f571ba8e3bcf0d70671894172bc` |
| Persona A | 503808 | `7b69ef054b7235aeb2d9871ff6e0e9ae997f293d170b9416babba4e3fe034850` |
| Persona B | 503808 | `a327d1a53c073d7b502d8e0f7c17757830796662bc9a18f1c656d7e264b11641` |

The [independent audit](independent-audit.json) checks all 256 emitted MIF words
against each captured ROM source. All match, with one M20K at
`M20K_X24_Y8_N0`. Every revision retains placement `(2,7)-(26,12)` and routing
`(1,6)-(27,13)`, plus the same constrained 20 ns board clock and 10 ns vendor
internal clock. A/B Assembler reports explicitly verify persona mask against
`base.slot0.pmsf`, static mask against `base.static.msf` and root logic against
`base.sof`. Root preservation is 100% excluding PR sub-blocks, 42.56% including
them. The implementation reports do not reprint the base static-mask hash.

| Revision | Setup | Hold | Recovery | Removal | Pulse width |
| --- | ---: | ---: | ---: | ---: | ---: |
| Base | 18.005 | 0.102 | 19.043 | 0.279 | 9.483 |
| A | 18.039 | 0.102 | 19.043 | 0.279 | 9.483 |
| B | 18.039 | 0.102 | 19.043 | 0.279 | 9.483 |

All values are worst reported slack in ns; all 15 rows have zero TNS. Separate
completed-database queries check 312 reset/freeze interstage and fanout paths
across four published corners, with nonnegative setup and hold slack. Reports,
bitstreams and root QDB remain unchanged after these queries.

The first frozen orchestration stopped after A because Quartus reordered its
QPF revisions. B continued with the same explicit `-c`, checked project,
generated inputs, tools and initial root hash. Standalone verification fixes
only report labels/coordinates and metadata/provenance guards; no RTL, QSF,
SDC, IP, seed or timing constraint changed. The retained pre-persona root record
was normalized with [explicit provenance](root-qdb-provenance.json).

## CPU execution and rejection evidence

[proof.json](proof.json) records Go 1.26.5, static executable hashes and QEMU
7.2.22 with Cortex-A76. Native and AArch64 each compose both selections twice:
all four outputs for a selection are byte-identical. Each bundle is 1,502,208
bytes and contains exact original vendor payload bytes and the ordered plan.

| Selection | Bundle SHA-256 |
| --- | --- |
| A | `a88ab0bdda96cfb2a9ec173ae634fad837f2e158ad4cc02b0bd9278a0a97ffe9` |
| B | `34f5eb7b777647c0db0a76e90e8b59cc8588e2995ce32ed4f60bcd66ace681a2` |

Twelve controls reject wrong shell/base pins, a foreign root binding, a changed
descriptor, corrupt persona bytes and overlapping selections on both CPU
architectures. Rejections publish no output. The foreign-root case has a
complete payload inventory and its mutated descriptor pin; the replacement
case is otherwise valid and retains the original external descriptor pin.

The [RTL simulation](simulation.json) exercises real shell/persona modules,
48 exact-latency ROM/counter/ID responses and A/B/A/B stop/drain/reset/clamp
transitions with persistent static state. Its mux and behavioral memory model
do not simulate physical PR or M20K primitives. Ten final focused Python/RTL
tests pass. Native Go tests/vet and nine Go test groups under AArch64 QEMU pass;
[target-checks.json](target-checks.json) explicitly identifies synthetic unit
controls, separately from the real-artifact end-to-end proof. `make deadcode`
and generated consistency checks pass (33 consumers, 37 fixtures).

[Software CI at `995a51d0f8`](https://github.com/DeanoC/fes/actions/runs/40855650705)
passes every selected job. The broader local aggregate completes with 1,092
parent tests/two skips passing, followed by 1,480 FPGA software tests with two
failures, one error and 35 skips. [local-checks.json](local-checks.json) retains
the tracebacks and exact limits; no local aggregate success is claimed.
Two existing OSS-purity assertions scan absolute paths and reject `quartus`
in this worktree's directory name. A [paired unchanged-base bootstrap control](path-control.json)
passes under a plain directory and fails under the keyword-bearing directory.
The existing HPS simulation wrapper times out after 600 seconds covering both
builds and runs. Its [27 consumed sources and 10 derived files](hps-source-control.json)
are unchanged from base; the timeout cause remains unproven without a baseline
execution. No surviving HPS simulation child was found. Existing tests and
timeout limits are unchanged. Final verifier guards pass separately at `18be`;
software CI binds the complete published code to `995a`.

## Remaining gates and retained outputs

Overall Design Assistant closure remains FAIL, with zero waivers:
TMC-20012 names eight LED outputs; BBD-60000 names `slot_inst|req`,
`slot_inst|reset`, `slot_inst|rsp_valid`. The logical protocol is registered;
direct physical registration of every partition port is unqualified. Early
imported-root checks also report RES-10204 while the root is opaque. The
completed database retains its reset chain, but this does not grant all-phase
methodology closure. Both synchronizer chains are excluded from MTBF analysis;
MTBF is not calculated. No clock or timing requirement was relaxed.

Evidence class is compiler diagnostic plus host/QEMU software execution. The
runtime PR protocol controller, including freeze, drain, reset, recovery and
identity checks, was exercised against the RTL in host and QEMU runs. Physical
AArch64 execution, SDM loading, electrical behavior and exact-artifact board
acceptance remain untested. No kit, image, boot configuration, flash or harness
was touched. The next integration step is to connect the controller to the
physical MMIO/SDM path and run base/A/B load-and-switch acceptance on an
identified SCS board.

The task worktree retains the compiler project and raw reports at
`out/link-proof-final-4048ee2727/`, binaries/bundles/logs at
`out/link-proof-aarch64-18be/`, and failed probes/metadata-guard attempts under
their original output names. [artifacts.json](artifacts.json) is the exact
canonical target descriptor; the evidence copies do not relocate its payloads.
