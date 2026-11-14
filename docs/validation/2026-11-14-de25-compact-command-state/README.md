# DE25 private command-state layout

The first full normal build at `63c9e242f5d4ac852faf1965218a5f6393e4be64`
successfully rebuilt the SDK and CPU entry, then failed linking BL31. Its
private NOBITS region ended at `0x60080`, 128 bytes past the fixed `0x60000`
limit. That source remains frozen in `de25-publication-proof`; no later stage
or hardware operation was executed. Its object-only predecessor checks did
not establish that the complete image fit.

The queue controller stores flags, six-slot counts, eleven stages and the
selected policy in byte-sized fields. Read/poll/sample counters retain their
full width; clocks and identities remain 64-bit. Natural alignment remains,
without packed structures. The private controller is not a wire ABI, and all
its fields remain included in the existing pinned engine snapshots.

On the selected 64-bit ABI the controller shrinks from 224 to 192 bytes.
A compile-time size gate protects that budget. No observation counter is
clipped, no checks or fields are removed, and deadlines and source-release
policy are unchanged. The original 64 KiB private region, its four 8 KiB
stacks, memory mappings and CPU-release rules remain unchanged.

All 31 sanitized source/retirement tests pass with this layout, including
generation/wrap, callback mutation, ownership, cost, publication and retained
failure/replay cases. A private copy of the failed vendor tree, with only the
current header replaced, links successfully: NOBITS is `0x50000..0x5ff40`,
leaving 192 bytes. The source region and stack boundaries are checked from
the resulting ELF symbols. This preview deliberately has stale producer
bindings and is not admitted as a boot image. A fresh source-bound matched
build is recorded in the [subsequent matched candidate](../2026-11-14-de25-publication-matched/README.md).
Whole-EL3 stack and physical behavior remain unqualified.

## Software-reset investigation

The Linux branch's recovery procedure reloads the verified image containing
the HPS loader after cold reset. Its persistent HPS-first QSPI bootstrap also
supports recorded ordinary reboots. These are artifact-specific Linux results;
the top kit's QSPI is unchanged by this work. See
[the Linux recovery investigation](../2026-10-30-de25-reboot/README.md) and
[persistent bootstrap](../../de25-reboot-bootstrap.md).

Altera's [mailbox-triggered cold-reset description](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/hps-mailbox-triggered-cold-reset?contentId=Kmc34tJYkB5dm0qQf~OeWg)
also requires redelivery of the bitstream containing the first-stage loader
through the interface selected at power-on. This supports testing reset and
loader redelivery as a complete procedure; it does not qualify our failed
secure-call state or replace the fresh device/ownership checks.

The native failed source call remains inside `fes_nano_owned_call` while
holding `fes_nano_owned_lock`; its terminal branch waits indefinitely without
returning or releasing buffers. The ordinary PSCI cold-reset wrapper first
calls `fes_nano_bridge_platform_invalidate`, which acquires the same lock.
Therefore directly invoking that ordinary wrapper from this parked call would
attempt recursive acquisition before reaching the SDM command. This is a
source-derived reason to design a recovery entry that respects the held lock,
not a hardware observation of a reset request.

A future recovery prototype needs a separately selected one-shot cold-reset
entry under the already held lock, retained failure state, the exact vendor
mailbox path and bootloader redelivery. It must not resume ordinary commands
or reuse the source on an uncertain result. Both SMMU banks and the new boot
epoch need fresh admission before recovery can substitute for a physical
cycle. The existing read-only recovery gate observes only the Non-secure bank.
No reset command or changed recovery behavior is implemented or executed here.
