# Reusable storage for live mapping observations

The previous physical game-tape trial stopped at the one-second media-operation
limit before a verified computer launch. Its last register access spent about
487 microseconds in the first ownership observation, 285 in mapping/register
access and 495 in the final ownership observation. That failed image and its
consumed procedure remain unchanged. This change targets allocation overhead
inside mapping inspection; it is not a claim to have fixed that board failure.

The inspector previously passed a new local 16-byte record through the hash
interface for every page-table read. It now reuses one record and digest-output
buffer, uses five fixed bookkeeping slots for the five permitted table pages,
and resets that bookkeeping and the hash for the second sample. No observation
or permission is cached across samples or register accesses. The descriptor
stream, before/after CPU checks, both boundary translations, firmware exclusion,
alias audit, ownership checks, bounds and failure/retirement behavior remain.

Against base `762959ed6b854adc07cc964d962b054ada7bf310`, the host benchmark of a
four-byte control mapping plus the complete 64 KiB firmware exclusion produced
the same CPU state and descriptor SHA-256, with 100 table reads and 148 hardware
translation callbacks per observation. Allocations fell from 108 to 2 and
allocated bytes from 2,304 to 288. Three 10,000-iteration runs had median host
times of 6,403 and 5,361 ns/op, a 16.27% reduction. This injected-table benchmark
measures software implementation cost, not physical MMIO or secure-call latency.

The regression test bounds allocation growth while requiring identical repeated
observations. Existing tests exercise every firmware page/read-write boundary,
changed CPU/table state, aliases, failed probes/publication and retirement.
The actual AArch64 bare-metal QEMU mapcheck also passes: heap, UART and uncached
DMA mappings, live read/write translations and an unaligned DMA copy. QEMU uses
its own RAM layout and does not qualify Nano firmware exclusion or performance.

Raw logs, benchmark comparison and the source-bound QEMU result are under
`out/nano-mapping-scratch-20261108/`. The accompanying result records the final
affected-check outcome. No board, card, programmer or shared tool lease was used.
The goal remains incomplete. Firmware ownership observations dominate the saved
failure timing, and their transfer cost still needs a concrete solution before
building and attempting another matched chain. Physical tape delivery/ROM
consumption, visible controls/output, broader admission and appliance acceptance
remain open.
