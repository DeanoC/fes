# FES structure and refactor status

Reconciled on 2026-09-17 against FES `095cc7a` and its selected FogCast
`c6b7841`. This is the current ownership decision, not a queue of migrations
to repeat. Historical plans describe the implementation sequence; their
unchecked execution steps are not evidence that merged work is absent.

## Decision

Keep FES plus its four component repositories. FES owns appliance assembly,
boot software, component selection and integration evidence. FogCast owns the
host application, clients and network-facing target agent. Runtime hardware
control, FPGA builds and shared definitions retain their existing owners.
Main_MiSTer is reference/test material, not a native production dependency.

Keep the target implementation in the FogCast root Go module for now. Its
public contracts and dependency guards are already separated; a further
module/repository split needs an independent release/consumer requirement or
measured build/test coupling, not just a desire to move directories.
Keep the appliance schema/store as one
nested module consumed by both FES boot and FogCast updates.

## Landed work

| Area | Implemented boundary | Merge evidence |
| --- | --- | --- |
| Image assembly | FES `image/`; no second native image recipe in FogCast | FES [#26](https://github.com/DeanoC/fes/pull/26), [#27](https://github.com/DeanoC/fes/pull/27) |
| Build reuse | Host-only inputs, validated FPGA artifact reuse, shared compiler cache, HIP-first format-2 package lanes | FES [#36](https://github.com/DeanoC/fes/pull/36)–[#40](https://github.com/DeanoC/fes/pull/40), [#44](https://github.com/DeanoC/fes/pull/44) |
| UI and transport | Explicit `ui/` namespace and separate `targetclient` | FES [#48](https://github.com/DeanoC/fes/pull/48), [#49](https://github.com/DeanoC/fes/pull/49) |
| Shared appliance module | FogCast `appliance/` owns release schema and `store/` | FES [#52](https://github.com/DeanoC/fes/pull/52), FogCast [#238](https://github.com/DeanoC/FogCast/pull/238) |
| Boot ownership | FES `platform/` owns boot command and Linux helpers | FES [#55](https://github.com/DeanoC/fes/pull/55), FogCast [#243](https://github.com/DeanoC/FogCast/pull/243) |
| Target contracts | Public core-package/lease contracts and executable dependency guards | FES [#57](https://github.com/DeanoC/fes/pull/57), FogCast [#244](https://github.com/DeanoC/FogCast/pull/244) |
| Shared client logic | UI-independent session decoder, library client, launch eligibility and artwork-handle normalization | FES [#58](https://github.com/DeanoC/fes/pull/58), [#59](https://github.com/DeanoC/fes/pull/59) |
| One launch path | CLI and Kit use the persistent host session API; shutdown cleans up only owned sessions | FogCast [#241](https://github.com/DeanoC/FogCast/pull/241), [#249](https://github.com/DeanoC/FogCast/pull/249); FES [#63](https://github.com/DeanoC/fes/pull/63) |
| Package development | Package-only admission and isolated host restart diagnostics, separate from image assembly | FES [#62](https://github.com/DeanoC/fes/pull/62), [#64](https://github.com/DeanoC/fes/pull/64) |
| CLI contract follow-up | Preserve mutation deadlines and complete public input status | FES [#66](https://github.com/DeanoC/fes/pull/66), FogCast [#250](https://github.com/DeanoC/FogCast/pull/250) |

These merges establish source integration, not blanket hardware acceptance.
The factory image still selects Pong/ZX81/Coleco. Admitting another supported-ABI
package through the host library does not add it to that factory image.

## Current source ownership

| Responsibility | Owner and source |
| --- | --- |
| Image, media, release assembly | FES `image/`, `scripts/appliance*.py` |
| Boot policy, Linux mechanisms and boot executable | FES `platform/cmd/fes-boot`, `platform/internal/applianceboot`, `platform/internal/bootlinux` |
| Shared appliance release schema and immutable store | FogCast nested module `appliance/`, `appliance/store` |
| Network update admission | FogCast `internal/applianceupdate` with target-agent coordination |
| Operator update client | FogCast `cmd/fes-update` |
| Target HTTP/cache/session coordination | FogCast `cmd/mister-agent`, `internal/agent`, `internal/httpapi` |
| Public target contracts and transport | FogCast `corepackage`, `kitlease`, `protocol`, `targetclient` |
| Host/library services and browser API | FogCast `host`, `fogcast`, `catalog`, `internal/hostapi` |
| Shared host client and UI strategies | FogCast `hostclient`, `ui/tenfoot`, `ui/kitlauncher`, browser assets |
| Physical FPGA/media/input lifecycle | libmister-runtime library and daemon |
| FPGA recipes, compiler tools and source builds | misteross |
| Board/ABI definitions and generation | mister-packages |

The host chooses content; the agent coordinates network requests and leases;
the runtime performs physical transitions. No repository split should create a
second implementation of any of those decisions.

## Rules retained by the refactor

- One appliance manifest/store implementation. FES builds against the selected
  immutable FogCast module source; published inputs contain no worker paths.
- Test nested Go modules explicitly: root `go test ./...` does not include them.
- Bootstrap evidence identifies FES platform source, selected appliance module,
  Go toolchain and flags. Do not attribute FES boot source to FogCast.
- Keep target implementation free of host/UI dependencies and public contracts
  free of target internals; the selected FogCast Makefile runs the boundary tests.
- Share transport/schema and genuine policy, not device-specific rendering or
  input. Browser JavaScript and Go client models can differ intentionally;
  shared fixtures test the contract without claiming complete decoder identity.
- Host/UI changes do not require FPGA compilation. Use `make host`, component
  tests and parent consistency checks. Image, media and exact-artifact hardware
  gates remain separate; compiler caches are not acceptance evidence.

## Remaining work, without another broad migration

The bounded source audit at the revisions above found these follow-ups. These
are source-level discrepancies, not reproduced hardware failures. Establish
contract tests before changing behavior; this reconciliation changes no client
implementation.

| Priority | Finding | Next bounded action |
| --- | --- | --- |
| First | Kit grid/detail admission checks `Game.Launchable` plus host readiness, while `hostclient.Game.LaunchBlock` also checks source/readability state (`ui/kitlauncher/model.go`, `detail.go`, `hostclient/library_models.go`) | Add missing/offline/invalid/available cases and use shared catalog eligibility while retaining session/target readiness checks |
| First | Browser `launchBlockReason` checks offline roots, but `launchAllowed` omits that check; missing booleans differ from Go zero values (`internal/hostapi/ui_app.js`) | Define one bounded cross-client eligibility fixture; preserve additional session-authority checks |
| Later | CLI and `hostclient` retain different session projections, success validation and mutation timeout policy (`internal/fogcastcli/session.go`, `hostclient/client.go`, `session_client.go`) | Specify the intended differences before sharing transport; do not undo the CLI deadline fix |
| Small cleanup | Exact `fes.keyboard` interface-version recognition is repeated in shared session decoding and Kit capability handling (`hostclient/session.go`, `ui/kitlauncher/client.go`) | Share only the capability predicate if a focused test demonstrates equivalent semantics |

Separate browser/Go decoders, CLI full input metrics, Kit's narrow session
projection and device-specific rendering are not by themselves duplication bugs.

1. Retire obsolete worktrees only after checking ownership, unique changes,
   nested worktrees, running jobs and retained evidence. See the dated
   [workspace audit](validation/2026-09-17-workspace-tidying.md). The inventory
   is not a deletion list. Bring the canonical checkout forward in a separate
   coordinated operation after its selected components and users are checked.
2. Fix concrete duplicated behavior when demonstrated by a failing contract
   test. Do not merge all clients or introduce another abstraction solely to
   eliminate similar-looking structs.
3. Revisit target extraction only if a separately released agent, an external
   consumer, or measured build/test coupling requires it. Account for agent,
   cache, input, kit launcher and updater dependencies together before moving.
4. Keep hardware acceptance attached to exact artifacts. SG-1000 display and
   controller acceptance remains separate from the passed package lifecycle
   diagnostic; new core development does not reopen the completed cache work.

There is no further repository split or cache rewrite scheduled by this
decision. Future feature work should use the established boundaries.
