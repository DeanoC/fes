# DE25 HPS-controlled PR diagnostic, 2026-10-29

The diagnostic adds one HPS-controlled partial-reconfiguration slot and a native
runtime protocol consumer. It stops requests, drains replies, holds the slot in
reset and confirms FROZEN before calling the selected persona-configuration
operation. Resume validates the selected base and probes the persona identity
before reporting success. The format-2 composer carries the base control ID and
persona hardware ID with the opaque base/persona bytes. This is diagnostic
software and RTL evidence; no board was available or programmed.

Base: `f5542eaba6989b5612adbbefc6290eb225910d49`.
RTL and co-simulation source: `9663da282aade3e61de1702455636c3e3ec058c5`.
Selected 128-bit control build ID: `6aede5b08dad99a7139ed4f03b063d20`.
The [operator guide](../../../boards/de25nano/link-proof/README.md) describes the
maintained build and simulation commands. The earlier FPGA-only 50 MHz proof
retains its original source and artifact identity; this record independently
validates the changed HPS-controlled base.

## Compiler, packaging and actual-artifact composition

Quartus Pro 26.3.0 Build 114 SC Pro Edition compiled the exact
`A5EB013BB23BE4SCS`, seed 1 and two workers. Base completed with zero errors and
309 warnings; A and B each completed with zero errors and 73 warnings.
The compiled producer is `9663da282aade3e61de1702455636c3e3ec058c5`.
Packaging and the completed-database verifier are separately committed at
`1ec76837bb1cdf0f78b1dc0d5b20898cf94b1775`; they preserve the compiled inputs.
The [build receipt](build.json), [original inputs](inputs.json),
[independent final audit](independent-final-audit.json) and
[independent cross-check](cross-check.json) retain those identities.
All 70 original compiled artifacts/reports remain unchanged across packaging
and verification. Both personas import the final root QDB, whose SHA-256 remains
`37d7e071596a0a337bede8217df4b9cf9344a9ec3c52635de70cf2eef814ec53`.

The base uses Terasic's captured `sof_with_hps.bat` sequence: enrich the compiled
SOF with `software/u-boot/spl/u-boot-spl-dtb.hex`, then convert that enriched SOF
to RBF. The 721,613-byte SPL SHA-256 is
`096f53ea0bb0ba6e7449b2acf012eae734840dbcdd2943b506567bad2f72f823`.
The original SOF is explicitly recorded as incomplete before this step; its
inspection is metadata-only. Final enriched SOF/RBF checks reject PFG errors
even when the process exits zero. Base and both personas pass integrity checks
and retain POF0 region `0x01000000`, hash
`a6fd335d94952434da577527fa1d87f27ef0fd9a0b152423bd33ffa4bac5ac54`.
Unsigned `pfg --compare` owner-key output is retained but is not additional
pairing proof.

| Artifact | Bytes | SHA-256 |
| --- | ---: | --- |
| Base RBF | 1986560 | `9440adb021226e6df6334bd15e433d8879dfcc7e3642a947d79671cc3c59d0c6` |
| Persona A | 503808 | `894ebc43c3e613204a4179081d52146e74a1d1741ba1f1e132fe009f6c2cdec5` |
| Persona B | 503808 | `8183eedb6356824d0bed6020994d5bf8f4dbe467577dcf24b2d0a3808b4a6c7a` |

All 35 clocks are constrained. Each revision has 23 nonnegative reported timing
rows with zero TNS. Worst slack across all reported clocks, in ns:

| Revision | Setup | Hold | Recovery | Removal | Pulse width |
| --- | ---: | ---: | ---: | ---: | ---: |
| base | 3.489 | 0.000 | 7.676 | 0.158 | 0.220 |
| persona_a | 3.489 | 0.000 | 7.676 | 0.158 | 0.220 |
| persona_b | 3.489 | 0.000 | 7.676 | 0.158 | 0.220 |

Hold is rounded to 0.000 ns; no positive hold margin is inferred. Separate
completed-database queries retain exact physical endpoint coverage for setup
and hold at every published corner, including Quartus-created replicas:
[base](timing/base.tsv) covers 45 boundary FFs and 368 rows;
[A](timing/persona_a.tsv) and [B](timing/persona_b.tsv) each cover 43 FFs and
352 rows. Each set includes the reset-release path, spans four corners and has
minimum slack +0.111 ns. Launch/capture clock identity and non-inverted edges
are checked. No timing requirement is relaxed.

BBD-60000/60001/60002, CLK-30032 and TMC-20025 have zero violations and zero
waivers. Overall Design Assistant closure remains FAIL on TMC-20012,
LNT-30023, RDC-50003 and RES-50101. The clock/port coverage counters do not
establish physical I/O timing. MTBF analysis excludes 18 of 42 found chains;
its reported pass covers only the modeled subset.

The independent audit compares all 768 emitted ROM words with the captured
hex sources. Each revision has one 256x32 M20K in the PR slot; base/A use
`M20K_X24_Y11_N0` and B uses `M20K_X24_Y12_N0`. All retain placement
`(2,7)-(26,12)` and routing `(1,6)-(27,13)`. Assembler verifies the persona mask,
static mask and root preservation against the base artifacts. All 23 captured
source files, 16 prepared guards, 587 generated inputs, 497 HPS/EMIF parameters
and four vendor boot files remain intact.

[composition-proof.json](composition-proof.json) records Go 1.26.5 and static
native/AArch64 executables with QEMU 7.2.22 Cortex-A76. Each architecture composes
both actual artifact selections twice. All four outputs for each selection are
byte-identical and contain the exact vendor payload bytes. Each bundle is
2,497,536 bytes. Twelve wrong-pin, changed-descriptor, foreign-root,
corrupt-payload and overlapping-selection controls reject without output.

| Selection | Bundle SHA-256 |
| --- | --- |
| counter_a | `dc6f43b2ad30c8fe1884456dfe3ec22bf7925da49a940080a54f010049533ee1` |
| counter_b | `5869d44d4ff46e2c40e28eafbd79984c85d18dc99998b73f745215d375ab771d` |

The [rejected 803d candidate](rejected/803d3fbd2a.json) completed base
compilation with zero errors and 311 warnings, with 23 nonnegative reported
STA rows. It remained unacceptable: BBD-60000 identified `slot_inst|reset`,
and the inherited manual EMIF reference-clock declaration was ignored because
the generated IP had already assigned the same port. That run was stopped
during persona A fitting; B was not started and no final descriptor or bundle
was issued. Its reports and artifacts retain the 803d identity.

The 9663 source starts the static `slot_reset` boundary register at its native
low power-up level, then asserts reset on the first fabric clock and holds it
through RESET_SETTLE before any request or FROZEN acknowledgement. The register
is preserved and gate push-back is disabled at that boundary. This addresses
the synthesized inversion that prevented direct registration. The final
compiler reports above confirm that the boundary findings are cleared.

The recipe removes only the exact duplicate manual `EMIF_REF_CLOCK` declaration
from the extracted project. It preserves the generated 6.000 ns reference
clock, verifies its identity/period in both constraint and final timing checks,
and rejects an unexpected manual declaration. It does not change the 100 MHz
slot clock, the four-cycle persona latency or vendor HPS/EMIF parameters.

## Runtime against the actual endpoint RTL

[control-proof.json](control-proof.json) binds 52 committed source files, the
native/AArch64 executable hashes, Verilator and compiler hashes, QEMU identity
and the selected build ID. All 14 cases pass: seven scenarios on native and on
AArch64 Cortex-A76 under QEMU 7.2.22.

| Scenario | Result | Writes | Logical persona selections |
| --- | --- | ---: | ---: |
| A/B/A sequence | pass | 14 | 3 |
| Configuration callback failure | pass | 2 | 0 |
| Wrong persona | pass | 4 | 1 |
| Ambiguous write | pass | 2 | 0 |
| Missing response / RTL timeout | pass | 4 | 1 |
| Later reset | pass | 0 | 0 |
| Wrong base build ID | pass | 0 | 0 |

The client uses the actual `De25PrControl`; the native Verilator server uses the
actual CSR controller and both personas. The AArch64 executable is static and
runs through a private Unix socket to that server. Configuration is an explicit
persona mux selected only after the simulated endpoint confirms frozen/reset.
It does not simulate SDM configuration or generated HPS bridge transactions.
The missing-response case triggers the RTL's one-million-cycle timeout. Failed
or ambiguous operations poison the runtime controller and never trigger an
automatic retry or resume. A failed callback does not establish the remaining
hardware configuration state.

## RTL reset, response and fault priority

[rtl/simulation.json](rtl/simulation.json) records the actual shell/persona
simulation with unspecified-register initialization randomized at seeds 17, 29
and 41. Its source hashes match the stated 9663 revision. Each run passes A/B/A/B
transitions and 26 four-cycle ROM/ID replies, with drain/reset, sequencing,
partial write, busy, wrong-persona, timeout and unsolicited-response checks.
The bench also covers cold-start response garbage, reset initially released,
an unsolicited matching-ID reply before a request is outstanding, and faults
that coincide with probe or freeze completion.

Four deliberate regressions have retained assertion logs:

| Regression | Expected rejection |
| --- | --- |
| Omit reset assertion on the first fabric clock | `cold reset must assert before any acknowledgement or request` |
| Consume uninitialized replies during initial reset settling | `unsolicited reply retired` |
| Accept a matching-ID reply without an outstanding request | `premature ID reply acknowledged an unissued probe` |
| Allow completion state to advance while fault wins | `probe completion advanced while fault won` |

The first-clock reset, cold-start and fault-priority negative receipts record
exit `-6`. The premature-reply case retains the actual build/run command and assertion log,
without a separate structured exit-code receipt. These are deliberately
modified simulation snapshots, not production artifacts. The positive bench
checks fixed four-cycle latency; hardware response admission requires an
existing outstanding request and does not separately enforce response age.

## Native integration and consistency

[runtime/checks.json](runtime/checks.json) records native and static AArch64 unit
success, production-target refusal, archive audit, hardware-support truth and
the frozen-source active-tree/deterministic-archive check. These unit tests were
compiled from captured working bytes subsequently verified against commit
`d1197220156fdc26e62e07a7caed9b3e0d19a09a`. The original receipt retains that
identity. A separate [source recheck](source-verification.json) confirms all 76 files in
[runtime/frozen-source.json](runtime/frozen-source.json) still match the 9663
revision. Earlier active-tree attempts failed before the stable frozen rerun;
only [runtime/policy-frozen.log](runtime/policy-frozen.log) is the passing final
run. No earlier failure is represented as success.

On verifier commit 1ec76837bb, the [17 focused board/recipe tests](checks/focused-tests.log)
pass, including rejection of changed/duplicate manual EMIF declarations. The
retained [generated-consistency check](checks/generated-consistency.log) passed
on 803d for 36 generated consumers and 37 fixture copies; no new execution is
implied by retaining that unchanged-contract check. The shared diagnostic definition
adds three generated consumers. It adds no production profile, daemon API,
physical device opener or programming backend. The production AArch64/DE25
construction guard remains closed.

The local `make test-changed` campaign completed all selected lanes. Parent
tests passed (1,094 cases, two skips), as did host/UI, runtime and contract
checks. The FPGA suite ran 1,483 tests with 35 skips and one failure:
`test_timeout_cancels_multithreaded_child_group_without_masking_timeout` still
observed a PID at its process-cleanup assertion. The
[retained failure and focused follow-up](checks/read-audit/README.md) record that
the exact method passes in isolation with its 0.5-second timeout unchanged, and
all four relevant implementation/test files match the base branch. The failure
does not identify whether that PID was live or an unreaped zombie. No aggregate
success or baseline execution is claimed. This campaign ran while the
diagnostic was refined; the final-source focused tests and co-simulation above
bind the changed RTL. Final-head CI is reported separately in the pull request.

The [strict deployment classification](checks/hil-plan.json) is
`OVERLAY mister-runtime`. The new diagnostic definition does not hide the
runtime archive change. Existing-kit runtime install/test/restore and
exact-artifact DE25 acceptance remain separate integration checks; neither was
run here.

## Remaining physical gates

No kit, flash, boot image/configuration or physical programming was touched.
The proof does not establish bounded real MMIO, Linux bridge mapping or cache
semantics, SDM sequencing/firmware pairing, physical reset behavior, DDR
calibration or integrity, electrical timing, MTBF, or exact-artifact hardware
acceptance. The caller remains responsible for bounded ordered register access
and a bounded configuration callback. The arriving board must first establish
those bindings and then exercise frozen-state readback, A/B/A reconfiguration,
persona identity and post-switch continuity with the exact retained artifacts.

## Retained outputs

The task worktree keeps the complete compiler project and raw reports under
`out/pr-control-final-9663da282a/hps/project/`, executable bundles and logs under
`out/pr-control-composition-1ec76837bb/`, and actual-runtime co-simulation under
`out/pr-control-cosim-9663da282a/`. Rejected compilation/packaging/audit attempts
remain under their original or explicitly suffixed paths. The descriptor
copied here preserves paths relative to the original compiler project; copying
this evidence does not relocate its payloads.
