# Retained-source retirement hardware diagnostic

The [format-11 completion policy](../2026-11-13-de25-command-completion/README.md)
passes one bounded physical trial on TRWU8CBL. Source
`f26b3626b3655fae686fb8987e9a38db60e3a2de` selects unchanged production bytes
from `494ffcf5136123ab5abf0b804faa39a7afd95761`. Procedure
`2db3b49429f454c1f4bba4d90c53bcfbb0a3f3820fba0dc809b350d3608407f1`
is consumed once under standing human approval. No power cycle is required
during this trial. Later evidence publication changes documentation only.

The [frozen procedure](procedure-proof.json) covers 30,685 files. Its four
binding, 62 controller, five preflight, seven latest-restoration and three
failure-tail tests pass. Eight actual crossed-role/digest admission checks,
both full fresh native admissions and actual user/root default CLI checks
pass. The latest actual cold-reset baseline restoration replays from its
immutable archive before attempt reservation or device observation. Those
[checks](checks.json) do not themselves authorize hardware: a separate exact
procedure approval is bound to the user's standing authorization.

Chain `fc340ff0efd7803dc970ef7dcc3566fcffe94c43cb65afb7c6c125599f539c47`,
boot `d5a984131fbd2286`, FIT
`b080302e115f00cc5316b5bf033e2dc2886bed93e2842437b213e48ea4c926db`
(8,514,256 bytes within the explicit 16 MiB boot allocation).
All 1,708,032 core source bytes transfer in two posts/two acknowledgements and
11,639,990 us, with matching SHA-384 and no pending/outstanding bytes or source
errors. No launcher App command is sent to the candidate.

The [raw-wire replay and result](result.json) establish a successful retirement
receipt: phase 6, status 0, retired 1, five control writes, final control 0 and
abort policy `0x100000`, producer/consumer 12. All five source/table validations
and 1,186 controller reads complete without error. Owned command consumption
passes the pointer and postguard checks at 97,359 us from the original command
origin, inside its unchanged 100 ms bound. Both mandatory final snapshots and
the final guard complete at 118,481 us from that same origin. Total retirement
is 403,340 us, inside the independent, earlier one-second retirement window.
No clock is rebased and no table/register/source/ownership check is omitted.
The raw consumer alone supplies no completion authority; the independent
source, retirement and timing proofs must also pass.

Restoration-only recovery programming passes. The exact original FIT
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
is restored/read back; all 17 protected-file observations before/after each
refresh and read-only SMMU reset gates pass. A fresh original-baseline boot
completes all 51 commands, ZX81/ZX80 launch/return and final USB STOP generation
3/Park generation 5. All four role cleanups pass. Independent program/wire,
source/retirement/queue, swap and baseline replay pass; recorded owned workers
stop, root process-FD inspection finds UART free, and JTAG auto/15 MHz plus
exact USB identity/inode/permissions are restored. Overall trial time is
866.643 seconds. The earlier format-10 failure remains consumed and failed;
this warm recovery success does not explain or repair its USB ownership panic.

[Exact byte/hash/mode archive readback](retention-proof.json) verifies
`/home/deano/fes-retained/de25-command-completion-physical-20261113-r1/physical-trial.tar.gz`,
SHA-256 `40d5e4b65f2933ec51f3b4073b00237e2673c06374b3245b2b3a78d0e673f1fd`,
with explicit prepared-procedure dependencies. The original failed traces and
cold-restoration archives remain separately retained. The
[publication file check](publication-files.json) confirms unchanged native
file/tree membership and referenced archive identities; it adds no device
actions or general hardware acceptance.

Current verified kit state is original native baseline final Park, test card
installed, all owned host handles closed, top claim held. QSPI, factory card
and bottom kit are untouched. This qualifies the selected retained-source
retirement diagnostic only: source/control/table/queue storage remains held,
`reuse=0`, `activated=0`. It supplies no independent interconnect-drain proof,
memory reuse, normal runtime activation or general core-load acceptance.

The next integration step is a bounded retained continuation: return a
successful retirement receipt through the existing configuration and activation
ownership checks, while retaining all memory. Model successful continuation,
owner/source/clock changes and failed retirement before a fresh matched build
and a new bounded physical test.
