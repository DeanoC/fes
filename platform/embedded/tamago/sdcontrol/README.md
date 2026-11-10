# Firmware-owned SD PIO session

This package connects the actual `sdhci.Device` to the experimental
[shared SD lease](../../../../sources/mister-packages/docs/embedded-sd.md).
It is a runtime adapter with injectable firmware transport and registers.
The matched Nano SD profile selects its real register/PHY callbacks. Other
profiles supply no SD callbacks by default. The cold read/contain/resume path
has separate hardware evidence; model tests and a linked firmware build do not
establish physical acceptance for a changed path.

`Open` takes a trusted `Config`, queries containment and obtains a firmware
lease. `InputClockHz` supplies the actual controller input; zero `WriteCard`
selects read-only access, including in the current Nano board. Package metadata
cannot choose access. To authorize writes, the trusted caller supplies an
independently expected card CID and sector count in `WriteCard`. Malformed
identities reject before firmware or register access. Fresh card discovery must
match both fields before a usable writable session is returned. A mismatch
retires the session and retains its invalid receipt and actual card identity;
it issues no sector write, automatic stop or retry. During discovery, the driver
resets its host and establishes the internal clock. Its `PrepareClock` callback
asks firmware to prepare the platform PHY before enabling card clocks or
issuing commands. Input clock and all platform qualifications come from the
matched trusted board profile; the wire tag supplies no authentication.

An admitted `WriteCard` connects the actual single-sector PIO writer to the same private
lease and stop/resume lifecycle. No new firmware call, DMA grant or concurrent
core/SD access is introduced. Known protection/range/read-only refusals preserve
a healthy session. An uncertain write retires both driver and session, retains
the last receipt as invalid, and performs no automatic firmware stop or retry.
Host tests join actual C firmware, PIO write/readback and a real FAT32 settings
update/remount, including independent file readback. They also reject wrong
initial CID/capacity, a replacement card on resume and physical write protection.
Expected identity is a media-ownership check, not cryptographic card authentication
or a restriction on which LBAs an admitted writer may modify. The platform must
still establish where the expected identity came from and which writes it owns.
Ordinary startup does not select this mode. The dedicated plan-bound Nano
writer now passes the bounded eight-sector firmware/PIO lifecycle, with a
[separate fresh recovery readback](../../../../docs/validation/2026-11-10-de25-sd-independent-readback/README.md).
The trusted board propagates its explicit no-mechanical-WP wiring property;
default socket checks and card CSD protection remain separate. This is limited
raw-sector evidence, not FAT/install or power-loss qualification. A completed
sector or batch is not power-loss atomicity.

Trusted `Config.TransferClockHz=25000000` now forwards the driver's separate
default-speed experiment through the actual owned session. Invalid rates or
inadequate input clocks reject before firmware or MMIO. Discovery/PHY setup
stay at identification speed; data requests check the selected divider. The
driver restores the original disabled identification clock before secure Stop,
and same-card resume selects the faster rate again. Actual C-service/PIO tests
cover read-only and writable lifecycles, FAT persistence, divider/stability and
restoration failures, replacement cards and no fallback or automatic stop.
The Nano `sdfile --sd-transfer-clock 25000000` diagnostic is the only build
selection; ordinary startup remains unchanged. Physical qualification of this
changed clock path is separate from the previous 400 kHz write evidence.

`Quiesce` fences new/queued reads and writes, finishes the current sector and stops the
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

The separate `sdwrite` executable now connects the actual writer to a bounded
spare-card diagnostic. `ScratchPlan` binds the expected CID, capacity, MBR hash
and original contents of the final eight sectors. Before writing, it requires
one supported FAT MBR partition, no overlapping/out-of-range partitions, and
all partitions ending before that scratch region. It rejects protective GPT,
extended/unknown partition types and an already-written test pattern. This
partition check alone does not establish that other unpartitioned firmware
data is disposable; the operator's spare-card layout remains a prerequisite.

`RunScratch` writes eight sectors once, flushes and reads them back, contains
and resumes through the actual firmware lease, checks the data and MBR again,
and ends contained. Receipt ownership and exact generation changes are checked
at each logged stage. Failures perform no rollback, retry or automatic stop.
The pattern stays on the card for independent recovery readback. A successful
plan is consumed and cannot be replayed against those changed original bytes.
This is not FAT write, power-loss or ordinary installer qualification.

The existing `storage --sd-scratch-inspect` diagnostic obtains candidate plan
data through its read-only session and then completes its usual contain/resume
test. Its output grants no write authority. Build `sdwrite --sd-write-plan FILE`
separately after reviewing the real spare-card identity, layout and original
hashes. The plan is compiled into the native executable and checked against the
build receipt. Ordinary `OpenStorage` and core-loading images remain read-only.
Both modes require the closed SD-only Nano profile and are rejected by QEMU.

The separate [`sdfile` diagnostic](../fatcheck/README.md) now joins new FAT file
allocation, shrinking updates and a fresh post-resume mount to this same owned
writer. It requires its own compiled card/file plan and an absent unique target;
raw scratch-write evidence does not qualify that file path physically.

Run host checks with `go test -race -count=1 -timeout 90s ./sdcontrol ./sdhci
./fat ./fatcheck` from the TamaGo module. The SD Session tests compile the actual C
firmware/dispatcher with the host C compiler; `-count=1` prevents Go's result
cache from hiding changes to those external C inputs. The affected runner uses
this command automatically.
