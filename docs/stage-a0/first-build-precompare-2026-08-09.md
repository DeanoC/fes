# Stage A0 preliminary first-build comparison

## Status

The repository now contains `stage-a0-firstbuild-precompare`, a deterministic
comparison tool for two retained first-build captures. It is a concrete
Software-tested milestone, not the final reproducibility claim.

On success the tool writes `comparison.json` containing:

- canonical receipt SHA-256 values (without physical paths or timestamps);
- canonical build-log SHA-256 values, with the fixed adapter job-count marker
  required on both logs;
- equality of the locked source, toolchain, container, and build observations;
- equality of the complete 227-entry `bin/` inventory; and
- byte equality of the retained `MiSTer` and `MiSTer.elf` payloads.

The final logical-path adapter pair from 2026-08-09 produced distinct
build-log digests
(`179d2f2036e82549b5bf5e5bf5ed35e782e0fae9d2a55ea43609519d029a7297` and
`d123f062cb79cf6b90e41b93e88242f9c8f459315686b215dc13aae823eb8b34`) while
retaining identical receipts, inventories, and final artifact bytes. This is
stronger than comparing two copied payload directories, but remains a
Software-tested/local-only observation rather than a completed lock gate.

The report is explicitly `Software-tested`, `source_availability=local-only`,
and `two_builds_byte_identical=true`. The last field means the retained final
binaries and the complete observed inventory agree; it is not a claim that all
material provenance, policy closure, or the final lock is complete. It also
does not prove that the two roots were independently produced: the current
captures have identical receipt hashes and may be repeated captures. The
final gate must create two fresh trees from the completed lock and retain
independent build/run manifests.

## Command

```text
stage-a0-firstbuild-precompare \
  --left /absolute/path/to/capture-a \
  --right /absolute/path/to/capture-b \
  --report /absolute/path/to/new-precompare-report
```

The report directory must not already exist, and the two capture roots must be
distinct directories. The command never fetches, uses a target, contacts
hardware, or serializes the supplied paths. It rejects non-canonical receipts,
symlinked capture roots or payload files, unsafe modes, missing payloads,
adapter-unbound logs, receipt drift, and byte mismatches with stable
`PRECOMPARE_*` error codes.

The implementation is in `internal/stagea0/precompare` and is covered by
`make stage-a0-check`. The final Stage A0 gate still requires the valid material
lock, six policy files, immutable image/toolchain/license closure, exact
compile/link and dependency manifests, and two fresh builds from that lock.
