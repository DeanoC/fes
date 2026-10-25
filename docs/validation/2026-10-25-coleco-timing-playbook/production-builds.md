# Coleco production builds and seals

- **Date:** 2026-10-25
- **Evidence class:** host production build and seal; Kit HIL is pending.
- **Source revision used for every artifact:** `c1b21cb5bd744da1145648f0e116ecbef47c924d`
- **Target:** `5CSEBA6U23I7`

The Coleco toolchain lock SHA-256 is
`cb2c2809dd1b9687fbb1f50d3c6ea3a8e01ebf328b1ea29c8d567854c93d0fdb`.
It selects nextpnr-mistral `bec82e36d436acec7cbeb248725acfde72b8f220`
(SHA-256 `203e2c70364d58226ac075a9f99eaf7875ec73edebccb73e386130e9b3ed89f4`),
Yosys `5391eeb1e91b38a3d0e96d04f24cf921743c9c78`
(SHA-256 `50f85e8fdc078226b726bbfe8b8c9f199788152c9898f5b4e1ed4ffac4e11eca`),
and Mistral `8fcc4cb41c51f8918f1d3ad70def2febcbf20d8f`
(SHA-256 `ca28a9c8663b0e5b7f22ea7e3a030ee886d16f69dfc98f92739d55eb8656b296`).
The nextpnr pin includes #296 and #299; the Yosys and Mistral pins remain
unchanged. All production routes used authenticated live HIP GPU leases.

## Timing and sealed identities

Fmax values below are the final structured timing-report values. The requested
clocks were `pixel_clk` 74.250 MHz, `system_clock.clocks[0]` 52.225 MHz, and
`system_clock.clocks[1]` 12.288 MHz.

| Build | Route seed and settings | `pixel_clk` MHz | `system_clock.clocks[0]` MHz | `system_clock.clocks[1]` MHz | Result |
| --- | --- | ---: | ---: | ---: | --- |
| Native video shell | 3; weight 2000; critexp 5 | 82.068115 | 52.427391 | 158.755356 | Sealed, all clocks pass |
| Direct part | 13; weight 300; default critexp 2 | 76.528656 | 52.427391 | 158.755356 | Sealed, all clocks and slot checks pass |
| Scanlines part | 13; weight 300; default critexp 2 | 76.569679 | 52.427391 | 158.755356 | Sealed, all clocks and slot checks pass |

The shell's registered `first-pass` recipe stopped after its seed-3 route
passed. The native video parts used `native_video_parts.PLACER_SEED=13`.
Scanlines passed at seed 13, so no seed-1 retry was needed.

| Artifact | Sealed ID | Workspace output path | Payload SHA-256 |
| --- | --- | --- | --- |
| Native video shell package | Package `30fada8472af52df38d356106c4656c0b7f4ce96567163b3b0897db1bdb1d5b2`; BUILD_ID `92b421570e67e387dc705cff6978dfd4` | `sources/misteross/build/packages/30fada8472af52df38d356106c4656c0b7f4ce96567163b3b0897db1bdb1d5b2` (producer output: `sources/misteross/build/fes-coleco-native-video`) | `ee19bdaa7bfc5e076e8fed8cebda5b30ab805fa2f8cb728b407a9c9d4ed12a6e` |
| Direct video part | Part `d168b95de20b534022a4d74b0c2c1e808def245ee62653e91117ec8501e40e0b` | `sources/misteross/build/video-parts/direct/c5e7621810941629e73d07d8e6de63ac011920fdfaf5bcffe3f30ba294b0e2d1/d168b95de20b534022a4d74b0c2c1e808def245ee62653e91117ec8501e40e0b.tar` | `d7c9a3708e90d62913d9131237f4348c29b0eb18bfd351c5407d903559dc1dd3` |
| Scanlines video part | Part `4924cd2e6b428e3c0ec6ab2852a607e02fef9a4296ac5fc92849003b04441592` | `sources/misteross/build/video-parts/scanlines/771f857641b57070c8b887c2e078713d20e2a73db183f8dcd892c896d2f03c2e/4924cd2e6b428e3c0ec6ab2852a607e02fef9a4296ac5fc92849003b04441592.tar` | `67d462df97a543d2e86a26439f302b8e404938d3e7917d8cbb4e1864db567f5f` |

Both part manifests reference shell package `30fada8472af52df38d356106c4656c0b7f4ce96567163b3b0897db1bdb1d5b2`, BUILD_ID `92b421570e67e387dc705cff6978dfd4`, and source revision `c1b21cb5bd744da1145648f0e116ecbef47c924d`. The two part CRAM diffs are wholly inside the Coleco native-video slot; their route contracts passed.

## HIL handoff

These are fresh host build and seal results, not hardware acceptance. Kit HIL
should use the exact shell package and Direct or Scanlines archive listed
above. No kit was accessed during this work.
