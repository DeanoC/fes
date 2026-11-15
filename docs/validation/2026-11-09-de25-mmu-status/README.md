# Read-only memory-error capture around native MMU activation

The preceding physical image `07e1efeab6cc2b0d` activated its selected EL3
translation/cache policy, returned to C, then parked after an asynchronous
SError (`ESR=be000011`). The interrupted address falls inside `tf_log`, but
neither that instruction nor the unqualified FAR identifies the failing access.
The previous handler's SP was SP_EL3 while the interrupted code used SP_EL0.
That image and both programming operations remain consumed and retired.

The selected format5 firmware captures both stack banks and adds a fixed
read-only inventory before activation and after a fault. Sixteen 32-bit reads
observe the previously reviewed CCU/DSU, OCRAM and ECC-manager registers.
Optional CPU RAS reads inspect the current bank only, after feature, CPU,
record-count and selector guards. No selector, error status, detector control
or ECC initialization register is written. Unsupported RAS reads are explicitly
skipped. A printed prefix identifies a status read that does not return.

The existing stack-free transition and fault handlers retain the 384-byte
per-line budget, 100000 UART polls per character, nested permanent-park vector,
and exact restoration/readback of the original runtime vector on success.
The observer requires a complete baseline inventory before accepting activation.
A complete fault frame remains available even after an interrupted status line;
faults and subsequent inventories can never restore boot admission.

The firmware's CPU/ownership guards, static mapping policy, memory probe,
secure ROM/tape engines and one-second tape deadline are unchanged. This
diagnostic does not initialize memory, acknowledge errors or change permissions
to work around the observed fault.

Validation is retained under `out/nano-mmu-status-20261109`:

- Focused status, boot, linked-code and joined-provider tests pass. The boot
  observer also covers an interrupted hexadecimal value followed by a complete
  fault frame and a fault-frame flood, with admission remaining closed.
- Twenty-one actual-assembly QEMU cases pass, covering both stack banks,
  supported/skipped RAS fixtures, and nested faults during UART/status capture.
  QEMU uses an A53, substituted read targets and injected RAS values; this does
  not qualify physical Nano error reporting.
- The actual linked Nano BL31 passes its verifier. One hundred mutations of
  read addresses/widths, status writes, selector writes, SP_EL0 capture and
  terminal routing are rejected.
- Parent software checks and affected native-fabric/producer/board/firmware
  integrations pass. Unchanged FPGA software coverage is reused only after
  checking all tracked source hashes and membership against its retained proof.
- Fresh recovery builds reproduce the qualified firmware/FIT/SOF bytes and
  first 16 MiB of the installed recovery image.

The first U-Boot assembly attempt stopped because its source closure changed
during derivation. Its original snapshots were not retained, so the specific
change is unknown. A fresh sampled derivation passes with zero file or tree
changes and exactly matches the new BL31 profile; no guard was weakened.
The earlier diagnostic fixture failures and failed build remain retained.

Hardware qualification of format5 remains open. A matched build and a new
one-use bounded procedure are required before programming TRWU8CBL. Full core
loading, visible menu/core transitions, input/media and appliance acceptance
remain the goal; software checks alone do not establish them.
