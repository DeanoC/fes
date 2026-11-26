"""Each DE25 pc386 model's machine clock, and the reviewed pins that go with it.

The board's machine PLL (one per board, on CLOCK1_50) has one VCO for every
model, 50 MHz x 188/3 = 3,133.333 MHz. Its output 1, VCO / 255, is the
12.287582 MHz audio master clock (-34 ppm). Output 0, the machine clock, is
VCO / C0, and each model names its C0. docs/validation/2026-11-24-de25-486sx-fmax
measured which C0 close.

For each model:
- c0: output 0's divider.
- pll_ip and pll_ip_sha256: the model's PLL IP (pc386/<pll_ip>), output 0 at
  VCO / C0 to six decimals. scanout/audio_reference.py reviews the audio master
  against this digest.
- parent_sha256: the reviewed digest of the Qsys peripheral-subsystem parent
  that the model's clock generates. Qsys names the window's machine-clock
  interconnect module by a hash that includes the clock rate, so the parent
  differs between clocks in that name alone. The shared FIFO reset derivation
  checks it.

pc386/build.py and scanout/audio_reference.py both read this file, which is in
pc386's source closure only. It holds data alone, so either can load it by path.
"""

VCO_NUMERATOR, VCO_DENOMINATOR = 50_000_000 * 188, 3
AUDIO_C = 255

MODEL_CLOCKS = {
    # 486SX: C0 = 34, 92.156863 MHz (docs/validation/2026-11-25-de25-486sx-9216). Its parent is the
    # reviewed 61.44 MHz parent (10defc9a...) with the window interconnect's module name
    # ..._1920_xi5gepi changed to ..._1920_e7vlmii and nothing else (parent-review.txt there).
    "486sx": {"c0": 34, "pll_ip": "machine_pll_486sx.ip",
              "pll_ip_sha256": "04448872b6e9992a9ee70c6a4e11b1678a433d1743908aca38eb960c9580173e",
              "parent_sha256": "fefb39d68d09b2fad37d35be5d2686d557b9f5c8c97d2eb41bbd80400a31f7e7"},
    # 486DX: C0 = 30, 104.444444 MHz. The reviewed Qsys parent differs
    # from C0 = 34 in the window interconnect's module name.
    "486dx": {"c0": 30, "pll_ip": "machine_pll_486dx.ip",
              "pll_ip_sha256": "2236a6c50d119d3f14775d3cc194bce7f55e631acd0b4fd80a64ad9a5c90c9a2",
              "parent_sha256": "122e45081148f52a0d779568049425712dbdbc64d87da1642603a87ce347a657"},
}
