# pc386 Serpent Isle black capture: where the black is, 14 November 2026

Issue [#1494](https://github.com/DeanoC/fes/issues/1494). The
[10 November record](../2026-11-10-pc386-si-reload-presets/summary.md) left the
black picture placed after the core for the eight black DOS-prompt grabs taken
on its instrumented `d2` seal, with two candidates named: the ADV7513's
sampling, internal video path or TMDS video output, and Kit B's capture
grabber. This record closes that gap.

(Results pending: runs in progress.)

## What was already established

- **The core drives lit pixels.** On the `d2` seal, in all eight black
  DOS-prompt grabs, the 720p presenter's output and the display plane's output
  both had lit pixels in the last frame and the plane was closed. `top.sv`
  drives the plane's output straight onto `HDMI_TX_D`/`DE`/`HS`/`VS`.
- **The pictures are in DDR.** The capture buffers the presenter reads held the
  lit DOS-prompt sample hashes at every black grab, and VRAM matched the lit
  launches.
- **The transmitter is healthy and the link is up.** Run `k9` dumped the
  ADV7513's 256 main-map registers at every launch. In both of its black
  launches the readings match the lit launches: `0x41` 10 (powered up), `0x42`
  f0 (hot-plug and Rx sense), `0x9E` 12/13 (bit 4 set, pixel PLL locked),
  `0x3E` 10 (detected VIC 4, 720p60), `0xAF` 06 (HDMI mode), `0xBA` 20. The
  grabber received the game's HDMI audio at the lit level (−12.5/−12.7 dBFS
  against a −48 dBFS floor) during both.
- **The load and the menu are not slow.** The Bochs-oracle bench shows the menu
  3 s after Esc and a 5.5 s Journey Onward load, and all 22 loads in lit kit
  launches put the party back on the ship in 3–6 s. Nothing in the guest's own
  timing explains a picture that never arrives.

## The black frame is a uniform field

Every black grab in runs `k1`–`k9` is the same PNG hash `4c6d2c8d`. Measured
here: all of its 1920x1080 pixels are RGB 0, one single colour, zero variance
(`magick -format '%k'` = 1). The records' "mean 6.6, peak 7" is that same frame
through the runs' 64x36 grey reduction, which maps the uniform field to 6.61
with a peak of 7; a lit DOS prompt reduces to mean 16.25, peak 73. So the two
numbers describe one frame, not two.

That shape bounds the candidates in both directions.

- **It is not #1003's fault.** #1003, the project's one known marginal-sampling
  defect, looked completely different: the capture went black for about 32
  frames at a time (the grabber's relock), with 3 to 51 frames of picture
  between, **and the audio capture went silent in the same gaps, because the
  link dropped**. #1494's episodes hold a black picture for a whole launch with
  the link up and audio at full level.
- **It is not a frozen capture frame.** The ASUS 4KPRO freezes its last frame
  when HDMI sync drops (#1823, measured on Kit B; also this project's
  Powerboat notes, which add that its audio goes silent while it is unlocked).
  A frozen frame would therefore be stale and lit — the FES menu that was on
  screen when the core load dropped the link — and would come with silence.
  These grabs are a uniform black field with the introduction's music arriving
  at the lit level, so "the sink or the capture failed to relock" does not
  describe them as stated.
- **It does not clear the transmitter's own video path.** The runtime programs
  `0x17 = 0x62`: external DE, with the ADV7513's DE generator off
  ([`adv7513.hpp:157`](../../../sources/libmister-runtime/src/native/adv7513.hpp)).
  A DE mis-sampled low would send blanking while the sync, the pixel PLL and
  the audio islands all stayed valid — which is exactly a uniform black picture
  behind healthy registers. The detected VIC of 4 is an argument against it
  only if the ADV7513 derives that from measured input active video; that is
  not established here.

## Every core load drops the HDMI link on purpose

`QuiesceAdv` in
[`sources/libmister-runtime/src/native/video.cpp:65`](../../../sources/libmister-runtime/src/native/video.cpp)
writes the ADV7513's main power-down bit before each video bring-up, to
"present a clean link loss before the FPGA changes its pixel stream". So every
launch in #1494's runs — and every kit-software activation in
[#1823](https://github.com/DeanoC/fes/issues/1823) — takes the transmitter down
and up, and the sink and the grabber have to relock. #1823's read-only I²C
timing on Kit B measures that window as 2.81 s and 3.07 s of an unreadable
transmitter during a recovery reprogram.

## What #1003 settled, and what it leaves open

#1003 is the project's only measured HDMI sampling fault, and its sweep is the
method this record's remaining question needs. On Kit A, one 20 s capture per
setting of the ADV7513's input clock delay `0xBA[7:5]`:

| `0xBA` | Delay | Black frames of 600 | Link drops |
| --- | --- | --- | --- |
| `0x60` | none | 459 | 14 |
| `0x40` | −0.4 ns | 1 | 0 |
| `0x20` | −0.8 ns | 0 | 0 |
| `0x00` | −1.2 ns | 0 | 0 |
| `0x80` | +0.4 ns | 1 | 0 |
| `0xa0` | +0.8 ns | 1 | 0 |
| `0x60` again | none | 476 | 15 |

Two things follow. First, a transmitter sampling the pixel bus at the edge of
its window does show up as **black frames in the capture** — so black frames are
not by themselves evidence against the transmitter. Second, it also dropped the
link 14–15 times in 20 s, and #1494's episodes do not: their link stays up, with
audio at full level, for the whole launch. The runtime now sets `0xBA` to
`0x20`, mid-window in that sweep, and `k9` read `0x20` in every launch
including the black ones.

What #1003 did not settle is why a fixed build intermittently misbehaves. Its
own closing note says no `HDMI_TX_*` output carries an output-delay constraint,
so the pin-to-clock relationship is set by placement — fixed per build — while
the transmitter's input PLL relocks at every core load. Which stage of a relock
could land outside the window is not established.

## The grabber's own limits

#1823 established these independently, on Kit B, while investigating fully
black HDMI after a kit-software activation:

- the ASUS 4KPRO freezes its last frame when the signal drops, so the same
  ~3 s outage appeared as 0, 1 or 3 black frames depending on the run;
- a full 170 s capture across a bundle activation showed no black frames at
  all, although the outage happened;
- its conclusion: "Do not use an HDMI capture card as the oracle… Any HIL check
  that looks for blackness on captured video will pass whatever happens."

That is a reason not to trust the grabber to *prove a picture arrived*. It is
not an explanation of #1494's episodes, which are a uniform black field held for
a whole launch with audio arriving — not a stale lit frame, and not a missed
outage.

#1494's runs `k7`–`k9` could not have seen a frozen frame either way. Every
launch ended at the same DOS prompt, so a stale frame is indistinguishable from
a live one: across `k8`'s 101 prompt grabs there are only 10 distinct frame
hashes, each repeated 11–18 times (the prompt's blinking cursor). That is the
hole this record's runs close, by typing a per-launch nonce before the grab.

## The episode signature is Kit B's, in data already taken

The episode's frame has a signature that separates it from a dark game picture:
exactly one colour, RGB 0, over the whole 1920x1080 frame. Measured here across
every HDMI grab of the #1783 compatibility census, which ran package `27f478e0`
on both kits on image `179affbb` within the same two days:

| Kit | Grabs | Uniform-black frames | Runs |
| --- | --- | --- | --- |
| A | 204 | **0** | `k1-real`, `k2-xms`, `k4-dpmi`, `k12-pmode`, `k13-bench` |
| B | 229 | **14** | `k3-pmode` 6, `k6-bench` 6, `k8-realjemm` 1, `k10-real` 1; 0 in `k5-fpu`, `k7-xmsjemm`, `k9-pmodejemm`, `k11-xms`, `k14-xmsjemm` |

Kit A's 204 grabs do include genuinely dark game pictures — Wolfenstein 3D,
Ultima VII and Screamer each have a grab with a peak under 16 — and none of them
is a uniform field; they carry 100+ colours. So the signature is not "a dark
game picture", and on this package it has not appeared on Kit A at all.
`k3-pmode`'s six are the Rise of the Triad launch and `k6-bench`'s six the
SpeedSys launch already reported on #1494, both black from the DOS prompt before
the title ran.

That asymmetry does not separate Kit B's grabber from Kit B's ADV7513 — they
change together between kits — but it does place the fault in one kit's display
or capture path rather than in the pc386 package, which is the same on both. The
two kits' runtimes differ by bundle (`7ddb0bfb` on B from main `5a16b14d8`,
`a2756499` on A from main `2320f64da`), and `git diff` between those commits
shows no change at all under `sources/libmister-runtime/`, so the transmitter
bring-up both kits run is the same code.

## The same frame, on Kit A, with no pc386 core

The decisive evidence was already on Powerboat, in #1823's and #1767's runs. The
all-black grab in #1494 is the PNG
`4c6d2c8d8bbb3756757eacd65f2e662ac9a6f56594ea0e34760218bc21f02037` — a uniform
RGB-0 1920x1080 frame, so that hash is the signature of any flat-black grab,
whatever produced it. Searching every HDMI capture of those two runs' trees (345
PNGs) for it finds:

| Run | Kit | Grabber | Capture | Run's own verdict |
| --- | --- | --- | --- | --- |
| `k1767-hil/.../kitA-ramtest-20261113-101501` | **A** | ShadowCast 3 | `home-after-restore.png` | `home_after_restore_classification: REAL_FAILURE`, after `home_start_classification: PASS` |
| `k1770/runs/kitB-20261113-112300` | B | 4KPRO | `r0-timing.png` and its recheck | #1823 episode |
| `k1770/runs/kitB-20261113-133036` | B | 4KPRO | `r2-cheaper3.png` and its recheck | #1823 episode |
| `k1767-hil/.../kitA-ramtest-*` (5 runs) | A | ShadowCast 3 | `scan-latency-*.png` | not classified here |

All of them are byte-identical to #1494's black frame. The Kit A `home-after-restore`
case is #1823's third episode: that run captured a correct FES Home at the start
(`home-before.png`, 9,620 colours, mean luma 212, OCR reads "Home / Settings /
FES COMPUTER / Sinclair ZX81") and the same flat black after the baseline
restore, with the capture reported OK and the session idle.

**No pc386 package was loaded in any of them.** They are the FES menu and Home
path after a kit-software activation or restore. The same picture-level symptom
therefore happens on both kits, through two different grabbers and two different
ADV7513 boards, with no pc386 core present.

## During an episode the capture is locked and delivering black

#1823's Kit B episode has a video recording beside its grab
(`k1770/runs/kitB-20261113-112300/r0-timing.mkv`). Measured here: 360 frames
over 6.0 s at 60 fps, **every frame's peak grey 0**, no gap over 1 s in the
stream.

So in an episode the capture chain is not stalled and not holding a stale
frame — it delivers a full-rate stream of black frames. With the transmitter
reading powered, link-ready, PLL-locked and VIC 4 (#1494 `k9`), and the core
driving lit pixels with the picture present in DDR (#1494 `k8`), what is
established is that a locked HDMI stream carried a black picture while the FPGA
was driving a lit one.

## What this says about #1494

- **The symptom is not a pc386 defect.** It reproduces with no pc386 core, on
  both kits, in the shared menu and display path, and in pc386's own episodes
  the core was driving lit pixels onto the HDMI pins with the picture in DDR.
  Nothing in the pc386 package, the i386 CPU or the games image is implicated.
- **It is the defect #1823 already tracks** ("Intermittent fully-black HDMI
  after a kit-software activation, with every log reporting success"), seen
  through a core load instead of an activation. Both events run the same
  `QuiesceAdv` power-down and the same `hdmi_verify` bring-up.
- **Which stage blackens the picture is still open**, and belongs to #1823: the
  ADV7513's own video path (its sampling of an external DE, or its internal
  video processing and TMDS output) and the grabbers' video paths both remain.
  The two observations above narrow it: the sink is locked and delivering, and
  the transmitter's readable state is normal.
- **One cheap test would separate them**, and it is #1003's: during an episode,
  sweep the ADV7513's input clock delay `0xBA[7:5]` and count black frames per
  setting. If a delay clears the picture, the transmitter's input sampling is
  the stage. That is an I2C write and needs Deano's explicit OK, as it did for
  #1003.

## Serpent Isle's own behaviour

Nothing in #1494's original report needs a pc386 fix either:

- the save is made and listed back (run `k3`'s first launch made its save blind,
  through a black picture, and later launches list it);
- Journey Onward loads in 3–6 s in all 22 lit launches, against about 5.5 s on
  the Bochs-oracle machine;
- the 10–20 s black main menu of run `v1` is explained by the same black-capture
  episodes, not by the guest: the oracle bench shows the menu 3 s after Esc, and
  every lit kit launch showed it by Esc + 6 s;
- the missed Journey Onward clicks in `k1` and `k3` are explained by the menu's
  20 s idle return to the introduction while the black-grab probes held it idle
  for about 27 s (#1485's mechanism), not by lost input.
