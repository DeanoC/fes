# Proposed normal DE25 output-reset review scope

Approval requested only for the single raw `RDC-50003` finding in normal MENU and ordinary ZX81; no static-persona or failed-transfer recovery acceptance is included. The latest fitted source is a92f5d3cb31793f3d0f1771ebcdd54dfff794e5c. This proposal is not enabled admission and authorizes no board/image/flash operation.

The finding reports two independent asynchronous-reset release chains in the pixel domain: the existing cold-only Qsys reset receiver and the new two-register `fes_video_inst|output_release` chain. The new chain resets only HDMI data/DE and the display-ready acknowledgement. The cold-only receiver protects the live DMA/transport state; merging their resets would erase outstanding work on a clock/configuration fault. Retained epoch/visibility state is not asynchronously cleared.

A read-only query of private fitted copies found exactly one stage0 consumer (stage1) and exactly26 stage1 loads: HDMI_TX_D[0..23], HDMI_TX_DE and hdmi_active, with no DMA/reset-bridge consumers. All26 loads have finite nonnegative recovery and removal in all four fitted models (208 paths per design). MENU minima are recovery +11.910 ns and removal +0.224 ns; ordinary ZX81 minima are +11.722 ns and +0.245 ns. Raw reports and hashes are in output-reset-diagnostic-summary.json and the two diagnostic subdirectories beside this proposal.

The proposed admission implementation must bind the exact compiled sources, source closure, QDB, RBF, raw DRC, query tool and reports. It must accept only that one RDC rule, one pixel domain, two exact chain heads and the expected reset sources; stage fanout and every timed load/model/check must match the above restricted set. Missing coverage, changed consumers, negative/nonfinite timing or any other RDC finding stays blocked. Raw warnings stay visible and unwaived in Quartus. A changed fitted layout needs fresh queries.

Both board simulations pass stopped-clock recovery schedules, and the actual native/AArch64 combined client/RTL proof passes all14 cases. Those checks are digital/off-board evidence, not electrical or physical acceptance.

Separately, CLK-30028 is a genuine clock-definition error: Quartus implicitly chose the PLL intermediate N-counter as the HDMI master. A corrected read-only query binds the forwarded clock to the actual PLL output. It emits no clock warning and checks all216 external video setup/hold paths; minima are +8.608 ns setup and +0.102 ns hold. The repaired source keeps the same forwarded clock phase/rate and ADV7513 1.0 ns setup /0.7 ns hold requirements. CLK-30028 must be zero in new complete fits; it is not covered by this proposal. The external budget still assumes zero PCB flight time.

After approval, complete fresh MENU/ordinary-ZX81 production fits, all numerical checks, independent identical-RBF rebuilds, source-bound package checks and relevant physical acceptance remain required. No numerical timing check or seal is waived.
