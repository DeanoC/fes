# DE25 generated FPGA-DDR transport, 2026-10-29

The real generated FPGA-B transport passes an off-board transaction simulation.
This is a generated-RTL/software diagnostic, not physical memory acceptance.
The change adds a test harness; it changes no endpoint/controller RTL, FPGA
constraints, memory parameters, boot image, shared contract or HPS interlock.

The RAM source producer is `43c248d2dfbad7fa2d0ef4c72fa006fe21214dc9`.
The harness and bench are committed at `202ef4c64f`. The actual frozen run is
retained in the task worktree at `out/interconnect-final-202ef4c64f/`.
The original project is the existing `de25-ramtest-calibration-contract`
producer output under `out/dev/de25-ramtest/out/`, with build ID
`6ee668470911276a9e553d0025c7ef0b`.

[simulation.json](simulation.json) records all 185 selected generated
HDL/include source hashes, QIPs, input/generation identity, the verifier Git
revision and source hashes, wrapper, executable, tool version and nine results.
The original generated files and receipts remain unchanged after the run.
The actual endpoint's four-word build ID is checked against the producer
receipt. Verilator 5.032 executes the actual generated packet, width-adaptation
and response logic; the reachable transport contains no elaborated hard-RAM
primitive and no substitute storage model is supplied.

| Case | Observed result |
| --- | --- |
| Raw width/lane and backpressure | Both 128-bit halves of AXI256; partial byte strobes; 20 returned Avalon beats; stable AW/AR/W through stalls |
| Complete real endpoint scan | Seven patterns plus 1,024 latency samples; 21 AXI write commands, 1,045 read commands, 224 write beats and 2,272 read beats |
| Corrupted upper-half read | One error; first address `0xff0`; expected zero, actual one, bad-bit mask one |
| AXI read response error | SLVERR becomes the endpoint response fault; no false DONE |
| AXI write response error | SLVERR becomes the endpoint response fault; no false DONE |
| STOP with delayed B | Remains busy until the accepted write completion arrives; no new write command |
| STOP with delayed R | Remains busy until all accepted read beats arrive; no new read command |
| Reset with an outstanding read | Reset-uncertain state remains; CLEAR cannot invent a new healthy epoch or completion |
| Unsolicited AXI R response | Separate subprocess aborts at the original generated read-ID FIFO assertion |

[simulation.log](simulation.log) retains the eight normal case results.
[unsolicited-response.log](unsolicited-response.log) retains the negative
assertion. The assertion is enabled with Verilator `--assert`; the expected
nonzero termination is checked by the helper. It is a simulation checker,
not a claimed synthesized hardware fault flag or controller recovery path.

Twenty-six focused tests pass, including existing scan/endpoint/SDRAM benches
and six new harness-boundary controls. `make check` passes with 33 generated
consumers and 37 fixtures. [checks.json](checks.json) binds these local results.
The broader affected suite and CI are reported in the PR separately.

Initial development attempts remain under `out/interconnect-dev-*`:
the first kept the vendor assertion enabled and exposed the malformed
response as an abort, and a later moving-working-copy attempt correctly
failed the verifier source-stability guard. Neither is relabelled as the
committed final pass.

The AXI memory model provides independent byte storage and delayed/error
responses. It does not model EMIF calibration, the memory controller/PHY,
physical LPDDR4, analogue timing, full upstream CSR routing or the HPS memory
path. Production HPS writes remain disabled. No kit, device, image, flash or
harness was touched. Arrival-day memory calibration/reliability and exact-SOF
board acceptance remain the next physical integration gates.
