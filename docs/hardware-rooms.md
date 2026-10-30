# Hardware rooms and system workbenches

The tenfoot **Systems** room (`example.computers`) lists prepared playable
library entries and their editions. Confirm starts the focused entry through
`session.launch`; **Set up** opens its workbench with the selected game and
core IDs. With no entries, **Add system** opens the system workbench so setup
remains available. Home also includes prepared host-library systems beside
kit-local installed packages.

## System workbench

`example.system` covers the ZX81 family, Commodore 64, the Spectrum 48K/128K/+3
and Pentagon models, Apple II, Atari ST, ColecoVision, Sega Master System,
SG-1000, Pong, Catch, RAM Tester and RISC-V. **Next system** chooses one of
12 system families. **Variant** chooses a model within that family's workbench:
Spectrum has 48K, 128K, +3 and Pentagon 128; the ZX81 family has ZX81, ZX80,
TS1000 and TS1500. Each family remembers its last browsed variant. Single-model
families have no variant selector. The family stays in the header while the
selected variant supplies its own setup, media guidance and library entries.

**Next machine** chooses a saved entry for that variant; **Next edition**
chooses one of that entry's grouped library editions. Changing a family or
variant only changes navigation: it does not rewrite a saved package, ROM,
medium or expansion, or replace a running machine. Installed packages and their
manifests still determine what can launch. `example.c64` uses the same
workbench with Commodore 64 selected.

**Set up system** lists installed package versions for the selected core and
reads their sealed ROM requirements. Choose the required files and add a
library entry. The installed-package route needs no published catalogue;
**Browse published systems** and **Settings → Systems** also provide the
published installation and setup route. Firmware replacement remains an
explicit household action. File selection uses the launcher's existing local
and saved-drive picker. Setup does not start a machine.

**Media for next start** reads the exact entry's installed-package media
capabilities. It offers the declared tape, snapshot, disk or blob roles and
checks their formats and sizes before import. Saving compares the displayed
package and previous media ID; another client's edit requires a refresh.
Cartridge ROMs are chosen through setup when the package uses linked ROMs.
C64 uses a 1541 `.d64`, Apple II uses `.dsk`/`.do`, Atari ST uses `.st`, and
Spectrum formats follow the selected model's declared capabilities. No
unsupported medium is enabled by its filename alone.

**Expansion sockets** reads the selected package's real socket topology and
saved choices. Import an archive for that exact package and socket, then
select it in a separate action to save it for the next start. Remove clears
only the selected socket. The host rechecks compatibility and the previous
selection. The room shows selected cards without inventing empty slots or
features from a system name; a package without a supported socket says so.

**Start machine**, **Stop machine** and **Return to play** use the existing
session lifecycle. A live media button is enabled only for the running entry
when its active package supports the operation. Kit HDMI additionally needs
the session-display plane. The kit opens its live picker directly; a host
window opens the active system's workbench. Session, target, package and
generation guards preserve the running machine during media changes. Failed
hardware reads disable live controls until a successful refresh. Returning
from setup refreshes entries and saved selections while retaining navigation.

These are UI and host-software changes. They neither rebuild core packages
nor establish hardware acceptance of an installed core or software bundle.

## Sinclair ZX81 workbench

**The Sinclair ZX81 workbench** (`example.hardware`) retains its specialist
rear-connector shelf, admitted expansion previews and attributed starter
cassettes. **Set up ZX81** chooses ZX81, ZX80, TS1000 or TS1500, then an
installed sealed package and its BASIC ROM. Prepared ZX81-family rows open
this workbench; the general workbench remains available from Rooms. The
Amiga-themed `example.workbench` is a separate room.

## Use the Sinclair workbench

The room follows the Retro Workshop direction in [the UX brief](ux/README.md).
The setup column shows **Running**, **Next start** and, while an unsaved card is
inspected, **Previewing … (not saved)** together. The machine illustration draws a ready saved card solid and keeps it there while a
ready inspected card is outlined beside it with a **Preview** label. A saved
card the host marks unavailable keeps its **Next start** label and is drawn in
the shelf's unavailable colour, not as a fitted card. An unavailable preview
stays in the text column and is not drawn on the connector.
Without a session reading, **Running** says unknown rather than not running. The restart and memory consequences sit beside **Fit & save setup**.

Choose a card on the shelf to inspect its description, then **Fit & save
setup**. **Remove & save** clears the connector. Both actions save the
core entry on the host, so another LAN client reads the same choice. **Next
setup** cycles existing ZX81 entries. These are saved hardware selections for
the next start, not snapshots of running memory.

**Import expansion** offers RAM, Zon X and QS Character Board. All are supported
choices; Zon X and QS carry **In progress** presentation marks. Import a producer
archive matching the exact installed shell, then fit it separately from the
shelf. This preserves the current one-card rear connector: combining multiple
cards is outside this change. The selected family supplies household presentation
text, while the immutable archive and host admission decide compatibility.

**Choose cassette** saves a `.p` import, an embedded starter tape, or no tape for
the next launch. The host's cassette label is shown on its own row above the
machine controls. A starter uses its display name. An import is labelled
"Imported cassette" plus the first eight characters of its digest; the admitted
filename is not stored on the machine record
([#801](https://github.com/DeanoC/fes/issues/801)). A label that does not fit
keeps its start and its end. Selection compares both the package and previous
media ID; another client's change requires a refresh. Opening the shelf imports nothing.
The three attributed starter tapes are Guess the Number (MIT, 1 KiB), Aritm
(GPL-3.0-or-later, 16 KiB recommended), and Character Display (MIT, 16 KiB
recommended). Their sources, licences, provenance and checksums are retained in
[the cassette package](../sources/FogCast/internal/zx81tapes/README.md).

**Start machine** takes the ordinary library launch path, including firmware,
selected media, target readiness, kit ownership and launch failures. On the kit's
single HDMI output, a running ZX81 package that advertises
`fes.video.session-display` and `fes.memory.hps-ddr` can show the workbench and
cassette picker in its own shell. The launcher occupies the full screen while
CPU execution, RAM, audio and the fitted expansion continue. A completed frame
switches to the controls; closing them drains scanout and returns machine pixels.
When no compatible installed selector package is available, the workbench keeps
prelaunch selection. Idle rendering still uses MENU.

For a saved entry with no fitted expansion, Start uses an installed ZX81 package
for the same target when it also declares session display, HPS DDR, the rear
socket and live media. The host compares the saved package before changing it,
then applies its ordinary media, firmware and persistence checks before launch.
Entries with an exact-package expansion keep their selected package.

On the capable kit display and on a separate host display, the live room is available. During play,
the **Hardware room** button, keyboard **Home**, or a single controller
**Select/View** press opens the room without stopping the machine. The letter
H remains a computer key. Arrows/d-pad navigate; Enter/A confirms; Escape/B
returns. On kit HDMI the room is a dark translucent overlay, so the machine
stays visible, dimmed, behind it. **Return to play** or Back first closes the
display plane, or with the TV surround keeps it shown with machine focus,
before restoring machine input. Held keys are neutralized when the controls open and require a
release and fresh press after return. Select+Start held for one second retains
the ordinary Stop action. An unconfirmed close keeps input focused on the UI
and offers retry; it never silently stops or restarts BASIC.

The room shows **Next start** separately from **Running**. The latter uses
the session's target-reported package/composition receipt. Editing a running
setup never programs hardware. When the two differ, **Restart needed** means
use **Stop machine**, then **Start machine**. Stop uses the existing save and
failure behavior; a failed save does not authorize the next launch. Stopping
clears ZX81's running memory. The consequence is always visible beside the
machine; saving a setup does not save RAM.

**Choose / eject tape** opens the existing `.p` picker while the active
session advertises the supported live-media binding. Import/replace/eject
reuse the session, target, package and generation guards. The live picker offers
the same attributed starters and bounded `.p` imports without changing **Next start**. A successful swap
keeps the session and running machine. Enter `LOAD ""` on the ZX81 after
arming a tape; there is no cassette transport, playhead or automatic LOAD.
Busy, unavailable and failed operations retain their existing status and
retry handling. Returning from the picker keeps the room and focus.

In any browsed folder, including an empty one, **Eject media** and **Type a
path** remain available. The list keeps directories and only shows files that
match the active medium's format and size. Local mount roots appear only when
they exist on the kit; saved FogCast host drives use the host's storage service.

Pointer controls, directional keyboard/controller focus, Confirm, visible
Back and Home all reach the same actions. Dragging, long presses and chords
are unnecessary. The illustration is decorative and labelled; a drawn,
labelled computer replaces missing artwork. Expansion descriptions are
household catalogue text, never proof of historical compatibility or
hardware acceptance.

## Data and ownership

`GET /api/v1/library/hardware` returns typed machine records and the existing
host session projection for the paired kit or requested target. Machine records carry the entry/package identity,
firmware readiness, one rear socket, saved expansion and admitted choices.
Local readiness does not assert that a kit is connected or available. The
session is a separate observation; connection loss leaves it unavailable,
not optimistically stopped or ejected.

`PUT /api/v1/library/core-entries/{game_id}/expansion` fits a single rear
connector; `PUT /api/v1/library/core-entries/{game_id}/expansions/{slot}` fits
a declared physical slot. It requires the exact `package_id`, `expected_expansion_id` and new
`expansion_id` (empty removes). The host verifies the immutable expansion
against the selected shell and compares the old selection before writing.
`GET /api/v1/library/hardware` advertises that write as `expansion_selection`.
The paired launcher admits setup, package installation, bounded media and
expansion imports, ROM/media selections, and declared slot writes after its
bearer and paired-target checks. Presentation edits and host filesystem
settings remain host operations.
Stale clients refresh instead of overwriting another client's setup. A failed
or ambiguous save is not replayed by the room.

Optional `GET`/`PUT /api/v1/core-expansions/{expansion_id}/presentation` stores
`label`, `description` and `in_progress` against an exact expansion ID (catalogue
schema 16). Migration defaults existing assets to false; older presentation
edits that omit the flag preserve its value.
For example, an operator can describe a verified memory expansion from its
producer's documentation. Unlabelled assets show a readable fallback and
explicitly lack a feature description. Presentation never affects admission.
No RAM size, sound feature or software compatibility is inferred from a bus
name or illustration. The API and `hostclient` methods are reusable by a
future LAN interface without duplicating compatibility rules.

Room Stop actions carry the displayed session, game, launch flight, target,
package and generation through the existing Stop endpoint. Public session IDs
can survive launches, so an ID alone is insufficient. The host rejects stale
actions before coordinator cleanup. The final package binding check, input
and media cleanup, and physical Stop share the service lifecycle lock. Tape requests retain their
captured binding through retries instead of selecting a new foreground play.
Ordinary sessions without library package metadata retain their existing Stop
path.

The sandbox exposes `hardware.read(callback)` and
`hardware.select_expansion(selection, callback)` through optional typed
services. The snapshot's `expansion_selection` is save permission and is
separate from socket or card readiness. An explicit false keeps the shelf
inspectable and tells the operator to save Next start on the host computer's
ZX81 Workshop. An omitted field means an older host: a paired kit treats it
as read-only, and an unpaired host keeps the existing save.
`hardware.setup({core_id=...})`, `hardware.open_media(selection)` and
`hardware.open_expansions(selection)` dispatch the general workbench's typed
launcher actions. Calling `hardware.setup()` without options retains the ZX81
family selector. `hardware.open_tapes(selection)` and
`hardware.import_expansion(selection)` retain the specialist ZX81 actions. Lua has
no network API. `GET /api/v1/library/zx81-tapes` lists starter metadata;
`POST /api/v1/library/zx81-tapes/{tape_id}/import` imports only that exact embedded
asset through the ordinary media store. Neither endpoint selects or launches it.
Normal launch/Stop/tape/navigation are
launcher actions. Per-room storage contains navigation only: selected entry,
inspected card, shelf page and focus. Physical transitions remain in runtime;
the shared display contract is defined in
[session display](../sources/mister-packages/docs/session-display.md).
`POST /api/v1/session/display` carries an explicit `visible` boolean with the
captured session/target/package/generation headers. It names `focus`
(`"launcher"` or `"machine"`) only when it differs from the default, which is
launcher focus for a shown plane and machine focus for a closed one. The
hardware room takes launcher focus, and a closed plane always returns focus to
the machine. The target operation requires
the existing kit lease. The runtime admits immutable full frames only while the
plane is open and revokes their separate display generation on close, replacement
or Stop. A display fault disables only that plane; idle recovery cannot replace
the running core. Paired launchers use the same guarded setup, import and saved-selection
operations as the host UI. Their live mutations remain bound to the paired
kit and captured play. Host filesystem settings remain host operations.

## Evidence

The machine-centred illustration, inspectable shelf, host-owned selections,
clear restart consequences and retained focus come from the original ZX81
room. Its dated [hardware-room verification](validation/2026-09-27-hardware-room.md)
accepts only the artifacts named there. General workbench software checks do
not extend that hardware evidence to other systems, packages or bundles.
