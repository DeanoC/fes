# Persistent kit software release acceptance

The three-PR stack adds immutable kit bundles, a base-image supervisor and
operator/diagnostic integration. Host software and installed core packages remain
independent. The implementation started at `32085c8401a50821f764bde1aec48a341b33ab88`.
The accepted appliance and bundle payloads were built from
`b70dcbebba9ac13792321492f9c5f53f3a16329b`; the subsequent parent diagnostic fix
`2305a7b1f` scopes Stop confirmation to its original target and changes no image
or component bytes.

The [sanitized evidence](2026-10-15-kit-software-releases/evidence.json) records
artifact identities and results. This is exact-artifact lifecycle hardware
evidence plus an input-delivery diagnostic, with the limits below.

## Build and host checks

Both independent 192 MiB image passes produced
`dfc57838105da5f2d9518929df8b43099722300059e60e9bd5fac7e916f240d2`.
`make build`, `make verify` and release creation passed, including structural,
package and QEMU packaging checks. The image retains the 15% free-space guard.
QEMU does not emulate the FPGA hardware.

The 934-test parent suite, focused Go race checks, supervisor process/admission
integration tests, ARMv7 builds, image recipe tests and generated-consumer checks
passed. The original `make test-changed` invocation failed because an explicit
Python override leaked into nested Make command assertions. All nine affected
Python methods and all eighteen remaining RTL commands passed separately with
the corrected environment. Protected test-source identities were verified across
the capacity-only change. The confirmation-gate race regressions and all sixty
package-acceptance tests passed after the hardware findings were fixed.

## Designated-kit results

The authorized `.84` kit adopted and durably confirmed the exact image through
the leased appliance updater. Post-confirmation storage reconciliation restarted
the service group, opened persistent selection and the core library, and accepted
factory software without another reboot. Its boot ID remained
`0c44db4f-7de1-4c75-a2ef-9043e35dc8e0` throughout subsequent bundle tests.

Installation completed while an execution lease was held, preserving its owner,
active software and base identity. The first diagnostic inventory read exceeded
its five-second timeout; a read-only inventory check confirmed installation in
eight seconds. The diagnostic continued with a longer read timeout and did not
repeat the upload.

Duplicate requests were idempotent; conflicting requests and stale generations
were rejected. Soft Stop retained the lease and deferred activation. Explicit
cancellation removed the first request. A fresh request activated after release,
passed stable readiness and remained selected at generation 2 with unchanged
boot/base identity.

A deliberately failing agent bundle then exercised recovery. The supervisor
restored the accepted bundle, recorded one recovered receipt and reopened
admission without reboot or selection-generation change. Observations included
queued, applying and recovering phases and fifteen seconds of stable recovery.
REST observations do not independently count process starts; host crash-injection
tests cover the durable at-most-once recovery rule and stale-operation fencing.

All five archive, member and selection hashes for the pre-existing installed
Coleco package matched the pre-adoption baseline after activation and recovery.
An independently versioned private host passed two Pong launch/input/Stop cycles
with a host restart between them. Both cycles required explicit event
acknowledgements and increasing input-frame counters. HDMI capture showed the
launcher visible after recovery. No physical-controller or power-loss test is
claimed.

## Findings and retained state

The first assembly exceeded the 160 MiB image's free-space guard; capacity was
increased to 192 MiB without weakening the guard. Initial physical adoption also
exposed a stale trial-ticket storage gate. The corrected gate uses durable
confirmation for the exact boot and image, with regression coverage.

The private-host diagnostic exposed a pre-existing unscoped Stop-confirmation
assumption. It now polls the original named target while retaining exact session
and target identity checks.

Two unreferenced old appliance images were hash-verified into private local
backups before removing only their boot-volume copies under the kit lease.
Factory, known-good, previous and loop-referenced images were preserved. The
accepted software bundle remains selected; the failed bundle is retained as an
installed, unselected artifact. No automatic garbage collection was added.
