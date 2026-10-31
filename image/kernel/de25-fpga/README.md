# DE25 FPGA configuration failure recovery candidate

This patch targets only the exact Terasic kernel source identified in
`source.json`. It bounds the service's FPGA buffer-claim loop and honors its
thread-stop request. It does not fix the underlying failed configuration
transfer, change DMA translation, relax configuration deadlines, or install a
kernel into any image. The production runtime remains closed on DE25.

The existing `FPGA_CONFIG_DATA_CLAIM_TIMEOUT_MS` value becomes the maximum time
without another accumulated buffer. BUSY polling sleeps; firmware errors or
expired inactivity report one error callback with all four pointers cleared.
Cancellation returns without a late callback. Successful four-buffer return
retains the vendor driver's accumulation/callback behavior. The old completion,
which could not bound the BUSY branch, is removed.

The physical failure is recorded in
[the Linux-manager diagnostic](../../../docs/validation/2026-10-30-de25-linux-fpga/README.md)
and [#1298](https://github.com/DeanoC/fes/issues/1298). The change follows the
[Altera polling-fix proposal](https://lists.openwall.net/linux-kernel/2026/10/23/612),
adapted to the older vendor driver's four-buffer accumulator. This is a local
candidate; the proposal's upstream state is not an acceptance claim.

Run the focused source-level proof with the unmodified pinned source file:

```sh
python3 image/kernel/de25-fpga/verify_claim.py \
  --source /absolute/pinned-linux/drivers/firmware/stratix10-svc.c \
  --output /absolute/new-proof-directory
```

The runner verifies the complete input-file hash, applies the patch without fuzz,
verifies the complete result hash and compiles the actual changed function with
simulated firmware, clock and callback helpers. Nine cases cover permanent BUSY,
firmware error, cancellation before/during/after a call, four-buffer success,
repeated OK without progress, clock wrap, and progress across an overall elapsed
time longer than the inactivity limit. The unmodified function must hang on the
same permanent-BUSY control. Only the owned control child is terminated by its
one-second test deadline.

This proves host function behavior, not kernel scheduling, secure-monitor return,
DMA or physical reconfiguration. A separate compile against the real pinned
kernel headers/configuration is required. Booting the changed kernel and observing
bounded overlay cleanup on the same physical failure remain required before
shipping the recovery change. A synchronous secure-monitor call that never returns
cannot be interrupted by this software polling deadline.

An error callback or stopped service thread does not prove SDM/DMA quiescence.
After a failed configuration, retain the manager and its allocated buffers until
an independently verified reset/power transition fences that hardware epoch.
Do not unload modules, recycle buffers, or retry configuration merely because
the patched function returns. Normal runtime recovery still needs that fencing.

## Manager data-write loop

The opt-in permanent-owner kernel now also repairs a separate spin in
`s10_ops_write`: the vendor loop immediately retries every negative submission,
including permanent ENOMEM/EIO, and can ignore an error accompanying a progress
bit. Its source-bound patch is part of
[the permanent-owner profile](../de25-platform/README.md), behind
`CONFIG_FES_DE25_DMA_PLATFORM`. It retains the original error, waits on temporary
ENOBUFS, prioritizes error notifications and caps total data-write time at 30
seconds. This is a write-phase budget; it changes neither the existing request
nor per-buffer timeout and does not establish a whole-load deadline.

```sh
python3 image/kernel/de25-fpga/verify_write.py \
  --source /absolute/pinned-linux/drivers/fpga/stratix10-soc.c \
  --output /absolute/new-write-proof
```

The runner authenticates the complete original source and platform patch,
applies its manager section without fuzz and authenticates the complete result.
It compiles the actual changed write function against injected clock, service,
completion and callback helpers. Thirteen cases cover normal transfer, permanent
ENOMEM, silent backpressure, repeated backpressure notifications, error combined
with either progress bit, missing callbacks, EIO after partial submission,
claim-command failure, spurious completions, expiry inside submission, clock wrap and expiry between budget reads.
A single sampled clock supplies both the expiry check and remaining wait. The unmodified function must hang on permanent ENOMEM under a one-second
owned-child timeout. A profile-disabled positive transfer also passes.

These are host function tests. They do not prove real kernel scheduling,
callback concurrency, service-thread termination, secure-monitor return, SDM
abort, physical DMA drain or recovery after a failed load. In particular the
existing `stratix10_svc_done` thread join can still block in the unpatched
service loop. The earlier bounded-claim patch remains a separate candidate;
it is not automatically applied or admitted by this repair.
