# DE25 allocated-frame scanout: stream correction and bounded read-ahead

The exact `a82521947439e5773a1acee8f618c1bb824b6b95` diagnostic passed on the
bottom board using the new tracing-enabled FES Linux card. Three complete
frame submissions used slots 1, 0, 1, with two seconds of continued display
per submission. The actual client checked final geometry, identity, status,
errors and underflows before the owner proved STOP/drain and released the
allocation. The [kernel trace](mapping.trace) records its 8 MiB map and full
unmap. The [hardware result](hardware-result.json) defines the acceptance scope.
This is a diagnostic milestone, not admission of the normal production kit.

![First submitted frame: red with a green lower-right panel](red-green.png)
![Second submitted frame: green with a blue lower-right panel](green-blue.png)
![Third submitted frame: blue with a red lower-right panel](blue-red.png)

All three native capture frames match the CPU-written buffer contents,
including the white corner marker. A single orchestrator started the shared
HDMI alias capture, waited for its first frame, then launched the client. It
recorded 450 frames over 15 seconds, covering the complete client interval.
No raw capture device or other viewer was interrupted.

The [raw SMMU-port correction](../2026-11-01-de25-raw-smmu-layout/README.md)
first removed the AXI response error: raw bit 8 is SECSID and bits 24:9 are
SID, giving nonsecure SID 13 as `0x1ae0`. That first corrected image acknowledged
one complete frame but then underflowed. Its failure and guarded recovery
remain recorded; it was not retried.

The reader serialized every short sixteen-beat burst. A modeled 256-cycle
command-to-response delay at 100 MHz reproduced the throughput limitation.
DE25 now opts into read-ahead, reserving the entire next command plus all FIFO
and accepted-response storage within 256 beats. Other reader/video consumers
keep the single-burst default. The memory CDC response FIFO is 256 entries.
The Linux-only adapter's read/combined AXI credits are sixteen, matching the
actual HPS interface; write credits remain one. Generation verifies those
values, while the adapter keeps its ID-preserving pass-through wiring.

The intermediate `0f74` candidate still had the vendor adapter's original
one-read metadata. Its generated latency test failed at pixel 1600 and it was
never programmed. The original serialized-reader control also failed, at pixel
1536. Those controls are retained here. An earlier helper still expected the
old 32-entry response RAM and correctly stopped before simulation; the updated
helper requires the exact reviewed 256-entry shape and unchanged RAM semantics.

The final generated transport passed all ten cases in 71.1 seconds. Under the
modeled 2.56-microsecond response delay it completed both slots with sixteen
AXI requests outstanding; STOP also retained and retired multiple accepted
requests. This delay is a model parameter, not a physical HPS latency
measurement. The model excludes the HPS hard block, TBU/SMMU, firewall and DDR
controller. Every modeled source/QIP hash and generation hash was checked
against the final compiled project before the physical trial.

Quartus Pro 26.3 completed in 507.2 seconds. All 19 recorded report hashes and
74 nonnegative timing-summary slack rows were verified. Missing input/output
delays and the low-severity reset false-path rule remain, so full timing is
unqualified. The actual endpoint/board/guard/frame-ACK RTL checks passed in
37.6 seconds, including exact 256-beat reservation and complete STOP drain.
The default shared reader/video regressions passed in 12.1 seconds. All 22
scanout builder checks and 39 generated/41 fixture consistency checks passed.

The earlier diagnostic programmer was bound to the old 1,683,456-byte RBF and
correctly rejected the new 1,736,704-byte file before constructing the manager.
The rebuilt [artifact-bound programmer](program-scanout.cpp) requires the new
size and exact hash. A wrong-hash negative control was rejected before
programming, then the correctly bound image loaded successfully. The
[build receipt](program-build.json) retains its actual source and SDK hashes.
No gate was disabled to admit the new image.

Known-success cleanup removed only owned files, overlay and trace mounts under
the exclusive lease, then reverified the same boot, MAC and protected owner
node. No power cycle, QSPI change, SMMU bypass, firewall change, kernel update
or bundle installation was required. The board remains claimed with its
stopped diagnostic in SRAM; it was not promoted or released as a baseline.
Managed MENU, production startup/restart, normal catalog ZX81 lifecycle,
tape SAVE, global reset/failure/whole-load bounds and timing issue #1284 remain.

A subsequent [ordinary software reboot](software-reboot-result.json) passed
after this known successful STOP/drain/unmap and cleanup. The boot UUID changed
from `e5e896ba-e01e-4ab4-9b2f-a236045b7d39` to
`469d5abb-2fab-425d-93bc-aa92aac8c32b`. Authenticated pinned-host-key SSH verified
the unchanged card CID, kernel notes, root receipt, MAC, IP and protected owner
node/group. A new strict boot-UUID guard then passed independently. No physical
power operation or configuration update was used. This covers reboot after a
quiesced diagnostic; reset during active traffic and factory FPGA restoration
remain separate acceptance gates.
