# Bounded affinity-call trace trial

The selected firmware is `23db3f0d58c975e2610ff495376c778ada889fa7`.
Its [matched build and off-board validation](../2026-11-06-de25-affinity-call/README.md)
bind chain `471c895b3e7aeafd96a1c953be81a14cd043926dd4d01c7ec9e6b60534522c18`.
The native application/entry remain byte-identical to source `31d716957`.
This new compiled input has zero attempts before source freeze. The previous
20 campaign stops include eleven before the loader and two after the loader,
before native application startup. The consumed `7f3824490` input is not replayed.

Exact helpers under `out/nano-affinity-call-board-20261106` and fifteen completed
proof/fixture groups are pinned in [operator-source.json](operator-source.json).
The service gate now requires all 48 ordered call receipts: six per query,
four initial and four late, with exact query/CPU/scope, typed indices and eight
reported returns. It also requires all 26 loader records, both EXIT_CACHE
records, full 31-phase runtime startup and every existing boot/ownership/core
cycle proof. The actual modeled service exchange passes, and 117 damaged outer
results reject. Sixty-one new receive-only tail cases replay ARM-emitted call
messages in model boot contexts, preserving incomplete instruction/cache state
and partial final lines without granting boot or command permission. These
checks supply no new physical evidence.

The continuing claim covers TRWU8CBL only. Fresh preflight checks current
source/artifact identity, claim, exact USB identity, UART owners and host route.
One qualified volatile recovery freshly admits the spare card and protected
recovery files before any SD writes. Verify/reuse seven retained catalog files,
upload the new 8,194,104-byte FIT as the eighth fixed file, then freshly read
back all eight. The native attempt may send `fesboot` once. Only complete
native App Ready permits the existing ZX81 / JP-loop ROM / ZX81 launch-return-
quit sequence with fresh activation receipts, fetch and ownership proofs.
Shared USB idle reports do not qualify physical buttons; no button action is
requested in this test.

All bounds stay unchanged: prompt 50s, startup/launch 100s, configuration 20s,
controls 5s, native 450s, child 480s, work 740s and host 850s. ROM transfer stays
16 bytes per step, 400M ticks and 64 polls. Failure capture is receive-only for
two seconds with a 1MiB console bound. Partial receipts cannot prove which
instruction executed, and a false observation flag is not proof that execution
did not start. Stop at the first failure, consume the input and retain evidence;
no extra command, fallback or replay.

The normal-user shared Quartus lock encloses private top-only transport.
Programming uses 6M; cleanup restores original 15M, exact USB permissions,
owned private-server termination and UART free. These host checks cannot prove
CPU/FPGA halt. Retain claim and spare after a stop. Factory restoration remains
waived; factory card stays safe aside and TRWU8J8G receives no operations.

At source freeze no hardware attempt has been made with this input. Next run
fresh preflight and one bounded attempt, recording physical evidence separately
from builds and fixtures. Ordinary core loading and DE10-equivalent acceptance
remain incomplete.
