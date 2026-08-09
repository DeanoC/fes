# Stage A0 Main-lock candidate handoff

## Status

**Candidate / blocked.** `build/stage-a0-main.lock.toml` is a review draft,
not a build-ready lock and not an authorization to fetch, build, publish, or
touch a target. It intentionally contains empty values and `BLOCKED_*` license
identifiers where the observed first-build evidence is insufficient. The
strict V1 parser rejects it; no unresolved value is being promoted by
inference.

The measured first-build evidence remains **Software-tested** only. It does not
establish durable retrieval, **Reproducible**, HIL, or Accepted status.

## Provenance

- Candidate worktree base: `26994d5581d5d7c651b58ddb85faaa70b03a64be`.
- Candidate file: [`build/stage-a0-main.lock.toml`](../../build/stage-a0-main.lock.toml).
- Governing schema: [Stage A0 reproducible Main baseline design](../superpowers/specs/2026-08-08-stage-a0-reproducible-main-baseline-design.md).
- Parser: `internal/stagea0.ParseMainLock` / `ValidateMainLock`.
- Probe: a temporary host-only parser test read the candidate and rejected it
  with `LOCK_SCHEMA_INVALID: environment is invalid` because `umask` is empty
  and `job_count = 0`. The temporary probe was removed; it is not part of the
  deliverable.

The candidate has no self-hash. Its raw bytes must be hashed by an independent
review record only after all blockers below are resolved.

## Observed facts admitted to the draft

| Area | Observed value | Evidence boundary |
| --- | --- | --- |
| Main fork | commit `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d`, tree `efb9c24e8e27945a75d8c497b4b99ec249129075`, parent `7b5c8de5d3fb16f9cccc1f274a2ff1b481637e42`, clean branch `fogcast/stage-a-baseline` | Local Git observation; publication status remains local-only. |
| Main upstream | HTTPS `https://github.com/MiSTer-devel/Main_MiSTer.git`, commit `7b5c8de5d3fb16f9cccc1f274a2ff1b481637e42`, tree `04337bd664f5daa09b347fb2e4e991c66d84c89f` | Bootstrap-pinned source identity; retrieval and license review remain open. |
| Fork patch | one direct VDATE patch; isolated binary diff SHA-256 `15d56622e0e591d0dd61ebb4d9215175e42ce20bde9ecc1f1f1cfe6c34abb684` | Machine observation; exact policy file is not yet tracked. |
| Source inventory | 420 tracked paths and 113 direct Makefile source/image inputs | First-build report; complete source-set policy is not yet generated. |
| VDATE | `SOURCE_DATE_EPOCH=1786215171`, UTC `VDATE=260808` | Two clean engineering runs and prepared-image capture. |
| Arm archive | 104607124 bytes; SHA-256 `102825ae56c9e00142d06f35d2bdd3299edb6060e84a275a25b095e66fd3fc2a`; official locator recorded in candidate | Archive hash observation; license/corresponding-source record is open. |
| Cross compiler | GCC `10.2.1 20201103`; executable and binutils hashes are recorded in the draft | Extracted archive observation; complete toolchain/sysroot material closure is open. |
| Prepared image | local tag `stage-a0-firstbuild:debian12-arm102-v1`, ID `sha256:24045e0e800b0ce7df88076ccab628387b149f1bd0786fab46fffae07a859d0c`, Linux/amd64, network disabled during run | Local disposable image observation; no durable OCI manifest/config/source record. |
| Build utilities | bash 5.2.15, make 4.3, git 2.39.5, sed 4.9, coreutils 9.1 hashes recorded | Prepared-image observation; no locked utility package/material inventory or nproc shim. |
| Outputs | `MiSTer` 1157996 bytes SHA-256 `f9e6fd646740449186b74821a3684686d5dbc7b33e28052e9de28b8f4c751f2e`; `MiSTer.elf` 1380136 bytes SHA-256 `51a9864bb12ebdf8961b30ac0a45d533fd2a2885a96d81fc29f8363d1804706d` | Software-tested artifact observation only. |
| Output inventory | 227 regular `bin/` files; digest `7a2e77ffa919e504a4baa638d6b820085a4464a15281449f1abc2f4d66419cd5` | Capture inventory; complete canonical artifact/ELF/dependency/intermediate manifests are open. |

## Explicit blockers

1. **Environment adapter:** the measured capture explicitly set `umask 022`,
   but did not record or verify the final adapter contract, and it did not lock
   a fixed job count. The Makefile still discovers host `nproc`; no reviewed
   `/stage-a0/build-utils/bin/nproc` shim was used. The candidate therefore
   leaves both values unresolved. The `/stage-a0/*` paths in the draft are
   planned logical adapter roots, not paths observed in that capture.
2. **Container material:** the prepared image is a local Docker commit. Its
   image ID and parent Debian base identity were observed, but no durable OCI
   registry/reference, manifest digest, config digest, layer source record, or
   package/license manifest exists. The candidate's empty OCI fields are an
   intentional parser blocker.
3. **Fork retrieval:** the fork is available only from the operator's local
   checkout. No durable HTTPS fork locator or corresponding-source publication
   record exists; `local-only` is retained and no durable material ID is
   invented.
4. **Bundled Main inputs:** the 420-path tree includes third-party and bundled
   directories. Their canonical subtree IDs, source authorities, configured
   features, license notices, and corresponding-source records have not been
   inventoried. The draft does not silently promote them as one unreviewed
   material.
5. **Toolchain/sysroot closure:** executable hashes were observed for the
   principal compiler/binutils commands, but the archive's libc/sysroot roots,
   symlink/hard-link closure, package provenance, and license records are not
   represented as materials and policies.
6. **Build utility closure:** hashes/versions for observed utilities are not
   yet tied to immutable image layers or package records. The required
   nproc-shim is absent from the measured run.
7. **Configuration and policies:** the strict six-policy schema, lock-bound
   promotion validator, and Git-backed source-set/fork-delta candidate
   observer now exist and are tested. No six policy files have yet been
   generated as separately tracked UTF-8 LF artifacts; the four build-derived
   candidates (compile/link, ELF/dependency, generated-input, and
   intermediate-path) and all policy hashes therefore remain intentionally
   unresolved in the draft.
8. **Licenses:** Main, bundled libraries, the Arm archive, container packages,
   and policy/config materials lack reviewed SPDX expressions, notice
   locators, corresponding-source locators, and redistribution dispositions.
   `BLOCKED_LICENSE_*` IDs are markers only and must not be treated as legal
   conclusions.
9. **Canonical manifests/comparator:** source-set, compile/link, ELF,
   dynamic-dependency, generated-input, and intermediate-path manifests are
   not implemented as the final-lock gate. A preliminary comparator now exists
   for two retained local captures, but it only reports
   `Software-tested`/`local-only` and does not close this gate. Matching
   preliminary binaries do not close the final lock.
10. **Image/cache retrieval:** there is no content-addressed source/toolchain/
    image cache with revalidation, signed material metadata, or independent
    retrieval evidence. Local ignored artifacts are not a lock substitute.

## Verification run

The parser and focused implementation suites passed independently:

```text
go test ./internal/stagea0
go test -race ./internal/stagea0/firstbuild ./cmd/stage-a0-firstbuild
go vet ./internal/stagea0/firstbuild ./cmd/stage-a0-firstbuild
```

The candidate probe described above confirmed strict rejection. No target,
network fetch, credential, hardware, or deployment operation was performed by
this candidate-lock task.

## Next safe action

Keep this file and the candidate lock in review-only state. The next owner
should create the immutable material catalog and six policy artifacts from
verified source/cache observations, resolve the image and license provenance,
add the nproc-shim/job-count adapter, then rerun the strict parser and an
independent two-build comparison. Only a reviewed valid lock may feed the
canonical Stage A0 fetch/build tools.
