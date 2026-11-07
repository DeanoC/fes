# Atari ST game acceptance, 7 November 2026

Issue [#1528](https://github.com/DeanoC/fes/issues/1528). This record covers the
520ST only: 8 MHz 68000, 512 KiB, DE10-Nano. STE is out of scope. The game
matrix was [published on the issue](https://github.com/DeanoC/fes/issues/1528#issuecomment-6707411136)
before any kit run. Kit B ran five claims (`x1` to `x4`, with `x3` launching
nothing). All of them used the same sealed package, user-supplied TOS 1.04 and
private disks. Hashes are in [`kit-runs.json`](kit-runs.json).

**Result.** Two of the three cases have two passing runs. The mouse case is not
met, so this PR refers to #1528 and doesn't close it.

| Case | Run 1 | Run 2 | Case |
| --- | --- | --- | --- |
| 1. Xenon (joystick) | **PASS** (`x2`); `x1` stopped early by a physical kit fault, kept as INCONCLUSIVE | **PASS** (`x4`) | **PASS** |
| 2. Logical (mouse) | **INCONCLUSIVE** (`x2`): launched by mouse from the desktop, but play was never started | **INCONCLUSIVE** (`x4`): same | **INCONCLUSIVE** |
| 3. The Lurking Horror (keyboard, save) | **PASS** (`x2`): the saved disk matches Hatari except one directory time byte | **INCONCLUSIVE** (`x4`): save, relaunch and restore pass, but 4 bytes inside the save file differ and play was about 8 minutes | **PASS** on run 1 only |

Still owed on #1528:

- A mouse-driven game that reaches play, two runs.
- A second Lurking Horror run of at least 10 minutes with a whole-disk match,
  or an explanation of the 4-byte difference.
- Disk replacement during play. None of the three titles needs it, and the
  multi-disk candidates were rejected (see below).
- Runs on a package sealed from main. The only sealed ST package is from
  `f53eba76` (see Package).
- A gameplay reference for Xenon. Hatari's command interface has no joystick
  or mouse-move events, so only the no-input boot was compared.

## Package, firmware and software

- **Package.** `2dd2271c816ad3eb83fb73be9e47b08431eaadd9a4d12484c4d684259090bdb7`
  (file SHA-256 `9549c988…`), `fes.atari-st` 0.2.0, built ID
  `b6f137e9f6602bfbd2f35c9eb92361e4` from fes `f53eba76` (18 Oct). Toolchain:
  nextpnr `9128800f`, Mistral `8fcc4cb4`, Yosys `5391eeb1`, HIP. It uses the
  built-in Direct video path; no separate video part was selected.
- **Why not main.** Main's ST RTL has changed since `f53eba76` (#897, #913,
  #952). A HIP seal of main `607922c63` was attempted. The producer stopped
  after synthesis with `source list outside audited closure: .`. The same
  message stopped a Spectrum seal that day. The read audit was left as it is.
  No other sealed ST package exists, so this record doesn't accept main's ST
  bytes.
- **Firmware.** TOS 1.04 UK, 192 KiB, SHA-256 `c005b0ae…`, the ROM late
  520STFMs shipped with. It is selected per title through the library ROM
  slot. 512 KiB RAM; the core has no other RAM size.
- **Kit.** Kit B, core-only on baseline software `a8cd30c4` with image
  `f904961e`. Nothing was installed, the image wasn't changed and the kit
  wasn't rebooted by a run.
- **Host.** A private Powerboat FogCast host, the promoted `bb51f445`
  `fogcast-api`, with a private HOME and a single Kit B target. It used the
  normal library path: package, ROM and disk media installed, a library title
  created, `POST /api/v1/session/launch`, `/api/v1/session/input/event` and
  `/api/v1/session/stop`. The shared host was not used. Tenfoot menu
  navigation was not exercised.

## Disks

Every disk comes from Deano's IPF dumps of the original disks
(`/mnt/emuhd/Games/AtariST`). The core takes raw `.st` images, so a private
converter decoded each IPF with the SPS decoder library (capsimg
`ed4cb091`, a private build used under its non-commercial licence). It keeps an
image only when every sector the boot-sector BPB describes is present once,
with good ID and data CRCs, and no other sectors exist. 166 of the 408 IPFs
pass that check. Nothing from a disk, ROM or frame is committed.

| Title | Zip SHA-256 | IPF SHA-256 | `.st` SHA-256 | Shape |
| --- | --- | --- | --- | --- |
| Xenon (16 Blitz budget) | `dbdcaad1…` | `7462077f…` | `660e544f…` | 80×1×10, 409,600 B |
| Logical | `dddc9f4a…` | `f340bf07…` | `da73eb0e…` | 80×1×10, 409,600 B |
| The Lurking Horror r203 | `70ad9232…` | `06b3820a…` | `287ff5d0…` | 80×1×9, 368,640 B |

### Selection

Candidates were triaged in Hatari as a 520ST: 68000 at 8 MHz, 512 KiB,
`--compatible on`, TOS 1.02 and 1.04. These were rejected:

- **Copy check.** "This disc is a copy" once decoded to `.st`: Bomb Jack,
  Overlander, Thrust, Warhawk, Wanderer 3D, Metro-Cross and others.
- **Manual or code-wheel check.** Elite, Balance of Power, Another World.
- **Dongle.** Leader Board.
- **1 MiB from the desktop.** Battle Chess.
- **Black after launch in Hatari itself.** Déjà Vu, Shadowgate.

## Reference

The reference is Hatari 2.4.1 (Debian `2.4.1+dfsg-2`, binary `dab1d8a0…`),
set up as the 520ST above with TOS 1.04.

- **The Lurking Horror.** The fixed command script ([`lh-script.txt`](lh-script.txt))
  typed into Hatari gives `FES1528.SAV` `56ffdcd3…` and the whole disk
  `0833c310…`. Two independent runs match byte for byte. Two more runs started
  17 and 33 seconds later also give the same save; only the directory time
  differs.
- **Xenon and Logical.** Both boot, without input, to the same title and
  attract screens in Hatari and on the kit. Gameplay is judged on the kit by
  outcomes: response to the inputs sent, lives, score and restarts.

## Case 1: Xenon

Library Play loads the disk. Its boot sector loads the game with no desktop.
Fire starts a game. A scripted joystick loop drove play: random
left/right/up/down moves with fire taps (`xplay.py`, seed 1528).

- **`x2` (17:06–17:26 EET).**
  1. Title at 25 s.
  2. 11.5 minutes of play. The ship moved and fired, the stage scrolled,
     and YM music and effects played.
  3. Several whole games: lives went 3 → 2 → 1, game over, fire, and a new
     game at 3 lives. The best score was 850.
  4. Ctrl+Alt+Delete was ignored, as expected: the game owns the keyboard.
  5. Stop showed tenfoot's idle screen. Relaunch reached "GAME OVER PRESS
     FIRE TO START", and fire started a new game.
- **`x4` (20:27–20:40).** 11 minutes of play with the same outcomes (lives
  3/2/1 and restarts, best score 600). Stop, relaunch and fire then started a
  game again.
- **`x1` (16:00–16:05).**
  1. After about 4 minutes of play (score 250, lives 3 → 2), the YM audio
     stopped at 16:05:37.
  2. Kit B left the network and showed tenfoot's welcome screen.
  3. Deano traced it to a physical fault and power-cycled the kit. That is a
     harness problem, not a core result.
  4. The run halted with the kit IN USE. After the power cycle, the kit
     matched its baseline before it was released.
- **What wasn't reached.** Random play never reached the end of stage 1.

## Case 2: Logical

Library Play reaches the disk's own GEM desktop (German `DESKTOP.INF`). The
pointer moves one pixel per mouse count. Selecting `LOGICAL.PRG` and then
File > Open starts the game; double-clicks sent over the host API are too slow
for GEM.

The title, an options page and a high-score page then cycle in attract.
Neither run could start play:

- Joystick fire on port 1 or port 0, Space, Return, F1 and S did nothing.
- Left and right clicks on the title and the options page did nothing.

The same inputs did nothing in Hatari either, so this is an operator gap (the
start control is unknown), not a core finding. Mouse input itself works: it
drove the GEM desktop in every Logical and Lurking Horror launch.

## Case 3: The Lurking Horror

The flow, the same in both runs:

1. Library Play reaches the GEM desktop.
2. The mouse opens drive A, then `LURKING.PRG` through File > Open.
3. The script logs in, follows the dream, takes the stone and wakes at
   **Score 5, 19 moves**.
4. SAVE `FES1528.SAV` gives "Okay.".
5. Stop, then library relaunch.
6. The desktop lists `FES1528.SAV`.
7. RESTORE gives "Okay.". Inventory shows the stone and the assignment, at
   5/20.
8. Ten more commands, ending in `score` ("5 of a possible 100, in 29 moves").
9. Ctrl+Alt+Delete warm-resets to the desktop.
10. Stop.

The runs differ in what the saved disk shows:

- **`x2`.**
  - The saved disk from the kit's durable record is `f9a5babd…`.
  - It differs from Hatari's disk in **one byte**, the `FES1528.SAV`
    directory time (0:04 against 0:03; there is no clock, so it is time since
    boot).
  - `FES1528.SAV` itself is byte-identical to Hatari's (`56ffdcd3…`).
  - The record didn't change across the restore session.
  - About 11 minutes of play.
- **`x4`.**
  - The saved disk is `37355bc1…`. It shows the same directory time byte,
    plus 4 bytes inside `FES1528.SAV` (file offsets 1579, 2162, 2163 and 3226).
  - The game showed the same text, score and moves as `x2` throughout.
  - Hatari restoring the `x4` disk and the reference disk prints the same
    text for inventory, look, a move and a second look.
  - So the 4 bytes are game state these probes don't show. Whether they come
    from the core is not established. That, and about 8 minutes of play, make
    this run INCONCLUSIVE.
- **Warm-reset frame.** The desktop after the warm reset in `x4` is
  byte-identical to `x2`'s first desktop frame (`bd09294b…`). The
  after-restore frames of the two runs are also identical (`21bd46da…`).

## Other observations

- **Desktop launch and Show.** In one `x2` relaunch, the first desktop icon
  was now `FES1528.SAV`, so File > Open showed that file as text. That was an
  operator slip; Q returned to the desktop.
- **Audio.** Audio was captured as stereo 48 kHz WAV. YM output is present
  during play and silent on the Xenon and Logical titles. Fidelity is not
  qualified.

## Kit housekeeping

- **Claims.** Each claim logged in `kitB-STATUS.md`. A claim was released only
  after the post-run snapshot matched the probe and the agent was idle with a
  free lease.
- **`x1`.** Halted on the network loss. Released at 17:00 after Deano's power
  cycle, with the baseline verified (only `boot_id` changed). Its one disk
  record was removed.
- **Lock bug.** From 17:00 to 17:04, the private host restarted by `x1`'s
  runner still held Kit B's lock file, so other runners waited. Fixed: the
  host start now closes that descriptor.
- **`x2` and `x4`.** Each removed exactly the three disk-record trees it
  created before its file listing.
- **`x3`.** Its library-entry step failed before any launch, so it launched
  nothing and left nothing.
- **Final state.** Kit B was left FREE at 20:51 on software `a8cd30c4` and
  image `f904961e`, with the `usb-storage.quirks` line intact.
- **What stays on Powerboat.** Captures (`.mkv`, `.wav`, frames), disk records,
  the Hatari runs and the tools are in `~/fes-scratch/st1528`.
