# Cached secure ROM failure diagnostics

The first held-ROM hardware attempt stopped before CPU Run with a cleared bridge
reply. It did not establish the ROM loader's internal error. The new diagnostic
caches observations during the original provider checks and ROM accesses, then
emits one record after the existing containment callback. No endpoint access,
ownership check, clock sample, retry, grant or timeout change is added by reporting.
The original short-circuit ownership checks retain their order and conditions.

The actual strong provider and computer gate pass the C/RTL verifier for both
4 KiB and 8 KiB images. Each size exercises 18 reported failures, including digest,
CRC, scope, endpoint identity/state, clock regression and deadline/poll exhaustion.
The fixture asserts containment precedes output and repeated reporting performs
no reads, writes, ownership checks or clock samples. Valid load/held-return paths
also pass without a diagnostic. The adjacent computer-control suite passes three
tests, including 21 actual C control mutations. Source hashes in the ROM verifier
were rechecked against the working tree.

Evidence: `out/nano-rom-failure-trace-20261105/rom-model/result.json`, its
`csr-execute-{4096,8192}.log` files and `computer-tests.log`. These are host/model
results only. No hardware has been touched for this change. A freshly matched
firmware chain and bounded hardware trace are required; this change neither
qualifies ordinary package admission nor establishes DE10-equivalent readiness.
