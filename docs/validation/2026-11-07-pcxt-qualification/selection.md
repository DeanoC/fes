# PC/XT qualification game set, published before testing

Issue [#1529](https://github.com/DeanoC/fes/issues/1529). This file was
committed before the first kit run of the qualification, and the results are
recorded separately in `summary.md`. Nothing here is a result.

## Package and combination under test

- **Package.** `fes.pcxt-dos` package ID
  `87fd8149c09a7e319092c56802b891039a118dfe0378d302a6543bcf841659fc`, archive
  `19fd34ec5d193e2e8c6b7723ea86b02bb9e8f76bd4febf2a77c88a0759b224d8`, RBF
  `94b3c222bee23d24fd027a8c908d90aa9f53eae9b01db648e655f8b1d7ac0222`, produced
  from main `bb87b9b657bf9f1826dd1b9e001736e7bd8484ee`.
- **Selection.** The cache-only resolver (`core_dev.prepare` with building
  disabled) selects that same archive for main
  `607922c63d45f798dd3f6fb0ce443f346c324852`. No PC/XT source changed between
  the two commits. The archive comes from the private package cache of the
  5 November short smoke, `FES_ARTIFACT_CACHE_ROOT=/data/deano-pcxt-short-smoke-b-v2/cache/core-packages`;
  the shared cache has no copy. Prepared selection receipt
  `8c427a5dddeda7daa1cdb5985b9021aa19b7001e42bace302ac85107d460b6f9`.
- **Kit.** Whichever of Kit A or B is free, on its baseline software
  `a8cd30c4…` (main `bb51f445`) and image `f904961e…`. Nothing is installed.

## Disks

Every disk is the 5 November COM disk (`com.img`,
`ad4783e8b4decc6859df9389d374cb60ba55b735a0694901e48e02038aa67480`: FreeDOS
kernel 2043, FreeCOM 0.86 and `FESCOM.COM`) with one game's files copied to
the root by `mcopy`. So the DOS baseline is on every disk. The games come from
Deano's eXoDOS 6.04 zips. They are commercial or freeware payloads used
privately, and nothing from them is committed.

| Key | Title | Zip SHA-256 | Files on the disk | Disk SHA-256 |
| --- | --- | --- | --- | --- |
| `para` | Paratrooper (Orion Software, 1982) | `43242ed7…` | `TROOPER.COM` (16 400 B) | `c3440903…` |
| `cat` | Alley Cat (IBM/Synapse, 1984) | `bf6c5099…` | `CAT.EXE` (55 067 B), the CGA build | `18804578…` |
| `rogue` | Rogue (Epyx/A.I. Design PC release; eXoDOS "v1.1") | `ac3e503c…` | `ROGUE.EXE` (99 584 B) | `23974a97…` |
| `sop` | Sopwith (BMB Compuscience, 1985), optional fourth | `acbd93ee…` | `SOPWITH.EXE`, `SOPWITH0.DTA` | `37103623…` |

## Why these

- **Different loaders.** Paratrooper is a single 16 KB `.COM` that began as a
  self-booting disk. Alley Cat is an `.EXE` with relocations. Rogue is a large
  `.EXE` that opens, writes and reads its own files. Sopwith is an `.EXE` with
  a data file.
- **Different controls.** Paratrooper aims a turret with left and right and
  fires with up. Alley Cat moves and jumps with the arrow keys. Rogue takes
  typed letter commands. Sopwith flies with several letter keys held together.
- **Timing-sensitive graphics.** Paratrooper and Alley Cat drive CGA 320×200
  four-colour graphics from CPU-speed loops; at a wrong speed they run visibly
  fast or slow. Rogue uses CGA 80×25 text. Sopwith scrolls a landscape in CGA
  graphics.
- **Speaker.** Paratrooper's shots and explosions and Alley Cat's tunes play
  through the PIT-driven PC speaker.
- **Saves.** Rogue saves its game to a file on the floppy, and restores it
  when started with that file's name.

## Objectives and expected behaviour

Each game gets two independent runs (separate launches), with at least 10
minutes of active play in each run and one explicit objective.

| Game | Objective | Expected behaviour |
| --- | --- | --- |
| Paratrooper | The turret swings through its full left and right range and shoots down at least one helicopter or paratrooper (the score rises); play continues until a game over, then a new game starts. | Helicopters cross the screen, paratroopers drop and land; shots and explosions sound on the speaker. |
| Alley Cat | The cat moves left and right, jumps onto the fence and washing lines and enters a window (a level transition into a room), and a life is lost and play resumes. | The alley scrolls the bins, fence and lines; the title tune and in-game sounds play on the speaker. |
| Rogue | The rogue moves in all eight directions, finds the stairs and descends (`>`), reaching level 2 or deeper. | The level map draws in CGA text; the status line updates. |
| COM/DOS baseline | `FESCOM` shows `READY (PRESS K)`, `K` gives `PASS`, and the prompt returns. `DIR` lists the disk. | As in the 5 November smoke. |

## Save and reload

- **Library Save.** A guest file is written, the floppy is saved with the
  library Save, the machine is stopped and relaunched with the same library
  binding, and the file is read back. The saved image is compared byte for
  byte with the base image; only the new file's clusters, its directory entry
  and the two FAT copies should change.
- **Game save.** Rogue saves with `S` to a file, the floppy is saved, the
  machine is relaunched, and `ROGUE <file>` restores the same level and
  status line.
- **Unsaved writes.** A file is written with no Save before Stop. The result
  after relaunch (present or absent) is recorded as the observed loss policy.
  The same check is made for a raw (unbound) insert.
- **Cold power cycle.** Not done without Deano's separate OK.

## Reference

MAME 0.276 (Debian `mame 0.276+dfsg.1-1+deb13u1`), driver `ibm5150`: an
Intel 8088 at 4.77 MHz with IBM CGA and the PC speaker, booting the same
disk images with the IBM BIOS. It is run headless with its video and audio
recorded. The kit and reference are compared on game speed (the cadence of a
measured moving element), speaker pitch of a known sound and the stability of
the picture. Brightness or non-black frames are not used as proof of play.
