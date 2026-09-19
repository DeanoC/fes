# FES rooms experience 01 — Exploring and playing through rooms

**Status:** proposed experience specification (accepted for planning). Describes intended behaviour; not a claim that every capability is implemented.

**Reference room:** Mushroom Kingdom (with nested Mario Sports / Sports Island).

**Related:** FogCast `docs/rooms.md` (authoring). Tip pin: FES #85 / FogCast `690b51a` (rooms selected for tenfoot).

**Out of scope here (separate specs later):** room discovery/download UI, room authoring tools, automatic routing across LAN machines. This flow must accommodate those later without rewriting the core model.

---

## Core principle

A **room** controls its presentation and local navigation.

**FES** provides dependable system actions, game launching, session feedback, and recovery.

Authors may style prompts and navigation markers. They must not suppress **Back** or the **system menu**.

---

## 1. Home and room entry

Home provides access to:

- pinned rooms
- recently played games
- all installed rooms
- the full library

Selecting Mushroom Kingdom opens its overworld.

- **First entry:** focus the authored starting location.
- **Later visits:** restore the last valid location.

The room becomes navigable while artwork and library matches load. **Loading results must not move the player’s selection.**

A room transition should communicate arrival without delaying input. Reduced-motion settings apply across all rooms.

---

## 2. Exploring Mushroom Kingdom

Directional input moves between connected locations. One location is clearly selected (marker or outline as well as colour).

A compact information panel identifies the selected destination:

| Kind | Panel content |
| --- | --- |
| Game | Title, system, availability, primary action |
| Room | Destination name and “Enter room.” |
| Unresolved | Honest loading or matching message |

The map remains legible when artwork is absent. Long titles stay readable in the information panel.

Paths express relationships and navigation. They do **not** imply unlock gates unless the author explicitly introduces an unlocking mechanic.

---

## 3. Common controller actions

Use **logical actions**; button prompts match the connected controller.

| Action | Behaviour |
| --- | --- |
| Direction | Move within the room |
| Confirm | Perform the displayed primary action |
| Details | Open information and available actions for the selected game |
| Back | Close the current panel or return to the previous room |
| System menu | Open a FES-owned menu with a reliable route Home |

No essential action may require a long press or button combination as its **only** route.

Physical bindings for Xbox-style, PlayStation-style, generic SDL, keyboard,
and pointer are in
[rooms-controller-bindings.md](rooms-controller-bindings.md). Tip tenfoot
already has tap routes for Direction, Confirm, Back, System menu, and
Details (task #3: North / Y / Triangle / `NORTH`, keyboard `i`). Hold-B
rooms picker is a shortcut; Home is also reachable through the system menu.

---

## 4. Selecting a game

For an available, matched game, **Confirm** launches directly. The focused panel must show **Play** before activation.

**Details** is optional. It opens a shared game-information panel (room context retained), with Play as primary action and any curator’s note clearly attributed.

### Availability states (distinct treatment)

| State | Meaning | Confirm behaviour |
| --- | --- | --- |
| Checking | Library match still resolving | Honest wait / progress; never silent no-op |
| Missing | No matching game in this household’s library | Explanation + resolution action |
| Needs a choice | Several editions match; no preference saved | Force a clear choice; remember for household |
| Unavailable | Matched, but a requirement blocks play | Reason + specific next action |
| Ready | Can launch through the current setup | Launch (Play) |

Missing locations remain on the authored map so structure survives. Confirm must **never** silently do nothing.

Remember edition choices for the household; do not re-ask every visit.

---

## 5. Launch and return

Confirm produces immediate feedback. FES owns the launch overlay, prevents duplicate requests, and describes actual progress without inventing percentages.

On failure: keep the selected location; show the reason with a specific next action (e.g. Retry, Back to room).

After gameplay ends and any required saving finishes: restore the same room, location, and navigation history. A failed save must remain visible and must not look like a successful completion.

---

## 6. Nested rooms

Sports Island enters the Mario Sports room and records its parent location.

- **Back** from Mario Sports → Sports Island location in Mushroom Kingdom.
- Return from a game launched inside Mario Sports → stay inside Mario Sports.

Home is always available through the system menu, regardless of nesting depth.

---

## 7. Meaning and accessibility

Focus, availability, and recorded play history are **separate** visual states.

- **Played** = FES recorded play activity.
- **Completed** = explicit completion record; returning from a launch is insufficient.

Text size, contrast, controller prompts, and reduced motion remain usable across room styles. Important information must not rely on colour alone.

---

## Attract (policy — decided 2026-09-19)

**Attract policy (decided):**

- Default: attract is **off** while a room is the focused surface.
- Per-room opt-in: a room author may enable attract for that room if desired. Opt-in is customizable per room and may provide a **custom attract** defined by the room author, not merely a boolean on/off of the global attract.
- When attract runs (only if the room opted in): it must not steal focus, move the player’s selection, or block Direction/Confirm/Details/Back/System menu.

Tip smoke (2026-09-19): attract was not observed while Mushroom Kingdom was loaded. That is consistent with default-off.

---

## Acceptance scenarios

1. A controller-only player enters the room, launches a game, and returns to the same location.
2. Back from a nested room restores its parent location.
3. Missing artwork does not prevent identification or navigation.
4. A missing game or ambiguous edition offers an understandable next action.
5. Launch failure preserves focus and allows recovery.
6. Every room permits a dependable exit to Home.
7. Played games are not presented as completed without supporting information.

### Smoke notes (2026-09-19, tip rooms / FES #85)

- Nested map nav (Mushroom Kingdom → Sports Island → return): **pass**
- Keyboard + mouse: **pass** (gamepad not connected)
- Settings / system key: **pass**
- Launch / return after play: **blocked** (target setup)
- Attract in Mushroom Kingdom: **not observed** (see policy above)

---

## Ownership (soft)

- **Grok Bot team** (Foggy / Luna / Bob): strong fit for tenfoot UI, panels, bindings, availability presentation, accessibility chrome, PR review against this doc.
- **Kepler / CLI:** useful for larger host/session/launch plumbing, data contracts, or multi-file backend when that is the bottleneck.
- Division is **not** a hard fence — pick the tool that fits the task.

---

## Ordered work plan

See companion section below (also summarised in chat). Update status here as items land.

### Ordered tasks

| # | Task | Goal / done when | Suggested owner |
| --- | --- | --- | --- |
| 0 | Land this doc in-repo (`docs/rooms-experience.md`) | Merged; linked from `docs/rooms.md` | Bot team (doc PR) |
| 1 | Decide attract-in-room policy | **STATUS done / decided** (2026-09-19): default off while a room is focused; per-room opt-in with optional custom attract | Deano (+ Foggy) |
| 2 | Logical action ↔ controller binding table | **STATUS done** (2026-09-19): table in `docs/rooms-controller-bindings.md`; no essential long-press-only or chord-only. Details tap binding remains a follow-up with task #3 | Luna / UI |
| 3 | Info panel: five availability states | **STATUS done** (2026-09-19): compact selected-destination panel plus Details tap; Checking/Missing/Needs a choice/Unavailable/Ready have distinct copy and Confirm never no-ops | Luna / UI (host match hooks as needed) |
| 4 | Played vs Completed data contract | Written contract + UI uses it (no false “completed”) | Kepler or host owner + Luna |
| 5 | Target setup for launch smoke | Can Confirm→Play on at least one Ready title from Mushroom Kingdom | Deano / kit+host |
| 6 | Launch overlay + duplicate prevention + honest failure | §5 behaviour; acceptance 1 & 5 | Host/session (Kepler or Caster) + Luna chrome |
| 7 | Return restores room, location, history; failed save visible | §5 return path | Host/session + Luna |
| 8 | Home surface: pinned / recent / installed rooms / library | §1 Home (can phase after core room loop) | Luna / UI |
| 9 | Edition preference remembered for household | Storage owner chosen; choice persists | Host profile / FES |
| 10 | Accessibility pass | Reduced motion, contrast, non-colour focus; Back + system menu always reachable | Luna |
| 11 | Controller-only acceptance pass | Scenario 1 green on real pad | Luna + Deano smoke |
| 12 | Authoring note: paths ≠ unlocks | Short note in `docs/rooms.md` | Doc / authoring |

**Recommended sequence for starting now:** 5 whenever target is ready, then 6–7, then 8–12. (0–3 are done.)

**Do not start yet:** discovery/download, authoring tools, LAN routing (explicitly out of scope).
