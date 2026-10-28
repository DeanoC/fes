# Tenfoot menus and room polish — 28 October 2026

FogCast owns this presentation pass. Base is
`b5ec777d624a1c9dfa589ffcb4ae70e94ae21a58` (#1182); branch is
`codex/tenfoot-ux-polish`. The pass changes native menus, shared dialogs and
all 12 embedded room packs. No host/target API, runtime, FPGA, package manifest,
family registry or media-binding schema changes.

Home, Library and Details share paper surfaces, ink text and a green focus
outline. Titles sit on opaque plates; readiness has its own label. Settings,
room/system selectors and media pickers have larger rows and proportional
labels. Workbenches distinguish primary, secondary and unavailable controls,
show Running separately from saved next-start setup, and offer Set up directly
on an empty system. Discovery rooms keep their own palettes and motifs while
using larger titles, controlled labels and footer-safe layouts.

Directional action focus is local launcher state. Direct Confirm on a Ready
item still launches. Home Up enters hero actions, another Up reaches header
routes, and Down/Back returns to the shelf. Library Down from its bottom visible
row enters the footer actions; Left/Right selects, Confirm activates and
Up/Back returns to the same title. Captured title/package/media/ROM/expansion,
readiness intent, client, room and session guards reject replaced selections.

## Native review

The [representative references](../../ux/references/README.md#current-native-pass)
show production Go/Lua rendering with synthetic catalog, installed-core and
hardware responses. The task gallery exercises all 12 embedded packs at both
1280×720 and 1024×600: populated, directional focus and empty states; Home and
Library action focus; Sinclair variants; +3/Pentagon/Apple II/Atari ST/Coleco;
running and menu-plane fixtures; native grid/list/shelf, Details, long titles,
Settings, room/system selectors, firmware/media pickers, storage, filters,
collections and keyboard. Dedicated render checks also exercise 5% safe area.
No fixture title or state claims playable hardware support.

The initial task-wide gallery was captured while edits landed and is a mixed
snapshot; it is not the pristine base. The earlier Home/Library/Workshop native
captures were made before edits at the base above. The final gallery and retained
reference hashes identify the reviewed result. Dedicated Workshop captures wait
for the embedded illustration; the broad gallery can use its fallback motif.

Default/Night Library chrome uses warm paper. Arcade/custom resolved backgrounds
keep their look. Primary Library title contrast is 12.44:1 (`#243631` on
`#FFFCF5`); action text is 7.76:1 (`#FCFCF3` on `#235A46`). Existing strict C64
checks retain at least 7:1 for its rendered text in light and overlay palettes.
These results do not audit arbitrary custom palettes or every piece of artwork.

## Software checks

Final command results and artifact identity are recorded with this source after
validation. Focused regressions exercise directional and pointer action geometry,
Back/selection retention, direct launch, stale item/readiness/target/session
rejection, empty-system setup, family/variant persistence, authored safe-area
geometry, artwork title plates, native dialog text and cached/uncached pixel parity.

[CPU comparison](cpu-backend.json) records the existing warmed cached software
benchmark, three samples per scene on this shared x86 host, against an isolated
archive of the base. Median static/navigation/settings-navigation frame costs
were 95/769/747 µs before and 133/710/799 µs after; sample spread is wide. The
new action state reuses existing classification and builds identities only while
focused. Static allocations rise from 84 to 131 per frame; no causal speedup,
kit latency or numeric performance budget is claimed. Settled-frame reuse stays
covered by the existing cache parity tests.

## Hardware follow-up

Evidence here is host software rendering and software checks. This pass has not
been deployed or accepted on a kit; #1182's earlier hardware evidence does not
accept these bytes. The userspace bundle uses the retained compatible base;
no OS image or playable-core rebuild is required.

Remaining check: claim a designated kit, install/activate this bundle on its
running image through the software API, and inspect Home, Library, all room
layouts and selectors on the actual 1280×720 menu-display output at viewing
distance. Use keyboard and a sparse pad for Play/Details/Back, family/variant,
saved/live media and launch/stop; record input/frame traces and any watchdog
failures. Restore the status file's exact baseline bundle, verify active bundle
and unchanged booted image, idle runtime and free lease, then release the kit.
Review is required before merging.
