# PC/XT qualification reruns: plan

Issue [#1529](https://github.com/DeanoC/fes/issues/1529). This plan is
committed before any of these runs. It follows on from the
[7 November record](../2026-11-07-pcxt-qualification/summary.md). The game
set, disks and MAME reference are unchanged from that record's
[selection](../2026-11-07-pcxt-qualification/selection.md).

## What changed since the record

- [#1565](https://github.com/DeanoC/fes/pull/1565) (#1537): the CGA
  character ROM has all 256 CP437 glyphs, so Rogue's map draws.
- [#1569](https://github.com/DeanoC/fes/pull/1569) (#1534): Ctrl+Alt+Del
  finishes the warm POST, and every library launch gets a cold POST.
- [#1564](https://github.com/DeanoC/fes/pull/1564) (#1561): tenfoot's F12,
  Ctrl+Home and pad Guide stop a session that has no playing room. Plain
  Home goes to the core.

## Package under test

After #1569 merges, `fes.pcxt-dos` is sealed from a fresh checkout of that
main commit. The package, archive and RBF hashes, the producer commit and the
timing are recorded before the first kit run. Every case below except the
power-loss case uses that package. No earlier package's results carry over to
it.

The kit runs a software bundle that contains #1564. If the kit's baseline
bundle predates it, the main bundle is installed for the run and the baseline
is restored afterwards, under the kit rules.

## Cases

### Power loss with an unsaved write

This can run before #1569 merges. It uses the package sealed for #1565
(`4dd6d431…`), because the case tests the library record, not the core.

1. Launch the Paratrooper disk bound to a fresh game ID. Write `SAVED.TXT`
   and Save. Keep the record's SHA-256.
2. Write `UNSAVED.TXT`. No Save and no Stop.
3. Deano removes the kit's power at the wall, then restores it. The runner
   waits for the kit's boot ID to change.
4. **Expected:** the record is byte-identical to step 1. A relaunch cold-boots
   to `A:\>`, `TYPE SAVED.TXT` prints its marker, and `DIR` doesn't list
   `UNSAVED.TXT`. The library and agent start normally, with no repair
   prompt and no leftover session.
5. **PASS** if all of step 4 holds. A missing or changed record, a damaged
   FAT, or an agent that can't launch is a **FAIL**.

A raw (unbound) insert has no record to keep. The 7 November `s1` run
already showed that its writes are gone after Stop. This plan doesn't
cycle the power for it.

### The record's game cases

Each case from the 7 November table is run again with the same disks,
inputs and objectives:

- the COM/DOS baseline;
- Library Save, Stop, relaunch and readback, with the whole-image diff;
- an unsaved write, then Stop and relaunch;
- a raw insert, write, then Save, which must be refused;
- Paratrooper, 2 × 10 minutes;
- Alley Cat, 2 × 10 minutes;
- Rogue's game save and restore across a relaunch;
- speed and speaker pitch against MAME `ibm5150`, from Paratrooper and
  Alley Cat clips.

The pass rules are the 7 November rules. Alley Cat's window objective stays
**INCONCLUSIVE** unless the play actually enters a window.

### Rogue: two play runs and the stairs

Rogue's dungeon is random, and an XT has no clock, so each run's level
differs. The kit and MAME are compared on whether the objective is met and
how the map draws, not on matching frames.

Each run, up to 10 minutes of play from a fresh launch:

1. `ROGUE`, the name `fes`, and Enter.
2. **Map.** The lit start room draws: walls, floor and doors. The player is
   `☺` (01h). The status line shows level 1.
3. **Movement.** The player moves in all eight directions (`h j k l y u b n`),
   and the map and status line update.
4. **Stairs.** Explore until the stairs are in view (`≡`, F0h in the PC
   release; the glyph is confirmed in MAME before the kit runs). Walk onto
   them and press `>`.
5. **Objective met** when the status line shows level 2, the screen shows a
   new level map, and the turn counter keeps advancing after the descent.
   This is read from the captured frame's text, not from brightness or
   nonblank pixels.

Results:
- **PASS** if steps 2–5 hold.
- **FAIL** if the map doesn't draw, the game hangs or crashes, or input stops
  working.
- **INCONCLUSIVE** if the map draws and play works but the stairs aren't
  found within 10 minutes, for example because the rogue dies first or they
  are behind a secret door. That is the game's content, not a defect. The run
  is then repeated with a new game, up to two extra tries.

One MAME session is played to level 2 to show what a correct map and
descent look like on the reference.

### Ctrl+Alt+Del

At `A:\>`, press Ctrl+Alt+Del from the kit keyboard.

- **Expected:** the machine finishes a warm POST and boots the floppy back to
  `A:\>`. A following library relaunch cold-boots to `A:\>`.
- **PASS** if both happen. A hang, a beep halt or "Disk Boot Fail" is a
  **FAIL**.

### Repeated relaunch from the TV menu

Five cycles on the kit's tenfoot menu, paired with the shared Powerboat
FogCast host. The library entry from the 8 November `tf1` run is reused.

Each cycle:
1. Select the PC/XT entry from the Library and press Play.
2. Expect a cold POST, then `A:\>`.
3. Start Paratrooper and play about 30 seconds from the kit keyboard.
4. Stop: F12 in cycles 1–3, Ctrl+Home in cycle 4, and the pad's Guide button
   in cycle 5 if a pad is plugged in (F12 otherwise).
5. Expect the menu back, with the agent idle and no session left.

**PASS** if all five cycles complete. Any failed launch, any warm boot, or
any Stop that doesn't return to the menu is a **FAIL** for that cycle.

## What this plan leaves out

- **External timing.** External SDRAM, HDMI, HPS, reset and PLL limits stay
  with #835. nextpnr timing stays advisory under #840.
- **Physical ISA.** Physical ISA qualification is separate.
- **Sopwith.** It stays optional and isn't run.
- **#1429.** Its two items are RTL changes to the read-only `fes.pcxt`
  diagnostic (the unused `pcxt_fdc_card` wrapper, and `FLOPPY_WRITE` gating).
  They change package bytes and are out of scope for this record.
