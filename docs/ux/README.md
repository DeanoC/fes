# FES UX design

**Status:** product direction agreed with Deano on 17 October 2026. Detailed
visual choices and the implementation slices below are proposed until reviewed
on the native interface. This document records intended experience, not a claim
that every capability is implemented or hardware-accepted.

**Product owner:** Deano. **UI owner:** FogCast. Documentation preparation used
FES commit [`cf6987bb953f234da2ba54198280886e5553463f`](https://github.com/DeanoC/fes/commit/cf6987bb953f234da2ba54198280886e5553463f)
as its source baseline. The first implementation task must inspect its own
current base and the relevant open work before editing.

## Start here

| Read | Purpose |
| --- | --- |
| This brief | Product direction, navigation, core journeys and implementation scope |
| [Visual language](visual-language.md) | Visual hierarchy, typography, focus, artwork and display rules |
| [Concepts](concepts/README.md) | Portable interactive sketches, editable source and screenshots |
| [Acceptance](acceptance.md) | User tasks and the evidence required to assess the native result |

Delivery status, owners, dependencies and PRs belong in GitHub issues through
the existing [FES project](https://github.com/users/DeanoC/projects/4) and
[agent workflow](../agent-workflow.md#github-delivery-coordination). Keep this
section as the maintained design; do not turn it into a second live task board.

**Delivery issue:** [#770 — welcoming Home, clear game actions and machine setup](https://github.com/DeanoC/fes/issues/770).

## Direction

FES should be a welcoming place to play homebrew across a household of machines,
with a comprehensible workshop for real and fictional computers. Its immediate
UI target is the supported DE10-Nano-compatible FPGA path. The longer-term
direction includes other FPGA kits, Pis, PCs and Macs, followed by approachable
creation tools for retro and dream machines.

The everyday starting point is a calm game shelf. Authored rooms provide places
to explore. A visual workshop makes machine configurations and expansions
understandable. All three share dependable navigation, readiness, destination
selection and launch/return behavior.

The first useful outcome is a complete journey: choose a real supported game,
understand where it will appear, resolve a requirement if necessary, play, and
return to the same place. Visual polish is judged alongside that journey.

## Authority and current behavior

This brief supplies presentation direction and scope. Existing documents retain
ownership of the underlying rules:

| Subject | Existing source |
| --- | --- |
| Component ownership and current execution boundaries | [Project map](../project-map.md), [component boundaries](../component-boundaries.md), [FogCast architecture](../../sources/FogCast/docs/ARCHITECTURE.md) |
| Room navigation, shared actions, readiness and return | [Room experience](../../sources/FogCast/docs/rooms-experience.md), particularly its current availability section |
| Input actions and physical controller bindings | [Controller bindings](../../sources/FogCast/docs/rooms-controller-bindings.md) |
| Package, firmware, media and expansion readiness | [Launch composition](../../sources/FogCast/docs/launch-composition.md) |
| ZX81 configuration, running state and media behavior | [Hardware rooms](../hardware-rooms.md) |
| Destination defaults and mesh capabilities | [Mesh LAN](../mesh-lan.md), especially the M1 contract and placement policy |
| Persistence and capability-specific progress | [Core persistence](../core-persistence.md) |
| Build and hardware evidence | [Development](../development.md), [core status](../core-status.md), [kit sharing](../kit-sharing.md) |

When a design decision changes one of these rules, update that definition in
the same change and link the decision here. Do not independently implement
readiness or physical lifecycle in a room. The room experience guide's four
current availability names take precedence over older five-name summaries in
dated prose. Resolve other disagreements against code and observed behavior.

The [documentation index](../README.md) identifies `docs/superpowers/` as
historical plans. Its older native renderer brief is useful provenance, not a
replacement for the current component guides, code or acceptance evidence.

## Three complementary room concepts

### Cartridge Lounge: the everyday starting point

A selected game has a strong title, recognizable art, its creator, important
control requirements, current readiness and one clear primary action. A modest
shelf offers nearby choices. Recent and favourite content appears only when
real data supports it; empty sections disappear gracefully.

This is the proposed default visual direction. Home remains the household's
chosen starting room, with dependable routes to the Library and room selector.
The concept's names, content and compositions are illustrative.

### Homebrew Club: authored discovery

A room can feature a game, introduce its creator and offer an explanation of
how it was made. Editorial layouts and more spatial environments are possible
within the same navigation rules. A list of room destinations provides a direct
route to useful content without requiring avatar movement or hotspot hunting.

Curated room content is enough to begin. Accounts, comments, ratings and a live
community feed are separate future decisions. A room can have a distinctive
visual personality while Back, Home, Details and launch behavior stay familiar.

### Retro Workshop: understandable machine setups

A machine illustration explains compatible configuration choices and their
effect. The ZX81 example uses the workbench's single rear connector and a RAM
expansion to explore the interaction. It does not define a generic hot-plug
model or establish availability for other expansion families.

The workshop distinguishes a selected preview, a saved next-start setup and the
actual running setup. It makes restart and memory consequences visible before
the user acts. Expansion configuration should work entirely with directional
input and confirmation; dragging may be an additional interaction.

## Navigation and vocabulary

| Term | Meaning and user purpose |
| --- | --- |
| Home | The household's chosen starting room, familiar content and shortcuts |
| Library | The dependable way to find the same available content across rooms |
| Rooms | Authored environments and collections through which to explore content |
| Device | A physical participant, such as the living-room FPGA kit or a Mac |
| Machine | A real retro or fictional computer/console being run |
| Machine setup | The chosen machine configuration, including admitted expansions |
| Utilities | Diagnostic and maintenance tools, clearly identified as tools |
| Studio | A future workspace for editable projects and building playable outputs |

Home, Library and Rooms are navigation concepts, not a requirement for three
permanent TV tabs. Keep Settings, Utilities and the room selector reachable
through the established FES menu. Do not create competing starting points.

Within the Library, distinguish Games and Machines. Starting a computer in BASIC
is a valid intention even without a selected game. A game, a computer and a RAM
tester need different identities and action labels. Different entrances to a
machine setup must operate on the same configuration.

For an authored room, restore its last selection and position on return. The
first visit uses its authored starting location. Asynchronous artwork, metadata
and content updates must not move focus beneath the user. A selectable Home
route must remain available alongside any shortcut.

## The everyday play journey

### Arrive and choose

Paint a useful initial screen with text and fallback artwork as soon as the
existing path permits it. Add richer art without shifting selection. The
selected item answers: what is it, how is it controlled, where will the picture
appear, and can it start now?

A keyboard requirement, player count or other material input requirement is
visible before Play. Do not infer a controller mapping merely from the presence
of a connected pad.

### Understand readiness

Use the existing four names: **Checking**, **Ready**, **Needs a choice** and
**Unavailable**. Pair them with text and the appropriate next action. Colour
alone never carries the distinction.

- **Checking:** show a meaningful wait while the selected game and route resolve.
  Do not claim Ready or initiate duplicate launches. Resolve how a Confirm
  received during warm-up is acknowledged; never silently launch a different
  item after focus changes.
- **Ready:** Confirm launches directly; Details remains optional.
- **Needs a choice:** present viable editions or execution choices that matter
  to the user. Reuse existing household preference behavior.
- **Unavailable:** state the specific requirement or condition and offer the
  supported remedy. Missing content is a reason in this family.

Readiness describes the exact selected game, destination, composition and
current capability. An installed core, discovered node or saved configuration
alone does not establish it.

Prefer a headline, a short explanation and a useful action. For example,
"Needs the 16K RAM setup" can lead to machine setup. A required system file can
lead to the supported import picker, or to clear instructions naming the
computer where import is available. Inspect current capability before deciding
which remedy to present. Operational identifiers remain available in Details
or inspection tools when needed.

### Start and return

Show real launch stages in plain language and keep game identity visible. Use
percentages only when the underlying operation supplies meaningful progress.
The shared session path owns launch, cancellation, save completion and return.

Distinguish an in-session menu, suspension, stopping and exiting. Opening
controls for a supported running machine does not necessarily pause its CPU.
Changing the FPGA configuration may require leaving the current session. Where
exit is required, say so and describe the actual persistence behavior.

Do not label a recently played title Resume unless a compatible resumable state
exists. A required save write must succeed before exit is reported complete.
Restore the same room, selection and browsing position after returning, and
preserve them after a failed launch.

## Express the mesh through user intentions

Show a compact destination control in shared chrome, such as **Plays on:
Living-room TV**. The chooser first answers where the picture should appear;
Details can explain the execution device and route. Opening a menu on a Mac
does not itself change the household's chosen display.

| Intention | Meaning |
| --- | --- |
| Make a game available on another device | Transfer or ensure required content |
| Start on a chosen screen | Start a new session using a compatible execution and output route |
| Watch/control a running game on this screen | Use an explicitly supported picture/audio/input streaming route |
| Continue from saved progress on another device | Restore compatible saved progress/state using the admitted target |

Keep these actions distinct. Remote control of a TV does not imply that its
picture is streamed to the controlling screen. A capture preview does not
establish a supported play destination. Choose compatible execution through the
existing placement policy in the ordinary case; advanced overrides can remain
available when useful.

The first UI slice displays and operates supported existing destination
behavior. It does not add a placement algorithm, streaming, save migration or
live session transfer. Mesh capabilities and defaults must be read from the
current implementation and canonical mesh contract.

For a busy destination, explain that it is in use and offer an available
alternative. Loss of contact during play leaves the running state unknown; it
does not prove that the game stopped or make a replacement session safe.

First setup explains which named computer manages the library and how the kit
connects to it. A freshly unpaired kit, an empty connected library and an
established host going offline need different screens. Preserve a useful cached
browse state where supported. Offline launch eligibility is capability-specific:
verify the selected native path rather than copying behavior from a different
launcher or inferring that all offline entries can play.

## Expansion and machine configuration

Present an expansion's name, effect, fit and restart consequence before package
details. Show only choices admitted by the current machine/package contract;
unavailable or in-progress entries must be identified truthfully.

Keep three states visible when they differ:

1. **Selected:** the choice being previewed.
2. **Next start:** the setup successfully saved for a later start.
3. **Running:** the configuration the active machine actually uses.

Saving a setup does not rewrite the running configuration or save the program
in its memory. In the ZX81 example, explain that stopping clears running memory.
A saved expansion must not make the running machine appear upgraded. Apply
changes through the established lifecycle and refresh from its real result.

Named reusable setups may follow when users need to switch configurations.
Games can recommend compatible setups. The first workshop does not need a
general profile manager or arbitrary combinations of expansion cards.

## Homebrew and the future studio

Give the creator a visible identity beside the title. Details should support
controls, target machine, release information, credits and a short description.
Missing cover art should still produce a deliberate, recognizable entry.

Keep **Play**, **How it was made**, and **Make your own version** as separate
invitations. A playable release does not establish shared source or remix
permission. Offer project actions only when the content and permissions support
them, retain credit and give edits their own project copy.

The future studio begins with a short success loop: choose a working example,
change one thing, preview it, test it on a named device, and deliberately add a
playable result to the library. Substantial editing should first suit a PC/Mac;
the kit provides a named place to test and play. TV editing is a separate
interaction decision.

Distinguish the editable **project**, generated **build**, deliberately shared
or finished **release**, and target **machine setup**. A dream machine needs a
stable, versioned identity covering display, input, audio, memory/storage,
expansion rules and supported tools. It should be a learnable target. Studio
and dream-machine authoring are future work, not requirements for the first
native Home implementation.

## Implementation sequence

The sequence below describes design scope; live ownership and progress belong
in the linked GitHub work.

1. **Establish the native baseline.** Review current code and open work, map
   the approved direction onto supported interactions, and capture the existing
   1280x720 UI. Resolve the specific gaps that block the Home journey.
2. **Implement the Home journey.** Use the Lounge direction, real content,
   shared readiness and input, supported destination display, and one real
   launch/return path. Retain Library, Rooms, Settings and Utilities access.
3. **Extend shared Library and Details presentation.** Reuse the same state,
   focus, artwork and action rules; keep machine and utility identity clear.
4. **Apply the language to the ZX81 Workshop.** Preserve selected/saved/running
   distinctions and the current lifecycle and media contract.
5. **Develop richer authored discovery.** Add Club content and creator journeys
   using available releases and shared project material.

The immediate first implementation outcome is:

> A welcoming native Home room that launches a real supported game and returns
> to the same selection.

Relevant work to assess before editing includes
[#728, the tenfoot-only kit UI](https://github.com/DeanoC/fes/issues/728), and
[#764, installed-core readiness and Confirm during warm-up](https://github.com/DeanoC/fes/issues/764).
Coordinate against their actual current work; determine the specific dependency
instead of copying fixes or assuming an open issue establishes current code.

Use one FES task worktree and an explicit file scope. FogCast owns the UI;
physical transitions remain in libmister-runtime. Do not create another launcher,
parallel catalog, generic theme framework or new lifecycle coordinator for this
design. Extend existing room and native UI facilities.

Review each slice with target-resolution screenshots, a short interaction
demonstration and the evidence class it actually earned. Concept/browser checks,
host checks, hardware diagnostics and exact-artifact acceptance are distinct.
The design package alone establishes none of the latter. See [acceptance](acceptance.md).

## Native Home: first-slice decisions

**Status:** implemented for review in FogCast on the kit path
(`fogcast-tenfoot -gfx menu-display`, rooms home `example.fes-cores`). The
evidence so far is host-only: native software-renderer captures and focused
tests, recorded in
[the 17 October host record](../validation/2026-10-17-ux-lounge-home/README.md).
Hardware journeys H1–H9 remain pending.

- **Entry path.** The kit already opens tenfoot's rooms Home on
  `menu-display`, which defaults to `example.fes-cores`. The Lounge applies to
  that room; the retiring grid launcher (#728) is untouched. A
  `launcher.json` without `kit_ui = "tenfoot"` still boots the grid until #728
  lands.
- **Ownership split.** The room manifest selects
  `destination_panel = "hero"`. FES draws the selected item's title,
  required inputs, readiness name, consequence, destination and actions from
  the launcher's own classification (lease and unknown-status included). The
  room draws only the card, artwork motif, shelf and header routes, so a room
  cannot claim Ready.
- **Content.** The shelf is the kit's installed cores, names verbatim.
  Missing art is a deliberate motif chosen from the core id (colour) and
  required inputs (keyboard motif); titles stay outside the art. Creator is
  not shown because packages carry no creator. The shelf never says Ready.
- **Input requirements.** A required `fes.keyboard.hid` or `fes.mouse*`
  interface becomes "Needs a keyboard" / "Needs a mouse" before Play. Plain
  `fes.keyboard` 1.0 is shown only when the package also requires
  `fes.video.session-display` 1.0 and has no cartridge ROM, which is the
  ZX81 bring-up computer. Master System, SG-1000 and the earlier
  ColecoVision package use that matrix as a joystick and are not shown.
  Pads are not listed.
- **Destination.** A kit-local core plays on the kit's own HDMI output, so
  the hero says "Plays on this TV". No chooser is shown: this path has one
  destination.
- **Readiness copy.** Checking, Ready, Needs a choice and Unavailable appear
  as a labelled pill whose shape also differs (filled, outlined, outlined with
  a solid bar). Opaque blocks become consequences: `parts` is "Display setup
  incomplete" with "Start it from the FogCast library instead". Details
  (Y, or A on a blocked core) shows the core id, short package id and the
  kit's verbatim block in the hero; Back closes it. After an ambiguous local
  launch the core shows Checking, not Ready, until the agent reports idle.
- **Confirm during Checking.** Acknowledged ("Still checking. Press A again
  once it has finished.") and not queued, so a press can never start an item
  the player has not seen. This is the reviewed policy for #764's warm-up
  window.
- **Routes.** Library, Rooms (the lobby) and Settings sit one Up from the
  shelf in the hero and when the compact routes share a row, so Direction
  and Confirm reach the system menu on a pad without a Guide button. On a
  content box under 480×360 the same routes sit above the destination strip;
  when they stack, Down and Up walk that column and then the cores, and Up
  from the first core lands on Settings. Settings holds the system tools.
  Back at Home still opens the FES Home overlay.
- **Return.** The room instance is suspended during play, so Stop returns to
  the same selection; a later visit restores the last selected package from
  room `store`. Launch failure keeps the selection and shows the reason in the
  footer.
- **Type and layout at 1280×720.** Title 36 px bold, readiness and headline
  20–22 px, supporting lines 20 px, footer prompts 18 px in full words. The
  card takes 42% of the height. The shelf shows four cards at a full 1280
  content width and three with the default 5% TV safe area (as on the kit)
  or at 1024. A long title, explanation or Details list shrinks (one-line
  explanation, one-line title, no kicker) before it can reach the buttons.
- **Gameplay exit.** A kit-local core stops with one press of keyboard F12
  (MiSTer's menu key) or a pad's Guide button. Neither is forwarded to a
  core, and the menu cannot draw while the core owns HDMI, so neither asks
  for confirmation. The Select+Start one-second hold remains, and it is the
  only route on a pad without Guide; that is an accepted limit for sparse
  pads, since every other button belongs to the game. The hero names all
  three routes before Play. A press during loading stops the core once it is
  running, as the hold does. Hardware check of the new routes is pending.

## Library and Details: second-slice decisions

**Status:** implemented for review (#789). Evidence is host-only; L1/L2 on a
kit are pending.

- **Back returns to its origin.** The Library opened from Home, a room or the
  Home overlay keeps that room suspended, so Back (and the Home key) returns to
  the same room and selection rather than reloading it. A blocked Confirm in
  the Library is not replayed as a launch failure on that room. A Coleco BIOS
  import refreshes that suspended room's cached titles, so the resumed room
  shows the updated readiness.
- **Same rules as Home.** Library Details uses the same readiness pill and
  wording family as the Home hero, on the dark pane's seed roles. The block's
  short name is the headline and the launcher's sentence is the explanation,
  for example "Needs BIOS" with "Coleco BIOS required. Import household
  firmware before Play." Ready on a paired kit says "Plays on this TV": a
  paired client launches on its own target. A host-only title, or an unpaired
  launcher, shows the pill alone because the picture is elsewhere or
  unknown. An unknown paired-kit status keeps the title's own block empty and
  uses "Can't tell if this machine is free." as the headline; the Library
  header's readiness word, list rows and cover cells use the same classifier.
- **Actions follow Confirm.** Details offers A only when Confirm will play or
  import. A blocked Confirm opens Details for the title that was refused,
  including a Home recent that is not the focused Library row, instead of
  only changing header text. Favorite, pointer confirm, and presentation
  follow that title. Cancelling the BIOS picker returns to that
  Details.
- **Creator.** `Studio` appears as "by …" beside the title.
- **Cells.** A title that cannot play carries a labelled tag on a solid plate
  with a leading bar ("Needs BIOS", "Browse only", "Offline", "Needs a
  cartridge"). A playable title the classifier refuses for an unknown kit
  status or a foreign lease carries that same Details headline. A catalog
  reason such as Needs BIOS stays. Missing art names the platform instead of
  showing initials.
- **Not yet.** The grid, list and header chrome keep their dark sofa styling
  and debug-font prompts. The browse prompt still says "launch". Edition choice
  stays in rooms because Library rows are single editions.

## ZX81 Workshop: third-slice decisions

**Status:** implemented for review (#797). Evidence is host-only; Z1–Z3 on a
kit are pending.

- **The workbench is restyled, not rebuilt.** `example.hardware` keeps its
  host-owned setup, guarded saves, live tape, Stop and session-display
  behaviour. Only its layout and styling change, in the Lounge palette with
  accent-outline focus.
- **Three states together.** The setup column reads "Running: …", "Next start:
  …" and, while an unsaved card is inspected, "Previewing: … (not saved)".
  "Restart needed" says what to do. Without a session reading, Running is
  "unknown", and a stale reading is marked "may be out of date".
- **Preview is not fitted.** The illustration draws a ready saved card solid
  ("Next start") and a ready inspected card outlined ("Preview") beside it.
  A saved card the host marks unavailable keeps the Next start label and uses
  the shelf's unavailable colour. An unavailable preview is not drawn on the
  connector.
- **Consequences beside the change.** A warm box under Fit & save says that a
  setup change needs a restart, that stopping clears memory, and that saving
  never saves RAM.
- **Grouped actions.** The machine row holds Start/Stop, cassette/tape, Return
  to play, Next setup and Set up ZX81. The host cassette label is a wide row
  above those controls. A starter uses its display name; an import is
  "Imported cassette" plus a digest prefix, not the filename (#801). A label
  that does not fit keeps its start and its end. Setup tools hold Import expansion,
  Refresh, More cards and Library.
  Control IDs and guards are unchanged.
- **Not yet.** "Games for this machine" needs its own focus and launch design.

## Decisions still to validate

- Native typography, spacing, safe margins and comfortable TV reading distance
  on the real HDMI output (host captures exist; the TV check is pending).
- The F12 and Guide gameplay exits on the kit with a physical keyboard and a
  Guide-equipped pad.
- The first acceptance baseline is 1280x720 HDMI. CRT modes require an
  explicitly added scope and their own layout and hardware evidence.
- Which existing offline and alternate-destination capabilities can be exposed
  on the selected native path.
- The first machine, template and edit operation for a later Studio prototype.

## Research references

These precedents informed the design. They are not evidence of FES support.

| Reference | Observation and design use |
| --- | --- |
| [Degauss](https://misterdegauss.com/) and [ES-DE](https://es-de.org/) | Artwork-led browsing and controller-oriented libraries inform recognition and a dependable catalog beneath authored rooms. |
| [Steam Deck compatibility](https://www.steamdeck.com/en/verified) | Understandable compatibility categories and specific reasons inform the selected destination's readiness explanation. |
| [PICO-8](https://www.lexaloffle.com/pico-8.php) and [SPLORE/manual](https://www.lexaloffle.com/dl/docs/pico-8_manual.html) | A coherent machine, integrated tools and controller browsing inform the play-to-create direction. |
| [Pulp](https://play.date/pulp/) and [GB Studio getting started](https://www.gbstudio.dev/docs/getting-started/) | Approachable tools and working sample projects inform a short first creative success. |
| [GB Studio build/export](https://www.gbstudio.dev/docs/build/) and [Playdate device deployment](https://help.play.date/developer/deploying-your-pdx/) | Explicit build/export and device test steps inform separation of preview, hardware test and release. |
| [Spotify Connect](https://support.spotify.com/us/article/spotify-connect/) and [Steam Remote Play](https://store.steampowered.com/remoteplay?l=english) | Destination control and explicit streaming behavior inform distinct mesh intentions. |
| [itch.io install choices](https://itch.io/docs/itch/using/downloading.html) | Compatible choices first, with deliberate access to alternatives, informs progressive disclosure. |
| [Xbox navigation guidance](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/112) and [text guidance](https://learn.microsoft.com/en-us/xbox/accessibility/xbox-accessibility-guidelines/101) | Predictable focus, alternative routes and text legibility inform native validation. |

The initial visual review also used the repository's
[17 October menu capture](../validation/2026-10-17-same-process-host-recovery/final-menu.png).
It is a dated observation of that artifact, not a complete current live UI audit.
