# DE25 retirement: separate staging and completion budgets

The hardware trace from firmware `5764c9495` transferred all 1,708,032 bytes,
then exhausted its 100 ms queue guard while staging its first command slot.
The subsequent `35cbbed63` transition batching repair reduced surrounding
ownership work, but reproduced the queue timeout off-board. Source base for
this change is `3fc738a18c1e268c992f83d276f49653ccd43e77`.

The queue and retirement durations are software policies, not DE25 hardware
limits. The revised contract accounts separately for the work they cover:

| Operation | Budget | Origin |
| --- | ---: | --- |
| Retirement queue construction and prewrite admission | 2 seconds | Original queue start |
| Owned command-consumption observation | 1 second | Accepted counter from final prewrite guard |
| Mandatory final validation | Within 4 seconds | Original queue start, also subject to enclosing retirement |
| Complete retirement and continuation | Less than 4 seconds | Original retirement start |
| Input preparation | Unchanged, 1 second | Original preparation start |
| Source release | Unchanged, 1 second | Fresh owned release start |
| Full source flow | Unchanged, 20 seconds | Original source start |

These are provisional finite budgets. The highest previously reproduced cost
case completes retirement/continuation in about 2.16 seconds; a separate
384-microsecond injected case takes about 2.88 seconds. The four-second budget
provides model margin, not a measured physical worst-case guarantee. Uncertain
operations remain consumed, retained and non-retryable.

## Implementation and binding

The retirement-only timed publisher performs the same producer write through
the existing supplier, after the fresh full 24-register prewrite gate. It
reports that gate's accepted counter. The sample precedes the final ownership
check and barrier; it conservatively includes that remaining work and is not
an exact MMIO issue timestamp. A staging overrun rejects before the write.
Both postwrite guards remain mandatory. The queue rejects a returned sample
before its prior accepted clock, after its postguard clock, or outside staging.

The completion deadline is computed from the validated local callback result.
The local policy selects all deadlines; mutable observation fields never
select guards or extend a deadline. Initial snapshots, all six individually
guarded private command stores, prepublication checks, progress/pointer checks,
both final snapshots and final guard remain. All five control/publication
writes and all retained-table checks remain. No reset, bypass, acknowledgement,
activation or source-reuse grant is added.

The old retirement policy 1 is removed. Full-operation policy 0 and
preparation policy 2 retain their original limits; retirement selects policy 3
and requires the trusted timed publisher. Source release now names its own
one-second constant instead of sharing the retirement duration.

Base profile 16 and repeated-load profile 17 bind the new timing fields and
source closure. Frozen profiles 14/15, including attempts to rename only their
format, reject. The queue record adds required ordered `publication_seen` and
`publication_us` fields. The observer validates construction, publication,
consumption, final validation and enclosing clocks independently. Missing old
fields, old policy values, late samples and invented publication reject;
observations supply no hardware authority. Frozen prior operators remain
available for their own earlier artifacts.

## Off-board verification

- All 31 sanitized source/retirement tests pass with no skips, including 42
  publication cases through callback and direct-register queue paths.
- All 50 selected SDM profile/trace tests pass. The synthetic trace suite
  admits a long construction/final-validation case only when each independent
  completion/publication bound and existing source/retirement proof passes.
- Callback substitution, ownership loss, backwards/future timestamps, uncertain
  publication, staging overruns, exact completion boundaries, parent expiry
  and replay failures are exercised. Existing generation/wrap, prewrite,
  table-fault, source release and native readiness models remain passing.
- Nine current production units compile in the private vendor AArch64 tree.
  Selected static frames are queue submit 368 bytes, retirement 336 bytes,
  continuation 304 bytes and timed write 304 bytes. This manually enabled
  object check is not an installed profile, linked BL31, whole-EL3 stack proof
  or matched boot image.
- The contained firmware/RTL verifier passes. This is injected hardware state,
  not physical configuration acceptance.

| Injected raw-read cost | Staging | Completion observation | Retirement + continuation | Outcome |
| --- | ---: | ---: | ---: | --- |
| 224 microseconds | 336.077 ms | 248.693 ms | 1,679.989 ms | Completed |
| 256 microseconds | 384.077 ms | 284.213 ms | 1,919.957 ms | Completed |
| 288 microseconds | 432.077 ms | 319.733 ms | 2,159.925 ms | Completed |
| 384 microseconds | 576.077 ms | 426.293 ms | 2,879.829 ms | Completed |
| 1,000 microseconds | 1,500.077 ms | No accepted completion | Parent expiry | Retained failure |

These costs are injected into the real joined vendor/platform model. The first
three costs failed staging in the retained prior control. A 1,000-microsecond
counter cost also completes; 100,000 microseconds per counter expires. No row
is a physical latency measurement or evidence of repeated core loads on a kit.

The retained receipt accompanies this record. TRWU8CBL is untouched by this
work; its last verified state remains the restored original baseline at Park.
Next work is software-controlled recovery qualification and a fresh matched
boot chain before another bounded repeated-loading trial.
