# Native factory video and SGM — host validation, 2026-10-03

FES `3ffe989fbba6857b74f149112d1965156face870` produced the sealed native Coleco
shell, matching Direct/Scanlines parts and optional SGM part. Independent
composition proof passed, and two cold factory-image builds produced identical
128 MiB root filesystems. Hardware acceptance is deferred: the user confirmed
that `rb429-hil` owns Kit 2 and instructed this task to let that work finish.
The candidate image has not been deployed or captured.

## Selected artifacts

The factory selects `fes.coleco-native-video.parts/1`, map
`fes.coleco-native-video.socket/1` and marker
`fes.fabric.video.native-pixels` 1.0. Normal catalog Install imports the two
exact-shell video companions; library Play resolves the saved profile and
optional Coleco bus 2.0 SGM selection. A native shell requires a linked video
part. The existing audio, media and persistence contracts are unchanged.
SGM is imported separately when selected; the factory companion inventory
contains Direct and Scanlines.

| Shell identity | Value |
| --- | --- |
| Package ID | `936a14d37f178966868050a00005851d8619127425a08cc0cbd0dca59357961c` |
| BUILD_ID | `8d97fd1c36f5c79d08af074f972f96d0` |
| RBF SHA-256 | `863beeaae445b88b26c955e79113c6f6ea2a33c6bc1d296ac9ab838d7629cecb` |
| RBF size | 2,669,898 bytes |

| Part | Part ID | Archive bytes |
| --- | --- | ---: |
| Direct | `1942f52a6f9b8fc0a0b7d017cff3aa6b34d316e4f4335b7347f784d772e4da64` | 2,785,280 |
| Scanlines | `5fbc1de8600c25882e1d6e8fc2db77ff7fbc240a3e012b3339e171314ceb5624` | 2,785,280 |
| SGM | `a49942b5e71765a3c04c1f7452a597e940952b81f4b4c3d95fc6e7e3f8af54bf` | 2,693,120 |

The [build receipt](native-video-factory-2026-10-03/build.json) binds archive hashes, source,
recipes and authenticated compilers: HIP nextpnr `655f3833`, Yosys `e2d425de`
and Mistral `7ed06e21`. The shell passed at seed 3 and HeAP timing weight 2000.
The [prepared-byte binding](native-video-factory-2026-10-03/prepared-binding.json) verifies the
package and original companion archives against the proof inputs, including
canonical video index SHA-256
`2c5cacb765a1afd71837dbba0f6344473a3c35ac600325128b4a14f9fe82f66a`.

## Physical fit and timing

All separate shell/part routes completed with authenticated HIP and passed
all three clocks. Counts below cover the whole routed design; Fmax is in MHz.

| Routed design | COMB | FF | M10K | Pixel Fmax | System Fmax | Audio Fmax |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Shell | 4,967 | 1,756 | 171 | 381.242859 | 52.377960 | 197.316498 |
| Shell + Direct | 5,534 | 1,813 | 219 | 80.886520 | 52.377960 | 197.316498 |
| Shell + Scanlines | 5,560 | 1,814 | 219 | 91.382622 | 52.377960 | 197.316498 |
| Shell + SGM | 5,584 | 2,027 | 171 | 381.242859 | 52.377960 | 197.316498 |
| Effective constraint | — | — | — | 74.250069 | 52.224773 | 12.288032 |

Each native consumer adds 48 M10Ks within 55 reserved RAM sites. Retaining
all anchors leaves 316 usable video LABs; input-capacity lower bounds are 36
for Direct and 39 for Scanlines. SGM has 72 usable LABs and an input-capacity
lower bound of 37. Actual successful routes establish fit. The producer checked
153/154 active Direct/Scanlines clock pins against the pixel clock and 271 SGM
pins against the system clock.

Scaffold preparation preserves clock anchors, paired buffers and serialized
net records. Removing only their cells leaves shared constant-input wires at
sites the cart placer could reuse. Retention keeps placement consistent with
frozen routing; exact paired-buffer admission and paired boundary renaming
preserve the topology without weakening timing or containment.

The native CRAM fence `(124,1800,3906,3442)` and CPU fence
`(1769,32,2806,1800)` are disjoint. Direct changes 542,408 in-fence bits,
Scanlines 542,076 and SGM 37,427, with **zero strict outside-fence changes**
and unchanged non-CRAM headers. Native checks every decoded bit. SGM retains
its existing admission policy; these particular bytes also pass the stricter
all-bit comparison.

## Independent composition proof

The [verification receipt](native-video-factory-2026-10-03/composition-verification.json) passes
four cases. Production Go linking and independent Python overlays agree on
complete RBF bytes and decoded CRAM. Direct/Scanlines alone also match the
complete compiler RBFs; SGM combinations match the combined compiler previews.
No case changes bits outside the union of its declared regions.

| Case | Composition ID | Payload SHA-256 | Bytes |
| --- | --- | --- | ---: |
| direct | `bc7b88cb3cdf28c6f949c6abd4f32b1b6c9de82a3aa30b96194fb3a63933b3be` | `2d31b1744766b4e3a30e5c6e04da0460865715256489eccd179c9b572723ce9e` | 2,777,778 |
| direct-sgm | `00a732e23a52c03cdd21e39092b8ff4d615d5d0f8dfe62dfa1966a2c5581f08b` | `db718347ff97271b152b44b38d2b6ffcfb5df710fc3b9718b1f5a189cda3cb0b` | 2,791,563 |
| scanlines | `16e5fb76c1f958e9efefa04973efccc38ec4cd1873de2634f86a3247115c9533` | `12eb9e5857478324af9d651e71ed76984e3781dc85c62ce40dc3d67486f793ab` | 2,777,363 |
| scanlines-sgm | `d04bb677f219ba7c26ffccdc8ff1fd43347634557bfad12cf4e3dbb8a41fea0a` | `e0717dcd92fe347c88cc3bab874b26d43a01202975ff2ecda1a992e3cab274c9` | 2,791,147 |

The combined video+SGM payloads were not jointly placed and routed. The timing
evidence covers the separate shell/video and shell/SGM routes; composition
agreement does not establish physical video, audio or SGM execution.

## Software and image evidence

The [software receipt](native-video-factory-2026-10-03/software-tests.json) records 53 focused
producer tests, 78 parent recipe/catalog/inventory tests and `make check`
passing 17 generated consumers and 33 fixture copies. Producer tests ran on
the final diff before commit, with bytes verified identical to `3ffe989f`;
parent checks ran against the committed source. Independent source review
found no actionable admission, topology or importer mismatch.
All 44 recorded checks passed at `3ffe989f`: 43 jobs in
[CI run 37126851435](https://github.com/DeanoC/fes/actions/runs/37126851435)
and GitGuardian, as recorded in the
[derived CI summary](native-video-factory-2026-10-03/ci-summary.json).

Both cold image passes produced 134,217,728 bytes with SHA-256
`4c2de9f3edb28d71fd920eee66157b94b56d9b2dd9cf7adfea92aabd1c8c2628`.
[Image verification](native-video-factory-2026-10-03/image-verification.json)
passed structural checks, two-pass reproducibility and QEMU packaging smoke.
The [image binding](native-video-factory-2026-10-03/image-binding.json)
checks seven shell, video and selection files extracted from ext4 against the
proven originals and matches the image, verification and release receipts. Its SHA-256 is
`c82186d8afb1658da3e1006bb1ccb523b1483071f21e532c0254dd1ff0e0f45a`.

[Release](native-video-factory-2026-10-03/release.json)
`0.2.0-dev.native-video-3ffe989f` exported successfully after provisioning the
locked local boot payload. Its manifest SHA-256 is
`79494c7ae90957a112463e33ce004c515c73e1e4888340215d8ab6c444263742`;
[release evidence](native-video-factory-2026-10-03/release-evidence.json)
SHA-256 is `a84773c7207ac790e8852100b386bef95ec5579422629ea1fa3e282cc45326ad`.
The release records hardware as `not-run`. QEMU does not emulate FPGA video or audio.

## Hardware boundary and next acceptance

An [early admission diagnostic](native-video-factory-2026-10-03/existing-image-diagnostic.json)
used the existing image `86e4692059…`, whose
agent/runtime source was `b1207189947625d7df145dab6345ae6e109cf224`.
Catalog Install succeeded, but the target rejected the first native Direct
upload with HTTP 422 `INVALID_ARCHIVE`; the host reported HTTP 500. The old
agent accepts only raster video assets, and its C++ runtime also requires the
raster layout. Rejection occurred before new FPGA programming. There were no
captures, and cleanup recorded physical idle and a free lease. This observation
qualifies admission behavior only. Both the selected agent and runtime are
needed before exercising the new native artifacts.

The user's later instruction to leave Kit 2 with `rb429-hil` takes precedence;
this task performs no further target access. After that work releases the kit,
recheck ownership, actual boot/image identity and storage headroom before
acquiring a fresh lease and installing the exact release. Restore the baseline
observed at that handoff. The remaining acceptance is Direct, Scanlines and
Direct relaunch, then the same three cases with SGM, through ordinary
Install/library Play and persisted
settings. Capture all six outputs, check physical missing-part fallback and
rejection, then verify Stop, lease release and restoration. Software tests
separately cover corrupt selected-part rejection and valid Scanlines-only
inventories; the frozen hardware harness does not exercise those two cases.
Candidate-image hardware acceptance remains deferred.
