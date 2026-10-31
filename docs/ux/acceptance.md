# UX acceptance journeys

Expected behavior for native Home, Library and ZX81 Workshop. Assignments and
results belong in [#770](https://github.com/DeanoC/fes/issues/770) and its linked
implementation issues/PRs. The first three slices have native checks and kit
evidence. The remaining cases below must be qualified on their actual combined
result. Browser concept QA is illustration evidence only.

Use the existing [rooms experience](../../sources/FogCast/docs/rooms-experience.md),
[controller bindings](../../sources/FogCast/docs/rooms-controller-bindings.md),
[FogCast architecture](../../sources/FogCast/docs/ARCHITECTURE.md),
[hardware rooms](../hardware-rooms.md) and [core persistence](../core-persistence.md)
for their contracts. Resolve code/documentation discrepancies before accepting
changed behaviour; this guide defines journeys, not another hardware lifecycle.

## Evidence available and remaining scope

Reviewed source baseline: `9b463dcee641e3622ace0fffe6f516d8e7848632`, 18 October
2026. The [reference images](references/README.md) are fresh fixture renders
at this baseline. The kit runs below identify different, explicit artifacts.

| Area | Existing evidence | Still to establish |
| --- | --- | --- |
| Home entry and play/return | [#776 kit run](https://github.com/DeanoC/fes/pull/776#issuecomment-6234640302) | Default entry with older/missing configuration (#728); controlled Checking and Needs a choice; the Ready Details direction/Confirm route. |
| Gameplay exit | [#787 kit run](https://github.com/DeanoC/fes/pull/787#issuecomment-6236689248) | Regression check on the combined result. The Guide-less hold is an accepted exception, not an unimplemented one-press route. |
| Persistent UI state | [#786 persistence run](https://github.com/DeanoC/fes/pull/786#issuecomment-6239802675) | Preserve its behavior through the new navigation/content work; do not open a duplicate persistence implementation. |
| Library and Details | [#790 kit run](https://github.com/DeanoC/fes/pull/790#issuecomment-6237617795), [#796 in the slice handoff](https://github.com/DeanoC/fes/issues/770#issuecomment-6243878257) | Shared grid/list/header styling, selected-text contrast, full-word prompts and kit Library stop/lease behavior (#805). |
| Workshop | [#799 kit run](https://github.com/DeanoC/fes/pull/799#issuecomment-6243785291) | Paired-kit save/read-back, Restart needed, BASIC continuity and two-client conflict. Live opening/focus/return alone do not pass these. |
| Responsiveness | [#830 comparative kit run](https://github.com/DeanoC/fes/pull/830#issuecomment-6246976562) and [#811](https://github.com/DeanoC/fes/issues/811) | Cold and warm traces for the assembled UX, with watchdog cases #761/#784/#803 assessed on their actual room paths. |

Earlier journey-level passes remain evidence for the tested scenario. They do
not hide a newly identified counterexample: in particular H5's general sparse-pad
pass does not prove the missing Ready Details route.

## First slice: native Home

Accept one real supported title through room-driven tenfoot native Home. Retain
Library, Systems, Menu, Settings and Utilities access. Streaming, progress migration and
Studio implementation are outside this slice.

| Journey | Expected result | Evidence to collect |
| --- | --- | --- |
| H1. Arrive at Home | The native launcher opens Home without a terminal, technician dashboard or legacy-grid detour. An absent or invalid room retains a usable basic menu and explanation. | Boot and room-entry capture; launcher/room configuration, including an older configuration for default-boot assessment. |
| H2. Play and return | Select a Ready title, confirm once, play, then use the supported Stop/exit route. Kit-local F12/Guide stop with one press; Select+Start hold is the accepted exit on a Guide-less pad. Actual launch feedback appears immediately. After confirmed Stop and required saving, the same room, focused title and navigation context return. Repeated Confirm creates no duplicate launch. | Controller/keyboard recording plus correlated launch/Stop events and observed output. Name the actual route and mapping. Check the kit Library lease outcome separately from intentional sofa Soft-stop retention (#805). |
| H3. Know where play appears | A human-readable destination identifies the intended TV or screen before launch. Actual output matches it. Opening a computer menu does not silently change the household destination. | Destination label, resolved binding, output capture and input source. |
| H4. Wait without losing intent | Checking remains distinct from Ready. Confirm during a warm catalogue read receives visible feedback and follows an explicitly reviewed queue-or-retry policy. A pending action cannot launch a different item after focus or results change. | Delayed-read trace with Confirm before and after readiness; demonstrate cancellation/focus change and one launch at most. |
| H5. Use a sparse controller | Direction, Confirm and Back reach the room's essential UI actions, Details and system menu through visible controls. Ready-item Confirm still plays directly; a dedicated Details/Guide/North button is optional for navigation. Keyboard-only operation works and prompts match the mapping. H2's explicit gameplay-exit exception is separate. | Real sparse-pad and keyboard runs, including Ready item → visible Details → Back to the same item without launching. Name the focus path and device/mapping. Record unsupported UI routes as gaps, not passes using another controller. |
| H6. Read and navigate | At the actual 1280×720 output and viewing distance, labels, long titles and focus remain readable. Missing or late artwork leaves labelled destinations and stable focus. Colour alone conveys neither availability nor selection. | Native captures with missing art and long text; large-text/reduced-motion review; measured input-to-focus and redraw behaviour. Browser timing does not qualify native performance. |
| H7. Lose the library connection | Host-dependent rows retain cached browsing and explain the unavailable host. Any local launch remains enabled only when this launcher path verifies the required installed content, composition and ownership. Reconnection preserves focus. | Disconnect/reconnect the configured host; test one host-dependent row and, only if supported on this path, one eligible local row. |
| H8. Encounter occupied or uncertain hardware | Another owner's session says In use; a lifecycle transition says Starting or Stopping. Connection loss means unknown status, not stopped or available. No takeover, replacement launch or automatic retry changes another session. | Controlled busy, foreign-owner and lost-response observations; verify no unintended launch/Stop request. |
| H9. Recover from failure | Launch failure preserves selection and gives a next action. A failed or uncertain save stays visible and does not claim successful Stop, saved progress or completion. Recovery follows the runtime's reported outcome. | Focused failure-injection checks, followed by the relevant native observation; verify a failed save does not permit replacement play. |

The [FogCast README](../../sources/FogCast/README.md) describes different local
capabilities for tenfoot installed cores and the older grid's cached SMS rows.
Do not inherit another launcher's offline claim. Cached listings or installed
packages alone do not establish Ready.

Assess the relevant parts of [#728: native-only kit UI](https://github.com/DeanoC/fes/issues/728)
and [#764: readiness and Confirm](https://github.com/DeanoC/fes/issues/764) against
the first slice. Link any blocking sub-result and its evidence; neither whole
issue must automatically close before unrelated Home work proceeds.

## Library and ZX81 journeys

These journeys extend the same identity, focus, destination and status
contracts. Record the exercised sub-cases rather than marking an entire row
passed from a screenshot or a different client route.

| Journey | Expected result | Evidence to collect |
| --- | --- | --- |
| L1. Find and revisit a title | Home, Library and a room refer to the same title and favourite state. Search/filter changes preserve a valid selection. Back from Details or a requirement action returns to its context. | Cross-surface identity and navigation trace using real library entries. |
| L2. Supply a requirement | Missing firmware, media or support names the requirement and offers the remedy supported on this surface. Browsing imports nothing. Cancel preserves context. Needs a choice contains only viable alternatives and retains a valid saved preference. | Missing-requirement and cancellation cases; successful supported remedy; incompatible options remain blocked. |
| L3. Read the shared Library | Grid, list, Details and prompts use the FES hierarchy. Selected titles remain readable; focus and readiness are distinct. Missing art, long titles, blocked rows and empty filters remain useful at 1280×720 with the supported safe area. | Before/after native captures; measured text/background contrast including selected states; directional action/Back trace; cold/warm frame timings. |
| L4. Find games and machines honestly | Home and Workshop use real existing title IDs when showing games. Machine startup, setup and Utilities retain explicit identities. Home/Library/Workshop share title metadata, readiness and destination. | One real playable title launched from Home and revisited in Library; a machine startup and a utility entry remain distinct. Verify missing metadata, no matching games, host reconnection and no unsupported local-play claim. |
| Z1. Inspect, save, run | Highlighting a shelf card changes only the inspected selection. Fit and save updates Next start only after confirmation. Running reflects the target's actual setup. A mismatch says Restart needed; saving configuration never claims RAM or program preservation. | Inspect without mutation, save/read-back, and running receipt; verify no hardware programming while editing. |
| Z2. Handle stale setup | Another client's change causes refresh rather than overwriting. A failed or ambiguous save is not replayed automatically. Cancel leaves the confirmed setup intact. | Two-client selection conflict and failed-save cases. |
| Z3. Return to a running ZX81 | Live controls appear only for the running package's supported display/media capabilities. Returning preserves the session and program. Unconfirmed close retains UI input focus and offers recovery. Stop explains that running memory is cleared. | Exact-package kit run with observable BASIC program continuity and session identity; check held-input release and return. |

For Z1/Z2, exercise the real paired HTTP listener as well as the host-owned
selection service. A permissive fake `HardwareServices` implementation can
render a successful save while the production listener still returns 404.
The paired-save implementation needs these specific cases:

- A supported paired host admits only the intended single-entry expansion
  selection after the existing bearer/target checks. Unrelated writes remain
  denied; a different foreground target does not substitute for the paired kit.
- An older or unsupported paired host exposes an inspectable read-only setup
  and useful host instructions. Content Ready does not imply save permission.
- Fit and removal retain the expected package and previous-selection guards.
  Two clients cannot overwrite each other's confirmed selection silently.
- Saving sends one deliberate request, shows Saved only after validated
  acknowledgment, then reads back. A failed or ambiguous write is refreshed
  without replay; a failed read leaves a clear stale state.
- Runtime session identity, Running composition and RAM remain unchanged by
  the save. Missing running-composition evidence is unknown setup, not proof
  of an empty connector or a stopped machine (#768).

Use the admitted one-connector topology. Do not invent card stacks, cassette
transport or equate tape selection with saving a BASIC program. Program-save
claims require a supported operation and evidence.

## Responsiveness and combined-build checks

Use the existing [frame trace](../../sources/FogCast/docs/frame-trace.md) and
[#811](https://github.com/DeanoC/fes/issues/811). Record cold first entry and
warm repeated navigation separately for Home shelf/routes, Library grid/list,
Details and Workshop. Include the trace's per-stage and input-to-picture
p50/p95, sample counts, exact artifacts and any omitted cases. Distinguish
script time, raster work and transport/scanout before proposing another
optimization.

Preserve callback budgets and the supported settled-screen behavior. Record
every observed watchdog failure and link the concrete room issue; do not
hide it by increasing a timeout or call a browser/fixture timing a kit result.
#830's comparison is useful prior evidence for its tested interaction set;
it does not establish a frame-rate target or accept this later visual pass.

## Evidence record

Record journey IDs, date, source commit, actual software bundle/base-image and
package/content identities, launcher path, display, controller/mapping, setup,
outcome and captures/logs. Identify fixtures and omissions; exclude credentials.

Separate illustration review, host checks, hardware diagnostics and exact-artifact
hardware acceptance. Older captures, mockups and compilation do not accept the
new UI on a kit. Follow
[development](../development.md), [kit sharing](../kit-sharing.md) and
[artifact identities](../artifacts.md) for procedures; this guide authorizes no
deployment. Put results in the issue/PR and dated validation evidence.
