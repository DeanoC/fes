# pc386 hard-disk import from the host, 7 November 2026

This record checks the host-side path for putting a DOS hard disk on
`fes.pc386`: the CLI import, the web library panel's import and geometry
override, and a Kit A boot of each imported disk.

**Tree.** Branch `deano/pc386-hard-disk-host-import` at `7586a5f26`, on main
`4b33ec559`. The host and the CLI were built from that commit.

**Result.**
- **CLI.** `core-hard-disk-install` imported the 41,287,680-byte bench drive
  through `POST /api/v1/core-media/hard-disk`. That is above the 32 MiB
  general media limit. The host returned the drive's own SHA-256 and inferred
  geometry 80/16/63. `core-entry … hard-disk` created the entry, and
  `core-disk-geometry` then set 40/16/63, `infer` and 80/16/63. The host
  refused 81/16/63, which addresses more sectors than the image holds.
- **Web panel.** In headless Chromium, the panel imported a second build of
  the drive through `#core-media-file`. With the pc386 package selected, the
  label read "host storage up to 128 MiB", and it read 32 MiB before that.
  The panel showed "Disk geometry 80/16/63 … from its partition table",
  prefilled the new geometry fields with 80/16/63, and saved 40/16/63 and then
  80/16/63 as overrides. `GET /api/v1/library/core-entries` confirmed each
  value. The page logged no console errors ([`panel.log`](panel.log)).
- **Kit A.** Both entries, each with a stored 80/16/63 override, booted
  FreeDOS to `C:\>` ([CLI import](cli-prompt.png),
  [panel import](panel-prompt.png)).
- **The override reaches the kit.** Relaunched with a 40/16/63 override, the
  CLI entry booted the kernel. FreeCOM then failed with "Error reading from
  drive C:" ([frame](override-40-dos-error.png)), because the CHS geometry
  now ends halfway through the partition. The BIOS line still read
  39 MBytes ([frame](override-40-bios.png)), so its size line does not follow
  the CHS override.

## Inputs

| Input | Identity |
| --- | --- |
| Package | sealed `fes.pc386` `c89d6b0ce9e8174a…` (Quartus 17.0.2 seal of main's pc386 tree, #1522) |
| CLI disk | bench drive `90f9b17550cace11…`, 41,287,680 bytes, 80/16/63 |
| Panel disk | bench drive `4fbeb7d9d7391e25…`, an earlier build of the same drive |
| Kit | Kit A, baseline software `4efd34bf`, image `31b39063`, core-only (nothing installed) |

The bench drive comes from `~/fes-drives/pc386` (#1533). It holds
FreeDOS and PhilsComputerLab's freeware DOS Benchmark Pack, and no
commercial software. The two builds have different digests. That makes the
panel's import a real new import rather than a hit on the CLI's copy.

## How it ran

The shared Powerboat host held a live `kit2` session for another run. A
restart to swap its binary would have broken that session, so the shared host
was not touched. A private host built from the branch ran instead, on
`127.0.0.1:18788` with private state and Kit A as its only target. It
exercises the same host code.
- CLI: `fogcast --api http://127.0.0.1:18788 --json core-hard-disk-install …`,
  then `core-entry`, then `core-disk-geometry`
  ([`cli-hard-disk-install.json`](cli-hard-disk-install.json),
  [`cli-geometry.txt`](cli-geometry.txt)).
- Panel: a Node CDP driver opened `/`, chose the package, set the file
  input, and clicked Import media, Create entry and Override disk geometry
  ([screenshot](panel-override-80.png); the geometry fieldset is further down
  the dialog). The Claude-in-Chrome upload tool stops at 10 MB, too small for
  a disk image, so a CDP driver was used instead.
- Launches: `POST /api/v1/session/launch` with target `dev`. HDMI frames
  came from Kit A's ShadowCast. Each launch staged its image on the kit
  (about 24 s for 41 MiB, including boot).

Kit A was claimed in `kitA-STATUS.md`, with its lock held for the whole run.
After the run, the post snapshot matched the probe keys. The FogCast data the
run created on the kit was removed (`disk-images/` with both images,
`core-data/games/`, and two `core-data/media` records). The kit was then
logged FREE. Runners are in `~/fes-scratch/hd1541`.

## Limits and follow-ups

- The panel knows the inferred geometry only for an image imported during
  this panel visit, because the host keeps no inferred geometry on the entry.
  An entry selected later shows its override, or says it launches with the
  partition table's geometry.
- "From host drive" still stops at 32 MiB: `ui_storage.js` checks 32 MiB, and
  `storage.MaxFileBytes` sets the same limit on the server. So a disk image on
  a saved host drive cannot be imported from there. Raising that limit for
  the hard-disk route is separate storage work.
- The tenfoot side was not exercised. Launching from tenfoot is #1536.
