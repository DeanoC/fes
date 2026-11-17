# DE25 secure lifecycle investigation, 2026-11-17

This records the initial diagnostic milestone on `b700d743` / `3f103b19`.
The later instrumented physical failure and behavior repair are documented in
the [guard 34 checkpoint](../2026-11-17-de25-guard34-fix/README.md). Statements
below describe this earlier snapshot; its failed or unprogrammed verdicts are
not retroactively changed.

This is an off-board investigation milestone for [#1867](https://github.com/DeanoC/fes/issues/1867), preserved in [draft #1952](https://github.com/DeanoC/fes/pull/1952), `lane:deano`. No kit operation occurred.

The [design audit](../../design/de25-native-secure-lifecycle.md) maps the trust, memory, generations, clocks and recovery contract. Valid modeled generation-15 release passes. The historical physical failure remains unexplained; its original result is preserved. The five relevant historical release-source files are byte-identical to this milestone's main baseline, as recorded in [the source/trace comparison](historical-release-analysis.json).

Tested code: `b700d743c7e0799cf2a78ed2fce710b23a59f5ae`. Base: `a482f14c0f22cf67375e8eebce9211f80e64bd07`. Later changes in this PR add only documentation/evidence.

- All 38 focused sanitized supplier/retirement tests pass, including the joined actual native/C release boundary.
- All 11 affected scopes pass: 2,877 parent tests (21 skips), native/RTL/Tenfoot and producer integration, and 2,493 FPGA software tests (90 skips). Focused tests overlap the parent suite; these are not additive counts.
- The pre-change supplier control passes valid cycles and fails the new diagnostic assertions as expected. The repair is observability, not a claim to fix the historical hardware behavior.
- [Codex review of the code head](https://github.com/DeanoC/fes/pull/1952#issuecomment-6955135643) is clean. [CI run 44132649086](https://github.com/DeanoC/fes/actions/runs/44132649086) succeeds on that exact code head. The literal repository checks gate exits zero and prints `0`.
- The [clean target BL31 preview](unadmitted-bl31-preview.json) includes the new diagnostic and links at BSS end `0x5ff80`, leaving 128 bytes of the unchanged OCRAM reservation. It deliberately reuses an old generated profile, is unadmitted and must never be programmed. The earlier incremental preview is excluded because it did not rebuild the cold supplier; only clean r2 is used here.
- The [fresh complete matched chain](matched-boot-chain.json) passes SDK, entry, BL31, SPL/U-Boot, full-SOF composition and checked native file admission. FIT: 8,571,584 bytes, SHA-256 `bdd2093de0732df419dbdd3b3bbd74151b3da3eb62bc2c15765bd41ef31e074c`; full SOF: `5c5307f5038e95cec32b5402f75a96bf1ca8302456ebd1000bae3d2b1d271789`; native boot ID `7876283624f9c3d4`. Both FPGA core payloads are unchanged. Composition used `/tmp/lander-quartus-powerboat-pro.lock`. This artifact is unprogrammed and has no hardware acceptance.
- [Operation counts](release-cost-counts.json) give 296 initial versus 3,176 warm SMMU reads and 10,240 table-word loads in each synthetic release. The retained physical first release takes 929,971 microseconds of its one-second budget. This is a cost lead, not proof of the failed return's elapsed time or cause. No bound is changed.
- [Vendor review](vendor-review.json) distinguishes HPS/fabric/SDM reset domains and erratum applicability; source-DATA/NoC retirement guarantees remain open.

The combined test alternates 8/4 KiB ROMs, tape lengths 917/0/1/16,375, repeated launch/return, long simulated gameplay and native/model Park. Seven injected rejected returns preserve the complete retained source/ROM/header and cannot retry. Configuration payloads are synthetic 128-byte packages. MMIO, source hashing, activation/dispatch and tape firmware completion remain injected. Actual C tape processing is covered separately. Software Park is not physical USB/HDMI acceptance.

[Machine-readable software results](software-results.json) record scope and log identity. Tested source is retained at `/home/deano/fes-retained/de25-secure-lifecycle-20261117-r1/tested-source.tar.gz`, SHA-256 `ba98612d0bcf2a400b9bc8e4ef97721370590407c710c04bdd4192dde5be5d5d`; all 17,120 Git blobs were independently verified. The [retention receipts](retention.json) also bind the independently verified software archive (11,442 entries, SHA-256 `444b2312b7f072a88f11cd63f591c120934268bf78beec4253d92e4cca68d264`) and matched boot archive (46,685 entries, SHA-256 `2a3d6716a999e1a39732e72eeb614b7fbf2a04b5d6d3df0e5275c3c3f31f4963`). The later documentation delta is retained separately.

The fresh complete diagnostic chain is prepared. The known host restoration-cost problem must be addressed before a new bounded physical diagnostic; evidence-based repair and complete repeated physical lifecycle/HIL remain required. #1867 stays open. No recovery-controller, runtime recovery, secure-boot authentication, Linux feature-parity or hardware acceptance claim is made here.
