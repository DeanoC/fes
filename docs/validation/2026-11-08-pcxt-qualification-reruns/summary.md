# PC/XT qualification reruns, 8–10 November 2026

Issue [#1529](https://github.com/DeanoC/fes/issues/1529). The cases and pass
rules were published in [plan.md](plan.md), commit `bea000941`, before any of
these runs. Hashes are in [kit-runs.json](kit-runs.json). Nothing from the
games is committed.

## Result

| Case | Result | Run |
| --- | --- | --- |
| Power loss with an unsaved write | **PASS**: the saved record is byte-identical after the power loss; the saved file reads back and the unsaved file is gone | `pl1` |
| COM/DOS baseline: `FESCOM` READY → `K` → PASS, `DIR` | **PASS** | `q1` |
| Library Save, Stop, relaunch, readback | **PASS**: the marker reads back; the saved image differs from its base in the same five sectors as on 7 November | `q1`, `qd` |
| Unsaved write, then Stop and relaunch | **PASS** against the documented policy: Stop saves a bound disk, so the file survives | `q1` |
| Raw (unbound) insert: write, then Save | **PASS**: Save is refused (409 `BUSY`) and the write is gone after relaunch | `q1` |
| Paratrooper, 2 × 10 min | **PASS**: both runs play through game overs and restarts (score 61, high scores 70 and 41) | `q1`, `q3` |
| Alley Cat, 2 × 10 min | **PASS**, including the window objective: run 1 ends inside a room | `q2`, `q3` |
| Rogue, play to the stairs, 2 runs | **PASS** ×2: the map draws, the player moves in all eight directions, and `>` on the stairs reaches level 2 | `r1` |
| Rogue game save and restore across a relaunch | **PASS** | `rs1` |
| Speed and speaker pitch against MAME `ibm5150` | **PASS**: Paratrooper helicopters 72.86–72.95 vs MAME 72.8 CGA px/s, same main tones; Alley Cat note timing and tone bins match the 7 November kit run, which matched MAME | `q1`–`q3` |
| Ctrl+Alt+Del | **PASS**: warm POST back to `A:\>`; the next library launch cold-boots | `q2` |
| Repeated relaunch from the TV menu (Kit B's own host) | **PASS** for launch, cold POST, Stop and return, 5 of 5 cycles (F12 ×4, Ctrl+Home ×1). **Not run**: the 30 s of Paratrooper per cycle, because the kit keyboard doesn't reach the core in kit-host sessions on this bundle ([#1613](https://github.com/DeanoC/fes/issues/1613)) | `tf2` |

## Power loss with an unsaved write (`pl1`)

Kit B, 18:23–18:31 EET. Deano was at the kit and gave his OK, through the
qualification session, for one power pull on Kit B.

- **Package.** `fes.pcxt-dos` `4dd6d431b1abb442…`, the package sealed for
  #1565 (archive `4ef787f7…`). The plan uses it because this case tests the
  library record, not the core.
- **Runtime.** Kit B baseline software `a8cd30c4` (main `bb51f445`) on image
  `f904961e`, with the `usb-storage` quirk line. No install and no image
  change.
- **Disk.** The Paratrooper disk (`para.img`, `c3440903…`), bound to the new
  library game `xt1529-ploss`. The agent reported `persistence_mode`
  `persistent`.

Steps:
1. **Save.** At `A:\>`: `ECHO FES1529 SAVED BEFORE POWER LOSS > SAVED.TXT`,
   then a library Save (3.98 s). The run's one new kit file, the record
   `core-data/media/bcbdb947…/record.bin`, hashed `e9fc204e…`.
2. **Unsaved write.** `ECHO FES1529 NOT SAVED > UNSAVED.TXT`, then `DIR`.
   Both files were listed. No Save and no Stop. The record still hashed
   `e9fc204e…`, so the agent doesn't write the record between Saves.
3. **Power loss.** Deano pulled Kit B's power at the wall and plugged it back
   in. The boot ID changed from `26092622` to `c3067edf` at 18:27. The
   runner's session had no clean end; its process was stopped on Powerboat.
4. **Record after the power loss.** Still `e9fc204e…`, byte-identical to
   step 1. The agent came back idle with a free lease.
5. **Relaunch.** The same library game cold-booted to `A:\>`.
   - `TYPE SAVED.TXT` printed `FES1529 SAVED BEFORE POWER LOSS`.
   - `DIR` listed eight files: the base disk's seven plus `SAVED.TXT`. No
     `UNSAVED.TXT`.
   - `TYPE UNSAVED.TXT` printed `File not found. - 'UNSAVED.TXT'`.
   - The free space went from 206 848 bytes, with both files, to 207 872
     bytes. That is one 1 KiB cluster fewer, as expected for one less file.

**PASS.** A power loss keeps the last Save and drops writes made after it.
That matches the documented policy: only Save and Stop write a bound disk's
record. The guest saw a consistent disk, and the library and agent started
normally.

**Cleanup.** After the readback the machine was stopped. The one FogCast tree
the run created (`core-data/media/bcbdb947…`) was removed, and the data
directories match the pre-run listing. Kit B's post-run snapshot matches its
pre-run snapshot in every key except the boot ID: software `a8cd30c4` active
with nothing pending, image `f904961e` good with nothing pending, the quirk
line present, the agent idle and the lease free. Kit B was released at 18:31.

The private evidence is on Powerboat in `~/fes-scratch/xt1529/runs/pl1`. The
runner is `~/fes-scratch/xt1529/ploss-xt.sh`.

## Package under test

`fes.pcxt-dos` was sealed from a fresh, clean worktree of main `8d9b71fab`.
That is the first main after [#1619](https://github.com/DeanoC/fes/pull/1619)
(#1618), which stops Quartus from copying the reset registers that
`audio.sdc` binds. The 843d9fc2c seal, the first after #1569, failed seed 2 on
that binding, so no package was produced from it.

- Package `e73a0e87261a8481…`, archive `f2198ab0…`, RBF `fc22328b…`. The
  repeat seed-1 build gave the same RBF. Build ID `e41cb3d2…`.
- Producer: `scripts/build_fes_pcxt_quartus.py --dos-profile`, Quartus 17.0.2,
  the lock's Yosys (`d45b5d6e…`).
- All three seeds pass every reported endpoint check, with no failures. Each
  of the three reset sources is one register (`dont_replicate` = 1 each).

| Seed | Setup | Hold | Recovery | Removal |
| --- | --- | --- | --- | --- |
| 1 | 0.507 ns | 0.121 ns | 5.804 ns | 0.309 ns |
| 2 | 0.887 ns | 0.113 ns | 5.301 ns | 0.254 ns |
| 3 | 0.544 ns | 0.122 ns | 4.684 ns | 0.400 ns |

These are internal timing results. External SDRAM, HDMI, HPS, reset and PLL
limits stay with #835.

**Runtime.** Kit B, image `cd284ca3…` and baseline bundle `66316149…` (main
`5302f683`, which contains #1564). Nothing was installed. There was no image
change and no reboot: the boot ID stayed `66a149ef` from `q1` through `tf2`.

**Launch path.** The runner calls the kit agent the way a host does, as on 7
November (`/v1/library/core/load`, `/v1/library/media/insert`,
`/v1/library/media/save`, `/v1/stop`). Keys go through the agent's input
stream. `tf2` is the exception: it uses tenfoot and Kit B's own FogCast host.

## Runs

All on Kit B, 9–10 November (EET). Each run claimed the kit in
`kitB-STATUS.md` and logged it FREE only after the baseline checks passed.
Each run removed the FogCast data trees it created. Where the record was
needed, the runner pulled the record first (`run-qr.sh`).

| Run | Window | Cases |
| --- | --- | --- |
| `q1` | 22:32–22:59 | COM, library Save/readback, unsaved write, raw insert, Paratrooper run 1 |
| `q2` | 22:59–23:17 | Alley Cat run 1, Ctrl+Alt+Del |
| `q3` | 23:17–00:14 | Paratrooper run 2, Alley Cat run 2 |
| `r1` | 00:14–00:21 | Rogue play runs 1 and 2 |
| `qd` | 00:22–00:25 | `q1`'s Save steps again, to pull the saved image |
| `rs1` | 00:26–00:29 | Rogue game save and restore |
| `tf2` | 00:30–00:47 | Relaunch from the TV menu |

`q1`–`q3` replay the 7 November command lists, with new game IDs.

## The record's game cases (`q1`–`q3`, `qd`, `rs1`)

- **COM baseline.** `FESCOM` printed `FES COM READY (PRESS K)`, then
  `FES COM PASS`. The boot screen reads `Boot [ COLD ]`, with 640 KB, CGA and
  an 8088.
- **Library Save and readback.** `SAVED.TXT` was written and saved, then the
  machine was stopped and relaunched. `TYPE SAVED.TXT` printed the marker.
  - `q1`'s runner removed the record before it could be pulled. `qd` repeated
    the same steps on a fresh game ID and pulled it (`b5087bc8…`).
  - The record decodes as a `FESPCXT1` envelope for `fes.pcxt-floppy.image`.
  - Against the base image, exactly five 512-byte sectors differ: 1 and 3
    (both FATs, 3 bytes each), 5 (root directory, 36 bytes), 312 (23 bytes)
    and 314 (12 bytes). These are the same sectors and byte counts as on 7
    November.
- **Unsaved write.** `UNSAVED.TXT` was still there after Stop and relaunch, as
  on 7 November: Stop saves a bound disk. The free space went from 207 872 to
  206 848 bytes.
- **Raw insert.** The library Save was refused with 409 `BUSY` ("media unit
  delivery requires the current active computer package generation with that
  unit"). After a raw relaunch, `DIR` listed the base disk's seven files only.
- **Rogue game save and restore (`rs1`).**
  1. `ROGUE`, the name `fes` and three moves.
  2. `Shift+S` gave `Save file (press enter to default to "rogue.sav")`.
     Enter saved and quit, and `DIR` showed `ROGUE.SAV`, 40 448 bytes.
  3. A library Save (4.01 s), then Stop and relaunch.
  4. `ROGUE ROGUE.SAV` printed "Hello fes, Welcome back to the Dungeons of
     Doom!". The map, the items, the player and the status line match the
     frame before the save.

  The pulled record (`033916bf…`) holds the deleted root entry `?OGUE.SAV`
  (`E5`, cluster 233, 40 448 bytes). Both FATs are back to their base bytes,
  as on 7 November.

  `q3` had run this case from the 7 November command list first, and it did
  not save. That list was written when Rogue's map didn't draw. Now that the
  map draws, monsters reach the player: a pending `More` prompt took the
  `Shift+S`, so nothing was saved, and the restore printed
  `ROGUE.SAV not found`. That failure is in the script, not the machine, so
  `rs1` repeated the case, steered from the screen.

## Games

- **Paratrooper.**
  - Run 1 (`q1`) played through game overs and restarts, scoring 61 with a
    high score of 70.
  - Run 2 (`q3`) restarted after a game over, with a high score of 41.
  - The helicopters move at 72.95 and 72.86 CGA px/s, against MAME's 72.8.
    They move every 3 frames, as on 7 November.
  - The main speaker tones (920.7, 1381, 329.6 and 440.0 Hz) are the same
    bins and medians as the 7 November kit run.
- **Alley Cat.**
  - Run 1 (`q2`) ends inside a room (`qc1-end`). The cat entered a window, so
    the window objective that was **INCONCLUSIVE** on 7 November now passes.
    The command list is unchanged; the game's random events differ between
    runs.
  - Run 2 (`q3`) shows movement, lives lost and a high score of 0160.
  - The title tune's 29 note intervals match the 7 November kit run, and the
    main tone bins (2316, 4631 Hz) match.
  - The MAME comparison is transitive: the 7 November run had the same note
    timing and bins as MAME.

## Rogue: two play runs to the stairs (`r1`)

The screen text is decoded from each HDMI frame against the package's
CP437 font (`cgatext.py`).

- **Run 1** (`x1576-rp1`).
  - **Map.** The lit start room draws with its walls, floor, door and the
    player `☺`. The status line reads `Level:1  Hits:12(12)`.
  - **Movement.** `k l n u b y j h` moved the player one cell in each of the
    eight directions. Each step is a separate grab.
  - **Stairs.** Through two small rooms and a corridor, the stairs drew as
    F0h (`≡`) in the bottom row of a large room. Walking onto them and
    pressing `>` gave `Level:2` and a new map. The player kept moving on
    level 2.
- **Run 2** (`x1576-rp2`, a fresh launch).
  - It started in a different room.
  - The player killed two kestrels and a hobgoblin. Hits fell to 7(12), and
    the status line updated with each fight.
  - It found the stairs in the fourth room, and `>` gave `Level:2`: a dark
    room with only the cells next to the player shown. Movement continued.

PC Rogue draws the stairs as F0h in reverse video with blink, so it shows
black bars on a green cell. The kit frames caught the visible phase. The MAME
reference snapshot (`b19`) caught the blink-off phase, a plain green cell, and
MAME's text dump has F0h at that cell. The MAME reference session reached
level 2 (`b20`, from `moves-level2.txt`).

The status line's last field is a clock. It reads 12:00–12:02, because an XT
has no RTC. PC Rogue shows no turn counter, so "the turn counter keeps
advancing" (plan, step 5) was checked as play continuing on level 2.

## Ctrl+Alt+Del (`q2`)

At `A:\>` on the Alley Cat disk, Ctrl+Alt+Del was pressed from the agent's
input stream.

- 3 s later the BIOS showed `Boot [ WARM ]`, 640 KB OK.
- By 33 s it had booted the floppy back to `A:\>`. `VER` and `DIR` worked.
- After Stop, the library relaunch showed `Boot [ COLD ]`, counted the memory
  and booted to `A:\>`. `DIR` worked.

No hang, beep halt or "Disk Boot Fail". **PASS.**

## Relaunch from the TV menu (`tf2`)

Kit B now runs its own FogCast host (`fogcast-kit host`), and tenfoot talks to
it (`-api http://127.0.0.1:8787 -kit-host`). The plan's pairing with the
shared Powerboat host and the `tf1` entry no longer apply. Deano approved one
lasting library entry on Kit B for this case.

**The lasting entry stays on Kit B.** It was created through Kit B's host API
with the `fogcast` CLI:

- package `e73a0e87…` (`core-install`);
- the Paratrooper disk `c3440903…` (`core-media-install`);
- the entry "PC/XT DOS: Paratrooper", `game_id`
  `fpga-pc-xt-dos-paratrooper-a905363ab5b8`, media role `disk`.

The package files are under `/run/fogcast/fesdata3/core-library`, and the
entry and media are in the host's library database. FogCast has no delete for
entries, and this one is meant to stay. `kitB-STATUS.md` records it.

Keys came from a temporary uinput keyboard on the kit (`hilkb`), which
tenfoot opens like a USB keyboard. Each cycle:

1. Library → search `parat` → Enter (Play).
2. About 45 s later, the screen shows `Boot [ COLD ]` and `A:\>`.
3. 30 s idle at the prompt.
4. Stop.
5. The Library came back about 10 s after the Stop key, with the agent idle,
   the lease free and no host session left.

| Cycle | Stop key | Cold POST to `A:\>` | Menu back, idle, lease free |
| --- | --- | --- | --- |
| 1 | F12 | yes | yes |
| 2 | F12 | yes | yes |
| 3 | F12 | yes | yes (10.4 s) |
| 4 | Ctrl+Home | yes | yes (10.1 s) |
| 5 | F12 (Kit B has no pad) | yes | yes (10.1 s) |

**Not run: play in each cycle.** In these kit-host sessions the kit keyboard
did not reach the core. Typed keys never appeared at `A:\>`, yet the same
keyboard drove the menu and F12 and Ctrl+Home stopped the session.

- tenfoot had the `hilkb` device open.
- The host's `/api/v1/session/input/event` answered 503 `INPUT_UNAVAILABLE`
  ("no live input stream"), because remote input is off on the kit host.

This is consistent with [#1613](https://github.com/DeanoC/fes/issues/1613):
kit-host sessions on this bundle don't forward a USB keyboard. Its fix, PR
#1614, is open, so the baseline bundle `66316149` doesn't have it. Two log
lines from earlier in the evening show a `set_keyboard` call reaching
mister-runtime and being refused ("FES computer keyboard is inactive"). This
record doesn't say whether that is the same cause.

Play from the kit keyboard in a tenfoot session passed on 8 November (`tf1`,
through the shared host). It is owed again on Kit B's own host once #1614 is
in a bundle the kit runs.

**Cleanup.** Removed:

- the entry's saved floppy record (`core-data/media/9852d916…`). It was
  pulled first (`c7f17d85…`). The entry was then launched once more without
  it: it cold-booted to `A:\>`, and that Stop's record was removed too;
- `hilkb` and its FIFO;
- the ssh tunnel.

Apart from the package files, the data listing matches the pre-run one. The
kit snapshot matches the pre-run probe in every key: image `cd284ca3` good
with nothing pending, bundle `66316149` active and selected with nothing
pending, boot ID `66a149ef`, the quirk line, and `fat_idle` `64f1f16d`. The
agent was idle with the lease free, and Kit B was released at 00:47.

**Side effect, undone.** The first tunnel used Powerboat port 18787. Another
session's private ST host (`fogcast-api-bb5`, `~/fes-scratch/st1528/host`)
already listened there. So the first `core-install` and `core-media-install`
went into that host's library, and its `core-entry` call failed. Both
additions were then taken out:

- the package file was moved to `~/fes-scratch/xt1529/runs/tf2/st1528-undo`;
- the media rows were deleted from its database.

Its table counts match its pre-existing database (28 entries, 11 media), and
it still serves its 28 entries. The second tunnel used a port checked free
first, and its library listing was compared with Kit B's before any import.

## What remains for #1529

- Paratrooper play from the kit keyboard in a tenfoot session on Kit B's own
  host. This waits for #1614 (#1613).

The private evidence is on Powerboat in `~/fes-scratch/xt1529/runs/` (`q1`,
`q2`, `q3`, `r1`, `qd`, `rs1`, `tf2`). The seal is in
`~/fes-scratch/xt1529/seal-8d9b71fa`, and the MAME Rogue reference in
`~/fes-scratch/xt1529/mame/rogue`. Tool hashes are in
[kit-runs.json](kit-runs.json).
