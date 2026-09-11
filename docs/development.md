# Development through FES

New to the checkout? Begin with [getting started](getting-started.md). For
assignments and team handoffs, see [the agent workflow](agent-workflow.md).

Read [the parent instructions](../AGENTS.md) and
[component boundaries](component-boundaries.md) first. Choose the component
that owns the behavior, then read its instructions, README and current
architecture before editing. FES coordinates compatible revisions; each
component keeps its own implementation and focused development loop.

## Isolate component work

From the FES root, create a worktree from the selected clean component HEAD.
For example, for a FogCast task named `launch-status`:

```sh
mkdir -p out/dev/launch-status
git -C sources/FogCast worktree add -b feat/launch-status \
  "$PWD/out/dev/launch-status/FogCast" HEAD
```

Use the corresponding directory under `sources/` for another component.
`out/` is ignored. Use a unique task/branch name for each concurrent task in
the same component repository. Leave the root `sources/` checkouts clean and
at their selected pins so other integration work can build reproducibly.
Run component commands inside the worktree; parent builds use selected
revisions, not an uncommitted component worktree.

When parallel work helps, assign disjoint scopes and share contract changes
early. The integrator selects reviewed component results, updates parent
gitlinks and affected profile/contract inputs, and runs integration checks.
Workers do not move root component checkouts or update gitlinks independently.
Committing and publishing still require user authorization.

For a reviewed component commit already available in its repository, the
integrator selects it explicitly, for example:

```sh
git -C sources/FogCast checkout --detach REVIEWED_FOGCAST_COMMIT
git -C sources/libmister-runtime checkout --detach MATCHING_RUNTIME_COMMIT
git add sources/FogCast sources/libmister-runtime
make check
make host
```

Replace the uppercase placeholders with full reviewed commit IDs. If a worker
used another clone, fetch its branch into the component repository first. The
runtime commit must match FogCast's native input lock. Stage package or FPGA
gitlinks in the same way when they change. These staged gitlinks are what the
parent validates, so the candidate can be built before a parent commit or PR.
Before publishing the parent, ensure each selected commit is available from the
component remote; a local-only commit will break recursive clones elsewhere.

## Validate at the right scope

| Command from FES root | Purpose |
| --- | --- |
| `make test` | Parent regression tests |
| `make check` | Component pins, locks and generated-definition consistency |
| `make doctor` | Selected profile and build prerequisites |
| `make host` | Compile selected linux/amd64 host outputs and `host.json` |
| `make dev` | Incremental diagnostic native image using a persistent base |
| `make build` | Build selected host and cold two-pass native image outputs |
| `make verify` | Verify published outputs against receipts and child checks |
| `make media` | Assemble a flashable disk image from verified cold outputs |
| `make verify-media` | Verify `media/current/fes.img` and its host-side evidence without deployment |

The default is `native-integration-dev`. Select a historical profile explicitly
with `PROFILE=native-dev` or `PROFILE=native-source-dev`; their evidence applies
to those revisions and artifacts. Source builds require the configured
Quartus toolchain (`QUARTUS_ROOTDIR`). Build and verify do not deploy. The
default profile also authenticates the pinned open-source misteross tools before
selecting its described FES Pong package. See
[described FPGA core packages](core-packages.md) for first-checkout setup and
the inspect/load/Stop workflow. Host-only builds do not require those tools.

During component development, use the narrow relevant component tests and
build only changed artifacts. For target diagnostics, follow the selected
FogCast development guide's fast loop using a disposable copy of a verified
image. Label those results diagnostic. Reserve expensive full image and
two-pass reproducibility checks for stabilized integration; full rebuilds
are also required when image configuration, packaging or locked inputs
change. One parent build runs per checkout, enforced by its existing lock.
Coordinate shared expensive runs rather than starting one per agent.

## Incremental native image

Run `make dev` for the selected `native-integration-dev` revisions. It publishes
`out/native-integration-dev/development/linux.img` and a `development.json`
receipt after structural validation. It uses the same pinned child package,
overlay and image recipes as the clean build. It does not deploy, run QEMU,
produce two-pass evidence, or replace the clean image and receipts.

The development Buildroot volume retains the compiler, libraries and package
outputs. The Go agent uses Go's compilation cache; the runtime package is cleaned
and rebuilt when its selected commit changes. An unchanged complete output is
reused after receipt/hash checks. Otherwise, full Buildroot finalization runs to
install the current agent, selected RBF set and build-input record. A package
selection change retains the compiler/base volume but forces final image
assembly. The development receipt binds the emitted package selection and the
exact external `manifest.toml` and `core.rbf` bytes.
A failed build leaves no development success receipt; the next invocation can
resume package compilation.

A compatible existing clean pass can seed a new development volume. FES checks
its source/profile/Go identities, receipt hashes, both recorded image hashes and
the actual pass-two image before copying it with symlinks and modes preserved.
The clean volume is mounted read-only. Without a compatible seed, the first run
builds the base once. Subsequent runs retain it. Internal Buildroot paths remain
identical because its generated host tools are not generally relocatable.

The cache key covers the child's Buildroot tree (configuration, overlays,
patches and package recipes), container inputs, source/package locks, scripts,
Makefile, native input policy except the runtime commit, and the parent
incremental runner. Changes to these inputs select a separate fresh volume.
Application-source changes and runtime commit changes retain the base. The child
container cache identity uses input contents rather than checkout locations, so
identical compiler containers are shared across component worktrees. RBF
selection from different source-built bundles is installed during finalization;
a change to the locked RBF policy selects a new base. This intentionally
conservative key can be narrowed later with evidence.

`make dev` builds selected clean revisions, not arbitrary uncommitted worker
checkouts. Integrate reviewed component commits using the commands above before
running the parent build. Workers can still use their component's artifact-only
diagnostic loop. Historical profiles continue to use their cold builds.

Use `make build` and `make verify` for stabilized integration and release checks.
A warm development image is diagnostic evidence and cannot satisfy those
commands' release receipts. `make rebuild` still forces the full cold build,
including Quartus. Native development does not build the separate QEMU test
kernel. Unused development volumes may consume several GB each; their exact
names are recorded in the development `inputs.json`.

After those cold receipts pass, `make media` can assemble
`out/native-integration-dev/media/current/fes.img`. On a local checkout it
automatically embeds a target agent configuration derived from the private host
config, so the generated card is ready to boot. It reads the verified cold
rootfs and does not rebuild it; `make verify-media` revalidates the current
immutable media generation, including the embedded rootfs and QEMU packaging
check. Both commands operate on files under `out/` and never write a block
device. See [bootable media](bootable-media.md) for the unprovisioned override,
rollback and the physical acceptance boundary.

## Hardware and handoff

The exact disposable kit and its operating instructions are in the selected
FogCast [development guide](../sources/FogCast/docs/DEVELOPMENT.md), alongside
its [working policy](../sources/FogCast/AGENTS.md). Use that designation;
an arbitrary reachable device is not authorized by a successful build.
One operator owns the kit for the duration of a test. Arrange handoff before
another worker deploys, reboots or runs diagnostics.

Report component work with this short handoff:

- Scope and changed behavior.
- Base commit and result commit, or the location of the uncommitted diff.
- Commands run and results, including failures or untested paths.
- Hardware status: host-only, diagnostic, or acceptance with exact artifact
  identities and dated evidence.
- Parent pin or shared-contract effects and the next integration step.

Passing host tests or reusing historical hardware evidence does not establish
hardware acceptance of `native-integration-dev`. Record acceptance only for
the artifacts actually exercised. Name the selected component commits and
receipt hashes; an uncommitted worktree is not those artifacts. See
[artifact identities](artifacts.md). The separate migration of whole-system
image assembly into FES remains outside this workflow change.
