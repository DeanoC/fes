# FES v1 first release: first boot & setup proposal

> **Provenance:** copied on 2026-11-01 from the Cursor Page
> [FES v1 first release: first boot & setup proposal](https://cursor.com/canvas/store-69a21171-88eb-4513-a2c6-2ef67edd9c8f/264188bf-e444-4847-9e60-42e57fcded73)
> (needs a Cursor login). **This repo copy is now the editable source for agents**; update it here rather than on the Page.
>
> Milestone: [v1: DE10 first release](https://github.com/DeanoC/fes/milestone/2), issues #1341–#1356.
> Related: [GTM plan](de10-first-release-gtm.md), [support matrix](../support-matrix.md).

DE10-Nano v1 with four core families. Deano decided the five design questions on 1 Nov 2026; the screens, settings and milestone below reflect his answers.

**Decisions in brief (Deano, 1 Nov 2026):**

- v1 cores: ZX81 family, Spectrum family, Atari ST, C64. PC/XT (pcxt) and pc386 are v2.
- Home shows 4 family tiles plus a Recent/Continue row. No Library in v1.
- Home shows families only; the model is chosen inside the family.
- The user chooses the games folders; FES suggests but imposes no layout.
- Ship legally redistributable firmware where possible; show firmware info on first use of each system.
- Aim for broad controller support (GameControllerDB); test a short list well and mark those Tested.

## Contents

1. [v1 scope](#v1-scope)
2. [Design decisions](#design-decisions)
3. [First boot, screen by screen](#first-boot-screen-by-screen)
4. [Minimum v1 settings](#minimum-v1-settings)
5. [Milestone and issues](#milestone-and-issues)
6. [Sources](#sources)

## v1 scope

| Family | Models (packages) | v1 status |
| --- | --- | --- |
| Sinclair ZX81 | ZX81, ZX80, TS1000, TS1500 (`fes.zx81`, `fes.zx80`, `fes.ts1000`, `fes.ts1500`) | v1 |
| Sinclair Spectrum | 48K, 128K/+2, +2A/+3, Pentagon (`fes.spectrum`, `fes.spectrum128`, `fes.spectrum-plus3`, `fes.pentagon`) | v1 |
| Atari ST | 520ST (`fes.atari-st`); STE not yet packaged | v1 |
| Commodore 64 | C64 PAL (`fes.c64`); NTSC not yet packaged | v1 |
| PC/XT, pc386 | Bring-up | v2 |
| SMS, SG-1000, Coleco, Apple II, RISC-V, Pong | Legacy, not on current patterns | Held back |
| RAM Tester | Utility | Settings > Diagnostics only |

Held-back cores are hidden from the release catalog, not deleted. Because Pong is held back, the out-of-the-box playable path now depends on shipping legal firmware (decision 3), such as EmuTOS for the ST.

## Design decisions

| # | Question | Decision (Deano, 1 Nov) |
| --- | --- | --- |
| 1 | What Home shows | 4 family tiles plus a Recent/Continue row. No Library destination in v1. |
| 2 | Games folder layout | The user picks the folders. The picker suggests likely folders (e.g. existing `games/<System>` dirs) but no FES layout is imposed. |
| 3 | Firmware/ROMs | Ship legally redistributable firmware where possible (EmuTOS; Amstrad-permitted ZX/Spectrum ROMs; open C64 replacements if viable). Show firmware info on first use of each system. |
| 4 | Controllers | Aim to support any pad (GameControllerDB); test a short list well and mark those as Tested. |
| 5 | Model tiles on Home | Families only; the model is chosen inside the family. |

## First boot, screen by screen

| # | Screen | What the user sees and does | Default | Reused today | New |
| --- | --- | --- | --- | --- | --- |
| 0 | Boot / splash | FES logo while the kit comes up. With no first-run marker, go to 1; otherwise go to Home. | First run only | Idle/splash RBF and menu firmware | First-run marker, plus Run setup again in Settings |
| 1 | Welcome | What FES is, the four systems, and a note that some firmware is built in while other ROMs/games are the user's own. Continue or Skip setup. | Continue focused; Skip shows banners for unfinished steps on Home | Tenfoot renderer, paper/green chrome (#1187) | Welcome room and media copy |
| 2 | Controllers | Press any button: the pad is identified by name and becomes player 1, with a Tested badge for pads on the tested list. Bluetooth pairing steps. Keyboard works throughout. | First pad = P1, auto layout | GameControllerDB (#981), ports (#990), GUID profiles (#995), BT agent (#1058) | Pairing/status UI, button test, Tested-pad list (#980 slice 4) |
| 3 | Your games folders | For each family, pick a folder from USB, SD data or a network share. The picker suggests matching folders it finds (e.g. `games/Spectrum`); it can be skipped per family. Scan with progress, then show counts per family. | Suggested folder pre-selected when one is found; nothing created | Drive browser (storage_settings.go), library roots, scanner | Per-family folder picker with suggestions, in-UI scan and results |
| 4 | Ready | Summary: pad, folders, titles per family, and which families run on built-in firmware and which need the user's ROM. | Finish | Readiness reasons (library_detail.go) | Summary screen |
| 5 | Home | Recent/Continue row (hidden until a game is played) above four family tiles: ZX81, Spectrum, Atari ST, C64. Each tile shows Ready, Needs firmware or No games yet. No Library entry. | Focus on Continue if present, else the first Ready family | Home leads to Systems; family workbenches with a Variant selector (#1182); plain names (#1187) | Recent/Continue row, v1 family filter, icons, Library removed from navigation |
| 6 | First use of a family | One-time firmware info card: which firmware is built in and its licence (e.g. EmuTOS), or which ROM file the user needs and where to put it. Dismiss and go to the family workbench. | Shown once per family; re-openable from the family's Details | Readiness and Needs BIOS states | Firmware info card and bundled-firmware packaging |

> **Network is optional.** No first-boot step needs a network or host PC. The image carries no shared token, and pairing with a host is opt-in per device.

## Minimum v1 settings

| Section | Contents | Exists today? |
| --- | --- | --- |
| Controllers | Connected pads with a Tested badge, player, press-to-bind remap, Swap A/B, reset, Bluetooth pairing | Backend yes; page no (#980 slice 4) |
| Games folders | Folder per family, rescan, firmware folder for user ROMs | Partly: Drive screen and typed-path roots |
| Firmware | Per family: built-in or user-supplied, licence text, status | No |
| Display & sound | Resolution (720p default), safe area, volume | Partly (settings_video) |
| Network | Wired status, Wi-Fi, optional host pairing | Partly, developer-facing |
| Updates | System, FES software and core versions; update and roll back | Backend yes; UI no |
| About & help | Versions, kit ID, quickstart link, Run setup again, diagnostics, factory reset | No |

Out of v1: a Library destination, per-core remap overrides, themes, multiple users and mesh settings.

## Milestone and issues

Milestone [v1: DE10 first release](https://github.com/DeanoC/fes/milestone/2). Sizes: S = a few days, M = 1–2 weeks, L = 2–4 weeks. Rows marked Existing got the milestone and a cross-link rather than a new issue. The duplicate check on 1 Nov found no existing release milestone or matching issues; #489 (ST qualification) and #387 (licence-notice audit) are related and cross-linked.

| Item | Issue | Title | One line | Size | Lane |
| --- | --- | --- | --- | --- | --- |
| 1 | #1341 | v1 scope: 4 families in the catalog, hide legacy cores | Release filter in catalog and Home; support matrix marks v1, v2 and held back | S | lane:deano |
| 2 | #1342 | UX design: first boot, Home and Settings for v1 | Reference renders for screens 0–6 and the settings map, from the decisions above | M | lane:deano |
| 3 | #1343 | Firmware licensing research | Confirm redistribution terms for EmuTOS, Amstrad's Sinclair ROM permission (ZX80/81, Spectrum 48K/128K/+3) and open C64 KERNAL/BASIC replacements; decide what ships | S | lane:deano |
| 4 | #1344 | Bundle legal firmware and the first-use firmware info card | Package approved firmware with licences; one-time info card per family | M | lane:deano |
| 5 | #1345 | First-run flow: welcome, summary, marker, Run setup again | Tenfoot welcome and summary rooms; skip and resume | M | lane:deano |
| 6 | #1346 | Per-family games-folder picker with suggestions | User-chosen folders, suggested matches, in-UI scan with progress and results | L | lane:deano |
| 7 | #1347 | Home: family tiles plus Recent/Continue, no Library | Recent/Continue row, 4 family tiles with readiness, Library removed from navigation | M | lane:deano |
| 8 | #980 | Controllers page and first-run pad step (Existing #980) | Slice 4 plus a Tested badge and the first-run step | L | lane:deano |
| 9 | #1348 | Tested controller list | Pick and HIL a short list of wired and Bluetooth pads; publish as Tested | S | lane:deano |
| 10 | #1349 | Settings surface v1 | Display, network, firmware, About/help, factory reset, diagnostics | M | lane:deano |
| 11 | #1350 | Updates UI and recovery (Existing #917 folds in) | One Updates page over image, software and cores; keep/pin an image for rollback | M | lane:deano |
| 12 | #727 | Consumer release image with clean first boot (Existing #727 folds in) | Checksummed image, no dev token, per-device config, recovery; trial-boot hang fixed | M | lane:deano |
| 13 | #1351 | Family icons and empty/error states | Original icons for 4 families; copy for every not-ready state at 720p and 1024×600 | M | lane:deano |
| 14 | #1352 | HIL: exact release packages, ZX81 family | ZX81/ZX80/TS1000/TS1500 on release bytes: video, audio, input, load, SAVE, Stop | S | lane:lander |
| 15 | #1353 | HIL: exact release packages, Spectrum family | All 4 packages: tape, disk (+3/Beta), snapshot, input, Stop | M | lane:lander |
| 16 | #1354 | HIL: exact release packages, Atari ST and C64 | Re-seal on current bytes (see #489); ST GEM, disk, save; C64 BASIC and disk load | M | lane:lander |
| 17 | #1355 | Release-candidate acceptance on the named image | Full GTM checklist on a freshly flashed DE10, clean profile, offline | M | lane:lander |
| 18 | #1356 | Public quickstart and release page | Flash, first boot, pads, folders, firmware notes, known issues | M | lane:deano |

#770 (UX tracker) has the milestone and links to items 2, 5 and 7. Filed: 16 new issues, #1341–#1356 (all items except 8 and 12, which reuse #980 and #727; #917 folds into item 11 as a linked dependency, so item 11 is still new). #489 and #387 are cross-linked.

**Order:** items 1–3 first, then 4–7, 9, 10 and 12 in parallel, then 8, 11 and 13, then HIL 14–16 on frozen packages, and 17–18 last.

## Sources

- [GTM plan: de10-first-release-gtm.md](de10-first-release-gtm.md)
- [Support matrix](../support-matrix.md)
- [#1182 family workbenches (merged)](https://github.com/DeanoC/fes/pull/1182)
- [#1187 tenfoot names and navigation (merged)](https://github.com/DeanoC/fes/pull/1187)
- [#770 FES UX tracker](https://github.com/DeanoC/fes/issues/770)
- [#980 Input devices and Controllers page](https://github.com/DeanoC/fes/issues/980)
- [#727 image never confirms on kit B](https://github.com/DeanoC/fes/issues/727)
- [#917 release store prunes baseline image](https://github.com/DeanoC/fes/issues/917)
