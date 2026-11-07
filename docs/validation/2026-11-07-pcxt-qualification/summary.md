# PC/XT qualification, 7 November 2026

Issue [#1529](https://github.com/DeanoC/fes/issues/1529). The game set,
objectives and reference were published in [selection.md](selection.md),
commit `cf7f07a0e`, before the first kit run. Hashes and measurements are in
[kit-runs.json](kit-runs.json). Nothing from the games is committed.

All runs used Kit B. Kit A was held by Lander's #1490 restore all day.

## Result

| Case | Result | Runs |
| --- | --- | --- |
| COM/DOS baseline: `FESCOM` READY → `K` → PASS, `DIR` | **PASS** | `s1` |
| Library Save, Stop, relaunch, readback | **PASS**: marker read back; saved image differs from its base only in the expected sectors | `s1` |
| Unsaved write, then Stop and relaunch | **PASS** against the documented policy: Stop saves a bound disk, so the file survives | `s1` |
| Raw (unbound) insert: write, then Save | **PASS**: Save is refused (409) and the write is gone after relaunch | `s1` |
| Paratrooper (`.COM`), 2 × 10 min | ROW_PARA | `s1`, RUN2_PARA |
| Alley Cat (`.EXE`), 2 × 10 min | ROW_CAT | `s2`, RUN2_CAT |
| Rogue (large `.EXE`, game save), 2 runs | ROW_ROGUE | ROGUE_RUNS |
| Game speed against MAME `ibm5150` | **PASS**: Paratrooper helicopters 72.8–72.9 vs 72.8 CGA px/s; Alley Cat's title tune has the same note timing | `s1`, `s2` |
| Speaker pitch against MAME | **PASS**: Paratrooper's main tones agree within 0.2 cents; Alley Cat has the same tone set | `s1`, `s2` |
| Guest reset (Ctrl+Alt+Del) | **FAIL**: the machine hangs; the next library launches warm-boot and can't boot the floppy ([#1534](https://github.com/DeanoC/fes/issues/1534)) | `s2` |
| Recovery after that hang | **PASS** with a workaround: a launch with no floppy runs a cold POST, and the next library launch boots | `s2` |
| Library or menu selection through tenfoot and a host | **Not run** (see below) | none |
| Cold power cycle | **Not run**: needs Deano's OK | none |

#1529 stays open. This record refers to it and doesn't close it.

## What was under test

- **Package.** `fes.pcxt-dos` package ID `87fd8149c09a7e31…`:
  - archive `19fd34ec…`, RBF `94b3c222…`;
  - produced from main `bb87b9b6`;
  - the same bytes as the [5 November short smoke](../2026-11-05-pcxt-kit-b-short-smoke.md).
- **Selection.** The cache-only resolver (building disabled) selects that
  archive for main `607922c63`, the head this branch started from. No PC/XT
  source changed in between.
  - The archive came from the 5 November run's private package cache. The
    shared cache has no copy.
  - Hashes: selection TOML `8c427a5d…`, provenance `97951222…`, prepared
    record `cb9f8611…`.
  - [selection.md](selection.md) calls `8c427a5d…` the prepared receipt.
    It is the selection TOML.
- **Runtime.**
  - Kit B baseline software `a8cd30c4…` (main `bb51f445`) on image
    `f904961e…`, with the `usb-storage` quirk line in place.
  - Nothing was installed. No image change and no reboot.
- **Launch path.** No FogCast host is involved. The runner calls the kit
  agent the way the host does:
  1. `/v1/library/core/load` with the package;
  2. `/v1/library/media/insert`, which binds unit 0 to a library game ID and
     the base image's SHA-256;
  3. `/v1/library/media/save` for an explicit Save;
  4. `/v1/stop`.

  The agent reports the active interfaces as `fes.computer` 1.1,
  `fes.keyboard.hid`, `fes.media.pcxt-floppy`, `fes.media.pcxt-floppy-write`,
  `fes.audio.pcm-s16-stereo-48k` and `fes.video.fixed-720p60`.
- **Input and capture.**
  - Keys go through the agent's input stream (HID codes), as the host sends
    a session's keyboard.
  - HDMI comes from the Kit B 4KPRO at 1920×1080 and 60 fps. Video clips are
    stored as 480×270 gray.
  - Audio is ALSA at 48 kHz.
- **Disks.** Each disk is the 5 November COM disk plus one game, as listed
  in [selection.md](selection.md).
- **Tooling.** The runner is a copy of the pc386 acceptance console from
  #1520 (`hil_xt.py`, `run-xt.sh`), adapted for floppy media. The analysis
  scripts are `heli.py` (helicopter tracking), `tones.py` (speaker pitch by
  zero crossings) and `onsets.py` (note timing). Their hashes are in
  `kit-runs.json`.
  - These are Powerboat scratch tools, not repo tools. The COM case repeats
    the #1465 nightly COM check on hardware.

## Reference

The reference is MAME 0.276 (Debian `mame 0.276+dfsg.1-1+deb13u1`), driver
`ibm5150`: an 8088 at 4.77 MHz with IBM CGA and the PC speaker.

- It uses the 0.256 `ibm5150` ROM set and the 0.202 `keytronic_pc3270` and
  `kb_pcxt83` sets from Deano's MAME share. The fixed-disk slot is empty.
- It boots the same disk images with the IBM BIOS instead of GLaBIOS.
- It runs headless and replays the kit run's own command list on emulated
  time, from 110 s after power-on, when FreeDOS is at `A:\>`.
- Frames come straight from the CGA screen. The whole session is
  WAV-written at 48 kHz.

The game's random events differ from the kit's, so the comparisons are of
rates and tones, not frames.

## Save and reload (`s1`)

On the Paratrooper disk, bound as library game `xt1529-para`:

1. **Explicit Save.**
   - `FESCOM` showed `READY (PRESS K)`, then `PASS` after `K`.
   - `ECHO FES1529 SAVED MARKER > SAVED.TXT`, then the library Save. It took
     4.25 s and gave revision `fb3942ae…`.
   - Stop, then a relaunch with the same binding, booted COLD to `A:\>`, and
     `TYPE SAVED.TXT` printed the marker.
2. **Unsaved write.** `UNSAVED.TXT` was written with no Save, then Stop and
   a relaunch.
   - The file was there afterwards, and the revision had moved to
     `c78845c5…`.
   - This is the documented policy: Stop, eject and replacement save a bound
     disk (FogCast `ARCHITECTURE.md`).
   - A write is lost only if the machine never reaches a Stop, for example on
     a power loss. That needs the power-cycle test below.
3. **Raw insert.** The same image was inserted through
   `/v1/development/insert-media` (no binding) and `RAW.TXT` was written.
   - The library Save was refused with 409 `BUSY`: "media unit delivery
     requires the current active computer package generation with that unit".
   - After Stop and a raw relaunch, `RAW.TXT` was gone.
4. **Whole saved image.** The record file was pulled from the kit after the
   run (`record.bin`, `c78845c5…`).
   - It decodes cleanly: `FESPCXT1` envelope, checksum valid, identity
     `fes.pcxt-dos`/`xt1529-para`/base `c3440903…`.
   - Against the base image, exactly five 512-byte sectors differ:

     | Sector | Area | Bytes changed | What it is |
     | --- | --- | --- | --- |
     | 1 | FAT 1 | 3 | two new cluster entries |
     | 3 | FAT 2 | 3 | the same entries; the two FATs are identical |
     | 5 | root directory | 36 | two new entries |
     | 312 | cluster 152 | 23 | `SAVED.TXT` |
     | 314 | cluster 153 | 12 | `UNSAVED.TXT` |

   - Every other sector is byte-identical. File dates read 1980-01-01,
     because the XT has no clock.

ROGUE_SAVE_PLACEHOLDER

## Games

**Paratrooper, run 1 (`s1`).**
- The title, instructions and keyboard play all work. ← and → start the gun
  turning, and ↑ stops it and fires.
- **First script: failed attempt, kept.** The first 10-minute script swept
  the gun at random and never pressed space. The base was overrun within a
  minute (score 1), and the rest of the time sat on the attract screen. MAME
  ended the same way, with score 30. The script was cut after 9 minutes.
- **Second script.** It swings the gun between left and right with bursts of
  fire, and presses space every 20 s so a game over restarts at once. It ran
  for 10 minutes.
  - Grabs show helicopters, paratroopers landing, the gun at both sides and
    scores of 12, 52, 11, 5 and 30. The high score was 52.
  - There were several game overs, each followed by a new game.
- **Objective met.** The gun covers both sides, the score rises from hits,
  and a game over is followed by a new game.

**Alley Cat, run 1 (`s2`).**
- `CAT`, then N (no joystick), K (Kitten) and a key, then 10 minutes of
  scripted runs, straight jumps and running jumps. These were followed by
  3 minutes of steered climbing.
- Grabs show the alley, bins, fence, lines and windows. Lives were lost
  repeatedly (3 → 1), the game went over to the title, and new games
  started.
- **Window entry not reached.** No window opened or was entered, and the
  score stayed 0. MAME, given the same input, didn't enter one either. The
  window part of the objective is **INCONCLUSIVE**: the scripted input
  couldn't steer it. The life-lost-and-resume part is met.

RUN2_AND_ROGUE_PLACEHOLDER

## Speed and sound against MAME

**Paratrooper helicopters.**
- `heli.py` tracks each helicopter across the picture in a 60-second clip.
  It fits a line to its position and reports the median speed in CGA pixels
  per second.

  | Clip | Tracks | Median speed (CGA px/s) | Move period (frames) |
  | --- | --- | --- | --- |
  | Kit `s1` clip 1, upper lane | 6 | 72.94 (72.66–72.96) | 3 |
  | Kit `s1` clip 1, lower lane | 7 | 72.97 (72.50–73.01) | 3 |
  | Kit `s1` clip 2, upper lane | 9 | 72.81 (71.91–73.10) | 3 |
  | Kit `s1` clip 2, lower lane | 9 | 72.90 (71.91–75.45) | 3 |
  | MAME, upper lane | 3 | 72.84 (72.71–72.86) | 3 |
  | MAME, lower lane | 1 | 72.83 | 3 |

- The kit's clip 2 holds 3553 frames for 60 s, so the capture dropped about
  1.3% of its frames there. Clip 1 holds all 3600.

**Paratrooper speaker.**
- `tones.py` estimates each 50 ms window's tone from its zero crossings.
- It compares the kit's 10-minute play recording with MAME's session from
  the same command list:

  | Tone | Kit median | MAME median | Kit windows | MAME windows |
  | --- | --- | --- | --- | --- |
  | 919 Hz | 920.72 Hz | 920.63 Hz | 819 | 812 |
  | 1377 Hz | 1380.99 Hz | 1380.82 Hz | 817 | 675 |
  | 330 Hz | 329.61 Hz | 329.61 Hz | 190 | 169 |
  | 440 Hz | 439.98 Hz | 439.98 Hz | 117 | 111 |

- The speaker sounds in 37.6% of the kit's windows and 36.6% of MAME's.
  Between sounds both are silent (muted).
- The differences are at most 0.2 cents. That is within the method's
  resolution.

**Alley Cat.**
- The title tune's note onsets fall at the same intervals on both:
  0.33, 0.33, 0.33, 0.33, 0.22, 0.11, 0.22, 0.33, 0.11, 0.11, 0.11, 0.44,
  0.66, 0.66 s. The first 15 intervals agree within 5 ms.
- Gameplay sound gives the same tone set: 2316, 4631, 1158 and 2349 Hz bins.
  - The speaker sounds in 16.7% of the kit's windows and 15.5% of MAME's.
  - MAME has an extra 7.2 kHz bin, which the kit's HDMI audio doesn't show
    among its top tones. This is noted, not judged: the method is coarse at
    that frequency.

**Video stability.**
- Every grab and clip in these runs shows a stable picture.
- The one exception is the black picture after Ctrl+Alt+Del, below, which
  was the guest hanging, not the capture.
- Brightness and non-black frames were not used as evidence of play.

## Reset and recovery (`s2`)

This is recorded apart from play and filed as
[#1534](https://github.com/DeanoC/fes/issues/1534).

- **Every ordinary Stop and relaunch worked.** Eight library launches before
  the reset came up `Boot [ COLD ]` and booted FreeDOS. Seven were in `s1`
  and one in `s2`.
  - A load took 6.75–7.25 s. A floppy insert took 4.2–5.9 s.
- **Ctrl+Alt+Del in Alley Cat.**
  - The HDMI picture went fully black. Every captured pixel was 0 for over
    60 s at 60 fps.
  - The speaker kept playing a repeating jingle: three long 523 Hz notes,
    then five short 698 Hz notes.
- **Ctrl+Alt+Del at a cold-booted `A:\>`.**
  - The screen stayed as it was and typed keys no longer echoed.
  - The same jingle played.
- **After the hang, the library launch failed.** Three library relaunches
  with the floppy came up `Boot [ WARM ]` and `Disk Boot Fail. You monster.`
  - A key at "Press the Any Key" failed again.
  - The saved floppy record pulled from the kit is byte-identical to its
    base, so the disk was not the cause.
- **Recovery.**
  - A launch with no floppy ran a COLD POST.
  - Inserting the floppy and pressing a key then booted FreeDOS.
  - The next normal library launch booted COLD.
  - The kit was left in that state, so the next PC/XT user won't start from
    a warm flag.
- **Reference.** MAME's IBM BIOS is not GLaBIOS, so no reference comparison
  was made for the reset.

## Not done, and why

- **Menu selection through tenfoot and a host.**
  - The shared Powerboat host is paired with Kit A, which was held all day,
    and it has no `fes.pcxt-dos` entry. Adding one is a permanent library
    change.
  - These runs used the agent's library API, which is the call the host
    makes after selection. They are not labelled as menu selection.
  - The host-path evidence for these package and software bytes is still the
    5 November smoke, which used a private host: import, launch, session
    disk Save, Stop and relaunch.
- **Cold power cycle.** It needs Deano's separate OK. Persistence across
  power loss, and the loss of writes made after the last Save or Stop, are
  untested.
- **External timing.** External SDRAM, HDMI, HPS, reset and PLL limits stay
  with #835, and nextpnr timing stays advisory under #840. Physical ISA is
  separate.
- **Sopwith.** It was listed as optional and was not run.
