# DE25 Spectrum and Atari ST: producer signoff

2026-11-18, Quartus Prime Pro 26.3.0 Build 114. Evidence class: host-only.
Nothing was programmed; neither result is a hardware acceptance.

Both runs are the package producer (`boards/de25nano/producers/build_*_package.py`),
which compiles, runs the Design Assistant signoff and the fitted input,
asynchronous-input and audio-clock reviews, and packages only when all 85
signoff rules have no unreviewed failure, every reported timing check is
non-negative and an independent repeat compile gives the same RBF.

The first compiles are recorded in
[Spectrum](../2026-11-17-de25-spectrum-compile/README.md) and
[Atari ST](../2026-11-17-de25-atari-st-compile/README.md).

## ZX Spectrum 48K: packaged

Source `c93ff9543`, `fes-build-3090` (chip-30x0-ai-dev).

| | |
| --- | --- |
| Build ID | `8c6b60c30febf90f4680b4179e387abc` |
| Package | `ce5d11bb9850e6086511148062234d62c71a0930f1d9192fa6d9be35ced8240d` |
| `.fcore` SHA-256 | `6a091240b6e40c0b29345edb83f775acd225cf41d439d2955a08272364007bf1` |
| `core.rbf` SHA-256 (first and repeat compile) | `4b9ee148ebe599c70a25443d07027852c45766da2b8987c027d18720afe13af6` |
| Classification | `de25-spectrum-package-candidate`, `hardware_acceptance` false |
| Logic (ALMs) | 10,272 / 46,800 (22%) |
| Block memory bits | 3,484,672 / 7,331,840 (48%) |

Signoff: three rules fail and all three are the shell's reviewed findings
(RDC-50003 once, TMC-20011 twice, TMC-20012 five times). No unreviewed
failure. All 63 timing analyses pass: worst setup slack 2.061 ns on the
machine clock and 1.704 ns on the pixel clock, worst hold 0.001 ns.

The audio-clock review ran under the new policy
`de25-machine-pll-audio-clocks-v1`, which takes the audio master from the
machine PLL in place of the stock audio PLL. Nobody has reviewed that policy
yet.

## ZX Spectrum 128K: packaged

Same source and host, `--model 128`.

| | |
| --- | --- |
| Build ID | `d53a78489bd1a8eda327dcee51c75ac8` |
| Package | `9e3e826ff67c6df7503748aeeb37f97c7e0adbe3ae7fd2d70b867c612e7a06e7` |
| `.fcore` SHA-256 | `d4e4135db88d97f3b0903e3190d77d93dcf8cede4fded4f957053024c9a9cbda` |
| `core.rbf` SHA-256 (first and repeat compile) | `d18e3a20b6ea3109b12e9e9ce28baf3922f2c3ea6dea9bd67c7a7341ef2d6f85` |

The same three reviewed rules fail and nothing else; every reported timing
check passes.

Both Spectrum packages were built before main was merged into the branch.
The merge changes files the build record binds, so a build from the merged
head has a different build ID and has not been run.

## Atari ST 520ST: not packaged

Source `34c03780c`, Powerboat. Build ID `38238a040a06b8fac2d66abee83abafa`.
15,451 ALMs (33%), 5,203,400 block memory bits (71%).

Every reported timing check passes and the three reviews pass. Four rules
fail; three are the shell's reviewed findings. The fourth is unreviewed and
the producer refuses the package on it:

- TMC-20602 (Low), registers with high timing path endpoint tension, on
  `core|machine_reset` and `core|system|machine|cpu|cpu|oReset`. Both are
  resets with a wide fan-out across the machine.

There was no repeat compile and no export.

## What changed to get here

- SDRAM and I2S pin delays from the ISSI IS42/45VM16320G and ADV7513
  datasheets, with zero external flight time as the HDMI pins use.
- The ST samples SDRAM read data on the falling machine clock edge.
- Clock crossings in both machines' video and in the shared GP crossing are
  scalar toggles through two-stage synchronizers plus held bundles loaded on
  the synchronized strobe; no clock group is cut. The Spectrum's frame banks
  cross as codes whose registers power up zero.
- The audio hold takes the scanout clock fault through its own synchronizer.
