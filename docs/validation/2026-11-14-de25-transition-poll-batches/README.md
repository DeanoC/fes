# DE25 retirement transition polls: bounded read-only batches

The last hardware candidate transferred all 1,708,032 configuration bytes and
then stopped during private command staging, before command publication.
Its 100 ms command-queue guard expired at 116.787 ms; the enclosing retirement
was still within its original one-second bound. The original native baseline
was subsequently restored and passed all 51 launch/return/Park exchanges.
That hardware evidence belongs to its frozen earlier firmware, not this change.

This off-board repair removes redundant ownership observations from the four
surrounding transition polls. It does not repair the command-staging deadline
or authorize another hardware trial. Source base is
`091cf56768c75e72c0bf60696419acbff6f010ba`; the cost-model control uses the
last tested firmware at `5764c9495b6597da306546879194100021986a37`.

## Controller change

Each poll previously performed the fixed 19-register view and three individual
register checks, each with separate ownership/time guards. It now executes the
same ordered 22 reads in one bounded read-only batch. Abort polling checks CR0
and CR0ACK, then samples GBPA. Transition polling checks GBPA and CR0, then
samples CR0ACK. The changing value remains private until the post-batch guard
passes. The batch pins its phase as well as its existing source, callbacks and
completed setup controller.

Both fresh ownership/time guards, per-read ordering barriers and first-error
handling remain. All five control/publication writes retain their independent
fresh 24-register prewrite views. The command controller, private slot stores,
queue completion policy, 128-poll limits, one-second retirement/continuation
origin, and 25,600 retirement table-word reads are unchanged. No reset,
acknowledgement, memory release or activation capability is added.

## Software evidence

The sanitized retirement suite passes 29 tests. Its new transition fixture
executes four successful phases and 880 fault cases: ten fault types at each
of 22 read positions in each phase. These cover returned mismatches/read
errors, ownership loss, elapsed deadline, callback/context substitution, source
binding changes, phase changes and prepared-controller substitution. Every
failure retains memory, rejects replay and stops before another control write.
The six source-selection/profile tests also pass.

In the unchanged joined supplier model, zero injected cost produces:

| Observation | Control | Candidate |
| --- | ---: | ---: |
| Raw register reads, retirement only | 6,991 | 6,583 |
| Counter reads, retirement only | 228 | 204 |
| Retained table-word reads | 25,600 | 25,600 |
| Control/publication writes | 5 | 5 |
| Private command-word stores | 12 | 12 |

Entry, command invalidation and terminal phase counts are identical. Each of
the four changed phases saves 102 raw reads and six counter reads. These are
injected operation counts, not measured board latency. Ten read-cost settings
per source were compared. At an injected 224 microseconds per raw read, both
sources still reject command staging; this is an explicit remaining failure.

Both sources compile as standalone AArch64 objects with the matched cross
compiler, strict warnings and stack-usage output. The candidate's observation
frame is 336 bytes versus 320; the initial-transition frame is 144 versus 112.
This is object compilation, not a linked BL31, whole-EL3 stack proof, admitted
boot chain or hardware acceptance.

The host proof is retained under
`/home/deano/fes/out/dev/de25-source-release/out/retire-transition-batch-proof/`;
its result includes both model outputs, source hashes, compiler commands,
object hashes and stack records. The retained archive receipt accompanies
this record.

## Remaining work

The 100 ms queue and one-second retirement budgets are software policies,
not DE25 hardware limits. This isolated batching change keeps them unchanged;
that does not establish that they are appropriate for the complete operation.
Command construction and published-command completion need separately defined
bounds with explicit owned callback contracts and measured margins. A larger
finite software budget remains an option where the required work justifies
it. Mutable diagnostic fields cannot select which checks execute. The cost
experiments have not established an acceptable complete implementation.

The broader host-hook verifier's initial 180-second outer cap was too short
for its sequential 90-second test groups. Three groups completed before that
supervisor timed out in a later group. The four unfinished groups were then
executed separately, all passing with their original 90-second Go test limits
and source/fixture hashes pinned around the run. Completed earlier groups were
not rerun. The original verifier remains classified as timed out; these
completed group logs and the separate remaining-group receipt accompany it.

Software-controlled recovery also remains unqualified. Inspection of the
matched Agilex 5 U-Boot reset driver shows that it selects warm/cold reset from
the volatile `reset` environment value; the generic `reset -w` argument alone
does not select the vendor route. The matched TF-A cold handler invalidates
platform ownership and sends the SDM `REBOOT_HPS` command. These source paths
do not prove recovery from a parked, failed secure call or that either SMMU
bank is reset. A separate bounded recovery qualification is required. No
reset command or other hardware operation was executed for this repair.
