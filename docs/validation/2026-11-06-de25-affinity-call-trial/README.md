# Bounded affinity-call trace trial

One physical attempt entered the secure affinity handler and reached its cache
flush bracket, then stopped at the unchanged application startup deadline. No
core was launched. The [physical result](physical-result.json) is separate from
build, emulation and model evidence.

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

The exact frozen operator was `ddb0e93c074a69faafe3673aedbae2e23b7f586e`.
One fresh recovery, eight fixed uploads and eight fresh SD readbacks passed.
Exact chip/programming, SPL FIT/hash, both private exit-cache receipts,
full 31-phase runtime progression and matched U-Boot prompt passed. One
`fesboot` command then emitted six loader markers through `cpu0-before` and
three call receipts: `smc-before`, `affinity-enter`, `cache-before`, all for
initial CPU zero. No cache/affinity/SMC return, owned receipt, native entry or
App Ready arrived within the unchanged 100 seconds. No launch/control command
followed. The new downstream handler receipt establishes the SMC crossing into
the selected affinity function; this is a source-bound observation, not an
independent instruction/PC trace.

The unresolved interval is now the final cache-before UART drain/possible
park, return to PSCI, pure per-CPU address calculation, original range-flush
setup/`DC CIVAC`/`DSB SY`, and return before the missing cache-after marker.
The actual `_cpu_data_by_index` helper is arithmetic only, with 64-byte stride
and base `0x58000`. The affinity-state field is four bytes at offset `0x18`,
giving candidate address `0x58018` for CPU zero. No exact stopped/faulting PC
or actual cache-instruction execution is established. Runtime controls were
not independently sampled at this cache bracket.

The [matching A55 r2p0 TRM](https://documentation-service.arm.com/static/649ac6d4df6cd61d528c2bf1),
sections 2.6.2.3/2.6.2.4 (PDF page index 55), explains that disabling data caching
bypasses data/unified caches for ordinary accesses but does not disable cache
maintenance. It also describes clean/invalidate behavior of setway operations.
This rules out treating cache-off as an empty-cache proof. A runtime substitute
must account for the now-active nonsecure U-Boot caches and mapping history;
the earlier cold all-cache qualification cannot simply be inherited. This
manual review supplies constraints, not causality or a hardware fix.

Independent raw replay matches the stored decoder exactly: valid incomplete
three-record call progression, zero reported instruction returns and no native
boot completion. The two-second tail retains incomplete instruction/cache
uncertainty, full prior runtime/exit state and restored logging, with no partial
final call line. Independent cleanup confirms original 15M, exact USB
permissions, owned private-server termination and UART free. CPU/FPGA halt is
unproved; claim and spare remain retained. Factory card and bottom kit are
untouched.

This is campaign stop 21, with eleven previous pre-loader stops and now three
post-loader application-start stops. The `23db3f0d5` input is consumed and will
not be replayed. Native ROM/Run/return and ordinary package admission remain
unqualified.

Next prototype narrowly admitted private per-CPU affinity maintenance while
preserving clean/invalidate intent. Qualify exact ranges, live controls and
CPU/master ownership plus the nonsecure cache/mapping history before choosing
any all-cache alternative. Test active EL2 data preservation and modeled
cache-history/admission/failure paths; keep physical cache limits explicit.
Preserve nonprivate maintenance, all bounds and first-failure behavior. Do not
blindly skip maintenance, reuse the cold-only scope or assume cache contents.

Full trial evidence and source pack are verified by archive readback/import at
`/home/deano/fes-retained/de25-affinity-call-stop-20261106-r1`, with prerequisite
`de25-affinity-call-20261106-r1`. The supplemental loader source/ELF audit made
after that build archive is included in this trial archive as well.
