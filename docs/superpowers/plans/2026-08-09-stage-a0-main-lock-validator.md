# Stage A0 Final-Lock Validator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development and superpowers:test-driven-development
> task by task.

**Goal:** Implement a pure, closed-schema parser and semantic validator for the
Stage A0 final build lock before resolving or fetching any real build material.

**Architecture:** Separate public immutable value types from private
pointer-based TOML wire types so missing fields are distinguishable from valid
zero values. Parse canonical raw bytes with the existing strict TOML decoder,
convert only after structural validation, then run deterministic semantic
validation over values and references. Reject noncanonical input; never mutate,
sort, normalize, fetch, hash external files, or inspect the filesystem.

**Tech stack:** Go 1.26.5, existing `github.com/pelletier/go-toml/v2`, standard
library, inline synthetic TOML fixtures.

## Scope and ownership

- Root coordinator owns the committed schema decision, integration, commits,
  review, and handoff.
- The writable worktree is
  `/Users/clawzai/Developer/mister-remote/.worktrees/stage-a0-main-baseline-design`;
  implementation begins from the documentation-decision commit created from
  this plan.
- One Luna owner using the documented Terra fallback writes only
  `internal/stagea0/mainlock_types.go`, `mainlock.go`, `mainlock_test.go`, and
  the minimal new failure constants in `internal/stagea0/types.go`.
- Sol and Vega review read-only. No worker writes the local Main fork.
- This slice is host-only and does not use network, cache, container, target,
  credentials, hardware, or the disposable-kit authorization.
- Do not add a CLI, script, candidate real lock, serializer, cache, report,
  policy file, dependency, or generated artifact.
- Evidence is **Software-tested** for the parser/validator only.
- Durable ignored records live under
  `.superpowers/sdd/2026-08-09-stage-a0-main-lock-validator/` as
  `implementation.md`, `sol-review.md`, and `vega-review.md` with raw SHA-256
  recorded in the coordinator handoff.

## Public interfaces

```go
func ParseMainLock(raw []byte) (MainLock, error)
func ValidateMainLock(lock MainLock) error
```

Public types live in `mainlock_types.go`. `Material` exposes one pointer for
each of the six variant value types; exactly one must match `Kind`.
`ToolchainComponent` uses pointer executable fields so the validator can
require or forbid them by the fixed V1 role set. No public type exposes raw
TOML representation or mutable maps.

New failure codes in `types.go` are exactly:

```go
CodeLockSchemaInvalid      Code = "LOCK_SCHEMA_INVALID"
CodeLockMutableIdentity    Code = "LOCK_MUTABLE_IDENTITY"
CodeLicenseRecordIncomplete Code = "LICENSE_RECORD_INCOMPLETE"
```

Failure precedence is exact. Raw encoding, a structurally absent field/table,
unknown/duplicate field, wrong TOML type, union shape, enum outside a
license record, order, path, reference, or fixed-value violation uses
`CodeLockSchemaInvalid`. A non-license locator that parses structurally but contains a
mutable identity feature such as userinfo, query, fragment, tag, non-HTTPS
scheme, or non-digest OCI reference uses `CodeLockMutableIdentity`. A present
license record with an empty/invalid locator, including any mutable URL
feature, backlink, SPDX syntax, or
redistribution disposition uses `CodeLicenseRecordIncomplete`; a missing
license field remains structural and uses `CodeLockSchemaInvalid`. Tests assert
the exact code for every hostile row.

## Task 1: Closed wire shape and canonical bytes

1. Add a minimal valid inline fixture covering all six material variants,
   baseline and optional tool roles, all policy kinds, local-only publication,
   and more than one sortable record.
2. Write RED tests for empty input, invalid UTF-8, BOM, CRLF/CR, NUL/DEL/control,
   missing terminal LF, missing/unknown/duplicate scalar/table fields, wrong
   scalar types, missing arrays, and absent versus explicit zero/empty values.
3. Add the public types, private pointer-based wire types, and raw-byte gate.
   Decode with `DisallowUnknownFields`; require every shown V1 field/table and
   convert only after presence/shape checks.
4. Run RED then GREEN:

   ```sh
   mise exec go@1.26.5 -- go test -count=1 ./internal/stagea0 \
     -run 'TestParseMainLockCanonicalWire|TestParseMainLockRejectsClosedSchema'
   ```

## Task 2: Formats, closed unions, paths, and identities

1. Add table tests for every material role/kind, missing/mismatched/extra union
   tables, negative sizes/epoch, zero jobs, invalid commit/tree/SHA/digest,
   exact normalized HTTPS grammar, digest-only OCI grammar, safe relative paths, logical
   roots, exact environment/build constants, and entrypoint argv.
2. Implement closed enum/format/path/URL/digest helpers without dependencies.
   Reject rather than rewrite case, order, ports, slashes, tags, or paths.
3. Require exact build artifacts and fixed environment values from the design.
4. Run:

   ```sh
   mise exec go@1.26.5 -- go test -count=1 ./internal/stagea0 \
     -run 'TestValidateMainLock(Formats|MaterialUnion|EnvironmentAndBuild)'
   ```

## Task 3: Ordering, references, tools, policies, and licenses

1. Add hostile tests for every ordering key, duplicate ID/tuple/license,
   forward/missing parent, unresolved/mismatched references, publication matrix,
   patch list rules, upstream/fork/container identity coupling, consumed-role
   coupling for components/utilities/configs/policies, tool role and
   executable-field matrix, material-file-backed policy equality, license
   backlink, exact canonical SPDX grammar/precedence, and redistribution enum.
2. Implement deterministic indexed validation. Material parents must reference
   an earlier record; all other references resolve against complete indexes.
3. Require the baseline toolchain and build-utility roles, allow traced extra
   executable roles only under the canonical role grammar, and reject duplicate
   roles within a toolchain or utility inventory. Require exactly one of every
   V1 policy kind and all nonempty closure arrays stated by the design.
4. Run:

   ```sh
   mise exec go@1.26.5 -- go test -count=1 ./internal/stagea0 \
     -run 'TestValidateMainLock(OrderAndReferences|ToolRoles|Policies|Licenses|Publication)'
   ```

## Task 4: Full verification and independent review

1. Run the complete focused suite and package tests:

   ```sh
   mise exec go@1.26.5 -- go test -count=1 ./internal/stagea0 \
     -run 'TestParseMainLock|TestValidateMainLock'
   mise exec go@1.26.5 -- go test -count=1 ./internal/stagea0
   make stage-a0-check
   make test
   make vet
   git diff --check
   ```

2. Run mandatory no-index whitespace checks and SHA-256 over every new untracked
   Go deliverable before staging.
3. Add deep-copy tests proving `ValidateMainLock` neither sorts nor otherwise
   mutates public slices, pointed variants, or strings on success or failure.
   Every hostile table row asserts its exact stable failure code.
4. Sol reviews the exact schema/semantic diff and decisions; Vega independently
   reviews implementation quality, hostile coverage, and verification. Resolve
   all Critical and Important findings.
5. Commit only the independently approved four-file implementation diff as a
   descendant of the documentation decision. Record base/branch/worktree,
   exact diff and commit/tree hashes, commands/results, reviewers/models,
   evidence class, remaining risks, and next safe action.

The next safe action after this slice is source-authority resolution for the
real Main toolchain, sysroot, container, build utilities, bundled inputs,
licenses, policy candidates, and known-good artifact. It is not creation of a
guessed candidate lock.
