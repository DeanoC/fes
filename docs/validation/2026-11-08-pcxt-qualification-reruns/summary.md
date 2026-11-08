# PC/XT qualification reruns, 8 November 2026

Issue [#1529](https://github.com/DeanoC/fes/issues/1529). The cases and pass
rules were published in [plan.md](plan.md), commit `bea000941`, before any of
these runs. Hashes are in [kit-runs.json](kit-runs.json). Nothing from the
games is committed.

## Result

| Case | Result | Run |
| --- | --- | --- |
| Power loss with an unsaved write | **PASS**: the saved record is byte-identical after the power loss; the saved file reads back and the unsaved file is gone | `pl1` |

The remaining cases wait for #1569 and main's sealed package.

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
