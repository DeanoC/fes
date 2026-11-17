# DE25 contained large-block transfer, 2026-11-02

The first complete Linux-free cold FPGA transfer **passed** on TRWU8CBL.
All 2,027,520 sealed bytes completed, firmware reported ready, and two live
identity snapshots matched the expected ABI and build ID. The control bridge
was reclosed at generation 3; all data paths remained held. This is a contained
hardware diagnostic, not runtime package admission or active-core launch/return.

Native source: `cac97213eee7619bd40d99173b46f3ce1c14968f`. Matched boot: `ee88aa1a65d7861e`.
See the [raw serial trace](native-console.log), [independent audit](hardware.json)
and [saved observations](evidence.json).

| Observation | Result |
| --- | --- |
| Core source | `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c` |
| Package | `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63` |
| FPGA build | `e25bb65a1d14a2bbbddf4c770ea033e5` |
| Compiled block size | 1,048,512 bytes (`0xfffc0`) |
| Buffers | 1,048,512 + 979,008 bytes; both acknowledged |
| Firmware limits | Four credits, 1,048,560 bytes per block |
| Secure ARM to COMPLETE interval | 16.295608 seconds |
| Digest time within that interval | 11.580124 seconds |
| Firmware readiness | 1 |
| Identity/control | Expected ABI/build; closed at generation 3 |

The ARM interval begins after the first whole-image digest; it is not the
end-to-end load time. The native loop retained its 20-second deadline and
checked it before and after each synchronous step. No deadline was increased.

## Controlled comparison

The [preceding 64 KiB trial](../2026-11-02-de25-hps-failure-status/README.md)
stopped at 786,432 completed bytes, with four possible outstanding buffers.
This comparison uses the same RBF and fixed uncached 8 MiB arena at
`0x94800000..0x95000000`. The [input comparison](comparison-inputs.json)
confirms that only block size and per-block hashes differ in the configuration
map; raw transfer code, mailbox parser/status reader, SHA implementation and
configuration gate match the prior candidate. The strong platform now validates
the generated aligned block bound instead of requiring a 64 KiB literal.

The producer rounds the pinned vendor client's `0xffff0` limit down to a
64-byte boundary. The native constructor takes its DRAM base and block size
from the checked compiled map, copies that map for deferred preparation, and
requires the original closed source before START. Secure firmware independently
checks its compiled extents and whole/per-block digests. Retirement checks the
same map, retains uncertain buffers and preserves the original failure.

The larger blocks avoid the observed stop, establishing that this exact image
can complete and expose its expected live endpoint. They do not identify the
cause of the smaller-block acknowledgement failure. This attempt used two jobs;
physical queue/ring turnover and repeated loads remain unqualified. Diagnostic
CPU work is substantial: no runtime latency or responsiveness claim follows.

## Ownership and evidence limits

One CPU ran; secondary release remained denied. Closed source ownership stayed
at generation 1 through transfer and readiness. The identity probe temporarily
opened control only, checked two identity snapshots, and closed it again. It
made no active-master stop/drain claim, data release or activation. The arena
stays boot-pinned even after DMA completes. This locally source-bound boot
chain is neither signed/measured boot nor a production admission receipt.

The [Ethernet upload and independent SD readback](upload.json) passed while
protected boot files and recovery inputs stayed unchanged. There were no native
SD writes. The [cleanup audit](hardware.json) checked the original 15 MHz JTAG
clock readback, restored USB ownership/mode and a free UART. The spare card and
exclusive top-board claim remain retained; the factory card stays safe. This
was one native attempt on the exact source, with no retry or factory restore.

## Software and retention

All ten [affected check groups](software.json) pass on the native source,
including 1264 parent tests and
1576 FPGA host tests, native Tenfoot launch/return and failure
integration, producer/control integration and firmware/identity/drain RTL.
Generated consumers match. The real strong provider fixture exercises large
full/tail blocks and ownership/failure handling with the pinned SHA algorithm.
The existing trace case now also covers two large-buffer completions without
changing test-selection rules. Private boot/cleanup helpers pass 66 cases;
retirement-capture fixtures pass 28 cases. The [linked-code audit](linked-firmware.json)
checks eleven strong symbols and the unchanged 2,912-word scalar SHA transform.
All six matched components built successfully.

The first candidate had a stale per-block hash fixture and hit the existing
test-selection count assertion. Its remaining owned worker was stopped after
that failure; no hardware was touched. Its [build archive](first-candidate-build-retention.json)
and [failure/session archive](first-candidate-session-retention.json) are
retained. The repair updates the fixture and keeps the added trace scenarios
inside the existing test method, preserving all coverage and thresholds.

The [final matched-build archive](build-retention.json) retains 43,681
individually verified members, SHA256 `04314d906bee4b85f6a41b327e486a94873049cdf74b352330723148bf2336c4`. Incremental source
bundles require their documented predecessor chain; they are not standalone
complete-history backups. Runtime admission, repeated physical launch/return,
master drain and the complete peripheral/UI path remain required.
