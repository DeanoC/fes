# Rejected PIC event-decode diagnostics

Both source-equivalent event rewrites lose their standalone LUT-depth benefit
in the actual board synthesis. The original PIC RTL remains selected. Neither
candidate was routed or run on hardware.

[The board analysis](BOARD-ANALYSIS.md) records all eight service-register
paths, complete PIC input-cone inventories, unchanged RAM/PLL contracts and
the exact functional-proof boundaries. `board-capture.tar.gz` contains both
complete derived source snapshots and synthesis results, plus the original
standalone proofs, subset screen and failing mutation witnesses.

Run `python3 readback-boards.py` here for archive-only verification. Root
independently checked all 126 outer members, the 166/162 nested proof members
and both 1,854-file source snapshots. `root-external-baseline-verification.json`
also binds all seven baseline dependencies to the already committed nextpnr
ADD experiment archive, SHA256
`52004db16988c99fb4abc13e257126ae8049178a224a5210a9c7ae53cd51a4b7`.

The archive SHA256 is
`645d4d8b2c13737278d1fb8eb27e8f50047d7f625fc8c9254b380736d2b87007`.
These host synthesis/proof checks add zero GPU/full-route charge. No clock,
production recipe, toolchain lock, shared contract or acceptance gate changes.
