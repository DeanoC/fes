# Hardware rooms: the Sinclair ZX81 workbench

The tenfoot Home screen includes **The Sinclair ZX81 workbench** (`example.hardware`).
It shows the household's existing ZX81 core entries, the rear expansion
connector, installed expansions admitted for the exact selected package, and
prelaunch cassette selection. **Set up ZX81** first asks which machine — ZX81,
ZX80, TS1000 or TS1500 — then lists installed packages of that core and creates
a library entry from an installed sealed package and an explicitly selected
household BASIC ROM. This
uses the local library APIs even when the published-core catalogue is unavailable;
published Systems remains an optional installation route. No hardware is fabricated when the
library is empty. The Amiga-themed `example.workbench` remains a separate room. **Computers** (`example.computers`) is a shelf of prepared ZX81 and Commodore 64 library entries. Confirm launches the focused row through `session.launch`. **Set up** opens that row's workbench: `example.hardware` for ZX81 and `example.c64` for Commodore 64.

## Commodore 64 setup

`example.c64` lists prepared `fes.c64` library entries. **Next machine** and **Next edition** choose among those entries and their grouped editions. Socket 1, socket 2, and any other saved expansion are shown from `slot_expansions` and `expansion_id` already on the entry. The saved disk is the entry's `media_id` when `media_role` is `disk`, shown as a short prefix. This core's media unit is a 1541 `.d64` on unit 0. `.tap`, `.t64`, and `.crt` are not offered. **Start machine** calls `session.launch`. While that entry is running and the session reports removable media, **Choose disk** calls `session.open_tape` for the live disk picker. **Stop machine** uses the ordinary stop path. The room does not invent an REU or another drive.

## Use the workbench

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

`PUT /api/v1/library/core-entries/{game_id}/expansion` remains the sole fitting
operation. It requires the exact `package_id`, `expected_expansion_id` and new
`expansion_id` (empty removes). The host verifies the immutable expansion
against the selected shell and compares the old selection before writing.
`GET /api/v1/library/hardware` advertises that write as `expansion_selection`.
The paired launcher admits this one PUT after the bearer and paired-target
checks and forwards the body unchanged. It does not admit slot writes,
presentation edits, other library writes, or a different foreground target.
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
`hardware.setup()`, `hardware.open_tapes(selection)` and
`hardware.import_expansion(selection)` dispatch typed launcher actions. Lua has
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
the running core. Paired launchers have access to hardware/starter reads, exact
starter imports, tape imports of 1..16384 bytes, captured live operations, and
the single-entry next-start expansion PUT. Other library writes and host
filesystem settings remain host operations.

## Design provenance and next families

This is the working first slice of the 26 September 2026 hardware-room design,
originally explored with an Apple II Plus concept. Its durable decisions are
retained here: machine-centred illustration, an inspectable shelf, host-owned
configuration, clear restart consequences, live media and retained focus.
The original ignored `out/design/2026-09-26-hardware-room` preview had simulated
hardware and no backend; its browser checks are not acceptance of this room.

The implementation was rebased conceptually on FES
`aaf32d3f290ba67929e751a2436ebedf658fbf40`, rather than the design's `566e6660`.
Selected source now contains separate Apple II work, but this room offers only
ZX81. It neither exposes nor accepts Apple II cards/drives. A later machine
family must project its real admitted topology and capabilities through the
host; the single ZX81 connector does not imply multi-card composition. Coleco
and the Apple II Plus remain later room integrations. Disk writes, tape
transport, arbitrary hot-plugging and RAM snapshots are outside this slice.

The software checks and remaining native/controller/kit gates are recorded in
[hardware-room verification](validation/2026-09-27-hardware-room.md).
