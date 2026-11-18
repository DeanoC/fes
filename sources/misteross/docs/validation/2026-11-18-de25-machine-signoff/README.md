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

All three runs below are on Powerboat with `--license-file` naming the local
Quartus license. Without it the fitter stopped on error 119013 after warning
24850, "Failed to acquire license for Quartus Agilex 5"; the morning's runs
needed no license file.

## ZX Spectrum: packaged

Source `4b2b45da6`. The later commits on the branch change only the Atari ST,
which the Spectrum build record does not bind.

| | 48K | 128K |
| --- | --- | --- |
| Build ID | `13e81c55a02fc8791e1d15055149a3e9` | `122a22ce6d2d6654982d994c7bcdb674` |
| Package | `a3ac387c2002d4bcd38bf31267a10125c18c50c9ae0b2300e2688dfc6f55d224` | `0f4b147626f4740773f797608a7e1a9e0b4d6d9fcd0c120f274b2f3c1841ee5d` |
| `.fcore` SHA-256 | `302666dad7e835d8b79188f8ac6bef5914a3dae8986374b158599dd0bade4684` | `26e4ad9f2db5801bb7d20194630aa94ca44deb42605af29a599cecc46549dcd2` |
| `core.rbf` SHA-256 (first and repeat compile) | `89319f79d7e7564c628301e0c62ce4ffd3d23528dd6a0cc70286dff661e2b7e2` | `5c3734eb302412a336e882301298e72ab11c3785437d1e64d52b8da462e68ed2` |
| Worst setup slack, machine clock | 2.048 ns | 2.054 ns |
| Worst setup slack, pixel clock | 1.669 ns | 1.958 ns |
| Worst hold slack | 0.000 ns | 0.000 ns |

Both are `de25-spectrum-package-candidate` with `hardware_acceptance` false.

Signoff, each model: three rules fail and all three are the shell's reviewed
findings (RDC-50003 once, TMC-20011 twice, TMC-20012 five times). No
unreviewed failure. All 63 timing analyses pass.

The audio-clock review ran under the new policy
`de25-machine-pll-audio-clocks-v1`, which takes the audio master from the
machine PLL in place of the stock audio PLL. Nobody has reviewed that policy
yet.

## Atari ST 520ST: packaged

Source `c17ed46e1`.

| | |
| --- | --- |
| Build ID | `34d30f9e958ae0b95e3d43a1b38d2be0` |
| Package | `8d0676f8329c65520ddf47901767157335d62df09a30372a4818f61e5b9f36c2` |
| `.fcore` SHA-256 | `e7bc81e241213180f57dd64dbf3fa7fe2daca25d3a5c4a203d343ff48c08f146` |
| `core.rbf` SHA-256 (first and repeat compile) | `386a6baa14c03f138e26eabc55daf02ebbb21d452df59ae68f9bf59b957d5f58` |
| Classification | `de25-atari-st-package-candidate`, `hardware_acceptance` false |
| Logic (ALMs) | 15,446 / 46,800 (33%) |
| Block memory bits | 5,203,400 / 7,331,840 (71%) |

Signoff: the same three reviewed rules fail and nothing else. All 61 timing
analyses pass: worst setup slack 2.164 ns on the machine clock and 1.949 ns
on the pixel clock, worst hold 0.000 ns.

### The reset finding (TMC-20602)

The rule reports a register whose timing path endpoints total more than
100,000 in distance from their centroid.

| Source | Resets | Result |
| --- | --- | --- |
| `34c03780c` | One machine reset (7,741 endpoints) and the 68000's `oReset` (5,745) | Refused: tension 166,885 and 114,819 |
| `7a78a57dd` | Three groups: processor and bus, I/O, raster | Packaged |
| `4b2b45da6` | The same three groups; only the SDC quoting changed | Refused: the I/O group's two registers, 5,761 endpoints each, tension 105,196 |
| `c17ed46e1` | Six groups | Packaged |

The three-group split passed on one placement and failed on the next, so the
`7a78a57dd` package was not a stable result. The I/O group reached the video
capture through the raster timing in `st_io` and the mailbox through the
keyboard. With six groups the largest register reaches 3,366 endpoints (the
processor's) and the others between 1,136 and 1,874.

## Earlier results on this branch

Each was a full producer run that packaged, on a source this record's
packages replace: Spectrum 48K and 128K at `c93ff9543` and at `a5e864578`,
and the Atari ST at `7a78a57dd`. The SDC bracket quoting was fixed after
them (`5e5827c56`): the quoted `get_registers` patterns left `[*]` unescaped.

## What changed to get here

- SDRAM and I2S pin delays from the ISSI IS42/45VM16320G and ADV7513
  datasheets, with zero external flight time as the HDMI pins use.
- The ST samples SDRAM read data on the falling machine clock edge.
- Clock crossings in both machines' video and in the shared GP crossing are
  scalar toggles through two-stage synchronizers plus held bundles loaded on
  the synchronized strobe; no clock group is cut. The Spectrum's frame banks
  cross as codes whose registers power up zero.
- The audio hold takes the scanout clock fault through its own synchronizer.
- The ST's bus, processor, I/O decode, keyboard and serial chips, sound chip
  and raster each read their own register of the machine reset, and the
  peripheral groups their own register of the 68000's RESET output.
