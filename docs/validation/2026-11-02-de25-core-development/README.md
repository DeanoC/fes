# DE25 one-core developer preparation

Implementation and software-validation source:
`c1aa5e40bcb96a3e809bd40c32dd135e3e942a58`.

The ordinary one-core command now selects the existing board-scoped recipe:

```sh
mkdir -p out/core-dev
make core-dev CORE_DEV_ARGS='prepare --board de25-nano --core fes.zx81 --output out/core-dev/de25-zx81-001'
```

The Python `prepare` API accepts the same explicit keyword. Its default remains
DE10-Nano. DE25 supports the registered MENU and BRAM ZX81 Quartus Pro recipes.
Unknown boards or unsupported board/core pairs fail before source staging;
producer rejection preserves failure and cannot switch to another recipe.
The existing resolver authenticates the selected board and source. Archive,
selection/provenance, media bounds and package admission stay in their existing
owners. No new receipt format or separate target allowlist is introduced.

## Software evidence

An independent clean source snapshot on `fes-build-3090` passes the actual
`make test-changed` plan against prior publication `98d877295` in 971.0 s.
The parent suite runs **1,393 cases**, with **1,385 passes and eight skips**,
in 963.8 s. It includes all 33 core-development cases: default DE10 behaviour,
DE25 CLI selection, actual package parsing and archive reconstruction,
unchanged original cache identity/source provenance, partial-output cleanup,
unsupported selection and rejection without producer fallback.
The two planner suites also pass 62 cases.

The separate `make check` passes in 6.9 s: 40 consumers, 42 fixture copies and
zero copied source pins. Document links pass. The actual Make command rejects
DE25/Pong before creating output; the actual CLI rejects an unknown board.
Their expected nonzero return codes are recorded as refusal controls in
[validation.json](validation.json). The complete job passes in 978.3 s.

The [unpatched prepared-candidate reader](acceptance-reader.json) also accepts
the reconstructed DE25 ZX81 archive and its exact source-selection provenance
with the ordinary registry. Its source/cache discovery is explicitly supplied
by unit fixtures; the package parser, archive reconstruction and candidate
reader execute. The fixture payload is test text, not a bitstream. No host
acceptance runner or target executes.

All 577 ZX81 and 377 MENU product inputs, plus 104 internal and 65/58 combined
client-model inputs, remain byte-identical to producer `658c97ac4`. The
[previous reset/capture builds and native/AArch64 model proofs](../2026-11-02-de25-zx81-reset/README.md)
remain evidence for those inputs. This frontend change selects no FPGA test
lane and performs no new FPGA build.

## Retention and remaining acceptance

All 33 software files restore and verify independently from an 86,794-byte
archive, SHA-256
`c6e37069866b4926fbdaea12df454a9a10431299efa79d95776f8fb33ed52429`.
The Git source restores with independent objects and no alternates. The actual
candidate reader accepts the restored, explicitly unqualified fixture again;
all product/model source hashes still match. [Retention](retention.json)
records this check, which passes in 2.1 s.

The real DE25 producers still require eligible timing/signoff and independent
identical-RBF reproduction before package export. Neither current canonical
candidate is admitted. Firmware pairing, an admitted factory/card and complete
native ABI3 MENU/catalog/ZX81 launch, return, relaunch and reboot acceptance
remain open. No board, UART, kit network, capture, allocation, FPGA, SD, kernel,
userspace, QSPI or power operation occurs. Existing claims and baselines remain
unchanged. Delivery remains in ready PR #1312, `lane:deano`, with no merge or
handoff.
