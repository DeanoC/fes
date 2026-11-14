# Contained decode recording around late MMU activation

The preceding [physical status test](../2026-11-09-de25-mmu-status-trial/analysis.md)
captured the same post-activation SError with an in-range interrupted SP_EL0.
Both complete status inventories were unchanged, while the sampled detection
controls were disabled. The `58a43a2afe6818d7` image and its operations remain
consumed and retired.

The selected format6 diagnostic gives late MMU activation a separate recording
lifetime. It requires zero DSU detection, interrupt and status registers and
sets only CAIUUEDR.DecErrDetEn, bit3 at `1c000100`. The retained official register
map identifies this bit as decode detection; the interrupt register and sticky
status are never written. This is the same reviewed bit used by the earlier
startup diagnostic, with a new lifetime spanning translated C execution.

Intent, active, restoring and retired states occupy a distinct initialized
eight-byte private DDR slot. CPU/MMU, single-CPU release and all-master reset/
power checks frame control writes. Normal cleanup and terminal fault cleanup
restore control0, verify readback, unchanged interrupts and preserved sticky
status. A newly recorded error prevents normal boot even if exception delivery
is delayed. Lost ownership, failed readback or nested faults park; restoration
is unproved when its sequence cannot complete. A restoring lifetime cannot retry.

The observer requires ordered arm/restore receipts at the corresponding MMU
checkpoints. Fault cleanup remains diagnostic and cannot reopen admission.
The final ELF checker restricts control and private-state writes to separately
declared sites with exact addresses, values, initialized storage and barriers.
Existing UART budgets, permanent nested-fault vectors and handler-size caps
remain. A local branch trampoline keeps short conditional branches within their
architectural range as the inline diagnostic grows.

Evidence under `out/nano-mmu-logging-20261109` includes focused observer/code
checks, the joined real-provider fixture, 35 emulated assembly cases and 152
actual linked-Nano-ELF mutation rejections. Injected cases include ignored arm/
restore writes, foreign controls, pending status, lost reset ownership, late
logged errors, interrupted restoration and a nested fault during cleanup.
Control models are RAM cells and explicit instruction mutations; they do not
qualify the physical CCU. An explanatory comment changed after execution; a
separate comparison proves identical stripped linked images and every global
symbol, retaining the original execution source and the failed filename-sensitive
comparison. Source-file symbols account for the unstripped ELF difference.

The initial and shared-cleanup-only builds exceeded the original 8 MiB FIT
limit and were never uploaded. Their source, images and failures remain
retained. Fixed UART addresses now initialize once per leaf entry, removing
duplicated inline constants while retaining every CPU/ownership guard, UART
poll/byte limit and fault path. Capture and restoration reinitialize those
registers after C execution; explicit clobber cases exercise both paths.

The final native firmware is 315733 bytes and passes its production verifier. The
bootloader, matched composition, broader software checks and new one-use physical
procedure must complete before claiming board qualification. CPU/owner guards,
memory policy, memory probe, ROM/tape engines and the one-second tape deadline
remain unchanged. Reliable core loading and full appliance acceptance remain open.
