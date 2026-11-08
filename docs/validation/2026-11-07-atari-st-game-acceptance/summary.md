# Atari ST game acceptance, 7 November 2026

Issue [#1528](https://github.com/DeanoC/fes/issues/1528). This record covers the
520ST only: 8 MHz 68000, 512 KiB, DE10-Nano. STE is out of scope. The game
matrix was [published on the issue](https://github.com/DeanoC/fes/issues/1528#issuecomment-6707411136)
before any kit run. A mouse case, 2b, was [added on the issue](https://github.com/DeanoC/fes/issues/1528#issuecomment-6711833775)
before its own runs.

- **Kit B** ran five claims, `x1` to `x4` (`x3` launched nothing), and on
  8 November a sixth, `n1`, for case 4.
- **Kit A** ran three claims: `g1` and `g2` for case 2b, and `h3` for a second
  full Lurking Horror run.

All of them used the same sealed package, user-supplied TOS 1.04 and private
disks. Hashes are in [`kit-runs.json`](kit-runs.json).

**Result.** The proposed minimum is met. Three games pass two independent runs
each, with at least 10 minutes of play per run and a declared objective:
joystick (Xenon), mouse (Gomoku) and keyboard with a disk save (The Lurking
Horror). Both Lurking Horror passes match Hatari's saved disk except one
directory time byte. On 8 November, case 4 added a disk swap inside a running
session (Terramex, two launches), and the 4 differing `x4` save bytes were
traced to the game's own randomness. Items below are still open, so this PR
refers to #1528 and leaves closing it to review.

| Case | Run 1 | Run 2 | Case |
| --- | --- | --- | --- |
| 1. Xenon (joystick) | **PASS** (`x2`); `x1` stopped early by a physical kit fault, kept as INCONCLUSIVE | **PASS** (`x4`) | **PASS** |
| 2. Logical (mouse) | **INCONCLUSIVE** (`x2`): launched by mouse from the desktop, but play was never started | **INCONCLUSIVE** (`x4`): same | **INCONCLUSIVE**, replaced by 2b |
| 2b. Gomoku and Renju (mouse) | **PASS** (`g1`): three games won to "Game won by White" | **PASS** (`g2`): three games won | **PASS** |
| 3. The Lurking Horror (keyboard, save) | **PASS** (`x2`): the saved disk matches Hatari except one directory time byte | **PASS** (`h3`, Kit A): same match. `x4` (Kit B): its 4 differing save bytes are an expected random outcome that Hatari reproduces byte for byte; INCONCLUSIVE only for its 8 minutes of play | **PASS** |
| 4. Terramex (disk swap, joystick) | **PASS** (`n1`, launch 1): disk 2 inserted into the running session at the game's prompt; about 14 minutes of play | **PASS** (`n1`, launch 2, after Stop and relaunch): disk 1 back in the drive, swapped again; about 11 minutes of play | **PASS** |

Still owed on #1528:

- A swap from tenfoot. Case 4 swaps through the host API, because the ST
  package has no session display and tenfoot has no ST swap control.
- Runs on a package sealed from main. The open PR
  [#1544](https://github.com/DeanoC/fes/pull/1544) fixes the producer's read
  audit and seals main's ST tree as `1f3b21b7…`. The rerun waits for it to
  merge.
- A gameplay reference for Xenon and Gomoku. Hatari's command interface has
  no joystick or mouse-move events, so only the no-input boot was compared.
- Protected originals. Most of the IPF library can't be flattened to `.st`;
  [#1541](https://github.com/DeanoC/fes/issues/1541) tracks running IPF
  images directly.

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
  When these runs were made, no other sealed ST package existed, so this
  record doesn't accept main's ST bytes. The open PR
  [#1544](https://github.com/DeanoC/fes/pull/1544) fixes the audit and seals
  main's tree as `1f3b21b7…`.
- **Firmware.** TOS 1.04 UK, 192 KiB, SHA-256 `c005b0ae…`, the ROM late
  520STFMs shipped with. It is selected per title through the library ROM
  slot. 512 KiB RAM; the core has no other RAM size.
- **Kits.** Both ran core-only: nothing was installed, the image wasn't changed
  and no run rebooted a kit.
  - Kit B was on baseline software `a8cd30c4` with image `f904961e`.
  - Kit A was on the recovery baseline Lander recorded at 20:53: software
    `4efd34bf` with factory image `31b39063` (from main `4b33ec55`).
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
| Gomoku and Renju | `3c9d80bf…` | `0dc360f7…` | `0805b45c…` | 80×1×9, 368,640 B |
| Night Shift, disk 1 | none | `c7fc7f4e…` | `08f7f230…` | 80×2×9, 737,280 B |
| Night Shift, disk 2 | none | `a46cfc94…` | `5cebcbfc…` | 80×2×9, 737,280 B |
| Terramex, disk 1 | none | `34d43f0a…` | `793a9da2…` | 80×1×10, 409,600 B |
| Terramex, disk 2 | none | `ad8f3648…` | `5fe2bb14…` | 80×1×10, 409,600 B |

No zip hash was recorded for the four case 4 disks.

### Selection

Candidates were triaged in Hatari as a 520ST: 68000 at 8 MHz, 512 KiB,
`--compatible on`, TOS 1.02 and 1.04. These were rejected:

- **Copy check.** "This disc is a copy" once decoded to `.st`: Bomb Jack,
  Overlander, Thrust, Warhawk, Wanderer 3D, Metro-Cross and others.
- **Manual or code-wheel check.** Elite, Balance of Power, Another World.
  Night Shift asks for a four-symbol fruit code after its disk 2 prompt; it
  was found on the kit during case 4, after its swap had run.
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
  differs. Those delays came after the game had started, so its random seed
  did not change. A different game start time can move one character; see
  [The `x4` save bytes](#the-x4-save-bytes).
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

## Case 2b: Gomoku and Renju

The GEM program is opened from the desktop with the mouse: select
`GOMOKU.PRG`, then File > Open. The computer, Black, opens at the centre point.
The panel then reads "Next to play White (that's you.)", as in Hatari.

- **How it was played.** A player script (`gomoku.py`) read the 15×15 board
  from the live HDMI capture. It chose White's move with a simple line-scoring
  rule, moved the pointer there with relative mouse packets and clicked. Then
  it waited for Black's reply stone. One mouse count moves the pointer one
  low-resolution pixel.
- **`g1` (Kit A, 21:16–21:33 EET).**
  1. Game 1 ended in "Game won by White" after 14 White moves.
  2. Stop, library relaunch and the desktop launch again led to game 2, won
     after 12 White moves.
  3. Game 3 was started from the program's own Restart (see below) and won
     after 7 moves.
  4. About 14 minutes of play in all.
- **`g2` (Kit A, 21:35–21:45 EET).** Three games, each from a Stop and library
  relaunch. All three ended in "Game won by White", after 7, 18 and 20 White
  moves. About 10 minutes of play.
- **What matches across runs.** The first board frame after each library
  launch is byte-identical in the four launches captured the same way: `g1`
  game 2 and all three `g2` games (`4db2dd70…`). Each run's disk record still holds the unchanged base image:
  the program writes nothing.

Mouse buttons in GEM dialogs and menus:

- Button packets with no motion worked on the desktop and on the board.
- In `g1`, the same zero-motion presses did not select "Restart" from the Game
  menu or "YES" in its "Start a new game?" alert, although the pointer was
  over them.
- The alert accepted the press when the press packet also carried a
  one-count move.
- This isn't reproduced in a reference, so it is recorded as an observation,
  not a defect.

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
- **`h3` (Kit A, 22:09–22:25 EET).**
  - The same flow, and a second relaunch and RESTORE (ending at 5/32): about
    16 minutes in the session, about 13 of them playing.
  - The saved disk is `a34565fd…`.
  - Like `x2`, it differs from Hatari's only in the directory time byte, and
    `FES1528.SAV` is byte-identical (`56ffdcd3…`). The record didn't change
    across either restore session.
  - The warm reset returned the same desktop frame as the launch
    (`49820cd3…`).
- **`x4`.**
  - The saved disk is `37355bc1…`. It shows the same directory time byte,
    plus 4 bytes inside `FES1528.SAV` (file offsets 1579, 2162, 2163 and 3226).
  - The game showed the same text, score and moves as `x2` throughout.
  - Hatari restoring the `x4` disk and the reference disk prints the same
    text for inventory, look, a move and a second look.
  - **Explained (8 November): an expected guest outcome, not a core
    defect.** See [The `x4` save bytes](#the-x4-save-bytes). The save itself
    passes. The run stays INCONCLUSIVE as a run only because it had about 8
    minutes of play, short of the 10-minute minimum.
- **Warm-reset frame.** The desktop after the warm reset in `x4` is
  byte-identical to `x2`'s first desktop frame (`bd09294b…`). The
  after-restore frames of the two runs are also identical (`21bd46da…`).

### The `x4` save bytes

Checked on 8 November against the story file and the interpreter on the disk:

- **What the bytes are.** `FES1528.SAV` holds a 512-byte stack block, then
  the Z-machine's dynamic memory (story release 203, serial 870506, dynamic
  memory up to `0x2c92`). The 4 bytes are object-table links that move one
  object, the wandering **urchin** (object 100). In `x4` it is in Stairway
  (object 35). In the reference it is in Infinite Corridor (object 218),
  ahead of the plastic container. Every global matches, including the score
  (5) and the move count (19). The stack matches too.
- **Why it can differ.** `LURKING.PRG` calls XBIOS `Random()` once at
  startup and keeps the result as its own random seed. TOS 1.04 seeds
  `Random()` from the 200 Hz timer `_hz_200` on its first call. A Hatari
  breakpoint on TOS's `Random()` shows a single call per boot, made by the
  game, with no earlier seed. So the seed is the timer value when the game
  starts. On the kit, that moment depends on mouse clicks sent over HTTP.
  The urchin's moves use the game's random numbers.
- **Reproduced in Hatari.** Four more Hatari 2.4.1 runs of the same script
  (`sd0` to `sd3`) started the game at `_hz_200` = `0x0f2e`, a forced seed,
  `0x0f4b` and `0x0f30`. `sd0` and `sd1` give the reference save
  (`56ffdcd3…`). `sd2` and `sd3` give a save **byte-identical to `x4`'s**
  (`d5227756…`). `sd2`'s whole disk differs from `x4`'s only in the same
  directory time byte as every kit run. A host Z-machine (dfrotz) with
  different seeds also moves the urchin.

So both outcomes are the game's own randomness, and both reproduce in the
reference emulator. No issue is filed. The Hatari runner with the breakpoint
is [`lh-seed.sh`](lh-seed.sh). Note that Hatari's debugger `w` command reads
bare values as decimal, which is why only `sd1`'s forced seed took.

## Case 4: Disk replacement during play (Terramex)

The case was [declared on the issue](https://github.com/DeanoC/fes/issues/1528#issuecomment-6718888958)
before its run, with Night Shift first and Terramex as the fallback if Night
Shift turned out to need a manual check. Both ran in one Kit B claim, `n1`
(8 November, 09:46–10:40 EET).

**How a disk is swapped.** Each disk is its own library entry on the same
package with TOS 1.04: `Terramex 1528 n1` (disk 1) and `Terramex 1528 n1 Disk
2`. Library Play launches disk 1. At the game's prompt, the host's library disk
insert (`POST /api/v1/session/disk/insert`, the same call as
`fogcast insert-library-disk <game-id> <base-media-id>`) puts the disk 2 entry
into unit 0 of the running session. The ST package has no session display, and
tenfoot has no ST swap control, so this host API call is the only way to swap
an ST disk today.

**Reference.** In Hatari, disk 1 shows "Please Insert Disk 2". After disk 2
goes in and a key is pressed, it shows the character title screen (Help,
Fortisque-Smithe, Herr Krusche, Big John Caine, Wu Pong, Henri Beaucoup).
Hatari's command interface has no joystick, so choosing a character and
playing were judged on the kit by outcomes, as for Xenon.

**What kind of swap.** It is the game's own request for disk 2 after booting
from disk 1, the only swap either title makes. Play then runs from disk 2, and
game over returns to the title without asking for disk 1 again, so no swap
happens mid-level.

| Launch | Swap | Play | Result |
| --- | --- | --- | --- |
| 1 (generation 102) | Prompt at 60 s; insert accepted in 10.1 s; same session; unit 0 rebound from the disk 1 entry to the disk 2 entry; the title screen follows | Two games, 10:03–10:06 and 10:15–10:26 (about 14 minutes): joystick moves the menu pointer and the explorer, banana picked up (score 50), lives lost, game over returns to the title without asking for disk 1 | **PASS** |
| 2, after Stop and library relaunch (generation 103) | Relaunch boots disk 1: the prompt frame is byte-identical to launch 1's (`92fc75a7…`). Insert accepted in 10.7 s; unit 0 rebound to disk 2; the title follows | Three games, 10:29–10:40 (about 11 minutes): score 50 each, lives lost, two restarts from the title, and a second screen reached | **PASS** |

- **Disks after the run.** The kit kept one durable record per library disk.
  Each record's payload is byte-identical to its base disk (Terramex
  `793a9da2…` and `5fe2bb14…`), so the swaps wrote nothing to either disk.
  The run removed all four record trees it created before release.
- **Night Shift first.** Launched from its disk 1 entry, it showed "INSERT
  'IML' DISK 2 AND PRESS ANY KEY". The insert was accepted in 17.4 s
  (generation 100, same session, unit 0 rebound to the disk 2 entry). A key
  then took it to the job application screen Hatari shows, and the joystick
  chose Fred Fixit. The next screen asks for a four-symbol fruit code; a wrong
  code is crossed out. That is a manual check, so Night Shift was not played.
  Stop and relaunch booted disk 1 again (generation 101), and the prompt frame
  was byte-identical to the first launch (`6ebcc02b…`). Each disk record held
  exactly its base disk (`08f7f230…` and `5cebcbfc…`).
- **Not covered.** Ejecting the disk, swapping back to disk 1 mid-game (neither
  title asks), and a swap from tenfoot.

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
- **Kit A, `g1`, `g2` and `h3`.** Each removed the two trees it created (an empty
  `core-data` namespace directory and its disk record) before its file
  listing.
- **`n1` (8 November).** Night Shift and Terramex ran in one claim, with a
  line in `kitB-STATUS.md` when the run moved to the fallback. It removed the
  four disk-record trees it created; the post-run file listing equals the
  pre-run one.
- **Final state.** Kit B was left FREE at 20:51 on software `a8cd30c4` and
  image `f904961e`, with the `usb-storage.quirks` line intact. Kit A was left
  FREE at 22:25 on its recovery baseline. After `n1`, Kit B was left FREE at
  10:40 on 8 November, on the same baseline with no reboot.
- **What stays on Powerboat.** Captures (`.mkv`, `.wav`, frames), disk records,
  the Hatari runs and the tools are in `~/fes-scratch/st1528`.
