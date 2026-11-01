# Bundled firmware

**Status:** implemented for [#1344](https://github.com/DeanoC/fes/issues/1344).
No kit check has been run on it yet; see [Kit check](#kit-check).

FES v1 ships the firmware it may redistribute as separate, replaceable files
beside the cores. The host imports each one into its core-media store and
selects it by default for titles that have no firmware chosen. A user can
still choose their own image for any title. No firmware byte is in Git or in a
sealed core package.

## What ships

Deano chose the set on 1 November 2026, from the
[firmware licensing research](https://github.com/DeanoC/fes/pull/1376)
(`docs/plans/v1-firmware-licensing.md`, decisions in
[this comment](https://github.com/DeanoC/fes/pull/1376#issuecomment-6558814543)).

| Object | Core and ROM | Image | SHA-256 | Licence |
| --- | --- | --- | --- | --- |
| `amstrad-spectrum-48k` | `fes.spectrum` `spectrum-firmware` | 48K ROM, 16 KiB | `d55daa43…cb42` | Amstrad's 1999 permission |
| `amstrad-spectrum-128k` | `fes.spectrum128` `spectrum128-firmware` | 128K ROM 0 then ROM 1, 32 KiB | `c1ff621d…8d72` | Amstrad's 1999 permission |
| `amstrad-spectrum-plus3` | `fes.spectrum-plus3` `spectrum-plus3-firmware` | +2A/+3 ROMs 0–3, 64 KiB | `ee8218fa…bd1f2` | Amstrad's 1999 permission |
| `emutos-192us` | `fes.atari-st` `atari-st-firmware` | EmuTOS 1.4 `etos192us.img`, 192 KiB | `8fbbf8b4…4cfc` | GPL-2.0-only |

The three Spectrum images are the ones the
[2026-10-22 kit check](validation/2026-10-22-spectrum-models-seal.md#kit-a-check)
booted on each model (`48.rom`, `128.rom` and `plus3.rom`). They come from
Debian's `spectrum-roms_20081224.orig.tar.gz`: `48.rom`, `128-0.rom` +
`128-1.rom`, and `plus3-0.rom` to `plus3-3.rom`, concatenated in that order.
Other versions of these ROMs are not bundled; users can still choose them.

EmuTOS is labelled GPL-2.0-only. Its own files allow later versions, but the
Caldera GEM files it contains do not. Its object carries the GPL text from the
binary archive (`LICENSE.txt`) and names the corresponding source,
`emutos-src-1.4.tar.gz` (SHA-256 `98977ab2…9702`, tag `VERSION_1_4`). GPL v2
§3 needs that tarball to be offered from the same place as the binary, so the
release page must carry it ([#1356](https://github.com/DeanoC/fes/issues/1356));
`scripts/bundled_firmware.py --sources DIR` fetches it.

Not bundled, and user-supplied in v1: the ZX81, ZX80, TS1000 and TS1500 ROMs,
the whole 48 KiB Pentagon image (its TR-DOS half has no permission), and the
C64's 36 KiB image. Nothing is preinstalled on sold hardware.

The Amstrad permission asks that the copyright messages stay unaltered, that
no one charges for the ROMs, and that this sentence is shown:

> Amstrad have kindly given their permission for the redistribution of their
> copyrighted material but retain that copyright.

Each Spectrum object's notice carries it, and the first-use card shows it.

## Objects

[`scripts/bundled_firmware.py`](../scripts/bundled_firmware.py) pins every
archive by URL and SHA-256 and every image by SHA-256. It writes one directory
per object:

```text
firmware/
  amstrad-spectrum-48k/   manifest.json  spectrum-48k.rom
  emutos-192us/           manifest.json  etos192us.img  LICENSE.txt
  …
```

`manifest.json` (format 1) names the object `id` (its directory), `name`,
`version`, `core_id`, `rom_id`, `file`, `size`, `sha256`, `licence` (SPDX, or
`LicenseRef-Amstrad-ROM-permission`), `licence_name`, `notice`, the optional
`licence_file`, `origin` (the archive's URL and SHA-256) and, for free
software, `source` (the corresponding source's URL and SHA-256). FogCast's
[`internal/bundledfirmware`](../sources/FogCast/internal/bundledfirmware/bundledfirmware.go)
reads it strictly: unknown fields, a missing field, a bad digest or size, a
symlink, or a second object for the same core and ROM skips that object, and
the rest still load.

```sh
python3 scripts/bundled_firmware.py --output /tmp/fes-firmware
```

Downloads are cached in `out/firmware-cache` and reused only while their
digest matches.

## Where it ships

`make kit-software` stages the objects into the bundle at
`share/fogcast/firmware/`, so a kit has them at `/usr/share/fogcast/firmware/`.
The bundle receipt lists each object's ID, SHA-256 and size under
`bundled_firmware`.

The host that links firmware reads them from the directory its config names:

```toml
[firmware]
bundled_dir = "/usr/share/fogcast/firmware"
```

A host on a PC or Powerboat points `bundled_dir` at the output of
`scripts/bundled_firmware.py` instead. With no `[firmware]` table, or a
missing directory, the host has no bundled firmware and behaves as before.

## Host behaviour

At start the host imports every valid object into the core-media store; the
media ID is the image's SHA-256, so a restart imports nothing new. When a
format-3 title's package names a firmware ROM and the title has no selection,
the first firmware read selects the bundled image for that core, ROM ID and
exact size, with the existing compare-and-swap selection. A title with its
own selection keeps it. After a package change, a title still on the bundled
image moves to the new package if it names the same ROM ID and size; other
selections stay as the [named ROM selection](../sources/FogCast/docs/core-package-library.md#named-rom-selection-for-format-3-packages)
rules say. Library readiness (`firmware_ready`) and launch use the same path.

`GET /api/v1/library/firmware/bundled` lists the loaded objects without their
bytes: `id`, `name`, `version`, `core_id`, `rom_id`, `size`, `media_id`,
`licence`, `licence_name`, `licence_text`, `notice`, `origin_url` and, for
free software, `source_url` and `source_sha256`. Paired kits may read it.

The host used to embed the ZX81 BASIC ROM and select it for every ZX81 title.
That embed is gone: ZX81-family firmware is user-supplied in v1.

## The first-use card

The system workbench asks tenfoot for a family's firmware card whenever it
shows a family, including after **Next system**; the Sinclair setup room,
which the Systems shelf opens for the ZX81 family, asks on opening. Tenfoot shows it
once per family: **A** (*Continue to …*) records the family in `tenfoot.json`
under `firmware_info_seen`; **B** closes the card and leaves the workbench
without recording it, so it shows again next time. The workbench's
**Firmware** button in either room reopens it at any time.

The card says which models' firmware is built in, with the licence name and
notice, or which file each other model needs and that it is chosen in
**Set up system**. **Y** opens the licence page (licence, notice, origin,
source and the full licence text) when anything is built in, or system setup
when nothing is. Guided setup preselects the bundled image for a firmware ROM
and shows it as *built in*.

Host and kit software ship separately. Tenfoot reads the list from the new
route; a host without it answers 404, and tenfoot then claims no built-in
firmware: every model's card names the file the user needs. If the host
cannot be asked at all, the first-use card is skipped and not recorded.

Not built yet: the Home family tiles and their Details
([#1347](https://github.com/DeanoC/fes/issues/1347)) will open the card from
there, and Settings › Firmware ([#1349](https://github.com/DeanoC/fes/issues/1349))
will list each family's firmware and offer to return a title to the built-in
image.

## Kit check

Owed before release, on a kit-software bundle built from this change and
installed on the kit's running image:

1. A Spectrum 48K title with no user ROM boots to the Sinclair copyright
   screen on `amstrad-spectrum-48k`.
2. An Atari ST title with no user ROM boots EmuTOS 1.4 on `emutos-192us`.
3. The first-use card shows once per family, the Firmware button reopens it,
   and B leaves it unrecorded.
