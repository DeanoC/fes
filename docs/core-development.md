# Core developer workflow

Use this lane to prepare one described core for an **already-compatible**
platform. It never assembles `linux.img`, changes the factory package set,
updates target software, or silently falls back to Quartus.

## Ownership and source of truth

`config/core-recipes.toml` maps core IDs to the selected misteross producer,
authentication function and toolchain lock. It is versioned, strictly validated
data, not a list of shell commands. All current producers use HIP/nextpnr.
The existing image builder and the developer command consume the same registry.

Keep ABI, programming profile and interface declarations in the sealed package
manifest, generated from the owning shared/core definitions. Runtime compatibility
is negotiated by the existing host/agent/runtime path; do not copy capability
limits or controller mappings into this registry. Media is an explicit immutable
library selection, not a compiled-in host asset.

To add a compatible core, implement and test its producer in misteross, select
that reviewed component revision in FES, and add its recipe row. No new host/UI
allowlist is required. A new ABI or transport still requires separately reviewed
runtime support. A registry row alone cannot create that support.

## Prepare one package, without an image build

From a clean pinned FES checkout on the development machine:

```sh
mkdir -p out/core-dev
make core-dev CORE_DEV_ARGS='prepare --core fes.pong --output out/core-dev/pong-001'
```

Choose a new output directory for each candidate. Preparation checks selected
component revisions, takes the normal parent build lock and runs the existing
authenticated package resolver for **only** that core. Matching package/compiler
inputs are reused; a package miss runs its producer. Missing compiler prerequisites
fail with the existing toolchain setup guidance in [core packages](core-packages.md).
It does not invoke Docker/image assembly or contact a host API or kit.

The output contains a frozen `.fcore`, its selection and `prepared.json`.
The archive is checked against the resolved manifest/payload identity before
publishing that receipt. Partial failures have no success receipt. Do not edit
the frozen candidate to follow a newer branch; prepare a new candidate instead.

For a core requiring a diagnostic ROM, supply an explicit file and independently
checked digest:

```sh
make core-dev CORE_DEV_ARGS='prepare --core fes.sms --output out/core-dev/sms-001 --library-media /absolute/path/diagnostic.bin --expected-media-sha256 ROM_SHA256'
```

Preparation snapshots those media bytes. It does not infer cartridge format,
mapper support, target capacity or hardware success from the filename or size.
Package-specific capacity admission remains the host's responsibility.

## Exercise the frozen candidate

After obtaining exclusive kit use and authorization, use the existing isolated
host acceptance path via the prepared candidate adapter:

```sh
make core-dev-accept CORE_DEV_ACCEPT_ARGS='--prepared out/core-dev/pong-001/prepared.json \
  --host-binary /absolute/path/fogcast-api --expected-host-sha256 HOST_SHA256 \
  --container-image sha256:EXISTING_CONTAINER_IMAGE_SHA256 \
  --host-config /absolute/path/private/config.toml \
  --evidence-dir /absolute/path/new-acceptance-run \
  --expected-target-id AUTHORIZED_TARGET_ID \
  --expected-host-revision HOST_COMMIT --expected-agent-revision AGENT_COMMIT \
  --expected-runtime-revision RUNTIME_COMMIT \
  --new-entry-title "FES Pong development" --execute'
```

Replace placeholders with independently established identities. The adapter
rehashes the candidate files and supplies their exact identities to the existing
runner. You cannot override the candidate's archive/package/core/media fields.
Successful runs add `preparation.json` to the evidence directory, binding the
exact preparation receipt digest to that run. Source revisions and manifest
digests in the preparation record describe the earlier preparation checks;
the adapter does not independently re-attest build provenance. The existing
host package reader validates the imported archive against the expected package ID.
Target identity and software expectations are never discovered and blindly trusted.
No `--execute` means no acceptance execution. The original
[package acceptance guide](package-acceptance.md) defines the lease, version,
failure, cleanup and Docker-image requirements; all still apply.

For SMS stream-media diagnostics, explicitly add `--timeout 300 --runner-timeout 600`
as described there. No ambiguous mutation is automatically retried.

Acceptance imports the package/media into a private host catalog, performs
compatibility and selection checks, launches and stops, restarts the private
host, then relaunches and stops the same selection. It does not modify the live
host library. Imported media must survive that restart without reimport.

## Evidence boundary and next slice

Preparation proves a bound build artifact, not runtime compatibility.
Isolated acceptance proves the recorded package/media selection and lifecycle,
not visible pixels or controller behavior. It is not an image/release receipt.

The next slice is an explicit core-specific input diagnostic consumed by the
existing acceptance runner, with observed input-delivery evidence and separate
physical display/controller confirmation. Do not substitute generic Coleco
events for an unfamiliar core. Independent execution by another team is also
required before declaring new-core onboarding routine.
