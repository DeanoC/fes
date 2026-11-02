# HPS-first scanout phase artifacts

Source producer `36ee9c81f278a75ddaf89afcd5c83683c9d32800`, based on
`12678d182fdd281b88a386fb488232f656a1d2ac`. Scope: explicit HPS-first
packaging for the disabled allocated-buffer scanout design, sharing the
existing ZX81-HPS phase conversion helper. No RTL, clock or timing-constraint
change; no FPGA, QSPI, SD, kernel or kit deployment.

Quartus Pro 26.3 Build 114 on the second builder completes with zero errors
and 308 warnings. The complete source is a clean shallow copy at the exact
producer commit; the resource archive passes its original locked SHA256.
The job has a 30-second heartbeat and a 7,200-second cap and exits successfully
after 540.5 seconds. All 19 original report hashes match the build receipt.
The pixel PLL remains unchanged and the generated HPS/EMIF review reports no
changed original module parameters. The vendor loader hash also matches the
approved bottom-board bootstrap. These checks do not prove boot compatibility.

The shared converter produces these fresh, nonempty regular files and binds
them to captured source identity in [linux-programming.json](linux-programming.json):

| Artifact | SHA256 |
| --- | --- |
| Complete SOF | `ff1c64be5e0680fe0764bc12932c4647c2a2f3f59f1d056a86bd982c00531c4f` |
| HPS JIC candidate, not programmed | `ef9cab6524861c6163da8242123804958ad0d88cc76892b5beefa5a07abcbf52` |
| Linux core RBF candidate | `481eedd216fd8520a61c42c356ac28e32c1826a39ede98d2b6c89e9894920c49` |

The [74 timing-summary rows](timing-summary.json) have nonnegative reported
slack; every reported TNS is zero. The smallest hold is rounded 0.000 ns.
Quartus still reports incomplete setup/hold constraints and six failed
signoff rules with zero waivers. Timing acceptance remains **not qualified**.
Original STA, constraints and signoff reports are retained compressed here;
this summary does not replace endpoint coverage or external-I/O signoff.

Sixty-one focused software cases pass: HPS preparation 21, combined producer
7, bootstrap packaging 5, ZX81-HPS packaging 17 and scanout packaging 11.
The changed tests cover captured boot mode, actual common phase-converter
calls, output hashes, unchanged closed DMA admission and stale-output refusal.
`make check` passes 37 generated consumers and 37 fixture copies; docs and
whitespace checks pass. The earlier complete main-integration run passed on
`aaeb58b86`, including 1,669 FPGA cases; that is separate source evidence,
not an aggregate test result for this producer.

Full phase files and all original reports are independently rehashed and
read-only under
`/home/deano/fes/out/artifacts/de25-scanout-linux/36ee9c81f278a75ddaf89afcd5c83683c9d32800/`.
The complete generated project/database remains at
`fes-build-3090:/data/fes-de25-linux-platform-36ee9c81f/output/`.
Later evidence commits inherit no new compilation or hardware acceptance.

## DMA integration finding

The preserved vendor HPS IP has `f2sdram_SMMU=false`. In a separate scratch
project, loading the underlying component and enabling that parameter changes
both F2SDRAM address-channel USER ports from 8 to 25 bits. The saved IP and
raw transcript confirm value `1`; the first result checker expected literal
`true` and rejected the equivalent tool representation. The separately checked
[scratch proof](smmu-port-proof.json) records this distinction. Generating that
modified IP completes with zero errors/warnings. No full SMMU-enabled FPGA
image is compiled or programmed, and its stream-ID bit layout is not yet
established.

[Altera's component parameterization procedure](https://www.intel.com/programmable/technical-pdfs/683609.pdf)
explains loading the component after a saved system becomes generic.
[The F2SDRAM integration guide](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/device-tree-f2sdram-bridge-fpga-master-integration?contentId=aOAQ62QcYgHqNRLOrmPpZg)
requires the device-tree stream to match the FPGA-driven stream, and treats
cache coherency separately. A disabled port option alone does not prove that
translation is bypassed or that a chosen stream exists. No arbitrary physical
address, fabricated stream ID or `dma-coherent` declaration is used here.

Next: establish the exact bridge stream/firmware path, bind Linux-owned DMA
storage to that actual device, and verify frame ownership, drain and reset
lifetime on the board. Bootstrap compatibility, Linux loading, allocated DMA,
MENU, production runtime, managed bundle and catalog ZX81 launch remain open.
The installed bootstrap and both boards are unchanged and released.
