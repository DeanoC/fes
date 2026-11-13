# Native installed-package discovery preparation

The host verifier now distinguishes a first installation from a fresh boot that
already discovers an installed package. Source `d47639349e48c6828f0aaa47a243bd111f2cd911`
adds `installer_cycle` and `installer_recovery` under `platform/embedded/nano`.
No native firmware, Go runtime, FPGA payload or compiled boot image changes.

The first-install cycle opens the source, installs, opens the library, launches
the pinned program, returns to the menu and finally parks. The discovery cycle
opens the existing library and performs the same launch/return without Install.
Both require an independent mode and preserve the complete existing firmware,
menu, input-ownership, core identity and drain checks. Saved-wire replay checks
contiguous byte windows, original commands, polling times and phase deadlines.
Neither cycle alone establishes SD persistence or physical display acceptance.

The read-only recovery helper reads all ten original staged files through the
real UploadSession, including independently expected empty CORESEL.JSN and
UI.JSON. It rejects populated/ambiguous bootstraps and existing upload ledgers
before I/O. It checks each file hash, protected boot files, scratch ownership
and its session token, closes its session on success/failure and retains partial
failure receipts. It sends no network download, SD-write or activation command.
A fresh recovery boot and CID admission remain independent operator obligations.

The ignored `out/nano-installer-discovery-20261111` operator template connects
those checks to a separate recovery and native boot with private JTAG service
lifetimes. It requires a successful prior first-install capture and saved-wire
replay before live admission. It has no preflight or programming grants, and
is **not hardware-ready or admitted**. Preparation/final admission must still
be generated and checked against the then-current source, tools, card and kit.
It must perform only source readback between recovery and native programming;
it cannot upload a new catalogue or issue Install to manufacture discovery.
Any failed native step or transport cleanup leaves persistence unverified.

The actual latest isolated-JTAG trial is rejected as prior install evidence:
its matched firmware boot passed but native startup stopped in USB initialization
before SD open or installation. See [the hardware record](../2026-11-11-de25-native-isolated-jtag-trial/README.md).
The earlier visible launcher return belongs to its archived image and supplies
no installation or persistence verdict for this candidate. The current physical
USB connection question remains unanswered; this work performs no kit operation.

Validation receipts and retention details are in `software-result.json` and
`retention-proof.json` beside this document. All six selected software gates
pass, including 1,935 parent regressions. The focused installer suite passes
34 tests; the discovery transport suite passes six and the read-only operator
suite passes twelve (including the nine imported reader regressions).
The retained preparation is at
`/home/deano/fes-retained/de25-native-discovery-prepared-20261111-r1`.
Archive member hashes/modes and an independent source restore with strict Git
fsck pass. Its source bundle explicitly depends on the two retained bundles
named and hashed in the retention proof; the compiled native candidate is a
separate declared dependency. Focused model and operator tests
exercise success, mismatched discovery context, malformed captures, receipt
corruption, wrong card/content, incomplete reads and failure retirement. Those
are software evidence only. Physical installation, independent fresh-boot
discovery, ordinary core loading and DE10-equivalent appliance readiness remain
open. No push, PR, merge or cleanup of old outputs is performed.
