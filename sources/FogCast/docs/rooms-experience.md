# Tenfoot rooms: browse, set up and play

Home and Systems are the built-in navigation screens. System setup and Sinclair
setup are internal templates opened for the selected model, entry or running
session. Old demonstration rooms are removed. Independently installed room
packs remain supported; see [room authoring](rooms.md) and
[controller bindings](rooms-controller-bindings.md).

## Home and system selection

Home shows installed machines and prepared library systems with plain model
names. The product wordmark appears once in shared chrome; package names,
versions, hashes and test labels do not become machine headings or default
setup titles. Unknown systems retain their supplied name. User-authored game
and setup titles remain distinct from machine identity.

Library, Systems and Settings are visible header routes. Confirm on a Ready
shelf item plays directly. Up moves into its Play/Details controls; a second
Up reaches the header. Left/Right moves within a row, while Down or Back
returns to the same shelf selection. Delayed catalog or artwork replies must
not move focus. A changed item, readiness intent, package, media, target or
session invalidates captured action focus before activation.

Systems lists prepared entries and their editions. Confirm starts the selected
entry; Set up opens its exact model and entry. An empty shelf offers Add system.
Related models share a family workbench, including Spectrum 48K/128K/+3/Pentagon
and ZX81/ZX80/TS1000/TS1500. Family, model, setup and title edition are separate
choices; changing navigation does not rewrite any saved binding or running core.

Only Home, Systems and installed presentation rooms appear in room/default-start
pickers. Internal setup templates are not duplicate startup destinations. Missing
saved room IDs use the ordinary fallback without recreating removed demos.

## Library and Details

Library remains the complete catalog with grid, list and shelf layouts, search,
filters, favourites and collections. Confirm on a Ready title launches directly.
Down from the bottom visible item row enters Play/Details; Left/Right chooses,
Confirm acts, and Up or Back returns to the same title and browse position.

Details gives the title, known creator/system facts, readiness, destination and
supported next action. Curator notes are attributed and fit inside the panel.
Missing artwork leaves a labelled surface and keeps the same geometry. Focus,
readiness and recorded play history use separate labels or shapes.

| State | Meaning and Confirm outcome |
| --- | --- |
| Checking | Requirements are resolving. Acknowledge Confirm; never queue a launch for a later selection. |
| Ready | The current setup can launch the selected option. Confirm plays. |
| Needs a choice | More than one viable edition exists. Choose explicitly and retain a valid preference. |
| Unavailable | Explain the concrete requirement and supported remedy. A missing Coleco BIOS can open its import picker; other blocks retain Details and Back. |

Core installation alone does not establish readiness. Media, firmware, display,
expansion, ownership and session requirements still come from the existing
services. An unavailable backend does not become an offered choice.

## Setup and media

Setup lists installed versions and their real firmware/media requirements.
Versions and package identifiers distinguish package choices; they do not become
the saved title. Firmware replacement is explicit. Cartridge ROMs are selected
through setup, and removable media follows the chosen package's declared roles,
formats and sizes. See [system setup](../../../docs/hardware-rooms.md).

The setup shows Running separately from saved Next start configuration. Inspecting
an expansion changes only the preview. Fit/save changes Next start after a
validated reply; uncertain or conflicting replies require refresh rather than
silently replaying a mutation. Saving setup never claims RAM or program progress
was saved. Restart and Stop consequences stay beside the controls.

During play, the supported media selector opens over the active session and
returns to that session. Browse and Cancel preserve the running machine. Media
mutations stay bound to captured session, target, package and generation.
Unsupported or uncertain operations remain unavailable, with a retry or Back
route. Changing a saved entry's next-start medium does not silently change its
running machine.

## Launch, return and failure

The launcher owns launch feedback and duplicate-request prevention. Show observed
phases and elapsed time, without invented percentages. Failure retains the
selection and explains the next action. After Stop and required saving finish,
return to the same room and selected location. Failed saving remains visible
and cannot be presented as successful completion or permit replacement play.

Installed authored rooms can open child rooms. Back returns to the suspended
parent with its last valid selection; returning from a launched game preserves
the child and its parent stack. Opening Library suspends the room context until
Back returns to it. Authors cannot suppress Back or the system menu.

## Input and accessibility

Direction, Confirm, Details, Back and the system menu have visible routes. A
shortcut is additional; it is not the only menu route. Prompts match the active
input mapping. The kit's separately accepted gameplay-exit controls remain F12
or Guide, or a Select+Start hold on a pad without Guide; menu navigation does not
inherit that exception. See [UX acceptance](../../../docs/ux/acceptance.md).

Use readable text, controlled title surfaces, safe margins and a focus outline
independent of colour. Reduced motion does not remove feedback or navigation.
Missing and late artwork preserve identity and selection. User-visible errors
and unknown running state remain visible until resolved.

Played means recorded play activity (`play_count > 0` or `last_played_at > 0`).
Completed requires an explicit completion record; it is not inferred from a
launch, returning to a room, catalog metadata or a successful Stop. Catalog rows
currently have no household completion store.

Attract is off while a room is focused unless that room opts in. An authored
attract presentation must preserve focus and Direction/Confirm/Details/Back/
system-menu access. It does not imply gameplay is active.

## Evidence

Production-renderer captures test real Go/Lua drawing with fixture data. They do
not accept physical viewing distance, controller input, callback budgets or
menu-display transport. Hardware evidence names its exact bundle, base image,
package, inputs and exercised journey. Follow the
[acceptance record](../../../docs/ux/acceptance.md) and
[kit sharing procedure](../../../docs/kit-sharing.md); keep software checks,
hardware diagnostics and exact-artifact acceptance distinct.
