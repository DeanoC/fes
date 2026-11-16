# Retained Nano ROM publication through the native runtime

The native library now connects a retained format-three Nano firmware ROM and
its private activation receipt to the existing contained program lifecycle.
The explicit ROM verifier validates the complete compiled resources without
I/O. Ordinary boot verification and legacy fixed-source configuration reject
the ROM profile. Selected metadata cannot change platform addresses, master
inventory or stop policy. No new SMC or data release is introduced.

The private arena stages one 4/8 KiB ROM after the aligned FPGA payload, with a
version-two descriptor in the reserved final page. The SDM transfer sends the
unchanged FPGA bytes only. Duplicate or stale callbacks cannot publish another
lease. Uncertain copy, ordering, descriptor and firmware failures retire the
service, retain resources and keep SD contained; they cannot replay activation.
Healthy content rejection occurs before containment and leaves SD usable.

The full nativefabric race suite passed in the isolated prototype and again in
the tracked tree. The actual store, linker, loader, arena and service complete
three 8/4/8 KiB launch/Run/return cycles. Each published descriptor is parsed by
the actual C decoder against separately supplied modeled secure owner,
generation, boot digest and chunk geometry. Negative tests reject independent
context/ownership mismatches, foreign receipt/resources, damaged publication,
stale handles and modeled firmware failure. The C test uses UBSan.

Adjacent packagestore, modelbackend, computer and sdcontrol race suites passed.
The nativefabric library cross-compiles for ARM64 TamaGo. The first cross-build
omitted the existing private module cache and failed before compilation; the
corrected invocation passed. Prototype test preparation failures remain in the
logs rather than being replaced. The source and log hashes are in result.json.
Only SPDX comments were added to two test files after the tracked race run.

These tests use modeled firmware/registers and do not execute the physical ROM
FIFO. Provider/RTL model evidence belongs to the preceding firmware milestone.
No board operation or Quartus build ran. The command and cross-builder still
need explicit ROM selection and resource linking. The producer still needs a
newly sealed format-three package with a real retained receipt, followed by a
matched boot chain and bounded board test. Full DE10-equivalent loading remains
open; the earlier diagnostic FPGA artifact is not relabeled as a new package.
