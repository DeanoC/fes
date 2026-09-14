# FES build lanes and format-2 production design

## Decision

FES is the parent product repository. Its normal development path resolves
format-2 core packages from a small recipe registry and delegates FPGA
synthesis to the producer recorded by that recipe. misteross remains the
producer for the current FES packages; FES owns selection, pinning, image
assembly, receipts and cache policy.

Pong, ZX81 and Coleco are distinct format-2 recipes. They may share the
parent dispatch and package validation code, but they do not share a
toolchain lock when their tool identities or nextpnr databases differ.

## Standard FPGA lane

Every FES nextpnr production recipe uses the HIP/GPU router:

- the producer supplies --router gpu;
- the producer authenticates the requested HIP cache configuration;
- the route log must prove a live HIP backend;
- a CPU-reference fallback is an error, never a successful build;
- the package build record includes the router/backend and HIP architecture
  parameters.

The parent passes the shared cache root explicitly as --cache-root to the
producer. A producer does not select shared-cache mode from ambient
FES_TOOLCHAIN_* variables. Omitting --cache-root keeps the existing
worktree-local toolchain behavior.

The repository-wide lock is the HIP lane for Pong and ZX81. Coleco keeps its
specialized lock and cache identity. Cache keys therefore cannot alias a
Pong/ZX81 slot with a Coleco slot.

## Parent recipe registry

The parent has one descriptor for each supported recipe:

| Core | Producer | Lock | Router | HIP architectures | Quartus role |
| --- | --- | --- | --- | --- | --- |
| fes.pong | build_fes_pong.py | toolchain.lock | HIP | gfx1100;gfx1201 | check only when a twin exists |
| fes.zx81 | build_fes_zx81_oss.py | toolchain.lock | HIP | gfx1100;gfx1201 | bring-up/check oracle |
| fes.coleco | build_fes_coleco_oss.py | cores/fes-coleco/toolchain.lock | HIP | gfx1100;gfx1201 | bring-up/check oracle |

The descriptor owns the producer entry point, lock path, cache lane,
selection filename and optional Quartus oracle metadata. Resolution is
common: derive one canonical build-input record, reuse exactly one matching
package when possible, build on a miss, validate the closed package, and emit
the format-2 selection record. FES never installs a Quartus oracle artifact
as the OSS package.

The first production image remains a single selected format-2 package because
the current target-image selector admits one package record. The registry and
resolver must nevertheless cover all three recipes so ZX81/Coleco can be
enabled by a later image/profile change without another parent-dispatch
rewrite. A profile selecting an unsupported multi-package image is rejected
explicitly rather than silently dropping packages.

## Format-1 retirement

FES production no longer invokes generic format-1 misteross bundle recipes.
The default integration path retains only the idle RBF and the selected
format-2 package. MegaDrive, SNES, NES and catalog Pong format-1 outputs are
not rebuilt or installed by that path. Historical standalone profiles and
the independent misteross targets may remain until their own retirement; they
are not part of FES production.

This intentionally removes the old catalog cores from the default FES image.
The change is explicit in the profile and documentation; it is not a silent
format-1-to-format-2 substitution.

## Quartus and measurement

Nextpnr/HIP is the normal production route wherever the producer supports the
system. Quartus is a non-installing oracle/check for supported systems and
remains a production path only where nextpnr has no system support. There is
no automatic fallback from a failed HIP route to Quartus.

Validation records separate host cold/hit, shared toolchain cold/hit, package
miss/hit, nextpnr rerun/reuse, parent image/development reuse and Quartus
oracle timing. Hardware acceptance is a later claim tied to the exact package
and image hashes.

## Non-goals

- splitting misteross, runtime, FogCast or package schemas in this slice;
- changing the format-2 package ABI;
- claiming ZX81 or Coleco hardware acceptance from build or Quartus evidence;
- deleting historical standalone format-1 targets from misteross;
- making Quartus a hidden fallback for a failed HIP build.
