# Stage A0 first-build handoff

## Classification

**Software-tested only.** This handoff records a real network-disabled local
build and its checked receipt. It is not a Reproducible, HIL-observed, or
Accepted Stage A0 result.

## Review scope

- Base commit: `bf7143f7a0cb2e4fdef24c8abdd4fec476dec27e`
- Branch: `codex/stage-a0-main-baseline-design`
- Implementation: `internal/stagea0/firstbuild`,
  `internal/stagea0/precompare`, the two Stage A first-build commands, and the
  Stage A Makefile targets.
- Related test hardening: `internal/stagea0/git_test.go`.
- Durable design and result records: [first-build evidence design](../superpowers/specs/2026-08-09-stage-a0-first-build-evidence-design.md)
  and [Software-tested result](first-build-software-tested-2026-08-09.md).

The ignored local capture directory contains `first-build.json`, `build.log`,
and the two payload binaries. It is run evidence, not a source-controlled
lock or release artifact.

## Observed receipt

| Item | Value |
| --- | --- |
| Main fork | `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d`, tree `efb9c24e8e27945a75d8c497b4b99ec249129075` |
| Toolchain archive | 104607124 bytes, SHA-256 `102825ae56c9e00142d06f35d2bdd3299edb6060e84a275a25b095e66fd3fc2a` |
| Prepared image | `stage-a0-firstbuild:debian12-arm102-v1`, image ID `sha256:24045e0e800b0ce7df88076ccab628387b149f1bd0786fab46fffae07a859d0c`, Linux/amd64 |
| Build | `--network none`, `SOURCE_DATE_EPOCH=1786215171`, `VDATE=260808` |
| `bin/` inventory | 227 regular files, digest `7a2e77ffa919e504a4baa638d6b820085a4464a15281449f1abc2f4d66419cd5` |
| `MiSTer` | 1157996 bytes, SHA-256 `f9e6fd646740449186b74821a3684686d5dbc7b33e28052e9de28b8f4c751f2e` |
| `MiSTer.elf` | 1380136 bytes, SHA-256 `51a9864bb12ebdf8961b30ac0a45d533fd2a2885a96d81fc29f8363d1804706d` |

The final hardened capture and the prepared-image run-3 outputs were
byte-identical for both binaries. Each capture receipt deliberately keeps
`two_builds_byte_identical=false`. The preliminary comparator can compare two
retained captures and emit a separate `comparison.json`; that report remains
Software-tested/local-only and does not promote either capture to the final
reproducibility gate. In the recorded comparison the two receipt hashes are
identical, so it demonstrates receipt/payload equality—not independent-build
provenance. See [preliminary comparison](first-build-precompare-2026-08-09.md).

## Verification

The following checks passed on the authoring worktree:

```text
make stage-a0-check
make test
make vet
go test -race ./...
go vet ./...
git diff --check
```

The first-build command was also run against the locked local Main checkout,
measured Arm archive, and prepared image with a new output directory. It
returned `Software-tested`; the payload modes were `0755`, and the JSON report
was byte-identical across repeated captures.

## Independent review

- Sol architecture/security review (`gpt-5.6-sol`): no Critical findings;
  source commit pinning, archive staging/revalidation, immutable image
  execution, output-boundary checks, and atomic publication were required and
  implemented.
- Vega independent read-only review (`gpt-5.6-sol`, no fallback): PASS, no Critical or Important blockers
  for this explicitly preliminary local-only milestone. The review did not
  authorize promotion to Reproducible or HIL evidence.
- Luna implementation role: completed the normal development slice; the
  runtime did not expose a separate model identifier to this handoff.

## Deferred gates and next action

The final Stage A0 lock still needs durable source/material retrieval, host
tool/runtime provenance, all Main prebuilt-library and license records,
network-off build adapters, complete source/compile/ELF/generated/intermediate
manifests, the final-lock two-build gate, and the narrow Overlord
DE10-Nano/Cyclone V slice. No target or HIL operation was performed.

Next safe action: promote the measured inputs into the reviewed lock/cache
format, then build two fresh isolated Main trees before touching Overlord or
the disposable MiSTer Pi.
