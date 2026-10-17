# UX acceptance journeys

Expected behaviour for native Home, then Library and ZX81 workbench. Assignments
and results belong in the implementation issue/PR. All native and hardware checks
are **pending**. Browser concept QA is illustration evidence only.

Use the existing [rooms experience](../../sources/FogCast/docs/rooms-experience.md),
[controller bindings](../../sources/FogCast/docs/rooms-controller-bindings.md),
[FogCast architecture](../../sources/FogCast/docs/ARCHITECTURE.md),
[hardware rooms](../hardware-rooms.md) and [core persistence](../core-persistence.md)
for their contracts. Resolve code/documentation discrepancies before accepting
changed behaviour; this guide defines journeys, not another hardware lifecycle.

## First slice: native Home

Accept one real supported title through room-driven tenfoot native Home. Retain
Library, Rooms, Settings and Utilities access. Streaming, progress migration and
Studio implementation are outside this slice.

| Journey | Expected result | Evidence to collect |
| --- | --- | --- |
| H1. Arrive at Home | The native launcher opens Home without a terminal, technician dashboard or legacy-grid detour. An absent or invalid room retains a usable basic menu and explanation. | Boot and room-entry capture; launcher/room configuration, including an older configuration for default-boot assessment. |
| H2. Play and return | Select a Ready title, confirm once, play, then use an accessible Stop/exit route. A chord or long hold cannot be its only route on the intended controller. Actual launch feedback appears immediately. After confirmed Stop and required saving, the same room, focused title and navigation context return. Repeated Confirm creates no duplicate launch. | Physical controller recording plus correlated launch/Stop events and the observed display result. Record the complete gameplay exit route; a sole chord binding is an unresolved first-slice gap, not a pass. |
| H3. Know where play appears | A human-readable destination identifies the intended TV or screen before launch. Actual output matches it. Opening a computer menu does not silently change the household destination. | Destination label, resolved binding, output capture and input source. |
| H4. Wait without losing intent | Checking remains distinct from Ready. Confirm during a warm catalogue read receives visible feedback and follows an explicitly reviewed queue-or-retry policy. A pending action cannot launch a different item after focus or results change. | Delayed-read trace with Confirm before and after readiness; demonstrate cancellation/focus change and one launch at most. |
| H5. Use a sparse controller | Direction, Confirm and Back reach the room's essential actions, Details and system menu through visible controls. No GUIDE button, North button, chord or long press is the only route. Keyboard-only operation also works. Prompts match the selected mapping. | Real sparse-pad and keyboard runs; list device and mapping. Record any unsupported essential route as a gap, not a pass using another controller. |
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

## Later slices: Library and ZX81

These native/hardware journeys are also **pending**, extending the same identity,
focus, destination and status contracts.

| Journey | Expected result | Evidence to collect |
| --- | --- | --- |
| L1. Find and revisit a title | Home, Library and a room refer to the same title and favourite state. Search/filter changes preserve a valid selection. Back from Details or a requirement action returns to its context. | Cross-surface identity and navigation trace using real library entries. |
| L2. Supply a requirement | Missing firmware, media or support names the requirement and offers the remedy supported on this surface. Browsing imports nothing. Cancel preserves context. Needs a choice contains only viable alternatives and retains a valid saved preference. | Missing-requirement and cancellation cases; successful supported remedy; incompatible options remain blocked. |
| Z1. Inspect, save, run | Highlighting a shelf card changes only the inspected selection. Fit and save updates Next start only after confirmation. Running reflects the target's actual setup. A mismatch says Restart needed; saving configuration never claims RAM or program preservation. | Inspect without mutation, save/read-back, and running receipt; verify no hardware programming while editing. |
| Z2. Handle stale setup | Another client's change causes refresh rather than overwriting. A failed or ambiguous save is not replayed automatically. Cancel leaves the confirmed setup intact. | Two-client selection conflict and failed-save cases. |
| Z3. Return to a running ZX81 | Live controls appear only for the running package's supported display/media capabilities. Returning preserves the session and program. Unconfirmed close retains UI input focus and offers recovery. Stop explains that running memory is cleared. | Exact-package kit run with observable BASIC program continuity and session identity; check held-input release and return. |

Use the admitted one-connector topology. Do not invent card stacks, cassette
transport or equate tape selection with saving a BASIC program. Program-save
claims require a supported operation and evidence.

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
