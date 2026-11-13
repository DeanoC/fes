# Canonical DE25 MENU compiler candidate

The MENU producer now derives its actual HDL BUILD_ID from the standard
format-2 functional build record before IP generation. The compiled candidate
passes the normal exporter's provenance checks. Package export remains closed
because four signoff rules fail. No package, SD image or FPGA configuration is
installed by this proof.

The producer is `262ee72db716f44b34018f43375672006c45d713`:

| Result | Evidence |
| --- | --- |
| Quartus 26.3 Pro compile-only | PASS, 635.3 s; [receipt](compile.json) |
| Generated transport, with modeled M20K storage | PASS, ten cases in 69.8 s; [receipt](generated-transport.json) |
| Independent artifact/source/identity verification | PASS, 0.7 s; [receipt](verification.json), [checks](checks.json) |
| Final package/export and preparation regressions | 39 package/export tests and 26 board-preparation tests pass; [receipt](focused-tests.json) |
| Broader software control, producer `222674ddf` | `make test-changed` PASS in 1,681.1 s; [receipt](software.json) |

The broader command includes the 1,109-case parent suite (three skips), the
1,727-case FPGA software suite (76 skips), planner tests and generated-consumer
consistency. Its source predates the final Pro inspector and format guards;
the final guards have the focused checks and actual compiler/model/provenance
proof above. This is not an exact-final-source full-suite claim.

Build ID: `6f64324f9f29e219d6bd6806228afec2`.
The 1,740,800-byte RBF has SHA256
`b16d1f080bbbac00cbc1be684f07a8da04a279c59ae1b5d78ae8b52ee93ff684`.
The canonical record has SHA256
`8e3e2046adb30f6f47fc9d3c8bbcbfc17e72048728b9958de91a4cf6c786c9e1`.

The record binds 365 tracked source files, the exact SCS device and generated
scanout ABI, the locked Terasic archive, real executable identities, the complete
selected tool installation, Python/runtime dependencies and controlled
execution. The Pro inspector uses the installed `qenv.sh` layout, including
both `quartus/linux64` and `qcore/linux64`. Actual version output is reduced to
its single version line; executable and installation hashes retain the byte
identity. Project-only inspection supplies no invented compiler identity.

The actual generated endpoint receives this BUILD_ID before synthesis. The
generated transport test reads all four expected ID words from that endpoint.
Its 175 HDL sources and 17 QIPs match the final compiled project; input,
generation, SOF and Linux phase-artifact hashes agree. The generic exporter
validates an in-memory manifest against the real RBF and canonical record;
it publishes no manifest, archive or package. The compile-only path creates
none of those outputs.

DE25 manifests accept only format 2 with `A5EB013BB23BE4SCS`. Cross-board part
pairs, the R1 part and format-3/4 Cyclone V ROM maps are rejected. The MENU
contract selects `de25-linux-menu-v1`, application 1.0, fixed 720p video,
menu-display 1.0 and DE25 scanout 1.1. This adds no ROM-map support for Agilex.

All 74 reported setup, hold, recovery, removal, pulse-width, skew and net-delay
checks have finite nonnegative slack. The complete signoff inventory and its
detailed results retain these failures, with zero waived findings:

| Rule | Severity | Violations |
| --- | --- | --- |
| TMC-20012, missing output delay | High | 30 |
| TMC-20011, missing input delay | High | 2 |
| RDC-50003, multiple asynchronous reset synchronizers | Medium | 2 |
| RES-50101, intra-clock false-path reset synchronizer | Low | 3 |

The normal package path requires all 85 rules and every reported timing check
to pass without waived findings, then a second compilation to reproduce the
RBF, before invoking the unchanged exporter. Neither package export nor that
independent successful-signoff rebuild is exercised here. No constraint,
required clock or timing/seal policy is relaxed.

The earlier `2f5a988da` control passes compilation in 604.7 s with the same four
failed rules, without a generated-model or physical acceptance claim. Its full
project and RBF are retained separately. The original `222674ddf` tool-inventory
failure (0.9 s, missing Pro qcore loader directory) and `049a4b167` record failure
(59.5 s, multiline version identity) remain failed, with their exact sources and
logs preserved. Corrected private launch-wrapper failures occur before any
compiler job starts and are retained as separate metadata.

Independent retention contains complete source bundles, compiler projects,
reports, artifacts, model binaries/logs and job records under
`out/artifacts/de25-canonical-menu/262ee72db/` and the separate
`out/artifacts/de25-canonical-menu/2f5a988da/` control. The final project/model
archive is 310,970,264 bytes, SHA256
`925071f8fe631ffb39945325742a9ebd03974431ab888747476183644c1f044a`.

[Source equality](source-equivalence.json) preserves the existing runtime,
agent, contracts, kernel, headless-image and supervisor evidence at its original
producers. This build adds no physical MENU, DMA, global-reset, timing or normal
kit acceptance. The bottom board remains on the earlier owner-API-2 tracing
card; the prepared owner-API-3 kernel and runtime remain uninstalled. The
[normal-kit acceptance gates](../../design/de25-linux-platform.md) and full
managed image/bootstrap remain open.
