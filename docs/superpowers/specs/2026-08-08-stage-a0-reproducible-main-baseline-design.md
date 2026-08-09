# Stage A0: Reproducible Main Baseline Design

## Status and authority

**Status:** Approved design pending implementation, dated 2026-08-08.

This focused design implements the source-provenance and deterministic-build
portion of Stage A in the active [migration roadmap](../../ROADMAP.md). It is
subordinate to the approved [FogCast architecture](../../ARCHITECTURE.md),
[ADR 0001](../../adr/0001-portable-target-runtime.md), [ADR 0002](../../adr/0002-disposable-local-development-target.md), and the detailed
[portable target runtime design](2026-08-08-fogcast-portable-target-runtime-design.md).
It does not authorize a commit, publication, deployment, target mutation, or
hardware observation.

Stage A0 establishes a reproducible source and artifact baseline for a narrow,
upstream-style Linux `Main_MiSTer` build. It is deliberately earlier and
narrower than Stage A completion. In particular, it does not assert FPGA launch
equivalence, HDMI/audio/input/save behavior, target-image equivalence, HIL
observation, or acceptance.

## Decision summary

FogCast will maintain a local sibling GPLv3 Main fork; it will not add a
submodule or vendor Main source:

```text
/Users/clawzai/Developer/
  mister-remote/     FogCast: locks, adapters, verifiers, and evidence
  Main_MiSTer/       local compatibility fork, selected by --destination
```

The displayed sibling path is this operator's intended initializer destination,
not a shared-lock value or cross-operator identity. Shared provenance uses
canonical logical roots and Git objects, never `/Users` paths or other machine
identities.

Stage A0 uses two phases. First, tracked machine-independent
`build/stage-a0-bootstrap.toml` pins the official Main source commit/tree,
branch policy, and official-base `VDATE` semantics. It contains no local fork
commit and no self-hash. Second, `scripts/stage-a0-init-main.sh` materializes
the fork and first patch; tooling then generates a candidate
`build/stage-a0-main.lock.toml` for independent review and promotion. The final
lock does not hash or identify itself: its SHA-256 belongs in an external review
and evidence record. `fogcast_base_revision` is the pre-generation FogCast
input commit, not a reference to the generated final lock.

FogCast owns the machine-readable cross-repository lock, deterministic build
adapter, verifier, comparison manifests, fixtures, and evidence reports. The
fork owns only the GPLv3 Main source and a small, reviewable,
metadata-limited patch series. Its first patch replaces build-time `VDATE` generation with an
explicit supplied input while preserving the selected official base's visible
`%y%m%d` representation: `YYMMDD`, six ASCII decimal digits rendered from the
locked UTC epoch. The expression and exact source evidence are verified from
the locked official base. Changing that representation is a separate behavior
decision. The patch changes no source-set selection, compiler mode,
optimization, link closure, runtime behavior, or public FogCast protocol.

The local fork may initially be `local-only`. It may support a local engineering
result but its maximum canonical evidence status is **Software-tested**, even
when two clean builds match. The result records
`two_builds_byte_identical = true` and `source_availability = "local-only"`
separately. **Reproducible** requires durable source retrieval and the
applicable independent gate; Stage A0 never creates HIL-observed or Accepted
evidence.

## Goals

1. Establish one auditable, pinned cross-repository input set for the narrow
   Main build used to begin Stage A.
2. Make its build metadata deterministic, starting with explicit `VDATE`.
3. Demonstrate byte-identical output from two isolated clean builds with a
   locked environment and toolchain.
4. Emit enough inventory to detect source-set, compiler, linker, ELF,
   dependency-closure, and generated-input drift before it reaches hardware.
5. Preserve the Main fork as a GPLv3 comparison and rollback source without
   making it a host protocol dependency or a vendored FogCast component.
6. Provide concrete handoff inputs for the subsequent narrow Overlord Linux
   slice, without making Overlord mandatory for POC6 or claiming it works.

## Non-goals

- Extracting `libmister-runtime`, defining its C ABI, or changing target
  lifecycle/resource ownership.
- Creating a submodule, vendoring Main, or moving Main source into FogCast.
- Publishing the fork, pushing a branch, or changing any public remote.
- Replacing Buildroot, the kernel, the POC6 media path, FFmpeg, SSH development
  access, or the Go agent.
- Resolving a target, connecting to hardware, deploying an image, rebooting,
  wiping, or changing target credentials.
- Claiming that the historical POC6 hashes describe this build or that two
  binaries prove physical equivalence.
- Accepting a merely normalized difference as reproducibility. Stage A0's two
  newly-built output artifacts must be byte-identical.

## Bootstrap and local fork

### Bootstrap input

`build/stage-a0-bootstrap.toml` is the only tracked input needed to create the
initial local fork. It is UTF-8, LF-only TOML and contains no local fork commit,
machine path, target identity, credential, or self-hash. Its concrete types are:

```toml
format = 1
schema = "fogcast.stage-a0-bootstrap"
fogcast_base_revision = "..."       # full 40-character pre-generation commit

[main_upstream]
repository_id = "mister-devel-main-mister"
fetch_url = "https://..."            # normalized official HTTPS URL
commit = "..."                       # full 40-character object
tree = "..."                         # full 40-character object

[branch]
name = "fogcast/stage-a-baseline"
parent_commit = "..."                # equal to main_upstream.commit

[vdate]
source_path = "..."                  # relative regular file in locked tree
source_evidence_sha256 = "..."       # SHA-256 of exact source bytes
official_expression = "%y%m%d"
format = "YYMMDD"
timezone = "UTC"
ascii_digits = 6

[patch]
recipe_version = "..."                # exact deterministic patch recipe version

[initial_commit]
author_name = "..."                   # exact project author identity
author_email = "..."                  # exact project author identity
committer_name = "..."                # exact project committer identity
committer_email = "..."               # exact project committer identity
author_timestamp = 0                   # non-negative Unix epoch
committer_timestamp = 0                # non-negative Unix epoch
commit_message = "..."                # exact UTF-8 LF message bytes
signing = false                        # exactly false
```

Ellipses show types only; implementation resolves and reviews actual values.
Bootstrap validation rejects abbreviated commits, a non-HTTPS official locator,
branch/tag identity, a parent different from the upstream commit, invalid or
missing patch/initial-commit metadata, signing other than `false`, or evidence
that does not prove the selected base's `%y%m%d` expression. The exact source
evidence comes from the locked tree, not a current upstream branch tip. The
initializer constructs both author and committer Git dates exactly as
`<epoch> +0000`, using their respective locked timestamps, and rejects any
other timezone or date spelling.

### Initializer contract

Only the initializer mutates the local fork, and it is explicit:

```text
scripts/stage-a0-init-main.sh \
  --bootstrap build/stage-a0-bootstrap.toml \
  --destination /Users/clawzai/Developer/Main_MiSTer
```

It accepts no credential argument and invokes no `git push`. For an absent
destination it verifies the locked official commit/tree, creates a local clone,
removes `origin`, and configures exactly one remote named `upstream`. Its fetch
URL must be the normalized official HTTPS bootstrap URL. It sets
`remote.upstream.pushurl` to the exact unsupported sentinel
`disabled://stage-a0/upstream` and verifies the effective push URL is that
sentinel. Git has no read-only-remote bit: this sentinel is a fail-closed guard,
not permission control, and no Stage A0 tool invokes push.

It then creates `fogcast/stage-a-baseline` from the locked base and makes the
one deterministic-`VDATE` patch as a separate local commit with that base as
its direct parent. The patch is functionally neutral except for deterministic,
visible build-date metadata; it changes no non-metadata runtime behavior. It
rejects extra remotes or any mismatch in effective fetch or push URL policy.

An existing destination is accepted only when it is the expected Git repository
with exactly the locked upstream remote policy, branch, base/tree, direct patch
parent, patch ancestry, and a clean tracked worktree. Otherwise it stops. It
never resets, rebases, overwrites, removes, or repairs an existing destination.
An exact rerun makes no change.

Bootstrap also locks a deterministic first-patch recipe/version and Git commit
metadata: project author and committer name/email, author and committer
timestamp, exact commit message, and signing disabled. The initializer uses an
isolated Git configuration and `commit-tree` with those locked values and exact
`<epoch> +0000` Git dates. Thus the same parent, patch tree, and bootstrap
create the same fork commit. Fixture tests prove this property. A promoted final
lock remains valid only for its locked object; deletion/recreation outside this
recipe/cache path fails closed.

The fork is responsible for:

- preserving upstream copyright, license notices, corresponding-source
  obligations, and the original build semantics except for the explicit
  deterministic metadata input;
- retaining an upstream-style `Main_MiSTer` executable as the comparison path;
- maintaining a small, ordered patch series whose first commit is the `VDATE`
  change; and
- recording each future upstream integration as a reviewed base-change and
  rerunning the applicable Stage A checks.

The fork does not own FogCast's locks, build reports, source authority for
Overlord or the catalog, deployment policy, public protocol, or target-private
configuration.

### FogCast responsibilities

FogCast is the sole owner of these Stage A0 deliverables and interfaces:

| Deliverable | Responsibility | Consumer |
| --- | --- | --- |
| `build/stage-a0-bootstrap.toml` | Machine-independent initializer input | initializer and review |
| `scripts/stage-a0-init-main.sh` | Idempotently create/verify local fork and deterministic patch | local fork creation |
| `build/stage-a0-main.lock.toml` | Candidate then reviewed immutable build input | all build tools |
| `scripts/stage-a0-fetch-verify.sh` | Fetch/cache admissible sources and verify each pinned object/hash | clean-build setup |
| `scripts/stage-a0-build-main.sh` | Build exactly one isolated output tree from the lock | two-build gate |
| `scripts/stage-a0-compare.sh` | Compare artifacts and inventories; produce a machine-readable result | review and future Overlord work |
| `build/stage-a0-fixtures/` | Safe synthetic lock, source, compiler-log, and ELF/dependency fixtures | automated tests |
| `build/stage-a0-policy/` | Separately tracked source/compile/ELF/generated/intermediate/fork-delta baselines | final-lock promotion and comparison |
| `docs/stage-a0/main-lock-reviews/<lock-sha256>.md` | Immutable candidate/promotion review record | independent review and evidence |
| `artifacts/stage-a0/` (ignored) | Per-run build reports, manifests, and comparison output | reviewer evidence |
| `docs/stage-a0/` | Current procedure and report format, if implementation needs durable operator documentation | human review |

The exact implementation may place reusable Go code under a scoped package,
but it must preserve the command-line contracts below. `artifacts/stage-a0/`
is generated evidence, excluded from source control unless a later approved
evidence-retention decision selects a non-secret subset for tracking.

FogCast source controls only adapters, locks, and verifiers. It must never
vendor or synthesize a substitute Main source tree. Each build requires an
ephemeral verified materialization from locked objects; it may not patch the
canonical fork worktree in place. The fork patch is made and reviewed in the
fork repository itself.

### Command interfaces

The fetch, build, and compare commands accept `--lock <path>` and fail if it is
absent,
malformed, outside the repository when a FogCast-local path is required, or
does not pass schema/version validation. Commands write their output only under
an explicitly supplied empty output directory; they refuse a pre-existing
non-empty directory.

```text
scripts/stage-a0-fetch-verify.sh --lock build/stage-a0-main.lock.toml \
  --main-repository <local-fork-path> --source-cache <approved-cache-root> \
  --report <empty-report-dir>

scripts/stage-a0-build-main.sh --lock build/stage-a0-main.lock.toml \
  --main-repository <canonical-fork-identity-input> \
  --source-cache <verified-cache-root> --sandbox <empty-build-sandbox> \
  --report <empty-report-dir>

scripts/stage-a0-compare.sh --lock build/stage-a0-main.lock.toml \
  --left <first-report-dir> --right <second-report-dir> \
  --report <empty-comparison-dir>
```

`fetch-verify` does not clone a source branch by name as a proof of identity.
`--main-repository` is required for a `git-local` fork material and forbidden
when no `git-local` material is selected.
`build-main` accepts the canonical fork only as an identity input; it makes a
fresh detached materialization inside each sandbox and never builds the
canonical worktree in place. `compare` treats a missing manifest, a different
schema, or an unrecognized artifact as a failure, not an empty value.

No command accepts credentials on the command line. Private source locations or
authentication mechanisms, if later needed, are resolved by the operator's
private configuration; neither their values nor target identities appear in a
lock, generated manifest, fixture, or report.

## Deterministic `VDATE` patch contract

The first fork commit accepts and validates explicit `VDATE`, deriving it from
the final lock's `source_date_epoch` in UTC. It must be exactly six ASCII
digits and equal the epoch rendered as `YYMMDD`; there is no wall-clock,
local-timezone, locale, or environment fallback. The build manifest records
the epoch and rendered value. The patch may change only the verified build-file
lines needed for this input; it may not add a compiler/linker flag, source file,
library, generated input, runtime configuration, network access, or behavior
path.

The initial patch review is source- and build-inventory-based, not a claim that
the historical upstream binary will equal a newly rebuilt binary. It must show:

- the full tracked source file set before and after is unchanged except for the
  build-file lines needed to accept/validate `VDATE`;
- compiler executable identity, language standards, optimization/debug flags,
  include directories, preprocessor definitions, linker executable/flags, and
  library order are unchanged relative to the locked intended baseline; and
- the resulting runtime dynamic-library closure and ELF dependency metadata
  contain no unreviewed addition, removal, or path substitution.

Any date-representation change or other alteration requires a separate approved
fork decision. The later library extraction begins only after its applicable
Stage A gates are satisfied.

### Official checkout-attribute compatibility boundary

The selected official Main tree applies `text=set` and `eol=lf` to both the
root `Makefile` and Markdown files. The Makefile blob is already LF-only, but
the locked `lib/miniz/ChangeLog.md` blob contains 176 CRLF endings. Git 2.55
materializes that blob unchanged, but the normal attribute-driven clean/index
comparison normalizes it and therefore reports the otherwise unmodified
worktree dirty. Normalizing the
blob in the fork would add a second source change and violate the approved
VDATE-only patch contract.

Stage A0 separates two concerns. Persistent worktree operations use raw Git
blob semantics by setting the closed local configuration
`attr.tree=4b825dc642cb6eb9a060e54bf8d69288fbee4904`, the canonical empty SHA-1 tree,
in addition to `core.autocrlf=false`, `core.eol=lf`, and
`core.attributesfile=/dev/null`. In a new repository, the initializer writes an
empty tree object deterministically and requires its OID to equal the canonical
value. It then verifies SHA-1 object format, object existence and tree type,
zero entries, empty effective persistent attributes after checkout, and
absence of `.git/info/attributes`. Checkout, status, and idempotence commands
never receive `GIT_ATTR_SOURCE`.

The locked patch tree's attributes are still security-relevant evidence. A
single command-scoped audit uses `GIT_ATTR_SOURCE=<full-patch-tree>` with
`check-attr --all -- <source_path>` and admits only an empty result or the exact
ordered LF-terminated `text=set`, `eol=lf` pair for the VDATE source. The pair
is admitted only when the patched source is valid UTF-8, contains no CR, and
ends in LF. This command-scoped source must not leak into materialization or
status. The functional audit is also the compatibility gate for Git's
`attr.tree`/`GIT_ATTR_SOURCE` support; an implementation that ignores either
fails closed.

After `checkout-index` materializes raw blobs, the initializer still requires
the materialized VDATE source SHA-256 to equal the committed patched blob
SHA-256, requires ordinary attribute resolution for that attributed source to
remain empty, and requires a clean tracked worktree. An existing destination
is verification-only and must already satisfy the exact `attr.tree` config,
object-format, empty-tree object/type/content, absent-info-attributes,
persistent-empty-attributes, and scoped locked-tree-audit gates. Regression fixtures include a
tracked CRLF file covered by `text eol=lf`, raw blob/worktree identity, clean
status after restat and a second checkout, exact locked-tree attribute recovery,
and rejection of wrong/nonempty attribute trees or repository-local attribute
overrides.

## Final build lock and canonical encodings

The candidate `build/stage-a0-main.lock.toml` is tracked UTF-8 LF TOML. It has
no self-hash, physical local path, hostname, target identity, credential, or
mutable identity. Its external review record hashes its raw tracked bytes and
records promotion. Its concrete tables and required scalar types are:

```toml
format = 1
schema = "fogcast.stage-a0-main-lock"
fogcast_base_revision = "..."       # full pre-generation FogCast commit
source_date_epoch = 0                # non-negative integer

[environment]
locale = "..."
timezone = "UTC"
umask = "..."
job_count = 1                         # positive; same for both builds
network = "disabled-during-build"
path_policy = ["/stage-a0/build-utils/bin", "/stage-a0/toolchain/bin"]
container_material_id = "..."        # references materials

[main]
upstream_material_id = "..."
fork_material_id = "..."
upstream_commit = "..."
upstream_tree = "..."
fork_commit = "..."
fork_tree = "..."
fork_parent_commit = "..."           # equals upstream_commit
patch_commits = ["..."]              # ordered; first is VDATE patch
publication_status = "local-only"    # local-only or durably-retrievable
durable_retrieval_material_id = "..." # required only when durable

[build]
entrypoint = ["...", "..."]
working_directory = "/stage-a0/src"
vdate_format = "YYMMDD"
vdate_expression = "%y%m%d"
allowed_final_artifacts = ["bin/MiSTer", "bin/MiSTer.elf"]

[[materials]]
id = "..."
role = "consumed-build-input"
kind = "git-local"
license_ids = ["..."]
purpose = "..."                      # optional

[materials.git_local]
repository_id = "..."
commit = "..."
tree = "..."

# Closed alternatives, each with one matching nested table only:
# kind = "git-https"; [materials.git_https] repository_id, url, commit, tree
# kind = "archive-https"; [materials.archive_https] url, size, sha256
# kind = "oci"; [materials.oci] reference, manifest_digest, config_digest, os, architecture
# kind = "git-subtree"; [materials.git_subtree] parent_id, path, tree
# kind = "material-file"; [materials.material_file] parent_id, path, size, sha256

[[toolchains]]
id = "..."
target_triple = "..."

[[toolchains.components]]
role = "compiler"
material_id = "..."
logical_path = "/stage-a0/toolchain/bin/..."
executable_sha256 = "..."
version = "..."

[[build_utilities]]
role = "make"
material_id = "..."
logical_path = "/stage-a0/build-utils/bin/..."
executable_sha256 = "..."
version = "..."

[[configs]]
id = "..."
material_id = "..."
path = "..."
sha256 = "..."
purpose = "..."

[[policies]]
id = "..."
material_id = "..."                  # a material-file or approved policy parent
path = "..."
sha256 = "..."
kind = "source-set"

[[licenses]]
id = "..."
material_id = "..."
spdx_expression = "..."
notice_locator = "..."
corresponding_source_locator = "..."
redistribution_status = "..."

```

Ellipses illustrate types only. The parser rejects unknown/missing keys,
duplicate IDs, unsorted arrays, absent references, invalid encodings, and
unresolved values. `[[materials]]` is the one canonical discriminated-union
material inventory. Its common keys are `id`, `role`, `kind`, `license_ids`, and
optional `purpose`; exactly one variant table matching `kind` is required, and
all keys/tables from every other variant are forbidden. `git-local` has no
locator and imports verified objects from `--main-repository`; `git-https` has
the normalized HTTPS locator. `archive-https`, `oci`, `git-subtree`, and
`material-file` require precisely the fields shown in the closed alternatives.
`kind` is exactly one of `git-local`, `git-https`, `archive-https`, `oci`,
`git-subtree`, or `material-file`; `role` is exactly one of
`consumed-build-input`, `context-only`, `future-overlord-input`, or
`historical-comparator`.
Variant requirements are closed: `git-local` requires only `repository_id`,
`commit`, and `tree` and forbids `locator`; `git-https` requires only
`repository_id`, normalized HTTPS `url`, `commit`, and `tree`; `archive-https`
requires only normalized HTTPS `url`, non-negative `size`, and `sha256`; `oci`
requires `reference`, `manifest_digest`, `config_digest`, `os`, and
`architecture`; `git-subtree` requires `parent_id`, relative safe `path`, and
`tree`; and `material-file` requires `parent_id`, relative safe `path`,
non-negative `size`, and `sha256`. Each variant forbids all fields specific to
the other variants. Canonical hashes are required where listed; parent IDs must
reference an earlier material ID and are resolved before child validation.
Materials sort by `id`; toolchain component records and build-utility records
sort by `(role, material_id, logical_path)`. Build utilities cover every invoked
utility, including bash, make, the locked git, sed, cp, mkdir, rm, and the nproc
shim. Toolchain records explicitly cover compiler, linker, binutils, libc, and
sysroot; each record supplies `role`, `material_id`, `logical_path`, executable
SHA-256/version where executable, and no undeclared tool is usable. Build
utilities use the same required record fields and include an `nproc-shim` role.
`[[policies]]` sort by ID and are separately tracked UTF-8 LF policy files
referenced by path plus SHA-256. Candidate policies are generated from the
locked upstream base **and** approved deterministic fork patch/fork tree,
independently reviewed, and promoted with the final lock.
Their `kind` is exactly one of `source-set`, `compile-link`, `elf-dependency`,
`generated-input`, `intermediate-path`, or `upstream-fork-delta`.
The `upstream-fork-delta` policy records the upstream commit/tree, fork
commit/tree, exact isolated patch-diff SHA-256, and the one approved purpose
`deterministic-vdate-input`; it permits only the recorded VDATE build-file
change. The source-set policy includes both locked upstream and fork-tree hashes
and declares that no other fork source-set change is admissible. The compile-link
policy permits only the locked deterministic VDATE value substitution after
logical-root normalization; no other compiler/linker argv, flag, input, output,
or library-order difference is normalized.
They lock expected source set, normalized compile/link commands, ELF/dependency
baseline, allowed generated inputs, allowed intermediate paths, and logical
roots so identical-but-wrong builds fail.

`[[materials]]` is the one canonical material inventory;
its `role` is exactly `consumed-build-input`, `context-only`,
`future-overlord-input`, or `historical-comparator`. The Main fork, bundled
third-party directories, prebuilt libraries, toolchain, sysroot, container, and
every actual Main build input are consumed inputs with a material/license record.
Buildroot/kernel/container context not consumed by this narrow build are
context-only. `DeanoC/overlord` and `DeanoC/ikuy_std_resources` remain future
Overlord inputs until a later slice promotes and consumes them.

Git commits/trees are full 40-character lowercase hexadecimal; SHA-256 values
are 64-character lowercase hexadecimal. URLs are normalized HTTPS locators:
lowercase scheme/host, no userinfo/fragment, default port removed, and one
parser-defined trailing-slash policy. Physical roots map only after containment
checks to `/stage-a0/src`, `/stage-a0/build`, `/stage-a0/build-utils`,
`/stage-a0/toolchain`, or `/stage-a0/sysroot`.

### Final-lock V1 closed-schema clarifications

The V1 pure validator uses the following closed interpretations. They remove
wire ambiguities before any real candidate lock is authored; a future need
outside them requires a reviewed schema revision rather than parser guesswork.

- `environment.locale` is exactly `C`, `umask` is exactly `022`, and
  `path_policy` is exactly the ordered pair shown in the schema. ID-bearing
  arrays sort by ID; material `license_ids` sort lexically; toolchain
  components and build utilities sort by `(role, material_id, logical_path)`.
  `patch_commits` and `entrypoint` retain semantic order.
- A toolchain has exactly one record for each baseline executable role
  `compiler`, `linker`, `assembler`, `archiver`, `objcopy`, `objdump`, `strip`,
  and `readelf`, plus exactly one non-executable root record for each of
  `binutils`, `libc`, and `sysroot`. Source-authority review may add an
  executable role discovered by command tracing when its role is canonical
  lowercase kebab case. Every executable role requires `executable_sha256` and
  `version`; the three non-executable root roles forbid them. Executable paths
  are below `/stage-a0/toolchain/bin/`; roots are contained by the applicable
  `/stage-a0/toolchain` or `/stage-a0/sysroot` logical root.
- Build utilities have unique canonical lowercase-kebab roles, always require
  executable SHA-256/version, and live below `/stage-a0/build-utils/bin/`.
  Exactly one each of `bash`, `make`, `git`, `sed`, `cp`, `mkdir`, `rm`, and
  `nproc-shim` is required; command tracing may add locked executable roles.
- OCI `reference` is
  `<registry>/<component>[/<component>...]@sha256:<64-lowercase-hex>` with no
  tag or port. `registry` is `localhost` or dot-separated lowercase DNS labels;
  a label starts/ends alphanumeric, contains only lowercase alphanumeric or
  hyphen, and is at most 63 bytes. A repository component is lowercase
  alphanumeric followed by zero or more groups of one separator (`.`, `_`, or
  `-`) and lowercase alphanumeric text. The entire reference is ASCII. Its
  digest equals `manifest_digest`; both manifest and config digests use exact
  `sha256:<64-lowercase-hex>` form. V1 `os` and `architecture` are exactly
  `linux` and `amd64`.
- Every V1 policy references a `material-file`. Its path and SHA-256 must equal
  that material-file variant. The unspecified policy-parent alternative is
  deferred to a later schema version.
- License locators are either normalized HTTPS URLs or safe relative paths
  within the referenced material. `spdx_expression` uses this exact
  dependency-free grammar, with `AND` binding more tightly than `OR` and
  `WITH` binding to the immediately preceding primary:

  ```text
  expression   = or-expression
  or-expression = and-expression *( " OR " and-expression )
  and-expression = with-expression *( " AND " with-expression )
  with-expression = license-id [ " WITH " spdx-id ] | "(" expression ")"
  license-id   = spdx-id | "LicenseRef-" ref-id |
                 "DocumentRef-" ref-id ":LicenseRef-" ref-id
  spdx-id      = ALNUM *( ALNUM | "." | "-" )
  ref-id       = ALNUM *( ALNUM | "." | "-" )
  ALNUM        = ASCII letter | ASCII digit
  ```

  This validates canonical syntax only. It does not validate official SPDX
  catalog membership, license accuracy/compatibility, or legal status.
  Redistribution status is exactly one of
  `redistributable`, `redistributable-with-corresponding-source`,
  `local-use-only`, or `review-required`. This field records review disposition;
  the parser does not make a legal conclusion.
- Raw lock bytes are valid UTF-8, contain no BOM, CR, NUL, DEL, or control byte
  other than LF/TAB, and end in LF. TOML comments and insignificant spacing are
  allowed because the external promotion record hashes raw bytes and V1 has no
  writer. A V1 lock is at most 8 MiB. Every decoded string is independently
  valid UTF-8 and contains no BOM rune, control rune, or DEL, including values
  created through TOML escapes. Required strings are nonempty; optional
  `purpose`, when present, is nonempty. An SPDX expression is at most 4096 bytes
  and its parenthesis/recursive nesting depth is at most 64; exceeding either
  bound is a license-record failure rather than a parser crash.
- Strict fields and type-specific locator grammars are the pure validator's
  secret boundary. V1 defines no heuristic secret/hostname scanner; leak
  scanning and human review remain promotion gates.
- `build.entrypoint` is nonempty. Its first element is a canonical absolute
  executable path below `/stage-a0/build-utils/bin/`. Other arguments are
  nonempty valid UTF-8 without control bytes; an absolute argument is allowed
  only when it is a canonical path within one of the five logical roots.
- A V1 safe relative path is nonempty ASCII, has no leading/trailing/repeated
  slash, and consists of slash-separated components containing only ASCII
  letters, digits, `.`, `_`, `-`, or `~`; a component is nonempty and is not
  exactly `.` or `..`. Backslash, control bytes, DEL, percent escapes, and every
  non-ASCII byte are forbidden. A canonical absolute logical path is exactly
  one of the five declared roots or that root plus `/` and a safe relative
  suffix; prefix lookalikes do not match. Executable toolchain and utility
  paths require a nonempty suffix below their respective `bin` root. The
  non-executable `binutils` root is exactly `/stage-a0/toolchain`; `libc` and
  `sysroot` roots are exactly `/stage-a0/sysroot`.
- V1 normalized HTTPS URLs use lowercase `https` and a lowercase DNS host with
  no port, userinfo, query, fragment, percent encoding, backslash, empty path
  component, `.`/`..` component, duplicate slash, or trailing slash. The path
  begins with `/`; each component contains only ASCII letters, digits, `.`,
  `_`, `-`, or `~`. URL parsing must round-trip to the exact input.

Cross-reference rules are also closed. `fork_parent_commit` equals
`upstream_commit`; `patch_commits` is nonempty and unique, and its last entry
equals `fork_commit`. The upstream material is consumed `git-https` and matches
the recorded upstream commit/tree. The fork material is consumed `git-local`
or `git-https` and matches the fork commit/tree. `local-only` forbids a durable
retrieval ID. `durably-retrievable` requires a consumed `git-https` material
with the same fork repository identity, commit, and tree; it may equal the fork
material. The environment container is a consumed OCI material. Every
component, utility, config, policy, and license material reference resolves.
Every material has nonempty sorted unique license IDs, and every referenced
license points back to that material. V1 requires nonempty materials,
toolchains/components, build utilities, configs, policies, and licenses.
Components, build utilities, configs, and policies reference only
`consumed-build-input` materials. There is exactly one policy for each of the
six V1 policy kinds; duplicate or missing kinds fail.

The deterministic fork-patch SHA-256 is calculated from stdout bytes of the
isolated command `env -i GIT_CONFIG_NOSYSTEM=1 HOME=<empty> LC_ALL=C TZ=UTC
/stage-a0/build-utils/bin/git -c core.pager=cat -c color.ui=false diff
--no-ext-diff --no-renames --binary --full-index <locked-parent>
<locked-commit>`. That `git` executable is a locked build-utility material; its
exact version and SHA-256 are recorded. The command has no color, pager, locale
header, reflog, or working-tree diff input. Its output hash is recorded in the
external review record. Generated JSON comes from schema
structs, never maps: UTF-8 LF, fixed field order, no insignificant whitespace,
logical paths, and arrays sorted by stated key. Detached `report.sha256` lists
deterministic manifest/artifact hashes in sorted path order, excludes itself and
run metadata, and is never self-referential.


## Fetch, verification, and build flow

### 1. Resolve and verify materials

The operator supplies the final lock, a permitted source-cache root, an empty
report directory, and `--main-repository` only when the selected fork material
is `git-local`. `stage-a0-fetch-verify.sh`:

1. validates the lock schema, all field formats, stable ordering, and that no
   secret-patterned or prohibited target fields occur;
2. for `git-local`, verifies the supplied repository's exact remote policy,
   locked commit/tree, parent, and patch series, then imports exactly those Git
   objects into a content-addressed bare cache; for `git-https`, retrieves only
   the recorded immutable commit/tree through its normalized HTTPS locator;
   for other kinds, verifies their closed-variant hashes/digests; all writes are
   restricted to the supplied source cache;
3. verifies Git object existence/ancestry, archive SHA-256, signed material
   metadata where the selected source authority requires it, license/source
   records, configuration hashes, and container/toolchain digests;
4. writes a normalized material manifest, including resolved full object IDs,
   content hashes, verification command versions, and publication status; and
5. exits non-zero without producing a build-ready report if any required
   material is absent, mismatched, ambiguous, mutable, or unverifiable.

No physical repository path enters the shared manifest or final lock. The
source cache is content-addressed by verified object/hash. It is an input cache,
not evidence or a trusted receipt. Before **every** build,
`stage-a0-build-main.sh` completely revalidates every consumed object, tree,
archive, config, toolchain, container, bundled directory, and prebuilt library
against the final lock before materialization. A cache hit is fully rechecked;
a miss may be fetched only by this verifier through an allowed read-only source
locator. The build itself always runs with networking disabled.

### 2. Create two isolated clean build roots

Each `stage-a0-build-main.sh` invocation constructs a fresh detached Main
materialization from the locked commit inside its caller-provided empty writable
sandbox outside the FogCast repository. The canonical fork worktree is an
identity input only and is never built in place. The two invocations must use:

- distinct source, output, temporary, home, compiler-cache, package-cache,
  toolchain, and sysroot paths;
- no writable shared state, and no source tree modification;
- the same verified source cache entries, lock, hermetic runner/container,
  toolchain, `SOURCE_DATE_EPOCH`, UTC timezone, locale, umask, path policy, and
  locked job count;
- networking disabled during build and post-build inventory collection; and
- only the locked Main fork commit and verified material/configuration inputs.

The command creates no-follow roots and validates every input/output/cache/lock
path through `realpath` and `lstat` containment. It rejects symlink, hard-link,
special-file, archive absolute-path, and `..` traversal escapes. It mounts
verified inputs read-only where available. Before build, the detached source
tree must match the locked Git tree and have no untracked or ignored input; the
wildcard-evaluated source set must exactly match the declared source manifest.
After build, only declared generated inputs, intermediates, and outputs may
exist in their allowed locations.

The adapter renders six-digit UTC `VDATE` from the locked epoch, sets
`SOURCE_DATE_EPOCH` to that epoch, clears unlisted build-influencing variables,
uses the locked job count, and invokes exactly the locked entry point. It
rejects compiler invocation/input outside policy, ambient `nproc` discovery,
network access, inherited writable cache, or writable shared state.

The locked Main adapter mounts the detached source read-only at
`/stage-a0/src`, places the locked `/stage-a0/build-utils/bin` first in `PATH`,
and invokes the locked Make executable with command-line
`SHELL=/stage-a0/build-utils/bin/bash -o pipefail
BUILDDIR=/stage-a0/build/bin`. The command-line `SHELL` value overrides the
official `SHELL = /bin/bash -o pipefail` assignment, so Make resolves the shell
executable only through the locked build-utility path; the adapter records the
effective shell path, shell executable hash/version, and `pipefail` binding.
`BUILDDIR` prevents output from entering the source tree. Its locked nproc shim
prints exactly `environment.job_count`; the adapter verifies effective Makefile
`MAKEFLAGS`/job count and records both requested and observed values. There is
no second Main patch for shell, build-directory, or parallelism behavior.
Fixtures reproduce exact official `SHELL = /bin/bash -o pipefail` and
`MAKEFLAGS += "-j $(shell nproc)"` rules plus external `BUILDDIR`, then prove
the command-line bindings are effective, source remains unchanged, and output
is confined to `/stage-a0/build`.

### 3. Record build manifests

Each build emits a schema-versioned, normalized report containing the following
separate manifests. Paths are expressed relative to the output root unless a
lock field defines a canonical source locator.

| Manifest | Required contents |
| --- | --- |
| `materials.json` | Material ID, role, commit/tree or content hash, license IDs, raw lock hash, and revalidation result, sorted by ID. |
| `environment.json` | Sanitized allowlist, locale, timezone, umask, locked job count, epoch, rendered `VDATE`, container identity, and network-disabled result. |
| `source-set.json` | Sorted logical source path, mode, SHA-256, material ID, and inclusion reason; untracked input is invalid. |
| `compile.json` | Records sorted by semantic `(output, source, tool)`; normalized argv/cwd/paths, tool identity, flags, and exact linker library order. |
| `artifacts.json` | Sorted logical original path, immutable report-payload path, type, mode, size, SHA-256, role, and final/intermediate classification. Exactly `bin/MiSTer` (stripped) and `bin/MiSTer.elf` (unstripped) are final. |
| `elf.json` | For each ELF output: class, endianness, machine, ABI, program headers, sections, build ID, interpreter, `DT_NEEDED`, RPATH/RUNPATH, symbols where policy requires, and normalized `readelf`/`objdump` hashes. |
| `dependencies.json` | Dynamic-library closure, symlink resolution, ABI/SONAME, SHA-256, size, source package/material, and search-path provenance. |
| `image-inputs.json` | Consumed/context/future material IDs and config hashes; explicitly records that no Overlord output is consumed in Stage A0. |
| `result.json` | Deterministic verdict, source availability, canonical evidence status, and detached inventory reference; it never hashes itself. |

`compile.json` preserves argv and linker-library order within a record while
normalizing scheduler arrival order. Run-only `run.json` is excluded from
comparison and `report.sha256`; it contains only start/end wall-clock time,
elapsed duration, PID/process tree, physical sandbox/cache paths, hostname/
kernel, cache-hit diagnostics, log locations, scheduler trace, and diagnostic
arrival ordering. The deterministic manifests are evidence about a defined
build, not a physical claim, and contain no target IDs, credentials, user
content, ROM data, or writable target paths.

`run.json` never enters shared or sanitized evidence by default. It may be
included only after an explicit redaction process removes hostname, physical
paths, PIDs, process-tree values, and log locations; that redaction produces a
new separately identified artifact and never replaces the deterministic package.

Each report atomically creates immutable payload copies at
`artifacts/bin/MiSTer` and `artifacts/bin/MiSTer.elf`, marks them read-only
after write, and lists them in detached `report.sha256`. These payload paths are
report-owned evidence copies; `bin/MiSTer` and `bin/MiSTer.elf` remain the
logical original build-output paths in manifests. The comparison command reads
and byte-compares the immutable report-owned payload copies, not a mutable
sandbox output tree.

### 4. Compare the clean builds

`stage-a0-compare.sh` validates both report schemas and lock hash, then compares
them in this order:

1. material, environment, source-set, and feature-config identity;
2. full compiler/linker command inventory after the lock-defined path
   normalization; and
3. mandatory `bin/MiSTer` (stripped) byte-for-byte and mandatory
   `bin/MiSTer.elf` (unstripped) byte-for-byte, then all ELF/dependency,
   image-input, and detached report-inventory values.

The two-build comparison requires both mandatory final artifacts with no opt-out
flag. Missing, unexpected, wrongly classified, or differing final artifacts
fail. Intermediate files are separately classified and allowed only by their
declared allowlist. An ELF, compiler, source, dependency, or generated-input
mismatch fails even when an executable hash matches. A historical known-good
artifact is a separate comparison and never weakens the two-new-build rule.

The comparison emits `two_builds_byte_identical = true|false` and
`source_availability = local-only|durably-retrievable` independently of status.
For `local-only`, the maximum canonical status is **Software-tested**, even
when both final artifacts match. For `durably-retrievable`, **Reproducible** is
permitted only after durable retrieval and the applicable roadmap gate pass.
Stage A0 never emits **HIL-observed** or **Accepted**.

## Fail-closed behavior

The tools return a stable machine-readable error code and a concise safe
message. They do not continue after these conditions:

| Code | Failure condition |
| --- | --- |
| `BOOTSTRAP_SCHEMA_INVALID` | Bootstrap has an unknown/missing field, invalid type/encoding, or invalid selected-base VDATE evidence. |
| `LOCK_SCHEMA_INVALID` | Unknown format/schema, missing required field, bad encoding, duplicate, unsorted material, or prohibited field. |
| `LOCK_MUTABLE_IDENTITY` | A branch, tag, abbreviated commit, mutable image tag, or unpinned locator is used as identity. |
| `MATERIAL_UNAVAILABLE` | A required verified source/object/archive/toolchain/container is unavailable. |
| `MATERIAL_HASH_MISMATCH` | Resolved bytes/object/hash/ancestry differ from lock. |
| `REPOSITORY_POLICY_MISMATCH` | A remote, effective fetch/push URL, branch/base/tree, patch ancestry, or clean canonical worktree differs from policy. |
| `BUILD_ENVIRONMENT_DRIFT` | Ambient environment, time, locale, tool path, network policy, cache, writable shared state, or forbidden host path affects the build. |
| `VDATE_INPUT_INVALID` | `VDATE` is missing, not six ASCII `YYMMDD` digits derived in UTC from the locked epoch, or differs from it. |
| `SANDBOX_PATH_UNSAFE` | A root, symlink, hard link, special file, archive member, or traversal escapes containment policy. |
| `SOURCE_SET_DRIFT` | A source/generated input is added, removed, untracked, or changes outside declared material/configuration changes. |
| `COMPILE_INVENTORY_DRIFT` | Compiler/linker executable, mode, flags, includes, definitions, library order, or invocation differs from the locked policy. |
| `DEPENDENCY_CLOSURE_DRIFT` | ELF interpreter, dynamic-library closure, SONAME, search path, or dependency hash differs unexpectedly. |
| `FINAL_ARTIFACT_INVALID` | A mandatory final artifact is missing, unexpected, wrongly classified, or differs byte-for-byte. |
| `REPORT_INCOMPLETE` | A required manifest/detached inventory is missing, malformed, schema-incompatible, unrecognized, or self-referential. |
| `LICENSE_RECORD_INCOMPLETE` | A material lacks its required license/corresponding-source/configuration record. |

## Verification and fixtures

Implementation must add deterministic, host-only tests before it attempts any
real source fetch or compiler invocation. Fixtures use tiny synthetic source
trees, archives, object inventories, compiler logs, ELF/dependency metadata,
and lock fragments; they must not include Main source, proprietary toolchains,
ROMs, target configurations, credentials, or machine identities.

The minimum automated matrix covers:

1. valid bootstrap/final-lock fixtures parse, normalize, and cross-reference;
2. every scalar, array order, role, URL, object, SHA-256, license, and
   no-self-reference rule fails closed when violated;
3. `local-only` remains **Software-tested** despite matching artifacts, while a
   durably retrievable source requires its own verified gate for **Reproducible**;
4. extra remotes, altered effective push URL, bad branch/base/tree/parent,
   dirty worktree, unexpected ancestry, non-idempotent destination, missing
   author/committer identity, non-UTC Git date spelling, or signing enabled fail;
5. absent/invalid `VDATE`, non-UTC rendering, wrong digit count, and
   epoch/render mismatch fail;
6. source-set, compiler/linker, ELF, dependency, material, image-input, or
   mandatory-final-artifact drift produces the matching failure code;
7. matching synthetic `bin/MiSTer` and `bin/MiSTer.elf` reports pass;
8. symlink/hard-link/special-file roots, lock/cache/output symlinks, and archive
   absolute or `..` paths, link escapes, or device entries fail;
9. inherited network/compiler cache/ambient variable, unapproved physical path,
   writable shared state, or different job count fails; and
10. JSON/report serialization is canonical, run metadata is excluded from the
    deterministic inventory, and no report hashes itself;
11. `git-local` import checks repository policy/object/tree/patch then inserts
    exact objects in bare cache without a physical path in shared manifests;
    `git-https` and every other closed material variant rejects forbidden or
    missing keys;
12. toolchain/build-utility records cover every invoked executable, including
    the locked git, bash, and nproc shim; the synthetic Main makefile verifies
    exact `SHELL = /bin/bash -o pipefail`, exact
    `MAKEFLAGS += "-j $(shell nproc)"`, and external `BUILDDIR`; and
13. report-owned payload copies are atomic/read-only, comparison reads only
    those copies, and candidate policy references make identical-but-wrong
    source/compile/ELF/generated/intermediate results fail;
14. a corrupted cache-hit material is completely revalidated and rejected before
    sandbox materialization;
15. URL userinfo, secret-bearing/prohibited target fields, and other forbidden
    shared-lock/report values are rejected; and
16. an unredacted `run.json` is rejected from shared or sanitized evidence, while
    a separately identified fully redacted derivative is permitted.

After the fixture suite, the narrow real-build gate runs twice in separate,
fresh sandboxes. Each report records commands run and machine-observed hashes
separately. A reviewer checks the deterministic-`VDATE` fork diff, bootstrap,
final lock, manifests, comparison report, license records, external lock-hash
review record, and fixture evidence. The result status is only
**Software-tested** or **Reproducible** as the source-availability rules allow;
a later HIL verifier independently records physical observations.

## Evidence, security, and licensing boundaries

Stage A0 report labels use the roadmap vocabulary exactly. The initializer and
fixture suite may be **Software-tested**. With `source_availability =
local-only`, matching clean builds remain **Software-tested**. A comparison may
be **Reproducible** only after durable retrieval and the applicable independent
roadmap gate. No action in this design supplies **HIL-observed** or
**Accepted** evidence.

Reports distinguish:

- commands run and exit results;
- machine-observed hashes, object IDs, byte comparisons, and manifests;
- operator observations, if any, which are out of scope for Stage A0; and
- inferences, including why a result does or does not support a reproducibility
  claim.

GPLv3 is accepted for the fork, but acceptance of GPLv3 does not remove the
need to preserve notices, make corresponding source available when distributing
the artifact, record exact configuration/feature selection, and evaluate all
other material licenses. The first Main patch stays small precisely so its
source obligations and upstream comparison remain intelligible. FogCast does
not embed Main source in a separate proprietary artifact or conceal a modified
binary's corresponding source.

No Stage A0 tool sends credentials in command arguments or records them in a
lock, report, source cache, build output, fixture, or source control. It does
not use SSH or a target as a runtime dependency. ADR 0002's disposable-kit
authorization does not apply because Stage A0 performs no target operation;
when a later task does, it must resolve and verify the designated kit only
through operator-controlled private configuration.

## Handoff to the Overlord vertical slice

The next focused design/plan may use the Stage A0 lock and manifests as its
input contract. It must not assume that a pinned Overlord or
`ikuy_std_resources` revision means their generated output already matches
MiSTer. Its narrow Linux slice begins by adding only the resource, board,
register-map, toolchain, and software-dependency descriptions needed to
reproduce the recorded Main build inputs.

The handoff package consists of the reviewed Stage A0 lock; normalized
materials, source-set, compile, ELF, dependency, and image-input manifests;
the deterministic Main patch commit/diff hash; comparison result; license
records; and explicit evidence classification. Overlord outputs are compared
against those locked inputs. Unexpected resource additions/removals, register
address changes, memory-topology changes, privilege changes, compiler/linker
changes, or dependency-closure changes fail the slice gate until reviewed.

Overlord remains optional for the retained POC6 testbed until that narrow Linux
slice passes its own software, reproducibility, and physical gates. This design
does not make the local Main fork, an Overlord generator, or any resulting
image a deployment target.

## Lock promotion record

`docs/stage-a0/main-lock-reviews/<lock-sha256>.md` is the tracked immutable
promotion record for one candidate final lock, where `<lock-sha256>` is the
candidate lock's lowercase raw-byte SHA-256. It has a front-matter schema
identifier and records at least: bootstrap raw SHA-256; final-lock raw SHA-256;
`fogcast_base_revision`; fork commit and tree; reviewer roles and actual
models/fallbacks; every Critical/Important finding and disposition; source
availability; commands and results; evidence status; candidate/promoted
decision; and decision date. It does not hash itself. The commit/review system
that contains it is external to both the lock and the record and identifies
their tracked raw bytes.

No candidate becomes a promoted build input without its hash-named immutable
record and independent review. A later lock update creates a new record and may
not overwrite or edit an earlier record into a different input set.

## Implementation handoff and review record

The implementation plan must assign disjoint writable ownership for the
FogCast lock/tool/test files and the local Main fork. The fork and FogCast
repositories are reviewed independently; a reviewer must inspect their exact
commits/diffs plus the untracked-file SHA-256 package if changes are not yet
committed. No agent stages, commits, pushes, publishes, or mutates hardware
without the user's explicit authorization.

Each handoff records the base commit; FogCast branch/worktree; fork repository
ID and commit/tree (with any physical path only in ignored sanitized local
provenance); governing documents; exact files/artifacts changed; commands and
results; evidence classification; reviewer role/model/fallback; unresolved
risks; and next safe action. The first safe implementation action is to create
the local fork from bootstrap and capture verified values into a candidate lock
without claiming it is accepted until fixture and two-build review complete.

## References

- [MiSTer-devel/Main_MiSTer official repository](https://github.com/MiSTer-devel/Main_MiSTer)
- [MiSTer-devel/Main_MiSTer official build file](https://github.com/MiSTer-devel/Main_MiSTer/blob/master/Makefile)
- [FogCast active migration roadmap](../../ROADMAP.md)
- [FogCast portable-target-runtime decision](../../adr/0001-portable-target-runtime.md)

The bootstrap's immutable commit/tree and source-evidence hash establish build
identity. The live official links are primary-source discovery references, not
the identity of a build input.

## Self-review

This design contains no fabricated resolved hashes, commits, tool versions, or
license verdicts. It separates bootstrap from final-lock creation, preserves
the official `%y%m%d` representation as six-digit `YYMMDD`, and keeps final-lock
identity external to the lock. It uses only the roadmap's canonical status
vocabulary, keeps shared provenance machine-independent, requires a fresh
detached sandbox materialization for both builds, uses a closed material union
with locked build utilities/policies, stores compared payloads in immutable
reports, and has no unresolved markers.
