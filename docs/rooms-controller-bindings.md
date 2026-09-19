# Room controller bindings

Maps the logical actions in [rooms-experience.md](rooms-experience.md) §3
onto the tip tenfoot / rooms input path. Button prompts should follow the
connected controller; this page is the physical table those prompts resolve
to.

**Present tense** describes `ui/tenfoot` on main tip (`input.go`,
`keyboard.go`, `sdl.go` GUIDE, `room.go` delivery). Rows marked **proposed**
or **gap** are not current room behaviour.

Attract is not a common controller action. Decided policy (2026-09-19, PR
#271): default **off** while a room is the focused surface; a room author may
opt in, including a **custom attract**. When attract runs it must not steal
focus, move selection, or block Direction / Confirm / Details / Back /
System menu. Missing attract-in-room is consistent with default-off, not a
rooms-input regression. Policy text lives in `rooms-experience.md`.

---

## Rule: no essential-only long press or chord

These five actions are essential in a room:

| Action | Behaviour |
| --- | --- |
| Direction | Move within the room |
| Confirm | Perform the displayed primary action |
| Details | Open information and available actions for the selected game |
| Back | Close the current panel or return to the previous room |
| System menu | Open a FES-owned menu with a reliable route Home |

**No essential action may have a long press or button combination as its only
route.** Long-press and chord bindings that already exist on the sofa are
optional shortcuts (see below). Authors may style prompts; they must not
suppress Back or the system menu.

---

## Logical action ↔ tip command

SDL gamepads use a **positional** face-button layout (Xbox-style). FogCast
maps those positions after SDL normalization (`CommandFromButton`,
`commandFromSDLButton`). `-input-profile identity|swap-ab|/path.json` remaps
buttons after that step; GUIDE / `CmdSettings` is not remapped.

| Logical action | Tenfoot command | Room `on_input` name | Tap / click route exists? |
| --- | --- | --- | --- |
| Direction | `CmdUp` `CmdDown` `CmdLeft` `CmdRight` | `up` `down` `left` `right` | Yes (d-pad, left stick, arrows) |
| Confirm | `CmdSelect` | `select` | Yes (South / Enter / primary click on a hit) |
| Details | none | none | **Gap** — see below |
| Back | `CmdBack` | `back` (unconsumed Back leaves the room) | Yes (East / Esc) |
| System menu | `CmdSettings` | not delivered (launcher owns it) | Yes (GUIDE / `o`) |

`CmdHome` (rooms picker) is **not** an essential action. It is a shortcut to
the picker; the essential route Home is **System menu → Home**.

While a room is focused, the launcher hold-gate is off for Confirm / Search /
Sort so those buttons fire on tap. Back still accepts an optional long-press
(Home / rooms picker) **in addition to** tap Back.

---

## Binding table (common pads)

PlayStation names below are the **SDL Western** layout (Cross = bottom =
South, Circle = right = East). Japanese Circle-confirm pads use
`-input-profile swap-ab`. Nintendo pads are also positional: South is
Nintendo B, East is Nintendo A — same `swap-ab` profile if the player wants
Nintendo A = Confirm.

| Logical action | Xbox-style | PlayStation-style | Generic SDL gamepad | Keyboard | Pointer |
| --- | --- | --- | --- | --- | --- |
| Direction | D-pad or left stick | D-pad or left stick | `DPAD_*` or left stick past the gate | Arrows; `W`/`A`/`D` (`S` is **Stop**, not Down) | Hover a room hit region |
| Confirm | **A** (bottom) | **Cross** (bottom) | `SOUTH` | Enter or Space | Primary click on a room hit region |
| Details | **not wired** (proposed: **Y**) | **not wired** (proposed: **Triangle**) | **not wired** (proposed: `NORTH`) | **not wired** (library browse uses last-row Down) | **not wired** |
| Back | **B** | **Circle** | `EAST` | Esc or Backspace | **no room route** (overlays: click empty) |
| System menu | **Xbox / Guide** | **PS button** | `GUIDE` | `o` | **no pointer route** |

Left-stick samples use the existing sofa gate / hysteresis
(`CommandFromStickHeld`); they emit the same Direction commands as the d-pad.

### What rooms actually receive

`docs/rooms.md` lists the command names delivered to `on_input`. Face and
shoulder buttons that are not Back / Home / Settings are passed through:

| Physical (Xbox / SDL) | Command name in the room |
| --- | --- |
| A / South | `select` |
| B / East | `back` |
| X / West | `sort` |
| Y / North | `search` |
| LB / RB | `filter_prev` / `filter_next` |
| Select / View / `BACK` | `layout_cycle` |
| Start / Menu | `quit` (launcher; not a room action) |
| Guide | settings overlay (not delivered) |
| hold B | rooms picker (`CmdHome`; not delivered) |

Example rooms today: Confirm (`select`) launches or enters a nested room.
`example.console-snes` uses `search` (Y) to hand off to the library, not to
open Details. Mushroom Kingdom does not handle `search` / Details.

---

## Long-press and chord shortcuts (not essential-only)

These are extra sofa routes. They must not be the only way to perform an
essential action.

| Shortcut | Binding | Result |
| --- | --- | --- |
| Rooms picker | hold East / B (≥450 ms), or `h` / Home | Toggle the room picker (`CmdHome`) |
| Library view list | hold South / A (library browse only; disabled in a room) | View picker |
| Favorite | hold North / Y (library browse only) | Toggle favorite |
| Filters | hold West / X (library browse only) | Filter overlay |
| Stop a live session | East / B, or `s`, while now-playing | Session Stop (not a room explore action) |

System menu (GUIDE / `o`) remains a tap. Settings includes a **Home** row, so
a controller-only player can reach Home without holding B.

---

## Gaps / follow-ups (not fixed in this pass)

Cheap sofa/room input already matches Direction, Confirm, Back, and System
menu on keyboard and a standard gamepad. The remaining gaps need a dedicated
change (task #3 and later), not a binding drive-by:

1. **Details has no tap binding in rooms.** Proposed physical: North / Y /
   Triangle / a keyboard letter (library last-row Down is not a room Details
   route). Needs a room command (new `details`, or a dedicated use of
   `search`) plus the shared info panel in task #3. Do not steal Y from
   rooms that already use `search` without an authoring note.
2. **Pointer-only Back and System menu.** Room empty-space clicks are a
   no-op (so clicking the map does not leave). Overlay “click empty” Back
   does not apply inside an open room. No on-screen settings control.
3. **Pads without a GUIDE button** have no gamepad System menu. Keyboard
   `o` is the tap fallback. Start remains Quit.
4. **A room script can consume `back` and swallow leave.** Experience rule:
   authors must not suppress Back. Launcher enforcement is task #10, not a
   remap.
5. **Attract-in-room** is policy, not a binding. Default off while a room is
   focused; do not re-open that decision here.

---

## Source

| Path | Role |
| --- | --- |
| `ui/tenfoot/input.go` | `CommandFromButton`, hold-gate, long-press map |
| `ui/tenfoot/keyboard.go` | `CommandFromKey` |
| `ui/tenfoot/sdl.go` | `SDL_GAMEPAD_BUTTON_GUIDE` → `CmdSettings` |
| `ui/tenfoot/room.go` | room delivery; Home vs unconsumed Back |
| `ui/tenfoot/hints.go` | affinity prompt words (A / B / GUIDE / Enter / Esc) |
| `ui/inputmap` | optional `swap-ab` and JSON remaps |
