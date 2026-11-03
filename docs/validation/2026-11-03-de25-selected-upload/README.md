# Nano SD-selected package upload and native load/return

TRWU8CBL passes the bounded four-file Ethernet update and matched native
SD-selected loader diagnostic. The uploader delivers the native FIT, package,
retained metadata and selection list in one owned recovery session, then freshly
reads and hashes all four files. The selected native frontend loads the sealed
ZX81 package, runs and holds its CPU, returns ownership, reopens SD and freshly
retains the next selection before a second load/run/return.

The two physical choices name the same package. This qualifies that frontend
and its exact matched image; different physical packages, ordinary Nano runtime
admission, native Tenfoot and physical USB/video/native-network acceptance remain
open. Metadata supplies compatibility assertions, not authentication or live boot
proof. Failure-path results below are software/model evidence.

## Exact source and artifact scope

- Functional uploader: `9657ff54ff24f995d8f999322e52b9a89285053d`, based on
  `f1cc8065a27c3db97df32bddb997770b9eba7155`.
- Native implementation/build: `068495f0cf1d6011c31447a63bde4b70e7056702`;
  boot ID `145213088b9b4dbb`.
- FPGA producer: `c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`;
  build ID `dda236646bb868e63c72bd0e777f3dd6`.
- Native SOF SHA-256:
  `b11468a4643e9c310238903d64ccaf87bbda729d12301da462253a07ffb214aa`.
- Native package ID:
  `b2353f540cc81b9896db86867248880aceffce8c81314042b02317f76f947f37`.

All 118 recorded native Git blobs and 1,298 pinned module/SDK inputs match both
the compiled native source and tested uploader checkout. The uploader change
touches no native build input. The existing matched SDK/entry/BL31/U-Boot/SOF
candidate and its eight software scopes are reused from the
[selected-program checkpoint](../2026-11-03-de25-selected-programs/README.md).
No new native cross-build, FPGA compile or 3090 Quartus job ran for this step.
Any future 3090 job must hold its existing shared Quartus lock throughout.

| Purpose | Fixed SD destination | Bytes | Payload SHA-256 |
| --- | --- | ---: | --- |
| Native boot | `FES/UPLOAD.BIN` | 6,762,676 | `5a3e8b2a3347294bcf3a1312c34a2b7138ef0b3725c7618966c1c350590df2a9` |
| Package | `FES/PKGTEST.BIN` | 1,649,664 | `75a2cdfb38f9e61f41ebe2554ca30de6e8e367246d294d3d8f00c514dbf47b73` |
| Metadata | `FES/PROGRAM.JSN` | 2,118 | `6fea347ab1db35f9b75e01fd9b0a394506d97a668774109791327f0c877f5ebc` |
| Selection | `FES/CORESEL.JSN` | 514 | `fb8b76dd6b802e9a06c6834a59547b6c07e8708a3f070d4bc303da3f05c9285c` |

The public uploader bounds metadata to 16 KiB and selection to 8 KiB. Each file
retains the protected recovery FIT/manifest, card/prefix, MAC/PHY, scratch-memory,
complete-transfer, SD readback and byte-comparison gates. All four final rereads
are read-only and belong to the same live console/session. A failed proof retires
the session and prevents native advance.

## Software verification

All six affected software scopes pass on uploader source `9657ff54ff`:
selection-policy tests, affected-runner tests, committed/working whitespace,
parent regressions and generated-consumer consistency. The exact parent selection
contains 1,345 tests: 1,343 pass and two explicitly skip for their environment.
[The skip record](environment-skips.json) names the absent unrelated locked
U-Boot cache and the macOS-only statfs layout on Linux.

The actual four-file helper executes over the real `UploadSession` and stateful
card/LMB model: 46 success/failure cases pass. Its saved successful receipts feed
185 content/reference rejection cases, including consistently rehashed damaged
metadata and selection receipts. The actual outer controller passes 164 cases
covering recovery, upload, native, phase and cleanup failures; rejected uploads
never reach native programming. Eleven private transport/parser/phase tests and
helper compilation/global-name checks pass. An initial fixture-only failure
incorrectly retained its successful recovery input in the missing-recovery case;
the corrected fixture removes it and the full controller set passes. No board
attempt occurred during that correction.

## Physical result

| Observation | Measured seconds | Bound seconds |
| --- | ---: | ---: |
| Initial SD selection/retention | 59.34 | 100 |
| First configuration | 13.3440 | 20 |
| First core observer | 18.52 | 45 |
| Returned SD/fresh retention | 55.16 | 100 |
| Second configuration | 13.3466 | 20 |
| Second core observer | 18.29 | 45 |
| Whole wrapper | 357.05 | Inner experiment 800; wrapper 900 |

The native trace verifies loaded BL31/EL3/EL1, owned source, live CPU mapping,
per-load descriptor publication, configuration completion, requested FPGA
identity, computer command/CPU fetch observations and complete master stop.
Activation/stop/return generations are 4/5/6 and 9/10/11. The independent audit
reproduces the decoder result from all 16,996 raw bytes and rejects 32 damaged
captures. Raw SHA-256:
`f80b7bbe0257e2540b0d5c0de0661360953eed1203a8f0d04688ef1211a3a9da`.

JTAG 15 MHz was restored and read back before the owned private service stopped.
Exact USB permissions, stopped process identity, free UART and continuing claim
were independently verified. TRWU8CBL is parked after return generation 11 with
SD contained and data held. The spare stays in the exclusively claimed board;
the factory card remains safe, with no requested factory restore. No card swap,
recovery upgrade or native SD write was needed. Recovery must use the matched
`out/nano-version-suffix-20261103/recovery-kit/network-recovery.sof`, with the
spare's current `phy-prepare` FIT/manifest. Preserve `FES/R1103.ITB` and
`FES/R1103.JSN`. This consumed trial must not replay.

[Summary](summary.json) and [independent physical audit](hardware-evidence.json)
retain the exact scope and input hashes. Full helpers, logs and traces are in
`out/nano-selected-upload-20261103/`; independently restored source and the
member-verified session archive are retained in
`/home/deano/fes-retained/de25-selected-upload-20261103/`. The archive explicitly
references the verified selected-program candidate and its earlier frozen-input
archives. No PR handoff or merge occurred; ownership remains `lane:deano`.

The next functional step is a persistent native request-driven launch/return
service using these guarded operations, with different eligible physical package
choices. Native input/display and ordinary runtime integration still need their
own implementation and physical evidence. The full core-load goal remains active.
