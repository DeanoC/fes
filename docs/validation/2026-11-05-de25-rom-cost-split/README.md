# Native ROM ownership-cost accounting

Base: `180cbde2823a8b9fc7be9026d87c624515de47e1`. The preceding physical
trial reached a verified written counter of 5,888/8,192 bytes before the
unchanged one-second ROM deadline. Ownership brackets consumed 94.4877% of
elapsed time, but the aggregate could not distinguish full observations from
the per-access guards inside a bounded step.

The strong firmware now classifies each bracket using the private step state
at entry, before common CPU/binding/configuration checks. Its existing next
timer sample updates the selected completed count, total and maximum as well
as the aggregate. No timer or device read was added. These are diagnostic
values; ownership, callback identity, live register checks, transfer commands,
PID, 16-byte framing and the one-second/64-poll limits are unchanged. Accounting
adds instructions and memory traffic, so physical overhead still needs measurement.

The existing `fast_checks` counts actual entry to the inner guard. The new
`fast_samples` includes completed brackets classified by step state at entry;
an earlier ownership failure may leave one pending measurement. These names
must not be interpreted as interchangeable. Valid completed full/step totals
sum to the aggregate. Clock regression, total or per-class overflow, sample
overflow and invalid classification mark the diagnostic invalid without
changing the returned timestamp or transfer decision. The failure report uses
cached values after the existing containment callback and adds no reads.

The 17 targeted tests passed, including the real C ownership/step code joined
to the trusted boot/USB/master model. The 4/8 KiB driver cases now exercise
separate known-duration brackets, idle samples, regression, both class
overflows, sample overflows and invalid classification. Register access counts
stay unchanged. Successful transfers check classification and sum consistency;
early failures retain the pending distinction. Both actual C/RTL transfer paths
passed all 23 phases. The unshared fixture still measures all ownership work
in the full class and verifies one-time reporting without additional I/O.

Exact source hashes and logs are in [the test receipt](result.json), with job
logs under `out/nano-rom-cost-split-20261105`. This is host/RTL evidence. Next,
build the matched image and run one bounded physical trace to determine which
ownership path needs optimization. USB failure diagnosis, complete ROM loading,
ordinary package admission and DE10-equivalent core readiness remain open.
