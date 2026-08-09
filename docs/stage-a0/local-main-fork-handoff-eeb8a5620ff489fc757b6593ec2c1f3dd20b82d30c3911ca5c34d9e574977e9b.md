# Stage A0 Local Main Fork Handoff

## Scope and evidence status

This handoff records the host-only creation and verification of the local
GPLv3 `Main_MiSTer` compatibility fork. The evidence status is
**Software-tested** for deterministic repository creation and local source
state only. No Main build, reproducibility comparison, target operation, FPGA
launch, HDMI, audio, input, save, or other HIL claim is made.

The destination is the operator-local sibling
`/Users/clawzai/Developer/Main_MiSTer`. That path is not a shared identity or
lock input. Shared identity uses only the Git objects and hashes below.

## Governing decision and ownership

- Governing design commit before implementation:
  `3dbf5b647c327fc75ee849e55ce109444518be75`
- Replacement C0 commit:
  `4f2346d1ec776ed6a81d396af2e003a4d76da2c1`
- Replacement C0 tree:
  `a5d1f644f78cd7d7740b1484a9c5d25b85d34270`
- Authoring branch: `codex/stage-a0-main-baseline-design`
- Main owner: Luna using `gpt-5.6-terra`, the documented fallback because
  `gpt-5.6-luna` was unavailable
- Architecture review: Sol using `gpt-5.6-sol`, no fallback
- Independent implementation/bootstrap review: Vega using `gpt-5.6-sol`, no
  fallback

The Main owner wrote only the sibling repository through the reviewed
initializer. No later Main commit, push, credential operation, FogCast source
edit, or target mutation occurred.

## Immutable inputs

| Input | Identity |
| --- | --- |
| Bootstrap raw SHA-256 | `eeb8a5620ff489fc757b6593ec2c1f3dd20b82d30c3911ca5c34d9e574977e9b` |
| Detached Stage A0 tool SHA-256 | `66740aaebcd99272628cbf0d041d65c4beb26fa928deb717e87cf75e46729be1` |
| Git | `git version 2.55.0` |
| Go tool used to build initializer | `go version go1.26.5 darwin/arm64` |
| Official repository | `https://github.com/MiSTer-devel/Main_MiSTer.git` |
| Official commit | `7b5c8de5d3fb16f9cccc1f274a2ff1b481637e42` |
| Official tree | `04337bd664f5daa09b347fb2e4e991c66d84c89f` |
| Official Makefile SHA-256 | `58e51fd732b66ed6cc799a664651b6ad63253410a9774b128d46c61dca9ed246` |

The detached C0 worktree was clean, detached at the exact C0, and
`bootstrap-verify` returned exit 0 with exact stdout `valid\n` and empty
stderr. Independent bootstrap review confirmed that only
`fogcast_base_revision` changed from the prior approved bootstrap and every
Main, source, recipe, and deterministic-commit field remained byte-identical.

## Created fork identity

| Property | Verified value |
| --- | --- |
| Branch | `fogcast/stage-a-baseline` |
| Patch commit | `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d` |
| Patch tree | `efb9c24e8e27945a75d8c497b4b99ec249129075` |
| Direct parent | `7b5c8de5d3fb16f9cccc1f274a2ff1b481637e42` |
| Parent tree | `04337bd664f5daa09b347fb2e4e991c66d84c89f` |
| Raw commit payload SHA-256 | `1c600992af520ebf8a7f6f2a54347e8821f993ca9c790d8a270a287dd6beb761` |
| Patched Makefile blob | `24c1d04ab833044266045f7334fc9820991c19a5` |
| Patched Makefile raw SHA-256 | `28683c9d859353bc89880d5fcc6db520181cce7b38ed7b05f4a2bb601f125c42` |
| Private binary parent-to-patch diff SHA-256 | `e61455419b03a8a8abf4ef9321d97b99ee486527479833e8cc7311a9bcbe32d6` |

The raw commit has exact author and committer
`FogCast <fogcast@example.invalid>`, both timestamps
`1786215171 +0000`, no signature, and exact message
`stage-a0: make VDATE reproducible\n`. Its direct-parent diff is exactly
`M\tMakefile\n`. The patched worktree Makefile raw SHA-256 equals the committed
patched blob SHA-256.

## Repository-policy and raw-byte verification

- Worktree status is empty and the only ref is the expected branch at the
  patch commit.
- `fsck --no-dangling` returned exit 0 with empty output.
- The sole remote is `upstream`; fetch is the official HTTPS URL and push is
  `disabled://stage-a0/upstream`.
- Local managed configuration is exact, including `core.autocrlf=false`,
  `core.eol=lf`, `core.attributesfile=/dev/null`, and
  `attr.tree=4b825dc642cb6eb9a060e54bf8d69288fbee4904`.
- `.git/info/attributes` is absent. Ordinary persistent attribute resolution
  is empty. Command-scoped locked-tree inspection returns exactly
  `text=set`, `eol=lf` for Makefile.
- Official and worktree `lib/miniz/ChangeLog.md` use blob
  `3ee292d7996d902ab26ee9507a4499cbc34c96e5` and raw SHA-256
  `f6e5947c7da9eb3720a3f0abc6cf9bf30fc4b501686159b30f570c83fc8f3874`.
  The bytes contain 176 CRLF endings and zero bare LF endings.
- Official and worktree `.gitattributes` use blob
  `0bd9e7376ace40b343b10e175b6ec5afe3cf72ea` and raw SHA-256
  `414d7713e26b7e7f8ae586e0a0c205a28e7b7ae7851828408de016096b4252da`.
- The publication lock is absent after success.
- Every closed review command used `GIT_NO_REPLACE_OBJECTS=1`. The initializer
  used the canonical repository index path `.git/index`; the final path is a
  regular non-symlink file. The exact command transcript proves
  `update-ref refs/heads/fogcast/stage-a-baseline <patch> <zero-oid>` used the
  all-zero old-value creation guard. Live `symbolic-ref -q HEAD` returned
  `refs/heads/fogcast/stage-a-baseline`, and the initializer's final attached-
  HEAD verification succeeded.

The exact second initializer invocation returned exit 0 with no stdout or
stderr. A corrected complete 512-path snapshot, including type, mode,
uid/gid, size, timestamps, inode/link metadata, symlink target, and content
SHA-256, was identical before and after one additional read-only verifier
invocation: both hashes were
`f3a6daa52c7b8618e647956f0222212a654917267ba8cc949523a4fd4bc37691`.
An earlier snapshot attempt accidentally used zsh's special `path` variable,
invalidated its own PATH, and was discarded; it is not evidence.

## Verification commands and results

The reviewed workflow ran the replacement tool's `bootstrap-verify` and
`init-main`, followed by isolated Git plumbing checks for commit/tree/parent,
raw commit bytes, one-path diff, refs, remote fetch/push URLs, exact local
configuration, attribute sources, raw blob/worktree hashes, line-ending counts,
clean status, publication-lock absence, and the second initializer invocation.
All passed. Before C0, the FogCast implementation passed:

```text
make stage-a0-check
make test
make vet
git diff --check
```

The implementation's frozen three-file diff SHA-256 was
`3ddb405d6480a713f1710f4d53524060f14255d26a52b062da8d8420184c164f`.
Sol returned PASS and Vega returned PASS/APPROVED with no Critical or Important
findings before C0 was committed.

At handoff review time, exact `git status --short --untracked-files=all` in the
FogCast authoring worktree was:

```text
?? build/stage-a0-bootstrap.toml
?? docs/stage-a0/local-main-fork-handoff-eeb8a5620ff489fc757b6593ec2c1f3dd20b82d30c3911ca5c34d9e574977e9b.md
```

`git diff --check`, no-index whitespace checks for both untracked deliverables,
and `gitleaks detect --no-git --redact` over `internal/stagea0`,
`cmd/stage-a0`, the initializer and wrapper test, the bootstrap, and this
handoff all passed with no leaks found.

## Remaining risks and next safe action

The fork is local-only and has not been built. Its current maximum evidence is
Software-tested; it is not yet a durable-source Reproducible result. Toolchain,
sysroot, build utilities, third-party/prebuilt materials, output semantics,
dynamic closure, known-good artifact provenance, and the narrow Overlord input
set remain unresolved.

The next safe action is the host-only strict final-lock schema/parser and
semantic validator using synthetic fixtures. Real material resolution, cache
verification, isolated builds, comparison, Overlord generation, and disposable
kit HIL follow as separately reviewed gates. The fork must remain unpushed and
unmodified until a later authorized fork decision.
