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
