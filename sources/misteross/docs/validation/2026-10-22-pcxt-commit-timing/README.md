# PC/XT commit eligibility experiment

This is diagnostic evidence for #840, not timing closure or promotion evidence.
The baseline timed out before final reports. The candidate completed routing but
failed timing. No matched routed improvement can be claimed from this pair.

Base: `714108a9353ba322e3678e224076ff4402b9539e` (main after #836).
RTL candidate: `be5f2253`. Only `pcxt_isa_endpoint.sv` differs between the
synthesized baselines. The candidate records commit eligibility on the same edge
that preparation completes, and clears it on commit/reset. It adds no bus cycle.
Whole-interface temporal induction proves all 82 output bits after reset with
arbitrary inputs; a mutation allowing repeated commits is rejected.

The mapped maximum CGA VRAM A1EN combinational cone falls from five to four
levels. All 32 timed CGA VRAM A1EN rows pass; the worst setup margin is +0.963 ns.
This is a structural reduction and a passing candidate path, not proof of an
improvement against a completed baseline route.

| Run | Outcome | Wall seconds charged | CPU/system MHz | Memory MHz | Microcode MHz | Pixel MHz |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| baseline | timeout, no final report | 450.413 | — | — | — | — |
| candidate | final analogue, timing fails | 192.141 | 76.406 | 273.973 | 103.950 | 83.354 |

Candidate setup WNS: CPU/system -2.555 ns, memory +3.175 ns, microcode +0.285 ns,
pixel +1.471 ns. All related hold rows pass; per-clock hold WNS is respectively
+0.784, +5.725, +4.255 and +0.707 ns. There are 257 failing setup rows among
17,153 endpoint keys; coverage remains incomplete with 105 unsupported boundaries.
The worst reported setup path is storage physical-address state to SDRAM_A;
the worst CPU path is DX/divide-condition logic to microcode next-address state
(-2.046 ns). CPU operand/result locality was not changed in this experiment.

Both use Yosys `3a5accbd7`, nextpnr source
`45476abd07a8881ebeee1e5d061be6b5fe123d71` (PR266 plus main), seed 1,
weight 2000, criticality exponent 5, fresh packing/placement, existing SDC/QSF,
and the same frozen binary. This is distinct from the older placed experimental
CPU-locality graph. The binary SHA256, exact argv/environment and every artifact
hash are in `runs.jsonl`; the archive includes the full source export, generated
ROMs, synthesis scripts/netlists, logs and candidate RBF/routed graph/reports.
The compiler binary itself is not embedded.

Both runs use HIP on the automatically leased RX 7900 XTX, with Router1 cleanup;
this is not a pure GPU-router cohort. Both GPUs were free and the queue empty
at preflight and after completion. Phase lease waits are excluded from each
450-second active timeout, but all wall time is conservatively charged to the
incident allowance: 1474.392638 seconds before, **831.838806 seconds remaining**.
No clocks, constraints, pins, locks, seals or hardware were changed.

Validation: the two endpoint proof tests, focused ISA/display/display-machine
simulation, parent consistency, 805 parent regressions and 1268 misteross tests
(1 skipped) passed. The full affected-suite PC/XT simulation sweep also passed; results are in
`test-changed.log.gz`. The mapped board uses three additional FFs and eleven
fewer combinational ALUTs. The first sandboxed parent test attempt failed on blocked local sockets;
the unrestricted rerun passed those tests.

`capture.tar.gz` retains both successful and failed artifacts. Verify member
bytes with `manifest.json`; `cones.py` reproduces the mapped cone count and
`analyse.py` extracts the final timing summaries. The scripts retain the exact
local diagnostic invocation paths; these are not registered producers.
No hardware ran. Neither this candidate nor nextpnr PR266 is promoted by this
record. Next work is the remaining storage and CPU control paths, followed by a
complete paired routed check and the existing seed/producer/hardware gates.
