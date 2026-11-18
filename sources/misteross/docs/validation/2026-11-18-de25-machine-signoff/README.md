# DE25 Spectrum and Atari ST: producer signoff

2026-11-18, Quartus Prime Pro 26.3.0 Build 114. Evidence class: host-only.
Nothing was programmed; neither result is a hardware acceptance.

Every run is the package producer (`boards/de25nano/producers/build_*_package.py`),
which compiles, runs the Design Assistant signoff and the fitted input,
asynchronous-input and audio-clock reviews, and packages only when all 85
signoff rules have no unreviewed failure, every reported timing check is
non-negative and an independent repeat compile gives the same RBF.

The first compiles are recorded in
[Spectrum](../2026-11-17-de25-spectrum-compile/README.md) and
[Atari ST](../2026-11-17-de25-atari-st-compile/README.md).

## ZX Spectrum: packaged

Source `a5e864578`, the branch with main merged in. The 48K model was built
on `fes-build-3090` (chip-30x0-ai-dev) and the 128K model on Powerboat.

| | 48K | 128K |
| --- | --- | --- |
| Build ID | `332b901530a405a79e00a8e2c4e5b6c7` | `7f0f362b64bea70f76964e8e99c0cdc3` |
| Package | `033642ab4950a3af9cdf34d0cc8db3c659b6904e992fd83c3889c6b6b3dd4846` | `766684af13230bc858ce81fbb620e21ae665bd29584cf46f6bc40b50f28a87ca` |
| `.fcore` SHA-256 | `eff1c2380b84655a77e5df3f3a60e96a5e40b13a48012ea8c4c1dc3ba9b7e327` | `b1aed624b6a77071aa06696e66e6128f9f208cdfab716dd00aedc94c11ef100d` |
| `core.rbf` SHA-256 (first and repeat compile) | `94eb274bb392e3b1610003712d7b8a44e0d78eceeab46f5261ff3a1d359c21f4` | `f77812be159bc440d6966fb7b1db77731be2afcbfa0b4097abc1808ee590d81e` |
| Worst setup slack, machine clock | 2.169 ns | 2.132 ns |
| Worst setup slack, pixel clock | 1.491 ns | 2.043 ns |
| Worst hold slack | 0.001 ns | 0.000 ns |

Both are `de25-spectrum-package-candidate` with `hardware_acceptance` false.

Signoff, each model: three rules fail and all three are the shell's reviewed
findings (RDC-50003 once, TMC-20011 twice, TMC-20012 five times). No
unreviewed failure. All 63 timing analyses pass.

The audio-clock review ran under the new policy
`de25-machine-pll-audio-clocks-v1`, which takes the audio master from the
machine PLL in place of the stock audio PLL. Nobody has reviewed that policy
yet.

Both models first packaged at `c93ff9543`, before main was merged in (48K
package `ce5d11bb…8240d`, 128K `9e3e826f…06e7`); these builds replace them.

## Atari ST 520ST: packaged

Source `7a78a57dd`, Powerboat. The commits after it change only this record,
the READMEs and a test fixture.

| | |
| --- | --- |
| Build ID | `addd94763028f74756693bc34475dc57` |
| Package | `ba37b83dc4ff55990be1a02ef6e2fa9469ce8125b1f108f7a4807e5581d04bf2` |
| `.fcore` SHA-256 | `b4ef8f51fd69ea5672db5b77f70b4983719c2cb02e81e9abf6b838c7dace0582` |
| `core.rbf` SHA-256 (first and repeat compile) | `8f1c982ceb0a0e3008242b25204175c1130e1e08b201975b7a32c8a6666f2e1e` |
| Classification | `de25-atari-st-package-candidate`, `hardware_acceptance` false |
| Logic (ALMs) | 15,423 / 46,800 (33%) |
| Block memory bits | 5,203,400 / 7,331,840 (71%) |

Signoff: the same three reviewed rules fail and nothing else. All 61 timing
analyses pass: worst setup slack 2.092 ns on the machine clock and 2.127 ns
on the pixel clock, worst hold 0.001 ns.

The run before this one, at `34c03780c`, was refused on TMC-20602 (Low,
registers with high timing path endpoint tension): `core|machine_reset`
reached 7,741 registers and the 68000's `oReset` 5,745, across the whole
machine. The processor and bus, the I/O chips and the memory and video
raster now have a reset register each, loaded from the same logic on the
same clock, and the rule reports no register.

## What changed to get here

- SDRAM and I2S pin delays from the ISSI IS42/45VM16320G and ADV7513
  datasheets, with zero external flight time as the HDMI pins use.
- The ST samples SDRAM read data on the falling machine clock edge.
- Clock crossings in both machines' video and in the shared GP crossing are
  scalar toggles through two-stage synchronizers plus held bundles loaded on
  the synchronized strobe; no clock group is cut. The Spectrum's frame banks
  cross as codes whose registers power up zero.
- The audio hold takes the scanout clock fault through its own synchronizer.
- The ST's processor, I/O chips and raster each read their own copy of the
  machine reset and of the 68000's RESET output.
