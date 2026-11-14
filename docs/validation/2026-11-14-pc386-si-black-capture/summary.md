# pc386 Serpent Isle black capture: where the black is, 14 November 2026

Issue [#1494](https://github.com/DeanoC/fes/issues/1494). The
[10 November record](../2026-11-10-pc386-si-reload-presets/summary.md) left the
black picture placed after the core for the eight black DOS-prompt grabs taken
on its instrumented `d2` seal, with two candidates named: the ADV7513's
sampling, internal video path or TMDS video output, and Kit B's capture
grabber. It proposed a Kit A run to tell the kits' paths apart.

**Result: the same black capture happens on Kit A, through a different grabber
and a different transmitter, with no pc386 core loaded — it is the FES Home
screen after a baseline restore in #1767's Kit A run, which that run itself
classified a real failure. The stage that blackens the picture is in the display
or capture path every core and the menu share, so #1494's symptom is not a pc386
defect. That is concluded for the eight episodes with pin-level probes; the four
earlier black launches had no probe of their own and stay unclassified, and a
probed recurrence belongs to #1833.
Which of those stages it is stays open as
[#1833](https://github.com/DeanoC/fes/issues/1833), split out of
[#1823](https://github.com/DeanoC/fes/issues/1823), and the two cheap tests that
would separate them need Deano's OK.** The evidence is below, all of it from
runs already taken; a kit run of this record's own is still to come and is
corroboration only.

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
- **A frozen capture frame does not describe it.** #1823 measured the ASUS
  4KPRO freezing its last frame when HDMI sync drops. A frame frozen at a core
  load would be stale and lit — the FES menu that was on screen when the load
  dropped the link — not a uniform black field. (A pipeline that stalls later,
  once black is already on screen, is a different story and is not excluded;
  see below.)
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

That is a reason not to trust the grabber to *prove a picture arrived*, and it
is why no HIL check in this project should gate on captured blackness. It is not
by itself an explanation of #1494's episodes, which hold a uniform black field
for a whole launch with HDMI audio arriving at the lit level.

#1494's runs `k7`–`k9` could not have seen a frozen frame either way. Every
launch ended at the same DOS prompt, so a stale frame is indistinguishable from
a live one: across `k8`'s 101 prompt grabs there are only 10 distinct frame
hashes, each repeated 11–18 times (the prompt's blinking cursor). That hole is
still open: the queued `b1` run types a per-launch nonce before the grab so that
a stale frame cannot pass as a live one, and until it lands no kit evidence here
distinguishes the two.

## The signature in data already taken

The episode's frame is a uniform field: exactly one colour, RGB 0, over the
whole 1920x1080 frame, PNG
`4c6d2c8d8bbb3756757eacd65f2e662ac9a6f56594ea0e34760218bc21f02037`. That is any
flat-black grab, whatever produced it, so a matching hash identifies the
*symptom*, never a cause. Measured across every HDMI grab of the #1783
compatibility census, which ran package `27f478e0` on both kits on image
`179affbb` within the same two days:

| Kit | Grabs | Uniform-black grabs | Where |
| --- | --- | --- | --- |
| A | 204 | 0 | — |
| B | 229 | 14 | 12 in two whole-launch cases (`k3-pmode` Rise of the Triad, `k6-bench` SpeedSys, both black from the DOS prompt before the title ran, already reported on #1494); 2 single grabs, `pinfan-long-g18` in `k8-realjemm` and in `k10-real` |

Counted by launch rather than by grab, Kit B's 14 grabs are 2 whole-launch cases
and 2 single-grab cases, and Kit A's census launches have none. Kit A's 204
grabs do include genuinely dark game pictures — Wolfenstein 3D, Ultima VII and
Screamer each have a grab with a peak under 16 — and none of them is a uniform
field; they carry 100+ colours. So the signature is not "a dark game picture".

The two `pinfan-long-g18` grabs are a different shape and are left open: in both
runs the grabs 6 s either side of them are lit pictures (5,548 and 28,783
colours), so those launches went black and came back. Kit A ran Pinball
Fantasies on another grab schedule (`pinfan-g18`, lit, 5,122 colours), so it is
not a comparison of the same moment.

The two kits' runtimes differ by bundle (`7ddb0bfb` on B, from main `5a16b14d8`;
`a2756499` on A, from main `2320f64da`), and `git diff` between those commits
shows no change at all under `sources/libmister-runtime/`, so the transmitter
bring-up both kits run is the same code.

## The same frame on Kit A, with no pc386 core

Searching every HDMI capture in #1823's and #1767's run trees (345 PNGs) for
that frame finds it on both kits, in runs with no pc386 package loaded at all:

| Run | Kit | Grabber | Capture | The run's own verdict |
| --- | --- | --- | --- | --- |
| `k1767-hil/.../kitA-ramtest-20261113-101501` | **A** | ShadowCast 3 | `home-after-restore.png` | `home_after_restore_classification: REAL_FAILURE`, after `home_start_classification: PASS` |
| `k1770/runs/kitB-20261113-112300` | B | 4KPRO | `r0-timing.png` and its recheck | #1823 episode |
| `k1770/runs/kitB-20261113-133036` | B | 4KPRO | `r2-cheaper3.png` and its recheck | #1823 episode |

The Kit A case is #1823's third episode. That run captured a correct FES Home at
its start (`home-before.png`, 9,620 colours, mean luma 212, OCR reads "Home /
Settings / FES COMPUTER / Sinclair ZX81") and this flat black after the baseline
restore, with the capture reported OK and the session idle.

(Five Kit A `scan-latency-*.png` grabs in #1767's runs are the same frame too.
Their runs failed on DDR scan rows, not on video, and other scan grabs in the
same runs are lit, so they are not classified here either way.)

**So the symptom occurs on both kits, through two different grabbers and two
different ADV7513 boards, in the FES menu and Home path with no pc386 core
present.**

## What a recording of an episode shows, and what it does not

#1823's Kit B episode has a video recording beside its grab
(`k1770/runs/kitB-20261113-112300/r0-timing.mkv`). Measured here: 360 frames
over 6.0 s at 60 fps, every frame's peak grey 0, and no gap over 1 s.

That rules out the capture simply stopping. It does **not** show that the sink
was locked: Powerboat's grabbers are read through a long-lived `ffmpeg` fan-out
into `v4l2loopback` (`/usr/local/bin/fes-4kpro-fanout`), and a reader of a
loopback whose writer has stalled is served the last frame again, which at full
rate looks exactly like this. A stalled pipeline repeating a black frame and a
locked link carrying black pixels are not separated by the recording.

## What this says about #1494

- **Nothing in pc386 is implicated.** In the eight black prompt grabs taken on
  the instrumented `d2` seal, the core drove lit pixels onto the HDMI pins with
  the plane closed and the picture present in DDR. The same symptom appears with
  no pc386 core at all, on the other kit. The four earlier black launches (on
  main's seal and `d1`) had no such probe, so for those this rests on the
  symptom being the shared path's, not on a reading of their own.
- **The remaining stages are shared by every core and the menu**: the ADV7513's
  own video path — its sampling of an external DE (`0x17 = 0x62`, DE generator
  off), its internal video processing, its TMDS video output — and the capture
  chain, the grabber and Powerboat's fan-out together. None of them is pc386's.
- **It is the same symptom #1823 tracks** ("Intermittent fully-black HDMI after
  a kit-software activation, with every log reporting success"), reached through
  a core load instead of an activation; both run the same `QuiesceAdv`
  power-down and `hdmi_verify` bring-up. Whether it is the same *cause* is not
  established: #1823's menu episodes have no FPGA-side reading, so its own
  hypothesis 2 (a `faulted` latch blanking scanout) is still open for them,
  while the `d2` probes rule that out for pc386's.
- **Two cheap tests would separate what is left**, both needing Deano's
  explicit OK. They are [#1833](https://github.com/DeanoC/fes/issues/1833),
  split out of #1823 so a `faulted` fix there cannot orphan them:
  1. #1003's sweep — during an episode, step the ADV7513's input clock delay
     `0xBA[7:5]` and count black frames per setting. A delay that clears the
     picture names the transmitter's input sampling. This is an I2C write, as it
     was for #1003.
  2. Restart `fes-4kpro-fanout` (or reopen the card) during an episode, without
     touching the kit. A picture that comes back names Powerboat's capture
     chain. This restarts a shared service that Deano's viewer also reads.

## The report's other two symptoms

- **The 10–20 s black main menu (run `v1`) is not reproduced.** In `v1` the
  picture came back inside the launch: a blind click opened the character
  screen, which then showed. None of the 400 probed launches did that — every
  black launch stayed black until it ended. `v1`'s frames are no longer on
  Powerboat (`~/fes-scratch/u7menu/runs/v1` is gone), so it cannot be
  re-measured. The two `pinfan-long-g18` grabs above are the only other
  black-then-lit case in this data, and they are on Kit B with no probe. So this
  is recorded as not reproduced, not as explained.
- **The guest's own timing explains neither.** On the Bochs-oracle bench the
  menu shows 3 s after Esc and Journey Onward loads in about 5.5 s; on the kit
  every lit launch showed the menu by Esc + 6 s and all 22 loads put the party
  on the ship in 3–6 s.

## Serpent Isle's save and load work

- The save is made and listed back: run `k3`'s first launch made its save blind,
  through a black picture, and the later launches list "fesk3".
- The missed Journey Onward clicks in `k1` and `k3` fit the menu's 20 s idle
  return to the introduction while the black-grab probes held it idle for about
  27 s (`k6` measured that stretch). The 10 November record marks that as not
  tested, and it stays untested here.

## Inputs and what was measured here

Most of this record needed no kit: those numbers come from runs already taken on
Powerboat, remeasured today. Run `b1` is this record's own kit run.

- **Captures read.** `~/fes-scratch/acc1368r/kit/runs/{k1..k9}` (#1494's own
  runs), `~/fes-scratch/census-p5/kit/runs/*` (433 census grabs, package
  `27f478e0`, both kits), `~/fes-hil/k1770/runs/*` and
  `~/fes-hil/k1767-hil/runs/*` (345 grabs, #1823's and #1767's runs). Frame
  measurements are `magick -format '%k %[fx:maxima*255]'` (colour count and peak)
  and `ffmpeg -vf scale=64:36,format=gray` for the runs' own grey reduction; the
  recording was reduced with `signalstats` per frame. No game or menu frame is
  committed.
- **Registers.** `k9`'s 101 ADV7513 main-map dumps, re-parsed for `0x41`,
  `0x42`, `0x9E` bit 4 and `0x3E`.
- **Code.** `QuiesceAdv` and the `hdmi_verify` phase in
  `sources/libmister-runtime/src/native/video.cpp`; the mode writes in
  `adv7513.hpp`; `git diff 5a16b14d8 2320f64da -- sources/libmister-runtime/`
  (empty) for the two kits' bundles.
- **Grey-scale reconciliation.** The runs' "black grab: mean 6.6, peak 7" and
  this record's "all pixels RGB 0" are the same frame: `ffmpeg`'s RGB-to-grey
  reduction maps that uniform field to mean 6.61, peak 7, where a lit DOS prompt
  reduces to mean 16.25, peak 73.

**Run `b1`** (Kit B, from 10:51 EET, core-only on its baseline — software
`7ddb0bfb`, image `179affbb`, no install, no image change, no reboot, no I2C
write). Package `27f478e0`, the Quartus 17.0.2 seal of main `7e08be726`; disk
`a-u7si.img` (`f32f047a…`), the FreeDOS games image with Serpent Isle from
Deano's eXoDOS zips, staged for the run and never committed; presets alternating
`max` and 486DX2-66; each launch stopped at a DOS-prompt grab. What it adds over
`k7`–`k9`: a per-launch nonce typed at the prompt (`echo B1P0N000 ###…`), so
every lit launch's frame is unique and a stale frame cannot pass as a live one; a
per-frame HDMI luma log across stop → load → prompt; and a read-only
`0x41`/`0x42`/`0x9E`/`0x3E` poll at about 20 Hz through each bring-up. Its
episodes can only be placed after the capture buffers, not after the core's
pins: `27f478e0` carries no presenter or display-plane probe. Driver
`~/fes-scratch/si1494/kit/si1494d.py`, data under `.../runs/b1`.

