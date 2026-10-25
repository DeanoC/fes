# 2026-10-24: Spectrum timing margin after #1001, and a kit A check

Record of the four Spectrum model packages after two timing changes on top
of `main` `e93b15b2`. It records routed, timing-closed bitstreams and the
host simulations behind them, and a kit A check of the sealed archives with
kit software built from `main`. It is not an image or a release.

## Why

Built from `main` after #1001, the +2A/+3 closed only on its recipe's sixth
seed (seed 6: system 53.17/52.224 MHz, pixel 76.34/74.25 MHz). Across seeds
5, 4, 2, 1, 3 and 6 its system clock reached 48.46 to 53.30 MHz and its pixel
clock 64.21 to 89.45 MHz, and seed 2 did not finish routing. Its worst paths:

- **System clock:** the tape deck's take decision, which comes out of its
  33-bit position compares, fed the carry-in of `fes_media_ddr`'s 25-bit
  end-of-stream compare: two carry chains in series (20.6 ns on seed 5).
- **Pixel clock:** the frame store spans many M10K blocks, and its output
  mux shared a pixel clock with `next_word` (about 10 ns of routing on
  seed 4).

## Changes

- `fes_media_ddr` computes "more bytes after this clock" for both outcomes
  from its own position and lets the consumer's take select between them, so
  the take no longer leads a carry chain.
- `spectrum_video` registers the frame store's muxed word once more and
  captures it one fetch phase later. A word is still captured five pixel
  clocks before it is shown.

## Artifacts

Source FES `ec33298d` (`sources/misteross`), producer
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`, unchanged),
HIP router, first-pass placement search in the recipe's seed order. Every
model is constrained to a 52.224 MHz system clock, the 74.25 MHz pixel clock
and a 12.288 MHz audio clock.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.4.0 | 5 | 57.46 | 87.77 | 214.78 | 9,303 | 4,727 | 129 (16 TDP) | 3 |
| `fes.spectrum128` 0.2.0 | 4 | 65.28 | 87.27 | 218.44 | 10,229 | 4,989 | 225 (32 TDP) | 3 |
| `fes.spectrum-plus3` 0.2.0 | 4 | 66.23 | 94.24 | 142.82 | 10,308 | 4,992 | 257 (32 TDP) | 3 |
| `fes.pentagon` 0.2.0 | 5 | 66.32 | 95.64 | 194.86 | 10,195 | 4,975 | 225 (32 TDP) | 3 |

- `fes.spectrum` 0.4.0: package `226617fee4b377afe07cf5ae31290331b1d4034368c2d88506c0994d4a617723`, build `96d48311510b4f6b5f2eef109528bbe0`, RBF sha256 `3e7af99249a9acddbdff5a0c7fd6348a312a0dcdacf44beb328a02af296ad4a4` (2,635,916 bytes)
- `fes.spectrum128` 0.2.0: package `c651c074455e009df2bccf1529534495e7a9f1bbbceb46f95ce619a92665745b`, build `f24679738316e1f5315de5275e144350`, RBF sha256 `fd2e82189b16206777aba79573fb50ebb69a773436f7b3011ee7d16c035a1935` (2,988,459 bytes)
- `fes.spectrum-plus3` 0.2.0: package `15abb75ca5f334332743522e1603d5b421d0ea96dda9761a28a1d1859d488211`, build `ce4de7dae8868c4ff81276f91391b056`, RBF sha256 `203402079e39856c53b3ac124044a2157b70ca4d4d52a2399936602065f38e08` (3,026,649 bytes)
- `fes.pentagon` 0.2.0: package `5e0cdec50936ffd4f11eecc1977ccbdfd4bd9e907fd9bfd7834621a88f3be658`, build `60136f4da73220efb46990bf8fc75856`, RBF sha256 `44a80d507108ee3647f2fe7bc5174ada9fd67058229a132de3310f6770930143` (2,967,458 bytes)

The 48K and Pentagon closed on their recipe's first seed (5). The 128K and
+2A/+3 closed on their second (seed 4); their seed 5 did not finish routing
in the 600-second lease. Every system clock now has 10 to 27% margin and
every pixel clock 17 to 29%.

The ZX81 uses neither changed module; its seal is unaffected.

## Host checks

`make sim-fes-spectrum` passes: all four machines with unchanged T-state
markers (48K 14950, 128K 14960, +2A/+3 14955, Pentagon 14946), the video
check (31 source frames in 40 HDMI frames, no tearing), the board bench and
both tape-deck reference expansions.

## Kit A check

Run `kswA1` (05:22 to 05:36 EET, 2026-10-25) ran the four archives sealed
above on kit A. Its running image `4db74864` was not changed. The kit SDK
was published with `make kit-sdk` (#1063) from `main` `0f3b35c9`, and kit
software built from that commit (bundle archive `0f1f757a…`, installed as
`115494f3…`) was installed and activated over the baseline `35d58489`. The
private host, which converts tapes, was built from the same commit. Each
archive was imported, checked for compatibility, linked to its locally held
ROM, launched, seen running on three status samples and stopped. Each model
loaded the turbo-loader `.tzx` of the
[2026-10-22 record](2026-10-22-spectrum-tape-ddr.md) from DDR with `LOAD ""`
(48K) or its menu's tape loader, and a grabbed frame was checked for the
eight stripes. A 20-second capture then counted HDMI relocks, and `0xBA` was
read without writing it.

| Core | Archive | Build seen on kit | Session | Turbo `.tzx` (stripes 0–7) | HDMI relocks in 20 s | `0xBA` |
| --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.4.0 | `226617fe…` | `96d48311…` | pass | pass | 0 | `0x20` |
| `fes.spectrum128` 0.2.0 | `c651c074…` | `f2467973…` | pass | pass | 0 | `0x20` |
| `fes.spectrum-plus3` 0.2.0 | `15abb75c…` | `ce4de7da…` | pass | pass | 0 | `0x20` |
| `fes.pentagon` 0.2.0 | `5e0cdec5…` | `60136f4d…` | pass | pass | 0 | `0x20` |

The software was restored to `35d58489`, the image fingerprints matched
before and after, and kit A was released at 05:36.

## Not established

- Routability: some seeds still do not finish routing in the 600-second
  lease.
