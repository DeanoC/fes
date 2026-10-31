# DE25 kernel programming failure retention

The opt-in [permanent-owner profile](../de25-platform/README.md) patches only
the exact Terasic files recorded in its `source.json`. The same default-off
`CONFIG_FES_DE25_DMA_PLATFORM` gates manager error retention, write deadlines and
service buffer-claim polling. Ordinary kernels keep the vendor programming
behavior. No normal FES runtime admission is enabled by this kernel profile.

The service-claim loop now honors thread stop, sleeps while busy and bounds
inactivity using the existing `FPGA_CONFIG_DATA_CLAIM_TIMEOUT_MS`. Only actual
accumulation of another buffer refreshes that inactivity deadline. Failure
publishes one error with all pointers cleared; cancellation publishes no late
callback. It never reports returned buffers on failure. The original vendor
completion path remains intact in profile-disabled builds.

The manager marks a request uncertain before sending it, retains that mark after
any init, data or completion failure, and rejects a new caller before issuing
commands or resetting buffer locks. Inactive failed calls cannot resume data or
completion. Only confirmed completion with every buffer returned and the service
worker joined can clear uncertainty; a final callback error during the join
prevents clearing too. There is no reset/clear operation. The supplier drivers
are built-in and their sysfs unbind paths are suppressed by the same profile.
These protections retain the programming buffers for the failed kernel epoch;
reboot is not declared a safe fence for an uncertain SDM transfer.

## Manager data-write loop

Permanent submission errors retain their errno. ENOBUFS waits for a callback;
errors take precedence over progress. The complete data-write phase shares one
30-second jiffies budget, initialized after successful request acceptance and
retained across Linux scatter/gather fragment callbacks. Each wait also retains
the existing five-second buffer timeout. Completion polling retains its existing
five-second phase budget, now accounting for time spent sending commands and
spurious notifications. Neither budget can interrupt a synchronous secure call.

## Source-function validation

Run against the unmodified pinned vendor files:

```sh
python3 image/kernel/de25-fpga/verify_claim.py \
  --source /absolute/pinned-linux/drivers/firmware/stratix10-svc.c \
  --output /absolute/new-claim-proof
python3 image/kernel/de25-fpga/verify_write.py \
  --source /absolute/pinned-linux/drivers/fpga/stratix10-soc.c \
  --output /absolute/new-write-proof
python3 image/kernel/de25-fpga/verify_retention.py \
  --source /absolute/pinned-linux/drivers/fpga/stratix10-soc.c \
  --output /absolute/new-retention-proof
```

Each runner authenticates the complete original file, the platform patch and the
complete patched file before compiling the actual changed C functions against
injected firmware, clock, callback and completion helpers. All have an explicit
profile-disabled positive control. The source-function proofs cover:

- Nine service cases: permanent BUSY, firmware error, cancellation before/during/
  after a call, four-buffer success, repeated OK without progress, clock wrap,
  and real buffer progress over an elapsed interval longer than the inactivity limit.
- Fourteen data cases: normal transfer, permanent ENOMEM, silent/notified
  backpressure, simultaneous error/progress, absent callbacks, partial-submit
  EIO, failed claim submission, spurious notifications, expiry during submit,
  clock wrap, expiry between clock reads, and successive fragments sharing a deadline.
- Fourteen lifecycle cases: two successful cycles; failed init submission,
  timeout or error-plus-OK; data failure; completion submission/timeout/error,
  buffers still locked, time spent submitting, spurious completion, late error
  or lock state at join; and validation refused before a request. Failed state
  rejects init/write/complete from a fresh caller without commands or lock reset.

The original manager must hang on permanent ENOMEM and the original service on
permanent BUSY under one-second owned-child controls. The original init path
also permits a new request after failure; a dedicated retry assertion reproduces
that regression. The lifecycle harness follows the kernel's existing unused
callback-parameter policy while keeping other compiler warnings fatal.

These are host function tests. A full AArch64 kernel build, matched image
preparation, physical boot and actual successful programming/reprogramming are
separate checks. Error callback or stopped thread alone does not prove secure
firmware stopped DMA. Synchronous secure-call return, shared-service mutex
blocking, physical failure fencing and reboot after uncertainty remain open.
Do not unload, recycle buffers, retry configuration or use reboot as a drain
merely because a patched polling function returns. Failed physical transfer
injection needs an independently verified hardware recovery/fence first.

The older standalone bounded-claim patch and source manifest have been removed;
the platform patch is the sole active source of this algorithm and its guard.
Dated evidence for that earlier candidate remains tied to its original commit.
