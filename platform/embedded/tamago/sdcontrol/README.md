# Firmware-owned SD PIO session

This package connects the actual `sdhci.Device` to the experimental
[shared SD lease](../../../../sources/mister-packages/docs/embedded-sd.md).
It is a runtime adapter with injectable firmware transport and registers.
The matched Nano SD profile selects its real register/PHY callbacks. Other
profiles supply no SD callbacks by default. The cold read/contain/resume path
has separate hardware evidence; model tests and a linked firmware build do not
establish physical acceptance for a changed path.

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

`ResumeAfterReturn(owner, generation)` crosses a completed core return explicitly.
It requires a newer, same-owner closed receipt, queries its exact live generation
and requests the grant once. Secure firmware separately requires its own successful
producer-stop and control-only bridge closure, with data continuously contained.
The real PIO driver then rediscovers the original CID/capacity. Reopening during
an active core, adopting a different owner and replaying an uncertain reply are
rejected. This returned-core path has software coverage, including three cycles
through the actual C service and Go PIO driver/card model; hardware qualification
is pending.

Any uncertain transport, malformed receipt, driver failure, firmware failure
or changed card retires the session. Its last known receipt is retained with
valid=false. No automatic stop, retry or reconstruction adopts an active lease.
A non-nil retired Session returned with an Open error retains this information.
The caller retains unresolved ownership until a separately qualified recovery.

Both modes keep all bridges reset while SD is leased. Concurrent SD/core access
and adoption by the normal native fabric actor are pending.

Run host checks with `go test -race -count=1 -timeout 90s ./sdcontrol ./sdhci
./fat` from the TamaGo module. The SD Session tests compile the actual C
firmware/dispatcher with the host C compiler; `-count=1` prevents Go's result
cache from hiding changes to those external C inputs. The affected runner uses
this command automatically.
