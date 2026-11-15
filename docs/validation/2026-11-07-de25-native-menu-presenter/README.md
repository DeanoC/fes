# Native owned-menu presenter result

The native `scanout.MenuPresenter` now connects CPU frame publication to the
private FMS1 menu controls and actual 720p video/DDR RTL. Implementation source
is `cc053761ef2b6f112e195eada7fcd8eb0cde346e`. The follow-up source
`85516d62c48404406924cda835240ce9e68e1ef3` changes only checksum-verifier fixture
selection; all Go, CSR, video and bus logic is byte-identical between them.

The presenter accepts trusted platform register, memory and ownership hooks.
It requires the compiled 8 MiB geometry and exact live FPGA identity/generation,
copies only the spare 4 MiB slot through the existing bounded XRGB copier, orders
publication, submits once and waits for a coherent complete-frame acknowledgement.
It waits separately for submission readiness during the engine's repeat-frame
prefetch. A copied presenter cannot fork its owner, and concurrent calls reject
before I/O. Stop requests local Quiesce once, waits for the real reader to stop,
then obtains the existing endpoint's exact generation/cookie drain. Slots remain
pinned; independent firmware master-stop and bridge closure are still required.

The committed integration run passes 29 native cases with Go race detection.
CPU writes fill independent model memory; publication and all active output
pixels must match the actual Tenfoot renderings. Three normal presentations
complete with generation 1, sequences 1/2/3 and slots 0/1/0, then local stop and
endpoint drain with cookie 7. The model checks at least 2,764,800 output pixels
and forbids CPU writes to the currently fetched slot, queued DDR traffic or a
held request. The separate direct video oracle also checks 2,764,800 exact pixels
through the Nano 128/256-bit adapter; its premature-ack mutation must fail.

Failure cases cover partial copy/publication, completed-but-uncertain Start,
Submit and Stop, canceled publication, ownership loss after copy/completion,
backwards time, corrupt identity/geometry/coherent status, invalid CSR accesses,
missing/error responses, a proven stalled address, source-only reset and copied
or concurrent handles. Failed operations return no frame/drain receipt, retire
the presenter and cannot replay platform I/O. All phases share a monotonic
one-second deadline and at most 64 polls; platform hooks must themselves return
within their bounds.

The pinned TamaGo compiler also produces a 366,598-byte `tamago arm64` library
archive on the implementation source. This proves target compilation, not boot
or board integration. The [result](result.json) records its compiler/archive
hashes, 483 selected source hashes, final model/log hashes and individual check
sources.

All nine effective affected checks pass across the implementation and the small
fixture-selection follow-up, totaling 811.282 seconds. Parent regressions pass
1,617 tests (3 skipped) on the implementation source; FPGA Python regressions
pass 1,590 tests (78 skipped) after the follow-up. Generated definitions are
unchanged and consistent. The menu verifier and existing producer/window checks
pass on the follow-up source. The latter retains all 24 actual Avalon cases and
its strict no-skip assertion. It excludes only the separately bound menu test
group from the checksum fixture's Go invocation. The menu group independently
requires all its cases to pass with no skips. Per-command source revisions are
recorded rather than attributing the earlier parent run to a later commit.

The initial host runs and their logs remain retained. They exposed the distinction
between full display completion and transient submission readiness, and a stop
negative control which had not yet observed an actual held address. The driver
now waits for readiness separately, and the negative control first observes
ARVALID with ARREADY low. The original broader verifier failure also remains:
all 24 checksum cases passed, but the new menu cases lacked their own fixture
and skipped. No hardware procedure was involved or replayed.

No Nano command, boot firmware or package selects this presenter yet. Its secure
8 MiB reservation, independent global return, sealed FPGA producer integration,
pixel-clock crossings and physical HDMI still need implementation/qualification.
No shared public FES ABI or programming profile changed. This is native-code and
actual-RTL host evidence, not ordinary package, physical display, DDR/timing or
full DE25 core-load acceptance. TRWU8CBL was not touched by this milestone.
