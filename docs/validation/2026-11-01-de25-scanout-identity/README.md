# DE25 scanout identity and HDMI readiness

This is off-board validation of the default-off allocated-buffer candidate.
It does not establish production MENU, native boot, DMA, timing or hardware
acceptance of the new image. The board keeps the previously verified ABI 2
kernel and drained diagnostic FPGA configuration; no new image is deployed.

Producer `8cace007e4d1e8a9e23f1e52675279d1e83033bc` adds fabric 1.1
build identity, Linux DMA ABI 3, cached privileged owner metadata and observed
HDMI readiness. Linux rejects the old endpoint before accessing new registers,
and clients query the owner ABI before allocating. HDMI readiness can be
observed before submission. Its loss during scanout faults the engine without
turning outstanding memory traffic into a drain or successful frame.

The generated-model validator is
`e531091e64b0497affe692228d855ff85ddf465b`. The first validator refused
Quartus's new parameterized wrapper before elaboration. The correction requires
explicit BUILD_ID forwarding and compares all four live words with the verified
producer value. All 34 selected functional source files are byte-identical
between these commits: build ID `115a31ba8625086218d3335cf97a3cf6`.
The corrected validator does not change the compiled FPGA sources.

Validation:

- Actual AArch64 QEMU kernel: all 48 required cases pass, 102.4 seconds including
  host packaging. This covers identity changes, old ABI rejection, cached owner
  observation, mapping lifetime and readiness polling without submission.
- Actual RTL: complete-frame acknowledgement, all 18 endpoint groups, board
  visibility and bounded control guard pass, 36.6 seconds. Board clocks and HDMI
  configuration are digital models; transmitter/cable behavior is not modeled.
- Generated transport: all ten required cases pass, 70.1 seconds, with actual
  generated CDC/interconnect sources and explicit modeled M20K storage. Hard HPS,
  SMMU, firewall, DDR and physical reset/timing are outside this model.
- Builder 24, plain C client 9 and exact combined-proof source gates 16 pass.
  Generation and repository consistency checks pass.
- The separate source-identical replay of the earlier ZX81 machine timeout
  passes on the second builder in 500.2 seconds with its original 600-second
  inner bound. The earlier whole suite remains a failed run; this is not an
  aggregate suite pass or a timeout relaxation.

The first Quartus compile passes in 507.1 seconds. RBF
`cfa6fe458af7f031a56229c479d2db50a49d44811f86650ad7b0ceda33ba11ea`
is 1,744,896 bytes. Final comparison matches all 175 generated model sources,
17 QIPs, inputs, generation receipt and all artifact digests. These are compile
and identity results, not timing acceptance.

All-family analysis reports **five of 85 signoff rules failed**, without waivers:
TMC-20012 (30 High), TMC-20011 (2 High), CDC-50002 (1 High), RDC-50003
(2 Medium), RES-50101 (3 Low). The new CDC row names
`hdmi_ready_source` to `hdmi_ready_sync[0]`. Actual top wiring places HDMI on
CLOCK0_50 and the system PLL on CLOCK2_50, so the crossing is asynchronous.
This first candidate remains retained and is not deployed. The correction
names only its registered source and stage0 D with exact collection checks;
later stages and consumers stay timed. That SDC correction changes the selected
build identity and needs a separate compile/model result. No timing waiver,
clock reduction, widened DMA permission or new hardware acceptance is claimed.

Remaining normal-kit work includes the DE25 MENU presenter and platform factory,
managed catalog launch/stop/relaunch, safe restart/failure recovery, native
acceptance of the new pair, physical media SAVE and the existing timing gates.
Cached owner information is an observation, not permission to adopt an unknown
configuration or discard peer mappings.
