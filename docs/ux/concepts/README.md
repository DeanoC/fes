# FES Room concepts

These are interactive design studies for the FES user experience. They show
three complementary surfaces with one library, selected game and output
destination. They are documentation assets, not production FogCast UI or a
hardware acceptance record.

Start with the [UX design index](../README.md) and the
[visual language](../visual-language.md) for the reasoning and scope.

## Open the concepts

Open [fes-rooms.html](fes-rooms.html) directly in a browser. Double-click the
file or use the browser's **Open file** command. No server, package install,
ChatGPT session or internet connection is needed. The complete interactive
page is contained in this one file, so it can also be copied elsewhere.

Use the three Room buttons above the product surface to switch between:

| Room | Question it explores |
| --- | --- |
| Cartridge Lounge | Can artwork, a small shelf and clear readiness make choosing a game welcoming? |
| Homebrew Club | Can an authored Room connect discovery, creator stories and learning? |
| Retro Workshop | Can a machine's expansion setup be understandable without confusing saved configuration with the running machine? |

The same choices are available through **FES Home → Rooms**. These are authored
FES Rooms. The physical destination, such as the living-room TV, is a separate
choice under **Change screen**.

Click or tap the controls, or use Tab and Enter/Space. Arrow keys select an
adjacent game when a game-shelf button has focus. Escape goes back. The footer's
D-pad/A/B legend sketches the intended console controls; this document does
not implement controller discovery or a complete gamepad input layer.

All state is local to the page and resets on reload. No game, hardware
connection, expansion operation, memory test, source project or publication is
started. The page makes no network requests.

## Useful paths to inspect

### Choose a game

The initial selection is **Tiny Tide**, which uses the fictional fixture's
built-in memory setup. Selecting **Night Orchard** or **Orbit Post** shows the
16K RAM pack requirement before offering an action. **See requirements** opens
game details; **Open Retro Workshop** then reaches the usable expansion
selector, rather than an explanatory page with a misleading return route.

The Homebrew Club pick for Night Orchard uses that same destination readiness,
not a fixed hardware caption. On the living-room TV the line reads **Needs 16K
RAM pack** and the action is **See requirements**. On the ready local player it
reads **Ready · 16K RAM pack**, matching the shelf, and the action becomes
**Play**. The fixture still requires a 16K RAM pack on the FPGA destination.

The game shelf, game details, local launch view, creator notes, project-copy
preview, Settings and Utilities each have working local navigation. A utility
is labeled as a utility and kept separate from the game shelf.

### Fit a saved expansion setup

In **Retro Workshop**, select **16K RAM pack**, then choose **Fit & save setup**.
The displayed states deliberately differ:

| State | Before saving | After saving the 16K setup |
| --- | --- | --- |
| Running | No expansion | No expansion |
| Next start | No changes saved | 16K RAM pack |
| Consequence | A setup change needs a restart | Restart needed; stopping clears running memory |

There is one rear connector and one selected expansion. The concept does not
show arbitrary combinations, hot plugging, a silent restart or a RAM snapshot.
Saving the setup does not make a 16K game ready in the still-running base
machine. See the current [ZX81 guide](../../fes-zx81.md),
[expansion bus](../../zx81-expansion-bus.md) and
[hardware Rooms](../../hardware-rooms.md) for implementation context.

### Choose where the picture appears

**Change screen** initially retains **Living-room TV · FPGA** even when the
page is open on a computer. It distinguishes the TV's output from controlling
that output on the current device. Remote control does not imply video
streaming back to the controller.

The selectable local-player fixture is explicitly ready; the Study Pi is
explicitly offline and has no enabled launch choice. These statuses are sample
data, not capability detection or a claim about the user's machines.

## Fictional content and proposed behavior

The four games, creators, cover art, descriptions, input mappings, compatibility
claims and project-sharing states are authored examples:

| Game | Fictional creator | Fixture requirement |
| --- | --- | --- |
| Tiny Tide | Soft Harbour | Built-in memory |
| Night Orchard | Bramble Works | 16K RAM pack |
| Orbit Post | Mira Byte | 16K RAM pack |
| Signal 81 | North Window | Built-in memory |

The living-room FPGA, ready local player and offline Study Pi are also
fixtures. There are no ROMs, commercial artwork, game assets or actual starter
projects in this directory. The colorful images are CSS cover illustrations,
not screenshots of ZX81 gameplay. The depicted ZX81 is a stylized illustration.

The studio, creator discovery, project copies, output picker and visual
presentation are proposed UX. A source-sharing label does not establish a
license or permission to remix a real game. Before implementing these ideas,
bind readiness, source availability and permissions to actual product data.

## Edit and rebuild

The editable inputs are:

| File | Purpose |
| --- | --- |
| [fes-rooms.fragment.html](fes-rooms.fragment.html) | Product layouts, styles, fictional fixtures and local interaction behavior |
| [standalone-shell.html](standalone-shell.html) | Portable document shell, concept labeling and Room navigation |
| [build.py](build.py) | Standard-library export helper; assembles the page and replaces host icon placeholders with local text symbols |
| [fes-rooms.html](fes-rooms.html) | Generated, complete offline page to open or share |

After editing an input, run this from the FES repository root:

```sh
python3 docs/ux/concepts/build.py
```

The helper has no package dependencies and writes only `fes-rooms.html` in
this directory. Commit the generated page with its edited inputs. Opening the
fragment or shell alone is not the complete concept; use `fes-rooms.html`.

Colors use product-scoped `--fes-*` tokens and the browser's light/dark
appearance. Artwork uses CSS and local text symbols. The standalone page has
no iframe, external font, icon library, host bridge or ChatGPT runtime. Its
content security policy blocks network connections.

## Screenshots and validation

The following screenshots were captured from a fresh offline page on
2026-10-17, in light appearance at one device pixel per CSS pixel:

| Screenshot | Dimensions | State |
| --- | --- | --- |
| [cartridge-lounge.png](cartridge-lounge.png) | 1024 × 821 | Tiny Tide selected and ready |
| [homebrew-club.png](homebrew-club.png) | 1024 × 821 | Night Orchard club pick with its visible memory requirement |
| [retro-workshop.png](retro-workshop.png) | 1024 × 821 | No expansion running; no pending setup |

These are browser concept dimensions, **not** native 1280 × 720 acceptance
captures. They establish no FPGA rendering, controller, performance or hardware
behavior. The product surface shares a stable height across the three Rooms;
the surrounding review page can scroll normally.

Browser checks completed for this export:

- Opened the single HTML file with the browser network disabled; no remote
  requests or JavaScript errors occurred.
- Checked all three Rooms at 1024 px and 320 px widths. Text and controls fit
  without horizontal page overflow. At 320 px, headings and shortcut lists
  reflow rather than splitting words into narrow columns.
- Checked game selection, readiness, details, keyboard selection, the library,
  Utilities, the Room selector, and the Lounge-to-Workshop route.
- Checked that opening the output chooser retains the TV, selecting the ready
  local player changes the output/readiness state, and the offline destination
  remains noninteractive.
- Checked that the Homebrew Club Night Orchard line follows that same
  destination: **Needs 16K RAM pack** and **See requirements** on the TV,
  **Ready · 16K RAM pack** and **Play** on the local player, including while
  another game stays selected.
- Checked that fitting the 16K setup changes **Next start**, preserves
  **Running**, and retains the restart/memory-loss message.
- Checked narrow game details, output choice and saved-setup states, plus dark
  appearance layouts.

After a visual edit, rebuild, open the page, repeat the relevant paths and
refresh any affected screenshots. This package changes no FES modules, shared
contracts, package selections, image recipes or hardware.
