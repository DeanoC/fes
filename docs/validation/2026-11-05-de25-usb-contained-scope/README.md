# Complete USB containment inside paired cold samples

The previous ROM hardware trace hit its unchanged one-second limit after the
last accepted-byte observation reached 416 of 8,192 bytes. This change reduces
one nested ownership cost; it does not claim to repair the complete transfer.

The trusted USB board supplier now supplies an optional complete read-only
containment callback. Its two fresh 13-register clock scans, two DMA/IRQ/PIO
checks and bounded AHB-idle observations run between ordered cold samples.
Every controller access also checks the USB module and packet-RAM reset pair.
Private boot owner/epoch, expected bridge targets, CPU identity, device
reservations and platform generation/active/confirmed state are rechecked
before publication. The shared composition captures the callback identity at
initialization and rejects removal or substitution before invoking it.

Unbound suppliers retain the existing separate path. Failure of a selected
complete observation retires the platform without trying another path. No
state is cached between calls; no reset, write, PHY request, readiness grant,
wire operation, ROM timeout or poll allowance is added. The ROM driver and its
per-access ownership brackets remain unchanged.

The actual C board model compares complete and separate observations: 26 versus
182 cold-register reads, with 26 clock-register reads in both cases. Physical
board reads increase from 38 to 42 because all eight controller reads now have
a separate reset guard. This is a count comparison, not a hardware timing
measurement. The model exercises every physical observation read failure and
both complete cold samples, 25 mid-observation drift cases, rejected foreign
bindings, five changed platform-state cases and bounded idle-poll exhaustion.
Existing full pin/PHY readiness and failed-object exclusion checks still pass.

The final checks include the USB/cold/shared-boot/master and coherent-dispatcher
suites, an additional size-optimized C execution with undefined-behavior
sanitization, and the existing 4/8 KiB C/RTL ROM verifier. The shared observer
matrix additionally rejects both missing and substituted containment callbacks.
Evidence is under `out/nano-rom-coherent-ownership-20261105`.

No hardware operation was performed for this change. The failed image remains
retired on TRWU8CBL, with its claim and spare card retained and its host resources
cleaned up. The next step is to consolidate the ROM-specific ownership path
against the trusted coherent boot binding, test its failures, then rebuild a
matched chain for a fresh bounded trial. Ordinary admission, physical ROM
completion and DE10-equivalent core loading remain open.
