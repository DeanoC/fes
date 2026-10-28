# Tenfoot menus and room polish — 28 October 2026

FogCast owns this presentation pass. Base is
`b5ec777d624a1c9dfa589ffcb4ae70e94ae21a58` (#1182); branch is
`codex/tenfoot-ux-polish`; executable result is
`9ab7e975cb249c97c4b95c52739800053b96ea7d`. The pass changes native menus, shared dialogs and
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
captures were made before edits at the base above. The final 134-image gallery and 47 native captures are rendered from a task-owned
`git archive` of the committed
executable result above, with no live-source edits during capture. The
[capture inventory](captures.json) records the retained reference hashes. Dedicated Workshop captures wait
for the embedded illustration; the broad gallery can use its fallback motif.

Default/Night Library chrome uses warm paper. Arcade/custom resolved backgrounds
keep their look. Primary Library title contrast is 12.44:1 (`#243631` on
`#FFFCF5`); action text is 7.76:1 (`#FCFCF3` on `#235A46`). Existing C64
checks retain at least 7:1 for its declared text/surface pairs in light and
overlay palettes.
These results do not audit arbitrary custom palettes or every piece of artwork.

The [capture summary](native-capture-summary.json), [source verification](capture-source-verification.json)
and [full PNG hashes](full-capture-inventory.json.gz) bind the capture set to
230 tracked UI files at the executable commit. The broad gallery's manual
fixture is retained as [capture-gallery.go.txt](capture-gallery.go.txt). Place
it at `ui/tenfoot/ux_pass_gallery_task_test.go` in a copied source tree, set
`FES_UX_PASS_GALLERY` to an absolute output directory, then run
`go test ./ui/tenfoot -run '^TestUXPassGalleryTask$' -count=1` from FogCast.
Remove the temporary test afterwards. Standard native tests use
`FES_ROOM_SCREENSHOT_DIR` and need no temporary source. Fixtures supply catalog
and hardware observations; they do not contact or program a kit.

## Software checks

[Final checks](checks.json) bind commands and artifact identity to the executable
source above. Planner tests (21), runner tests (31), 439 parent regressions and
generated consumers pass. `make check` and `make deadcode` pass. The complete
host lane passes on rerun: all seven Go/race/appliance/expansion/UI stages,
361 Node tests and 78 browser integration tests. The installable userspace
bundle builds and passes its producer inspection.

The original `make test-changed` exits remain failures in the retained logs;
their passed parent results are reused and the exact failed host lane was
rerun with `python3 scripts/host_tests.py --ui-if-needed`. The initial run
caught an empty-system test pressing Confirm during the partial upper frame;
it now waits for the actual setup control. An unchanged runtime fallback test
also failed once, then passed in isolation and in the final affected run.
The final aggregate race run had Home callback wall overruns of 55.86/48.60 ms
with 13.55/4.33 ms thread CPU and three failed Home assertions. The exact cases
pass in isolation on this result (2.404 s) and the pristine base (2.589 s);
the full host lane then passes. This does not establish a causal scheduling
or performance diagnosis. No callback deadline or production admission rule
was changed. See [initial log](test-changed.log.gz), [aggregate retry](test-changed-final.log.gz),
[Home case retry](home-watchdog-retry.log.gz), [base control](home-watchdog-base-control.log.gz)
and [completed host lane](host-lane-retry.log.gz).

 Focused regressions exercise directional and pointer action geometry,
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
accept these bytes. The final userspace bundle is
`75fa28c4265b2a31bc1d416f4d4ebffe320d9fdead59b49da89064ee37368c3c`,
built from that executable result; [producer receipt](bundle-receipt.json).
Retained base image SHA-256 is
`f904961e008a972db8f06f1bb47aff9631123d67215b9f4157ea1fbee83b98d4`.
The installable tar is retained on Powerboat at this task worktree's
`out/ux-pass/bundle-final/bundle.tar`; the [build log](bundle-final-build.log.gz)
records compilation and inspection. No OS image or playable-core rebuild is
required. The earlier `23bb3046aa` bundle `61ec515031df42790569d92655e2585dd2664ec9210e575280453dd7b285c31a`
was superseded before deployment by the bounded Details fix.

Remaining check: claim a designated kit, install/activate this bundle on its
running image through the software API, and inspect Home, Library, all room
layouts and selectors on the actual 1280×720 menu-display output at viewing
distance. Use keyboard and a sparse pad for Play/Details/Back, family/variant,
saved/live media and launch/stop; record input/frame traces and any watchdog
failures. Restore the status file's exact baseline bundle, verify active bundle
and unchanged booted image, idle runtime and free lease, then release the kit.
Review is required before merging.
