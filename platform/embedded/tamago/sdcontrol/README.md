# Firmware-owned SD PIO session

This package connects the actual `sdhci.Device` to the experimental
[shared SD lease](../../../../sources/mister-packages/docs/embedded-sd.md).
It is a runtime adapter with injectable firmware transport and registers.
The Nano board adapter and current cold serial boot do not select it; the
secure platform supplies no SD callbacks by default. Model tests and a linked
firmware build do not establish physical SD or core-load acceptance.

`Open` first queries containment and obtains a firmware lease. The driver then
resets its host and establishes the internal clock. Its `PrepareClock` callback
asks firmware to prepare the platform PHY before enabling card clocks or
issuing commands. Input clock and all platform qualifications come from the
matched trusted board profile; the wire tag supplies no authentication.

`Quiesce` fences new/queued reads, finishes the current sector and stops the
PIO driver. Firmware then independently checks drain and reset containment.
The returned `LastReceipt` uses the same owner/generation as bridge control.
`Resume` takes a new lease, reruns controller/card initialization, and requires
the original CID and capacity before the same FAT volume can read again.

Any uncertain transport, malformed receipt, driver failure, firmware failure
or changed card retires the session. Its last known receipt is retained with
valid=false. No automatic stop, retry or reconstruction adopts an active lease.
A non-nil retired Session returned with an Open error retains this information.
The caller retains unresolved ownership until a separately qualified recovery.

This first mode keeps all bridges reset. Loading packages into retained CPU RAM
before configuration can use it once the real platform hooks are qualified.
Concurrent SD/core access and adoption by the native fabric actor are pending.
