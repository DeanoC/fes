# Hardware rooms: the ZX81 workbench

The tenfoot Home screen includes **The ZX81 workbench** (`example.hardware`).
It shows the household's existing ZX81 core entries, the rear expansion
connector, installed expansions admitted for the exact selected package, and
the ordinary live tape picker. Create the entry and bind its BASIC firmware
through the existing FPGA library first. No hardware is fabricated when the
library is empty. The Amiga-themed `example.workbench` remains a separate room.

## Use the workbench

Choose a card on the shelf to inspect its description, then **Fit & save
setup**. **Remove & save setup** clears the connector. Both actions save the
core entry on the host, so another LAN client reads the same choice. **Next
setup** cycles existing ZX81 entries. These are saved hardware selections for
the next start, not snapshots of running memory.

**Start machine** takes the ordinary library launch path, including firmware
admission, target readiness, kit ownership and launch failures. During play,
the **Hardware room** button, keyboard **Home**, or a single controller
**Select/View** press opens the room without stopping the machine. The letter
H remains a computer key. **Return to play** or Back restores the playing view.

The room shows **Next start** separately from **Running**. The latter uses
the session's target-reported package/composition receipt. Editing a running
setup never programs hardware. When the two differ, **Restart needed** means
use **Stop machine**, then **Start machine**. Stop uses the existing save and
failure behavior; a failed save does not authorize the next launch. Stopping
clears ZX81's running memory. The consequence is always visible beside the
machine; saving a setup does not save RAM.

**Choose / eject tape** opens the existing `.p` picker while the active
session advertises the supported live-media binding. Import/replace/eject
reuse the session, target, package and generation guards. A successful swap
keeps the session and running machine. Enter `LOAD ""` on the ZX81 after
arming a tape; there is no cassette transport, playhead or automatic LOAD.
Busy, unavailable and failed operations retain their existing status and
retry handling. Returning from the picker keeps the room and focus.

Pointer controls, directional keyboard/controller focus, Confirm, visible
Back and Home all reach the same actions. Dragging, long presses and chords
are unnecessary. The illustration is decorative and labelled; a drawn,
labelled computer replaces missing artwork. Expansion descriptions are
household catalogue text, never proof of historical compatibility or
hardware acceptance.

## Data and ownership

`GET /api/v1/library/hardware` returns typed machine records and the existing
host session projection. Machine records carry the entry/package identity,
firmware readiness, one rear socket, saved expansion and admitted choices.
Local readiness does not assert that a kit is connected or available. The
session is a separate observation; connection loss leaves it unavailable,
not optimistically stopped or ejected.

`PUT /api/v1/library/core-entries/{game_id}/expansion` remains the sole fitting
operation. It requires the exact `package_id`, `expected_expansion_id` and new
`expansion_id` (empty removes). The host verifies the immutable expansion
against the selected shell and compares the old selection before writing.
Stale clients refresh instead of overwriting another client's setup. A failed
or ambiguous save is not replayed by the room.

Optional `GET`/`PUT /api/v1/core-expansions/{expansion_id}/presentation` stores
`label` and `description` against an exact expansion ID (catalogue schema 14).
For example, an operator can describe a verified memory expansion from its
producer's documentation. Unlabelled assets show a readable fallback and
explicitly lack a feature description. Presentation never affects admission.
No RAM size, sound feature or software compatibility is inferred from a bus
name or illustration. The API and `hostclient` methods are reusable by a
future LAN interface without duplicating compatibility rules.

Room Stop actions carry the displayed session, game, launch flight, target,
package and generation through the existing Stop endpoint. Public session IDs
can survive launches, so an ID alone is insufficient. The host rejects stale
actions before coordinator cleanup and rechecks the package binding under the
service lifecycle lock before physical Stop. Tape requests retain their
captured binding through retries instead of selecting a new foreground play.
Ordinary sessions without library package metadata retain their existing Stop
path.

The sandbox exposes `hardware.read(callback)` and
`hardware.select_expansion(selection, callback)` through optional typed
services. Lua has no network API. Normal launch/Stop/tape/navigation are
launcher actions. Per-room storage contains navigation only: selected entry,
inspected card, shelf page and focus. Physical transitions remain in runtime;
this slice changes no FPGA, runtime or shared wire definitions.

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
