# DE25 native launcher inspection refusal

Bottom TRWU8J8G runs the approved retained development image: kernel
`590a564e` / notes `be3332b7`, root `be191a1f`, factory software `258f478b`
generation36, MENU RBF `6413fd62` and stock Terasic FIT `bb6a48c7`. The
source producer is `edfae407e34fd5e1413257fc26a8ced54063f715`. The owned
host was subsequently updated to that source (`acb01bad`, PID2133120), with
its catalog, pairing and saved programs preserved. These are development
artifacts, not a baseline promoted from main.

Home/ZX81 workbench navigation works. The first UI controller stopped on a
transient host observation failure. One subsequent Start on the current host
returned HTTP500 `MISTER_UNAVAILABLE`; no Start was replayed. The native log
records an accepted execution lease, an HTTP503 core request, and release.
Retained runtime events show inspection and no FPGA programming dispatch.
Both synthetic input helpers were retired and removed. The board was released
verified idle and lease-free, with the launch-error dialog visible.

A read-only `inspect_core` diagnostic using the existing runtime and the exact
standard package `3da0f1df` exposes the cause. Its reply has `ok=true` and a
compatible inspected package, together with a retained top-level MENU error:
grant generation3, hardware errors `0x8`, one underflow. Existing runtime cleanup
had already restored MENU generation4; that generation reports available,
zero underflows and no MENU error. The full native reply is retained as
FogCast's `protocol-v2-de25-recovered-menu-inspection.jsonl` fixture. An initial
diagnostic path outside the configured trusted roots was refused; its separate
receipt remains retained, and the corrected diagnostic uses the normal trusted
staging root. Neither diagnostic loads a core or changes boot/software.

The adapter incorrectly interpreted the retained status error as failure of
the successful inspection. The repair uses the protocol's operation `ok` for
package/data inspection results, while retaining descriptor, package, data and
compatibility validation. Failed operations still fail; status reconciliation
still reports the historical error. No reset/recovery admission, kernel, RTL,
programmer or public protocol contract changes.

The regression reproduces failures in package inspection, data inspection and
bare library launch before the repair. The repaired cases pass; failed package
and data inspections and wrong identities remain refused before dispatch.
The exact native reply also passes the independent socket decoder. The two
regressions pass on native amd64 and AArch64 QEMU. Initial QEMU invocations
with an unavailable PATH command and then the wrong fixture working directory
are retained separately; neither establishes an implementation failure or a
hardware pass.

The underlying underflow is tracked in
[#1594](https://github.com/DeanoC/fes/issues/1594). Its cause and repeated native
launcher/display acceptance remain open. Adapter tests do not establish that
the repaired software runs on the kit: build/install and a fresh native
Start/Stop proof are still required. Failed-transfer containment in
[#1298](https://github.com/DeanoC/fes/issues/1298), owner review and landing
also remain required; only #1568 physical personas are waived.

Private Powerboat evidence is under
`/data/fes-de25-main-integration-20261105/partial-start-investigation-20261108/sdm-exclusive-producer-edfae407e/`:
`native-trial/complete.json`, `current-host-edfae/native-deploy/host-candidate/`,
and `native-ux/refusal-diagnosis/` (raw native reply, request/event logs,
helper/source identities, retirement receipts and AArch64 test log).
