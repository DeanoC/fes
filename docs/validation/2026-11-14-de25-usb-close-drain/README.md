# DE25 report drain before USB shutdown

Source `fdf6ce1650a3f1b882042688eed81b9c05ca1116` closes a gap in native USB shutdown. Returning from the last core resumes report polling. Final USB Close previously went directly to firmware Stop, so an asynchronous DWC2 packet or unread receive status could still be pending.

Close now revokes local polling and, when the driver exposes polling control, completes its existing bounded pause/drain before firmware Stop. The same source/client locks cover the drain and Stop. A stale receipt is rejected before driver access. An already-paused source is not drained twice, and an already-failed driver is never re-entered. Firmware still independently proves channel/PIO/master/reset state; a driver drain is not reset authority. Sources without asynchronous polling control retain their existing synchronous-report/firmware-stop contract.

A drain failure stays latched even if the one known firmware cleanup succeeds. Reports and failed objects remain retained; another poll, close or resume cannot restart them. Cancellation before firmware mutation retains the existing known-lease cleanup behavior, including successful drained state. No packet, preparation, firmware or application deadline was increased. No C firmware, CSR or wire protocol changed.

The physical preceding source reached fifty verified App commands and two core launches, then failed the fifty-first Quit at USB Close with firmware status 4. This repair addresses a source/model ordering gap. It does not identify that physical failure's exact cause, cover unrelated packet/PHY failures or recover its already-panicked instance.

## Completed checks

- The actual DWC2 driver and descriptor/FIFO/channel model now require pause, no pending packet/path-health work, empty receive queues and a retired channel before the modeled firmware Stop. Both keyboard and gamepad cover idle, pending and already-paused closure. Stuck channel, frozen clock and foreign DMA cases remain terminal.
- Separate tests run the actual C shared dispatcher/service/platform/cold/bridge implementations with controlled report-drain callbacks. They cover cancellation before/after drain, stale ownership, drain failure, foreign DMA and one-stop/no-replay behavior. This is register-model evidence, not physical USB acceptance.
- An overlay of the exact preceding `source.go` from `f595868d8…` fails four unpaused-close cases; its two already-paused controls pass. No production checkout was changed for that counterexample.
- Full `go test -race -count=1 ./usbcontrol ./dwc2 ./nativefabric` passes. Committed `make check` passes. All eight selected `make test-changed` phases pass in 1,149.140 seconds, including parent regressions and actual native runtime/firmware/FPGA integration.

[Source/models/counterexample retention](retention-proof.json) and [affected checks](affected-checks.json) bind the completed logs to the selected source. The source/model archive SHA-256 is `39e78237a44a61be1ff3e0e8a487801d8e80cdfb5f81747437e62a446cba8f00`; its byte/hash/mode readback passes. The affected log archive is separate, so no live log was included in a completed archive.

## Complete matched build

Normal SDK → entry → selected BL31 → SPL/U-Boot → HPS-first composition → full native file admission passes on the exact source above. The selected SDK's source map equals the new `usbcontrol/source.go` hash. The warm expected-table repair remains selected; normal shutdown and retained-source repeated loading still require physical acceptance on this new artifact.

| Artifact | Identity |
| --- | --- |
| Chain | `e29470c44b581055f7874380236b60d7cf5ff08289150518203ea12c8290f567` |
| Boot tag | `f3bfab1df6c6d1db` |
| FIT | `4831f267f89c66513c608c07ab720827f4f6df46a5d480c5fc5b4b82c179c7a4` |
| FIT size / bound | 8,547,024 / 16,777,216 bytes |

[Matched identities](matched.json), [selected SDK/object/layout checks](integration.json), and [matched retention](matched-retention-proof.json) record the exact build. The private layout remains `0x50000..0x5ff40`, 192 spare bytes, with four unchanged 8 KiB stacks. This is not a whole-EL3 stack qualification. Core payloads are unchanged. Composition holds the shared Quartus Pro lock and performs packaging only, with no synthesis, fitting or programming.

Matched archive SHA-256 `aba8a644bf7aa04039e47843415b30f9d50248de691a7493f30796f50721e9df` independently verifies all 46720 entries, including regular bytes, modes, symlinks and deduplicated hard-link targets. This new repaired image has not been loaded on hardware at this checkpoint.

The separate [volatile JTAG recovery probe](../2026-11-14-de25-jtag-usb-reset-observation/README.md) uses the original recovery loader and qualifies only its fresh prompt and USB reset-bit readback. It does not provide hardware acceptance of this SDK, re-enumeration or the Secure SMMU bank. Next is a fresh bounded native boot/core-cycle trial with explicit admission from the recovered state; the consumed failed baseline is not retried.
