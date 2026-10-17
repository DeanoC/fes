# Library and Details: host render record, 17 October 2026

**Evidence class: host-only.** These are native software-renderer captures
and focused host tests for [#789](https://github.com/DeanoC/fes/issues/789)
(the second #770 slice). No kit was used.

- Base: FES `315acf51`; branch `claude/ux-library-details`. The PR records
  the result commit.
- Renderer: `gfx.NewSoftware` through the production `presentFrame`.
- Data: the host-free Library fixture in `ui/tenfoot/library_render_test.go`
  (`libraryFixture`), with ready, missing-BIOS, browse-only, offline and
  missing-cartridge rows. Half the rows have generated cover art.

Regenerate from `sources/FogCast`:

```sh
FES_ROOM_SCREENSHOT_DIR=/tmp/lib go test ./ui/tenfoot/ -run TestLibraryNativeRender -count=1
```

| Scene | Before | After |
| --- | --- | --- |
| Grid | [before](before-grid-1280x720.png) | [after](after-grid-1280x720.png): cells that can't play are tagged; missing art names the platform |
| Blocked title focused | [before](before-blocked-1280x720.png) | [after](after-blocked-1280x720.png): Donkey Kong shows "Unavailable · Needs BIOS" |
| A on a browse-only title | [before](before-blocked-confirm-1280x720.png): header text only | [after](after-blocked-confirm-1280x720.png): Details opens with the reason; no Play offered |
| Details, ready | [before](before-details-1280x720.png) | [after](after-details-1280x720.png): "by Homebrew Collective" beside the title; Ready pill |
| Details, needs BIOS | [before](before-details-blocked-1280x720.png): no reason | [after](after-details-blocked-1280x720.png): pill, headline, the import sentence and "A import BIOS" |

## Focused checks

`go test -race ./ui/rooms/ ./ui/tenfoot/`: pass. New tests:

- Home → Library → Back resumes the same Home instance and selection.
- The Home key from the Library reclaims the suspended Home.
- A blocked Confirm opens Details with the reason, and the hint does not offer Play.
- Cancelling the BIOS picker returns to Details on the same title.
- Each block's cell tag.

## Not covered

L1 and L2 on a kit with a real host library: cross-surface identity, the BIOS
import remedy end to end, search/filter interplay with Back, and the offline
host case.
