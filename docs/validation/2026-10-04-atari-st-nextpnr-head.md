# Atari ST DeanoC/nextpnr head requalification

Pin: DeanoC/nextpnr `3d4a5b352b4edb478b744b82cc61333353751a80` (main, merge of
#115), with Yosys `886afa63953e97407153e9f4aae25fcedb639696` and Mistral
`7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039`. Those are the DeanoC heads for all
three tools. The pin lives in `sources/misteross/toolchains/atari-st.lock`.
`fes.ramtest` and `fes.menu` keep `toolchains/ramtest.lock` (nextpnr
`655f3833`).

## Results

The normal producer ran (`make build-fes-atari-st ST_VIDEO_OUTPUT=direct`,
authenticated HIP stack gfx1100;gfx1201, device `5CSEBA6U23I7`) at source
`fd45b2be7`. That source contains main `dfe13469`. The Atari RTL inputs are
unchanged from the [2026-10-03 qualification](2026-10-03-atari-st-integration.md).
Other selected inputs differ only in shared scripts, `fes-common` native-video
files from #441, and the lock. The producer searched its committed seed order
(4, 5, 2, 1, 3, 6, …) with timing weight 2000 and criticality exponent 5. It
kept the first candidate that passes all three clocks and default timing repair.

| Seed | Pixel MHz (req 74.250) | System MHz (req 52.225) | Audio MHz (req 12.288) | Result |
| --- | ---: | ---: | ---: | --- |
| 4 | 66.08 | 61.22 | 227.48 | fail (pixel) |
| 5 | 71.28 | 60.51 | 234.36 | fail (pixel) |
| 2 | 67.44 | 63.82 | 234.96 | fail (pixel) |
| 1 | 67.22 | 65.98 | 223.11 | fail (pixel) |
| 3 | 69.74 | 53.26 | 243.84 | fail (pixel) |
| 6 | **75.29** | **56.65** | **272.85** | **pass, sealed** |

The winning seed-6 shell has BUILD_ID `43ece831982c1e6e416cbf9bc4a6e2e9`. Its
RBF is 2,824,759 bytes, SHA-256
`d9ec1614430ca144ab72ea1bdbdf2caa9907d69ad14a429706bc34d55ef82e78`, and the
sealed package ID is
`7d213b4148a1ab659c61bd398c809c703d0c8a592309b7aa47782e0b1ab5e20a`. Resources
are 12,780 COMB, 6,082 FF, 201 M10K and 2,361 BUF. The producer's
RAM-footprint, socket-boundary, clock and seal gates all passed (`status: pass`).
The [build summary](2026-10-04-atari-st-nextpnr-head/build-summary.json) and
[seed ranking](2026-10-04-atari-st-nextpnr-head/qor-ranking.json) are retained
with host paths redacted.

Compared with `655f3833` (seed 4 at 72.11 MHz failed, seed 5 at 74.61 MHz
passed), the head improves system and audio slack and routes each seed in about
3–4 minutes instead of about 9. However, it regresses pixel timing on the
`st_video` active-resolution → renderer plane/staging paths, which are about
1–2.6 ns more routing delay. Five of six seeds now miss. That regression is
reported as [DeanoC/nextpnr#117](https://github.com/DeanoC/nextpnr/issues/117).
The earlier seed-4 miss is [DeanoC/nextpnr#116](https://github.com/DeanoC/nextpnr/issues/116).

The probe card was not rebuilt against this shell. No FPGA was programmed, and
no hardware acceptance follows from this record.
