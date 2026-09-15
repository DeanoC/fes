# FogCast target-client boundary design

## Goal

Separate FogCast's host application services from the client-side transport
used to control a target agent. This is a source-ownership and incremental
build improvement; it does not change the target protocol or runtime behavior.

## Current problem

The \`host\` package currently combines four unrelated surfaces:

- catalog/config/manifest and the host library facade;
- authenticated target HTTP requests, cache transfers, core/package loads,
  development loads, and appliance updates;
- target kit leases and endpoint reconciliation; and
- host-owned remote-input bridges.

\`ui/kitlauncher\` only needs the target HTTP and lease surface for its
hostless launch path, but therefore imports the broad \`host\` package. The
FogCast service and command packages also use \`host.Client\` as the concrete
target client. This makes the host package an accidental dependency hub and
prevents the source tree from expressing the host/target boundary.

## Proposed ownership

Create a top-level \`targetclient\` package containing the client-side target
transport and target ownership operations:

- \`Client\` and its health, status, launch, stop, cast, cache, core, media,
  development, and appliance methods;
- \`KitLease\`, lease request authorization, renewal/release, and status;
- endpoint adoption, peer clients, endpoint identity, and ownership metadata;
- target-facing error and response types such as \`CastStatus\`,
  \`KitOwnership\`, \`ApplianceStatus\`, and \`ResolveAppliance\`.

Keep these responsibilities in \`host\`:

- \`ConnectionConfig\`, \`LoadConnection\`, \`Manifest\`, and \`Library\`;
- \`RemoteInput\` and its host-side bridge interfaces; and
- \`HTTPBridgeStarter\`, which uses \`targetclient.KitLease\` but remains the
  host-owned input bridge implementation.

The target client package must not import \`host\`, either UI package, or the
target agent implementation. Host bridge code may depend on \`targetclient\`
through the small exported lease methods it needs (\`AuthorizeExisting\` and
\`CurrentToken\`).

## Compatibility and behavior

This is an in-module package move, not a compatibility-shim release. All
FogCast consumers and tests are updated to import \`targetclient\`; the old
\`host.Client\`, \`host.KitLease\`, and related symbols are removed rather than
duplicated. HTTP paths, request headers, validation, lease semantics, error
types, and runtime launch behavior remain unchanged.

## Guardrails

\`scripts/tests/target-boundary_test.sh\` will enforce:

- the \`targetclient\` package exists;
- \`ui/kitlauncher\` does not import \`host\`;
- \`targetclient\` does not import \`host\`, \`ui\`, or \`internal/agent\`; and
- no Go source retains the removed \`host.Client\`, \`host.KitLease\`, or
  \`host.NewClient\`/\`host.NewKitLease\` references.

The guard is structural only. Existing package and integration tests remain
the behavior proof for the moved code.

## Non-goals

- no target API or wire-format changes;
- no repository split or submodule changes;
- no changes to the \`internal/agent\` runtime adapter boundary in this slice;
- no hardware deployment; and
- no FES parent build or image changes.

