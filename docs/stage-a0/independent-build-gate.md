# Stage A0 bounded independent-build gate

`stage-a0-independent-build` is the next software-only gate after the
preliminary first-build comparison. It consumes the exact bytes of a parsed
final-lock document, verifies that its source, toolchain archive, container,
environment, and build contract are bound to the reviewed Stage A0 baseline,
then runs two fresh `firstbuild.Capture` operations in distinct, non-nested
output roots. The roots are compared with `precompare.Compare` before a report
is published.

The command is intentionally fail-closed. It refuses an invalid or unbound
lock, missing or symlinked inputs, an existing or reused capture root, nested
capture roots, and an existing report path. A failed second capture does not
produce a report; any first capture left on disk is diagnostic evidence and
must be handled by the operator before a new run.

```text
stage-a0-independent-build \
  --lock FILE \
  --source DIR \
  --toolchain-archive FILE \
  --left DIR \
  --right DIR \
  --report FILE
```

The report schema is
`fogcast.stage-a0.independent-build.v1`. It contains the lock SHA-256,
`fresh_captures = 2`, distinct-root attestation, and the canonical nested
precomparison. It contains no host paths, timestamps, credentials, or command
output. The only emitted evidence state is `Software-tested` with
`source_availability = local-only`; it is not a `Reproducible`, HIL-observed,
or `Accepted` result and does not authorize lock promotion or target mutation.

The current candidate lock is deliberately not a valid final lock, so this
gate must remain blocked until the final-lock/material/license and durable
retrieval work is complete. A successful run also requires the reviewed build
inputs and container to be available locally; no network pull is performed by
the first-build adapter.
