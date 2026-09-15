# FES-first UI Package Boundaries

## Goal

Make FogCast's application split visible in the source tree before moving any
application ownership into FES. The first slice gives the two UI applications
an explicit `ui/` namespace while preserving package names, commands, runtime
behavior, and all wire and image interfaces.

FES remains the integration repository: it selects component revisions,
assembles images, and records build evidence. FogCast remains a Go module for
this slice, containing the UI applications, host services, and target agent.

## Current state

FogCast currently mixes several responsibilities in directory names that no
longer describe ownership:

- the ten-foot UI is under `host/tenfoot`;
- the kit-launcher UI is at the module root in `kitlauncher`;
- host services and the target agent are already separate packages, but the
  source layout does not make the UI boundary obvious.

The FES parent already owns component selection and image integration. It
should not absorb UI implementation merely to make the integration tree look
complete.

## First slice

Move only the two UI package trees:

```text
FogCast/host/tenfoot  -> FogCast/ui/tenfoot
FogCast/kitlauncher   -> FogCast/ui/kitlauncher
```

Keep the Go package declarations as `tenfoot` and `kitlauncher`. Update Go
imports, build references, documentation, and test paths to the new source
locations. Add a structural regression test that rejects the old directories
and old repository import paths.

Do not move host services, the target agent, appliance helpers, or the command
entry points in this slice. Do not split or consolidate repositories yet.

## Resulting ownership

- `ui/tenfoot`: ten-foot UI domain, rendering, input mapping, animations, and
  the host API client types used by that UI.
- `ui/kitlauncher`: kit selection UI, framebuffer presentation, and UI
  controllers.
- `host` and `internal/hostapi`: host-facing services and their public API.
- `internal/agent`: target-side HTTP, cache, and coordination behavior.
- `internal/mister`: local MiSTer integration.
- `cmd`: thin executable wiring only.
- FES parent: component pins, image assembly, release evidence, and
  reproducibility checks.

The UIs may consume host API contracts, input, and protocol packages. They
must not own target handlers, runtime lifecycle, image assembly, or FPGA
builds. The target agent must not import UI packages.

## Compatibility and non-goals

- Preserve command names, flags, configuration keys, routes, package names,
  and Go module boundaries.
- Preserve sockets, HTTP APIs, media behavior, image contents, and target
  lifecycle behavior.
- Do not change hardware support or claim hardware acceptance from source
  tests.
- Do not move host/agent/appliance code or redesign APIs until this boundary
  has landed and been exercised.

## Verification

The component PR must pass the structural boundary test, focused UI tests,
`go test ./...`, `go vet ./...`, and the relevant FogCast build targets. The
parent PR must pass its normal `make test`, `make check`, and whitespace
checks with the new FogCast gitlink. Existing target image and physical
acceptance remain separate gates for when the user is at the machines.

## Follow-on slices

After this slice is merged, clarify and test the `host`, `internal/agent`, and
appliance/package ownership boundaries. Only then decide whether an FES-first
repository consolidation or a repository split reduces coupling; neither is
required to establish the first useful boundary.
