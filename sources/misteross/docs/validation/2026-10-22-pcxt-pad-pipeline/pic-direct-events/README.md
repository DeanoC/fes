# Rejected PIC event-decode diagnostics

Both source-equivalent event rewrites lose their standalone LUT-depth benefit
in the actual board synthesis. The original PIC RTL remains selected. Neither
candidate was routed or run on hardware.

[The board analysis](BOARD-ANALYSIS.md) records all eight service-register
paths, complete PIC input-cone inventories, unchanged RAM/PLL contracts and
the exact functional-proof boundaries. `board-capture.tar.gz` contained both
complete derived source snapshots and synthesis results, plus the original
standalone proofs, subset screen and failing mutation witnesses. It has been
removed (see [Removed archive](#removed-archive)), so this is now a partial
record and the capture can no longer be read back from this tree.

`board-evidence.json`, `board-readback.json`, `root-archive-readback.json` and
`root-external-baseline-verification.json` are unchanged. They still list the
removed archive's member hashes and record that, when it was present, Root
independently checked all 126 outer members, the 166/162 nested proof members
and both 1,854-file source snapshots. `root-external-baseline-verification.json`
also binds all seven baseline dependencies to the nextpnr ADD experiment
archive, SHA256
`52004db16988c99fb4abc13e257126ae8049178a224a5210a9c7ae53cd51a4b7`.
The archive-only verifier `readback-boards.py` was removed with the archive it
read.

These host synthesis/proof checks add zero GPU/full-route charge. No clock,
production recipe, toolchain lock, shared contract or acceptance gate changes.

## Removed archive

`board-capture.tar.gz` was removed from the repository on 13 November 2026.
Its four nested source snapshots (`board/` and `board-AP/`, each
`base-source.tar.gz` and `derived-source.tar.gz`) held the full ZX81 and ZX80
ROM images in `sources/misteross/cores/fes-zx81/rtl/zx8x.hex` and `zx8x.mif`,
plus the ZX81 character set in `expansions/qs_chrs.hex` and `qs_chrs_init.vh`.
FES has no verified permission to redistribute those ROMs (see the
[v1 firmware licensing record](../../../../../../docs/plans/v1-firmware-licensing.md#evidence),
Z0). The removed file was 89174365 bytes with SHA-256
`645d4d8b2c13737278d1fb8eb27e8f50047d7f625fc8c9254b380736d2b87007`. It
remains in Git history. Hashes and member counts quoted in this folder still
describe the original archive.
