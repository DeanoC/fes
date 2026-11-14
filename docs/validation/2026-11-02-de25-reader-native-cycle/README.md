# Native SD-package configuration and on-chip reader cycle

The bounded TRWU8CBL test passed on 2026-11-02. Native firmware verified the
retained SD package, completed all 2,027,520 FPGA bytes, matched the
live sealed identity, and exercised the on-chip reader's held address and
partial burst before accepting drain. Control reclosed with all data bridges
held. The saved trace was independently decoded and cleanup checked.

This is a contained physical diagnostic. DDR and other-master drain, data
release, runtime package admission, repeated core loads, physical launch/return
and failure recovery, and complete peripheral/UI acceptance remain open.

The compiled native source is `b08fc1143ddd3e6909ad31a3aced3d89bba31429`; the checkout is `6c12141dab602c2a3257af0c2d103e7228eca9e9`, which
adds only validation records. The six build stages and eight software groups in
the [matched preparation](../2026-11-02-de25-reader-native-preparation/README.md)
were reused unchanged. No successful build or software campaign was repeated.
The sealed package/core and pinned source maps were rechecked before access.
Ownership remains `lane:deano`.

The [first attempt](../2026-11-02-de25-reader-native-preparation/experiment.json)
stopped at recovery programming with a broken JTAG chain before any native
candidate programming or `fesboot`. Its FIT upload/readback, raw failure and
cleanup are preserved. Two subsequent read-only queries identified the exact
top chain. This second, bounded attempt first proved a new known recovery boot.
It then uploaded/read back `FES/PKGTEST.BIN` and freshly read/hashed the existing
`FES/UPLOAD.BIN` within that same qualified session. The old FIT receipt alone
could not authorize native boot. This avoids a second upload/recovery cycle;
the JTAG failure's root cause remains unproven. Neither attempt fuse was reused.

The [new experiment](experiment.json) shows one recovery boot, one package
upload/readback and one native attempt. The [fresh FIT receipt](live-fit.json)
binds the carried-forward upload, current recovery and current package receipt;
it contains only SD read/hash commands. Protected prefix, factory boot FIT and
recovery manifest checks passed. Network PHY, negotiated link and fixed MAC
were checked before transfer.

The [raw native trace](native-console.log) and [native receipt](native.json)
establish:

- Retained format-2 package identity and SD containment at generation 4.
- Full 2,027,520-byte transfer, the exact core digest, bounded write
  window and mailbox observations, complete SDM status and live FPGA identity.
- 83 extra read accesses and
  12 one-shot writes through the real secure
  reader operation. Reader generation 1/cookie 7 has no ACK with the address
  held or only the first beat complete. The final ACK requires zero pending
  requests and both final identity/state snapshots.
- Same-owner control-only opening at generation 5 and reclose at generation 6;
  all data reset bits remain asserted. The final reader receipt is complete.

The transfer observation interval was 16.358576
seconds, including 11.645006 seconds
attributed to hashing within that interval. Its firmware deadline remains 20
seconds. The 72.089539-second package observation
includes native startup and package verification; it is not raw SD throughput.
The existing 100-second storage and 45-second configuration/reader host windows
were unchanged.

All 99 focused helper cases passed: 46 native admission/late-failure cases,
37 resumed orchestration cases and 16 fresh-readback cases. Injected stale,
missing or replayed records, ownership loss, wrong hashes, early ACK, partial
responses, late failures and cleanup errors cannot grant success or a second
entry command. These negative cases are host fixtures, not physical fault
injection. The source-bound actual C/strong-provider/Avalon-RTL evidence remains
in the preceding preparation and reader-cycle records.

The [independent audit](cleanup.json) redecoded the saved bytes and verified
the original 15 MHz clock readback, root USB ownership/permissions and free
UART. The board is left in the completed native diagnostic with the spare
card inserted and exclusive top-board claim retained. Its factory card remains
safe outside. No factory restore, QSPI write, second CPU release or other-board
operation occurred. Native data-plane activation remains unavailable.

The next integration gate is a contained, explicitly bounded DDR/master path
with real bridge release and stop/drain evidence, followed by runtime admission
and repeated load/return. The on-chip responder does not satisfy that gate.
Source, helpers, receipts and raw traces are retained under
`/home/deano/fes-retained/de25-reader-native-resume-20261102`; the unchanged
compiled components remain in the [verified build archive](build-retention.json).
