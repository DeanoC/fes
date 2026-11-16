# DE25 original-FIT restoration stopped by native USB panic

After a fresh human-confirmed physical reset following the traced warm-preparation failure, restoration-only procedure `229e1b113a24512c792b1a7b3a388b1a587b4f3b0c364158614ca1ff91b5a60e` ran once and stopped after 371.635 seconds without an enclosing host deadline expiry.

Exact original FIT `d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1` is restored. All seventeen protected-file checks before and after the write pass, as do the read-only reset-register gate, original native programming and sealed boot. The candidate FIT is removed. No repaired expected-word image was programmed.

The original runtime then panicked before any baseline cycle command. USB interrupt-IN address 1 / endpoint 1 reports channel error `0x80`; containment subsequently exhausts its 500 ms channel-halt wait. The trace says `reboot required; resources retained`. The complete 51-command launch/return/Park cycle did not run. This USB failure remains unresolved and does not qualify or reject the separately built expected-word repair.

Independent root checks verify all eight owned processes stopped, UART free, both roles' auto/15 MHz JTAG settings, and exact USB device identity/inode/permissions restored. [Actual result](result.json), [process scan](owned-process-root-scan.json), [UART scan](uart-fd-scan.json), and [retention proof](retention-proof.json) record the evidence. The archive `/home/deano/fes-retained/de25-server-warm-restoration-usb-stop-20261114-r1/restoration-stop.tar.gz`, SHA-256 `866fb7e48e13785d7c43d35b608a2cbdf0f57bbdc88f9f89625edb78ccc39ed2`, independently verifies all 149 entries by bytes, hashes and modes. The prepared procedure archive and preceding failed diagnostic archive are retained dependencies.

The consumed diagnostic was not retried. At this checkpoint the spare card holds the original FIT, the runtime is stopped after its USB panic, all host handles are closed, and the continuing top-kit claim is retained. A fresh physical reset is required before read-only original-file and baseline verification. QSPI, factory card and bottom kit remain untouched. No full baseline Park, generic repeated loading or Linux-equivalent acceptance is claimed.

The repaired source's complete matched boot chain and retained archive separately pass; see [matched evidence](../2026-11-14-de25-expected-table-matched/README.md). Broader affected checks are still running at this checkpoint.
