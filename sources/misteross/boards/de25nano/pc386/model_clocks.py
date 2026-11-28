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
    # 486SX: C0 = 28, 111.904762 MHz (docs/validation/2026-11-28-de25-486sx-clock2), with the
    # eight-cycle presenter re-arm (derive.py). Its PLL IP is byte-identical to the 486DX's at the same C0.
    # Its generated Qsys parent is byte-identical to the 486DX's reviewed C0 = 28 parent (parent-c28.txt there).
    "486sx": {"c0": 28, "pll_ip": "machine_pll_486sx.ip",
              "pll_ip_sha256": "b6bfea6bab42fc4acd15ec6727204b4a5825e256ef383cd81ad2cad2d66c70a2",
              "parent_sha256": "0cccfdc88260c9bbde8c5fa1cbfbe1d8d0b689b69808ab7fd912ec42fae12435"},
    # 486DX: C0 = 28, 111.904762 MHz. The reviewed Qsys parent differs
    # from C0 = 34 in the window interconnect's module name.
    "486dx": {"c0": 28, "pll_ip": "machine_pll_486dx.ip",
              "pll_ip_sha256": "b6bfea6bab42fc4acd15ec6727204b4a5825e256ef383cd81ad2cad2d66c70a2",
              "parent_sha256": "0cccfdc88260c9bbde8c5fa1cbfbe1d8d0b689b69808ab7fd912ec42fae12435"},
}
