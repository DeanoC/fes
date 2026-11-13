# Nano native serial ownership receipt validation

The host decoder now requires one ordered SPL-to-EL3-to-EL1/Go probe. SPL's
reported owner must derive from the matched chain's expected seed and epoch,
and both pre-entry/post-removal snapshots must retain that owner, exact wire
tag/flags and generation. Mixed builds, missing/reordered/duplicate stages,
changed CPU state, failures and partial final lines cannot complete. It retains
partial UART progress. See [evidence](evidence.json).

The previous runner required the raw secure-provider bridge word to equal
`0xf`. The installed firmware checks only the required four bridge bits, and
the observed startup word includes another set bit (`0x4f`). The decoder uses
the same four-bit mask for raw reset words; clearing a required bit rejects.
Public snapshot flags remain exact. No firmware or target policy was changed.

The pending host runner consumes this decoder before sending its sole
`fesboot` command and when evaluating the resulting probe. A persistent
exclusive-create attempt record precedes device opening and JTAG. Existing,
interrupted or concurrent attempts cannot replace it. Source, decoder and
parked USB connection bindings are rechecked before the run. Both the matched
v12 artifact inputs and freshly recomputed cold-chain identity still match;
the verified spare card remains valid.

Nine decoder tests cover every UART fragment boundary, both permitted primary
CPU types, wrong/stale owners, flags, reset masks, source identity, sequencing,
rejections and capture bounds. All six affected checks pass. Nine fixtures
execute the actual runner's entry/acceptance code with no device I/O; six more
exercise its real attempt registration, interruption/concurrency and check-only
rejection path. These are host-only results. The [earlier initial parking
trace](../2026-11-01-de25-initial-parking-v2/README.md) remains the latest board
execution. The native run is pending physical card-swap confirmation. DDR,
secure callbacks, native peripherals and real core loading are not qualified
by this decoder.

Receipts, helper snapshots, source delta and software logs are retained under
`/home/deano/fes-retained/de25-nano-native-trace-validation-20261101/`.
