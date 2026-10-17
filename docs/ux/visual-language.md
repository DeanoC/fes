# Proposed visual language

**Status: design proposal, 17 October 2026.** These choices develop the
[room concepts](concepts/README.md). They are a starting point for implementation
and usability review, not evidence of native rendering or hardware capability.
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

**The real 1280×720 pass is pending.** That geometry comes from the documented
menu-display path, not the browser prototype. Its CSS dimensions and small
supporting text are not native specifications. Render the proposed roles through
the real Go renderer, then inspect long names, wrapping, glyph coverage,
selected states, and action hints on the target display.

Microsoft's [text-display guidance](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/101)
uses **26 px at 1080p** as a console baseline and recommends scaling up to
**200%**. Those figures are external guidance, not FES's implemented settings.
Evaluate larger text through reflow and fewer visible items; essential text
must remain available without marquee animation or two-axis scrolling.

## Focus, input, and artwork

Focus uses a solid outline plus a stable selected-title treatment. Availability
uses an adjacent icon and text. Pressing a direction follows the visible
arrangement; opening Details and returning preserves position. These choices
follow the [Xbox UI-navigation guidance](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/112)
on predictable focus and consistent prompts.

Bind logical actions through the existing
[controller bindings](../../sources/FogCast/docs/rooms-controller-bindings.md).
Prompts match the connected pad; an unknown layout gets clear logical labels
and an inspectable control map. Every essential action has a reachable route
without a required chord, long press, or drag. Test a pad without a Guide button
explicitly. Room authors cannot suppress Back or the FES System menu.

Prefer attributed game screenshots or supplied cover art. Missing artwork falls
back to a full title, creator when known, and a simple system motif. Machine
illustrations and tool icons distinguish those destinations from games. Avoid
acronym-only cards. Keep titles as text elements outside the art. Loading or
missing media retains card geometry and focus; an empty catalog offers a useful
setup action instead of invented games.

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
