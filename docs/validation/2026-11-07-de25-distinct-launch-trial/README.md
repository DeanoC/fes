# Bounded distinct-package native launch procedure

This newly frozen procedure prepares one TRWU8CBL attempt using the matched
[native chain](../2026-11-07-de25-distinct-native-chain/README.md) and two
independently sealed standard computer packages. Its intended sequence is
nine recovery uploads and fresh SD reads, then serial Tenfoot launch/return
for ZX81 → ZX80 → ZX81 and owned Quit. No hardware attempt is established
by this preparation record.

The new file is `FES/PKGALT.BIN`, an existing bounded recovery slot. Both actual
package/build/core/payload identities and their 8/4 KiB ROM receipts are
checked independently against the compiled version-two platform. The earlier
same-package alternate ROM's fixed-zero fetch expectation is removed; the
normal bounded fetch observation remains required. Neither archive can be
relabelled as the other package. All writes and fresh reads must complete in
one owned session before native programming.

Fourteen off-board fixture groups pass, including 96 actual uploader cases,
400 rehashed receipt mutations, 46 source/provenance rejections, seven
stale/damaged real-file plans and 16 outer lifecycle/cleanup cases. The
eight-trace fixture initially rejected the updated nine-file positive control;
the corrected fixture derives its trace count from the real nine-purpose
inventory. Its failed run is retained. Hardware was not involved.

The full affected command passes on code publication
`c9d797a1aaa516def6e99b19fde7be9cfaa2207c`: all ten planned commands complete,
including 1,614 parent tests with three environment skips, 1,590 FPGA Python
tests with 78 environment skips, generated-consumer consistency and both
embedded/native integration lanes. Later changes here are validation documents
and the ignored, hash-frozen operator files. These results do not waive the
four remaining FPGA timing/signoff findings.

The frozen [operator source](operator-source.json) retains the exact source and
input hashes. Six qualified safety helpers remain byte-identical, including
native execution, per-CPU checks, private JTAG, recovery, service-result and
clock handling. Upload receipt checks change only to cover the additional
fixed package slot. Existing USB policies remain unchanged.

The attempt remains single-use, with no automatic retry. Prompt/startup/launch/
configuration/control/native/process/outer/host bounds remain
50/100/100/20/5/450/480/740/850 seconds. First failure stops outgoing commands;
the bounded passive tail and exact clock, USB permission, private-JTAG and
UART cleanup remain mandatory. Recovery executable files keep their exact
pinned Git identities; the non-executable Nano README separately uses the
native implementation's reviewed documentation identity.

Working files are in `out/nano-distinct-launch-board-20261107/`. Physical
execution requires clean committed source, the continuing top-kit claim,
current adapter identity, a free UART, the exact prepared card/recovery files
and one recorded preflight before the attempt fuse is consumed. Factory
restoration remains waived by Deano; the factory card and bottom kit remain
outside this test. Physical display/input/media/native-network behavior,
general package acceptance, DDR drain and timing qualification remain open.
