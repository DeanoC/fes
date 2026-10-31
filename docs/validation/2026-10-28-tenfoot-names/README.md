# Tenfoot names and current rooms — 28 October 2026

This follow-up to the [visual pass](../2026-10-28-tenfoot-ux/README.md) starts
at `f31cfccc911702d723bede22ec38a96bf620ca1f` on `codex/tenfoot-ux-polish`
(PR #1187). Final executable result is `b1397abc6f78f1cc8e15f1c344c1e4375a0512ac`.
The naming-only software campaign uses `b3ff95896edd1643a4d1f532cb4b8f9c7cb2915f`;
the final result also merges the independently pushed Library Details shortcut
fix `bb350c62c1` through merge `62c4ac8608`, then adds its missing controller
Search hint and verifies the second-Y path.
FogCast owns display names, navigation and embedded room packs.

## Result

Home and Systems are the two navigation rooms. System setup and Sinclair setup
are internal templates opened for an exact model and entry; they are excluded
from startup and room pickers. Home leads directly to Systems and the native
overlay is titled Menu. Removed built-ins: Lobby, Amiga Workbench, Mario World,
Mario Sports, SNES, TMS, Coleco Arcade and the redundant C64 wrapper. The shared
C64 setup retains exact core/game selection, disk, expansion and session paths.
Installed room packs and the public widget/authoring APIs remain available.
Missing retired builtin pins are omitted; unavailable external pins remain.

Known first-party machine names use exact core IDs and plain labels, including
Pong, Catch, Sinclair ZX81, Commodore 64 and the selected Spectrum model. The
wordmark stays in shared chrome. New setup titles use the model name instead of
a package-choice label containing version/hash. Those details still distinguish
package versions in the chooser.

Existing generated machine defaults with a reserved HIL issue/timestamp or
version/hash shape are presented with the model name. The retained diagnostic
example `Coleco HIL 1120 123509` is covered, alongside the user's HIL790 form
and producer prerelease/build versions. Genuine game/custom titles and unknown
systems retain their supplied names. This is display projection: stored entries,
raw package metadata, exact package/media/ROM/expansion IDs, selected editions
and session generations are unchanged. Library projection applies to visible
items and individual selections, without scanning/copying the catalog per frame.

The version recognizer is a private presentation matcher for the old generated
label shape. Package validation is byte-identical to the base. No package format,
network API, runtime, FPGA or shared hardware contract changes. No core package or OS image rebuild is required.

## Checks and captures

The [bundle receipt](bundle-receipt.json), [capture summary](capture-summary.json),
[archive verification](archive-verification.json) and [reference hashes](captures.json)
bind the artifacts to that executable source. The capture run renders 86 gallery
and 47 native frames, with all four packs, 5 native tests and 45 subtests passing.
The final archive verifies all 333 tracked UI files plus three naming/validator
files against 336 exact Git blobs. The earlier 217-file verification covered a
narrower UI subset. Seven of 13 references change after the merge: native
Details prompts and asynchronously loaded Sinclair illustrations. Prior b3
capture metadata is retained alongside the current comparison. Both viewports
and all changed references were reviewed; no new blocking action overlap was found. The TS1500 no-art
fallback retains a small caption/expansion-marker intersection from the earlier
layout; physical readability acceptance remains open.

The initial full UI run caught three stale adopted-run title expectations and
a Home action bug: a ready local item was gated by an unrelated catalog load.
The gate now applies to Library only, retaining all session, overlay and stale
identity checks. Deterministic tests hold both library flags pending at both
sizes and open Details without launching. Full rooms and tenfoot checks pass;
[initial log](ui-focused.log.gz), [UI retry](ui-retry.log.gz) and
[pending-library regression](home-pending-library-regression.log.gz).

An intermediate dependency exported package version validation for UI label
matching. That unnecessary change selected a broad video/package campaign;
its [partial log](superseded-video-plan.log.gz) is retained, with the
[verified-own-process stop record](superseded-plan-stop.json). It is not a
passing run. The export is removed, package validation exactly matches the base,
and the UI owns the same historical display matcher; [scope proof](display-matcher-scope.json).
The final plan is rerun normally without video selection; no admission rule,
constraint, callback limit or test gate is relaxed.

The current [native references](../../ux/references/README.md#current-native-pass)
show all four retained templates and navigation pages at 1280×720 and 1024×600.
The dated visual-pass record retains the earlier 12-pack evidence as history.
The manual [gallery fixture](capture-gallery.go.txt) uses the real Menu row
builder so internal templates and removed demos cannot reappear in its screenshot.

The [affected report](test-changed-report.json) is source-bound to the naming
implementation `b3ff95896e` and reports all seven selected commands successful: 21 planner
tests, 31 runner tests, 864 parent regressions, generated consistency and the
complete seven-stage host lane. It records 126 Go package pass results, 361 Node
and 78 browser tests. `make check` and `make deadcode` also pass; see
[checks and artifact hash](checks.json) and [complete log](final-test-changed.log.gz).
The terminal wrapper returned 143 after the complete passing report was written;
this discrepancy is retained, not reported as an aggregate exit 0. An independent
check verifies the exact source and every selected command's return code 0.
Focused UI, rendering and bundle inspection passed independently.

After merging the separately pushed Details shortcut fix, the complete naming,
rooms and tenfoot [package tests](merged-ui-tests.log.gz) pass
(0.008s, 10.621s and 46.926s respectively).
The final controller hint change passes [focused Library/action/affinity checks](search-hint-tests.log.gz)
(4.823s) and their [race run](reviewed-ui-race.log.gz) (6.373s). A ready Library item advertises Y Details;
Details advertises Y Search, and a second Y opens Search without launching.
The original source-bound aggregate is retained without claiming it ran the
later merge. Final rendering, bundle and static checks identify the final source
separately in [checks](checks.json).

## Hardware boundary

Final userspace bundle `ee79404cf763b94a65d89425887304b2194bc04f25b536f53003f8fcfa473f19` builds and passes producer inspection
against retained base image SHA-256
`f904961e008a972db8f06f1bb47aff9631123d67215b9f4157ea1fbee83b98d4`. The tar is retained at this task worktree's
`out/ux-pass/names/reviewed-bundle/bundle.tar`; [build log](bundle-build.log.gz).
The naming-only [b3 receipt](naming-bundle-receipt-b3.json) and
[checks](naming-checks-b3.json) are retained. Both earlier naming bundles and the
intermediate merged-source bundle are superseded and were never deployed.

Software-renderer fixtures are host-only evidence. The new userspace bundle
must be activated on a claimed test kit's existing compatible image for physical
readability/controller/menu-display acceptance. Check plain names through Home,
Systems, variants, saved setup/media, Details, live media and launch/stop; confirm
no removed rooms or retired pins appear. Restore the authoritative status file's
baseline bundle/image as applicable, verify the active bundle, idle state and
free lease, then release. No deployment is claimed by this record.
