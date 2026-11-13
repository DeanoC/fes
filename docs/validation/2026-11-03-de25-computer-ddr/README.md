# Current computer payload: bounded FPGA DDR read/drain

TRWU8CBL passes one fixed read-only FPGA DDR read, master drain and bridge
reclosure with the current sealed ZX81 computer payload and freshly built
native firmware. Compiled source: `668b1c733155025e10a235dab8f6a82b5767017a`.
The earlier [fixed-read pass](../2026-11-02-de25-contained-console/README.md)
qualifies its earlier artifact; this candidate checks the changed computer
payload and current firmware. The [distinct-package switching result](../2026-11-03-de25-distinct-hardware/README.md)
remains a separate, data-held test.

Native boot: `cd19cf6e053fe413`.
FPGA package: `b2353f540cc81b9896db86867248880aceffce8c81314042b02317f76f947f37`.
FPGA build: `dda236646bb868e63c72bd0e777f3dd6`.
FIT SHA-256: `22b8d99e8535c9c2c75510dbd4554f848af499453edfc3cdff37edc46965e449`.
SOF SHA-256: `57c5296179c8766b23a3151791c26f03518e78aa7532091c1b2c7e8671041f74`.

The candidate uses the existing sealed computer payload without an FPGA
recompile. The matched native SDK, entry, secure firmware, SPL/U-Boot and
HPS-first composition are rebuilt. Exact source verification checks 122 Git
blobs and 1,298 pinned module/SDK files. Linked checks retain the vendor startup,
resolve relocation targets, verify the contained console's call graph, check
the actual CPU translation instructions and seal the loaded firmware before
handoff.

Focused source checks exercise the DDR adapter contract, native reservation
and probe, actual C/RTL reader/drain failures and the strong secure provider's
ownership/translation/firewall paths. The six-file uploader passes 69 stateful
runner cases and 271 receipt mutations. The outer DDR controller passes 51
cases, the DDR result gate rejects 54 damaged receipts, and six transport/helper
checks pass. The affected documentation checks and parent consistency check
pass, including 43 generated consumers and 37 fixture copies. These are
software evidence.

One preflight stopped on a missing host process-inspection utility before any
board mutation. Its log is retained. The corrected preflight uses the existing
root-visible, bounded process-descriptor inventory and keeps the hardware
phase limits. It validates the complete candidate twice and freezes the helpers
and inputs before the physical attempt.

The first physical attempt passed recovery, all six writes/fresh reads, native
handoff and SD package containment, then its observer rejected the CPU-mapping
and context records at configuration start. The reused helper had omitted the
current image's independently checked CPU-mapping descriptor. The original
failed receipt, attempt fuse and raw capture remain unchanged; configuration
completion and DDR closure were not captured. The autonomous native diagnostic
may have continued after the observer stopped.

The [stop audit](hardware-stop-evidence.json) independently verifies exact clock,
USB, private-service, UART and claim cleanup. Replaying only the saved prefix
with the corrected helper removes the ordering errors while keeping the result
incomplete. The helper regression reproduces the original omission and rejects
nine mapping-record mutations. The corrected outer controller passes 51 cases
and six transport/helper checks pass. A fresh attempt uses the same compiled
image and phase bounds; the first failure remains counted against the two-failure
limit for this implementation.

The physical test must configure the retained exact SD package, verify the
live FPGA identity, validate the pinned 8 KiB CPU pattern and fresh SMMU/firewall
observations, release DDR only, issue one 32-byte/two-beat read, and force its
completion through reader DISABLE. It must then verify the complete reader,
inherited probe and held-maintenance master stop, hardware DDR drain and bridge
reclosure. The 2 MiB arena stays pinned until reboot on every outcome. Bounds
remain prompt 50 seconds, package retention 100 seconds, configuration 20
seconds, transfer/probe 45 seconds, native phase 240 seconds and outer 800
seconds. A failed proof stops without replay.

TRWU8CBL passes the current-payload test. The raw 47,866-byte
capture independently decodes to `ddr_read_closed`, with no decoder errors.
The fixed read returns checksum `0xf1ffefc0` and final words
`369ca8ab 59542ed2 fb0d9419 1dc51a40`. Reader generation 1/cookie 7 matches;
all-master stop is accepted at ownership generation 6/reset `0x5`, and closure
finishes at generation 7/reset `0xf`, with hardware drain mask `0x8`.
Data continuously contained is truthfully `0` because the read released DDR.
The optional fabric-warning acknowledgement remains `0` and unqualified.

SD package retention takes 58.575 seconds and the
configuration interval is 13.365 seconds, within
the original 100/20-second limits. Native code performs no SD write. One
qualified recovery and one owned upload session precede the native boot;
protected recovery files and the raw prefix remain unchanged.

Independent replay agrees with the native receipt and rejects all
5 captures with a required DDR gate/result/closure/final record removed.
It verifies the complete final DDR result, master-stop and closure from raw
records. Clock `15M` is restored and read back before the owned private JTAG
service stops; exact USB permissions, a free UART and the continuing claim
are independently checked. The spare and completed diagnostic remain on the
top board under the user's retention policy; the factory card stays safe
outside it. No factory restore, flash write, card swap or new Quartus compile
is performed.

[Hardware evidence](hardware-evidence.json) and [summary](hardware-summary.json)
contain identities, observed timings and hashes. Full traces, frozen helpers,
matched native artifacts, the sealed FPGA source project and qualified recovery
kit are retained under `/home/deano/fes-retained/de25-computer-ddr-20261103/`.
The retention manifest distinguishes the compiled source from this documentation
commit and records independent archive/source readback.

This contract cannot qualify all 8 KiB, FPGA writes, general translation,
continuous scanout, repeated active-DDR launches, arbitrary incoming packages,
native USB/video/Ethernet or ordinary FES runtime admission. It grants no
buffer reclamation. The next memory step is bounded address/window coverage
and reuse after a verified drain, before adding a scanout producer and its
stop/drain callback to the sealed master inventory.
