# pc386: five 386DX-25 reference-board candidates

16 November 2026. This is a **reference-emulator benchmark**, not kit or game
acceptance. The five 86Box boards that [the 14 November pace record](../2026-11-14-pc386-u7-npc-pace/method.md)
left unmeasured were tried at 25 MHz. Every measured configuration scores
below the lower quartile of that record's pre-existing 386DX distribution;
none is selected as the mid-range reference. The score rule was fixed before
these trials. The candidate names and their first preliminary scores had been
seen before this retained-input repeat, so this is not a blind or precommitted
repeat.

## Results

| 86Box machine | TopBench | Score capture | 100% speed samples |
| --- | ---: | --- | ---: |
| `award495` | 36 | [screen](award495/score.png) | 115/115 |
| `dataexpert386wb` | 36 | [screen](dataexpert386wb/score.png) | 115/115 |
| `asus386` | 40 | [screen](asus386/score.png) | 115/115 |
| `ecs386v` | 40 | [screen](ecs386v/score.png) | 115/115 |
| `acc386` | 41 | [screen](acc386/score.png) | 115/115 |

Each `score-start.png` and `score.png` shows an early and a later iteration
in the same run (35 and 55 seconds from capture start); these are not
independent emulator boots.

The [selection rule](../2026-11-14-pc386-u7-npc-pace/method.md#the-rule)
asks for a 25 MHz cached 386DX reference mid-range among real boards scaled to
that clock; its lower quartile is 47 and median 57. All five scores here are
below 47. **This rejects only the specific configurations measured**: it is
not a finding that every possible 86Box configuration scores below the band,
or that a board with a different cache or chipset setup cannot qualify.
`award495` POST [reports no cache](award495/post.png), so that measured
configuration fails the cache criterion independently. `asus386` and `ecs386v`
POST report [64 KB](asus386/post.png) and [256 KB](ecs386v/post.png) cache
memory respectively; those messages alone do not establish that cache is
enabled. Cache enablement was not independently established on the other
four configurations. No board is selected and no game pace or kit
result follows from these measurements.

## Inputs and procedure

86Box 6.0 build 9001, interpreter (`cpu_use_dynarec = 0`), `i386dx` at
25,000,000 Hz, 8192 KiB, IBM VGA, XT-IDE and Sound Blaster 2.0 were used on
`chip-30x0-ai-dev`, one emulator at a time. The selected machine and other
actual inputs are in each directory's `86box.cfg` and pre-run `.nvr`; their
hashes and that run's benchmark disk hash are in `input.sha256`. The configs
contain relative disk paths from the private VM directory. Each disk was a
separate writable copy; disk bytes changed during boot and the hashes identify
the snapshots taken **before** the run. The five pre-run snapshots, original 800×520
screen recordings and initial scratch copies are retained locally under
`out/issue1902-ref-private/qualified-<machine>-<timestamp>/`; the private
benchmark disks and ROM bytes are not committed. Each disk's `TOPBENCH.EXE`
was extracted with mtools and had SHA-256
`4428fc27d5c2987e5bd177540843190ebb33f31ca6ddacc6e9f8fb994076e63d`.
Disk identity and executable identity do not establish identical BIOS setup
or game compatibility.

The extracted 86Box executable was SHA-256
`ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969`.
The copied ROM tree was checked against [roms.sha256](roms.sha256), whose
SHA-256 is `ec38293be553dabc711ef5dd62cb61b45167d2a00e04cc6c49fb8032aac09b94`.
The manifest lists ROM filenames and hashes, not ROM contents. The driver was
[run-board.sh](run-board.sh), called once per machine by

```sh
timeout 110s xvfb-run -a -s '-screen 0 1280x1024x24 -nolisten tcp -extension GLX' \
  bash /data/fes-1902-reference-20261116/run-board-v2.sh MACHINE
```

The driver copied the config, NVR and disk before starting the emulator,
recorded their hashes, booted DOS, ran `TOPBENCH -l`, and captured the upper-left 800×520 of the root X
screen at 4 fps. Each directory retains the timing and heartbeat log and emulator output.
The emulator logged unavailable Vulkan/DRI3 and audio capture; the retained
configuration selects Qt software video, and the screen recordings show the
benchmark running. POST
and initial CMOS setup were recorded in private output; `asus386` and
`ecs386v` needed BIOS-default setup and `ecs386v` needed a second CMOS save
for its memory-size mismatch. The retained inputs are **after** that setup.

## Speed evidence and verification

Each `speed.mkv` is a lossless 65×22 crop of the 86Box speed indicator from
35.00 s after capture start until its end: 115 frames at 4 fps, covering the
recorded benchmark interval. The source recordings used H.264 CRF 18, so the
crop is lossless with respect to the decoded capture, not the original X
pixels. Every retained sample shows `100%`, as in each score screenshot.
The readout **between** samples is not established. The `speed.framehash`
files record hashes after grayscale and a fixed 145 threshold; all 575 sampled
frames have the same hash. Run `python3 verify.py` in this directory to check
the pre-run config/CMOS hashes, frame count and decoded speed-frame hashes.
The program does not OCR the score screenshots or prove the accuracy of the
emulator's speed indicator. A full-speed assertion for a future pace window
must still meet the pace record's own validity gates.

No hardware was touched. Next is to determine a defensible reference meeting
the selection rule (including verified cache state), or make an explicit,
reasoned change to that rule before new pace captures. The Ultima VII
acceptance and #1902 remain open.
