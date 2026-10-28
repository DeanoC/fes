# FES visual language

**Status: updated 28 October 2026.** Home, Library, Details and Workshop use
the warm FES direction. Home and Systems are the built-in navigation screens, with internal System
and Sinclair setup templates. Installed authored rooms can supply their own
character while following the same text and focus rules.
The [room concepts](concepts/README.md) explain the broader direction;
[native references](references/README.md) show the reviewed renderer result
and a proposed Library target. Proposed layouts establish no hardware capability.
Read the [UX overview](README.md) for scope and [acceptance plan](acceptance.md)
for the outstanding checks.

FogCast owns presentation; its [canonical architecture](../../sources/FogCast/docs/ARCHITECTURE.md)
describes the existing renderer and theme roles. The
[rooms experience](../../sources/FogCast/docs/rooms-experience.md) supplies the
shared interaction contract.

## Hierarchy and room character

The first glance should identify the selected game or destination, whether it
can run on the selected setup, and the next action. Give that information a
stable place beside or below the selection. Show creator, machine, and a short
description next. Keep package identifiers, leases, generations, and diagnostics
inside Details or System where they answer a specific question.

The three complementary room types share these priorities:

- **Cartridge Lounge:** the everyday default, with a welcoming shelf, readable
  titles, recently played games, and an obvious route to the library.
- **Homebrew Club:** an authored discovery space using creator notes, featured
  projects, or navigable locations. Directional focus must still make each
  destination and its action clear.
- **Retro Workshop:** an inspectable machine and expansion shelf, with setup
  consequences visible beside the control that changes them.

Use one dominant action per selection. Home opens the chosen default room;
Library remains a dependable catalog; Rooms offers alternate presentations.
Authored artwork, layout, and tone may vary. Back, System, Details, readiness,
and output selection keep their meaning. Restoring a room restores its last
valid selection, including after delayed artwork arrives.

## Names and navigation

Machine labels are plain model names: Pong, Catch, Sinclair ZX81, Commodore 64
and the selected Spectrum model. Keep the FES wordmark in shared chrome; do not
prefix every machine or game. Package versions, hashes and HIL build tags belong
to package selection or technical details, not a machine heading or default setup
title. Existing generated machine titles are presented cleanly; genuine custom
and game titles remain intact. Core, package, media and saved-selection IDs do
not depend on these display names.

Home routes directly to Library, Systems and Settings. System setup and Sinclair
setup are internal templates opened from a selected system/session, excluded
from room and startup pickers. The former Lobby, Amiga, Mario, SNES, TMS and
Coleco demo rooms and separate C64 wrapper are removed. Independently installed
room packs remain available through the native menu.

## Palette seed

These **proposed seed values** retain the concept's warm paper, restrained green,
and muted colors in light and dark appearances. They describe design roles,
not a new theme-file schema.

| Role | Light | Dark | Use |
| --- | --- | --- | --- |
| Background | `#F4F0E8` | `#141918` | Main room field |
| Surface | `#FFFCF5` | `#202725` | Cards and shared panels |
| Raised surface | `#E9E6DC` | `#303936` | Groups and secondary controls |
| Primary text | `#243631` | `#EDF3E9` | Titles and essential information |
| Secondary text | `#53625A` | `#B5C3B9` | Supporting descriptions |
| Divider | `#CECFC0` | `#4A5650` | Decorative separation |
| Action/focus accent | `#235A46` | `#C8E3AC` | Primary controls and focus outline |
| Text on accent | `#FCFCF3` | `#203727` | Filled primary controls |
| Warm accent | `#EFD48D` | `#E6C882` | Editorial highlights |

Focus and availability need separate shapes and labels even when they share a
color. Gold is an editorial accent, not automatic evidence of a warning. Place
essential text on controlled surfaces rather than directly over variable art.

Check every rendered text/background pair, including selected and disabled
states. Microsoft's [Xbox contrast guidance](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/102)
specifies at least **4.5:1** for standard informational text and advises against
color-only meaning. The palette table is not a completed contrast audit.

## Typography and the native pass

Start with the existing embedded Go Regular and Go Bold faces, using bold for
titles and primary actions. Proposed native role seeds are:

| Existing role | Proposed size | Intended content |
| --- | --- | --- |
| `title_px` | 36 px | Room and selected-game headings |
| `body_px` | 24 px | Game labels, actions, and descriptions |
| `caption_px` | 22 px | Creator, machine, and supporting metadata |
| `status_px` | 24 px | Readiness and controller hints |

Native 1280×720 Home, Library and Workshop captures now exist. The
[reference guide](references/README.md) identifies their exact source and
fixtures; the [acceptance guide](acceptance.md) links the existing kit runs.
The Library and room pass has additional native captures at 1280×720 and
1024×600; see the [current reference set](references/README.md#current-native-pass).
These are software-renderer fixtures, with physical readability and transport
checks still required. Browser CSS dimensions and small supporting text are not
native specifications. Use the real Go
renderer to inspect long names, wrapping, glyph coverage, selected states and
action hints at the target resolution and default 5% safe area.

Microsoft's [text-display guidance](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/101)
uses **26 px at 1080p** as a console baseline and recommends scaling up to
**200%**. Those figures are external guidance, not FES's implemented settings.
Evaluate larger text through reflow and fewer visible items; essential text
must remain available without marquee animation or two-axis scrolling.

## Focus, input, and artwork

Focus uses a solid outline plus a stable selected-title treatment. Availability
uses a distinct labelled shape, with an icon where useful. Pressing a direction
follows the visible arrangement; opening Details and returning preserves position. These choices
follow the [Xbox UI-navigation guidance](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/112)
on predictable focus and consistent prompts.

Bind logical actions through the existing
[controller bindings](../../sources/FogCast/docs/rooms-controller-bindings.md).
Prompts match the connected pad; an unknown layout gets clear logical labels
and an inspectable control map. Every essential action has a reachable route
through directions and Confirm/Back while navigating the UI; a shortcut is an
additional route. Test a pad without a Guide or North button explicitly. Room
authors cannot suppress Back or the FES System menu. Kit-local gameplay exit
has one explicitly accepted exception: F12/Guide stop with one press, while a
Guide-less pad uses the Select+Start hold because its other buttons belong to
the game. See the [decision](https://github.com/DeanoC/fes/issues/770#issuecomment-6234959632)
and H2/H5 in [acceptance](acceptance.md). This exception does not apply to
Details or ordinary menu navigation.

Prefer attributed game screenshots or supplied cover art. Missing artwork falls
back to a full title, creator when known, and a simple system motif. Machine
illustrations and tool icons distinguish those destinations from games. Avoid
acronym-only cards. Keep titles as text elements outside the art. Loading or
missing media retains card geometry and focus; an empty catalog offers a useful
setup action instead of invented games.

## Library presentation

The shared presentation covers the existing Library grid, list, header, Details
presentation and their visible prompts. Reuse its data, view modes, filters,
artwork cache, readiness classifier, import flow and return behavior. The
[proposed Library sheet](references/library-target.svg) is a hierarchy and
interaction reference. Its fictional content and sample readiness are not a
production catalog specification.

| Element | Required result |
| --- | --- |
| Shared chrome | FES identity, warm neutral surfaces and the existing embedded proportional fonts; room character may vary within the shared language. |
| Selected title | Primary text stays on a controlled neutral surface, or uses the explicit text-on-accent role when filled. Selection never makes the title harder to read. |
| Focus | A solid outline and a stable selected treatment separate focus from readiness. Grid and list selection remain equally legible. |
| Selected-item summary | Title first, then known creator/machine metadata, truthful readiness and picture destination, then the useful next action. Omit absent metadata rather than invent it. |
| Actions | Confirm on a Ready item retains direct Play. A visible Details control is also reachable with directions and Confirm; Back restores the selected item and browse position. Blocked actions use the existing classifier and supported remedy. |
| Prompts | Full-word, readable prompts from the active logical mapping; do not reuse the small debug-font shortcut line as the primary instruction. Keep text out of variable cover art. |
| Missing art and empty results | A labelled machine/title fallback, stable geometry and useful filter/setup recovery. No fabricated library entries. |

Library uses paper title plates and a green outline independently of readiness.
Its header and footer use the same FES identity and proportional faces as Home.
Details keeps requirement reasons and the supported remedy. Default and Night
Library chrome share the warm surface; Arcade and custom resolved backgrounds
retain their existing look. No theme-file schema changed.

The earlier selected-grid title was `#ECF0F8` on `#FFB830` (about 1.51:1).
The current title uses `#243631` on `#FFFCF5` (about 12.44:1); action text uses
`#FCFCF3` on `#235A46` (about 7.76:1). These pairs do not constitute an audit of
arbitrary custom looks or authored artwork.

Treat the current renderer's Home and Workshop as implementation references.
Extend its existing color and text roles where necessary; this task needs no
general theme framework. Keep opaque surfaces and cached labels/artwork, and
measure changed-frame cost after the visual pass. The reference sheet does
not require an animated background, a new view mode, Games/Machines filtering
or a destination picker where only one output is supported.

### Directional action contract

Confirm on a selected Ready item plays directly. In Library, Down from the
bottom visible item row enters the footer actions; Left/Right chooses Play or
Details, Confirm activates, and Up or Back returns to the same item. Home places
its hero above the shelf: Up enters the hero actions, a second Up reaches the
header routes, and Down or Back returns to the same shelf item. Left/Right moves
within each row. Prompts describe the focused action.

Action focus captures item, package, media, expansion selection, readiness intent,
room, client and session context. A changed context cancels that focus; a stale
Confirm cannot activate the replacement item. Pointer and dedicated Details
commands retain their existing routes. This is launcher-local state, without a
new room or network API.

## Semantic states and copy

Preserve the shared room state names and add a concrete reason:

| State | Example supporting copy | Confirm outcome |
| --- | --- | --- |
| Checking | Checking the living-room setup… | Status remains visible; Back works |
| Ready | Ready on Living-room TV | Play |
| Needs a choice | Choose between two playable editions | Open the edition choice |
| Unavailable | Firmware required for this machine | Supported import action or Details |

Core installation alone does not establish readiness. Display, media, firmware,
expansion, and destination requirements come from existing services. Replace
opaque labels such as “Needs a video part” with the consequence and an available
action, such as “Display setup incomplete”; technical details remain inspectable.

Show where the picture will appear before launch. A remote controller is not
automatically a streamed display. Use “Recently played” without implying a
resumable snapshot. Preserve failed-save feedback according to
[core persistence](../core-persistence.md).

Follow [hardware rooms](../hardware-rooms.md): **Running** describes the observed
session; **Next start** describes saved configuration. “Saved for next start”
does not mean RAM was saved or running hardware changed. Show restart
consequences beside the action. Connection loss leaves running state unknown.

## Rendering budget and optional CRT work

The baseline is a complete still composition with cached artwork, opaque
panels, and limited compositing. Keep settled screens change-driven, while
preserving the existing status probes and recovery behavior. Avoid mandatory
parallax, continuously moving backgrounds, and stacked translucent effects.
Feedback must remain understandable when animation is disabled. Progress shows
only observed phases; a static launch frame must not promise live percentages.

Measure redraw cost, input response, and memory on the real menu-display path
before setting numeric performance budgets. Browser interaction is not that
measurement.

CRT presentation is an **optional follow-up scope** requiring supported output
and hardware checks. Explore fewer rows, pixel-aligned type, safe margins, and
picture-position adjustment rather than shrinking the HDMI layout. Zaparoo's
[Next frontend documentation](https://zaparoo.org/docs/next/frontend/setup/)
describes a useful CRT-specific precedent; it establishes no FES capability.
Track both the pending native pass and any later CRT checks in
[acceptance](acceptance.md).
