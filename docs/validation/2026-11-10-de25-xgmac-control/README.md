# Ordered XGMAC driver stop and qualified restart prototype

`Device.BindControl` now joins the actual polling XGMAC driver to explicit
platform Stop/Start callbacks for the existing network controller. Bind once
during device creation, before packet workers start. The trusted platform must
already own the initial MAC grant; the API does not establish that grant or
supply firmware callbacks. The board remains unbound and Ethernet disabled.

The driver follows the matched BSP's ordered MAC stages: stop TX DMA, wait for
transmit FIFO/read-state readiness, disable MAC TX/RX, wait for receive FIFO
empty, stop RX DMA, order writes and check all four enable bits. Each FIFO wait
keeps a 100 ms deadline and finite poll bound, plus context and retirement
checks. Cancellation on a read/write boundary stops later stages. The BSP's
`void xgmac_stop` does not report loop expiry; this implementation treats expiry
as terminal. FIFO readiness and stopped start bits are never a DMA fence.

Only then may the platform Stop callback independently prove interconnect DMA
and transactions drained. Its success may revoke register access. The driver
performs no further MMIO or descriptor access while contained; Receive/Transmit
return paused. Memory remains pinned, with device-owned RX descriptors neither
reclaimed nor treated as drain acknowledgements. Repeating a known stop makes
no callback. Restart obtains a new platform grant before any MMIO or descriptor
reinitialization, verifies the original memory geometry and exact PHY, and
reuses the same arena. The IP stack/provider remains attached. Unknown stop or
restart retires without register cleanup, callback retry or new buffer reuse.
Overlapping raw control attempts also retire the pending result rather than
allowing it to publish success after authority changed.

The matched Terasic U-Boot revision is
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`. The retained source header places
MTL TX/RX debug at `0x1108`/`0x1148`; its masks and read-state predicate define
the driver checks. The same BSP uses RX buffer shift 4 and its primary Ethernet
alias selects gmac0, with PHY address 1 in the board DTS. These are source facts,
not physical identity or DMA qualification; the existing synthetic PHY-4 model
and explicit layout selection are not changed into a board profile. Exact
source paths/hashes and the prior BSP build receipt are retained in
`out/nano-xgmac-control-20261110/bsp-reference.json`.

Race tests cover ordering, short FIFO waits, timeouts with running and frozen
clocks, stop readback, cancellation on busy/empty FIFO reads and MAC writes,
missing/duplicate binding, unpaired restart, lost platform replies, changed
PHY/window, overlapping control and no MMIO/memory access under a revoked
grant. Three restart cycles preserve the original backing allocation and the
unused window sentinel. The actual HTTP descriptor/IP-stack test now uses this
real driver adapter across three stop/reinitialize/request cycles; its platform
callbacks still model independent drain/grant and supply no physical proof.
The first HTTP attempt found an expected busy binding because it attached after
polling began. The test now follows the factory's pre-worker binding order;
that original failed log remains retained.

Commands, source snapshots and results remain under
`out/nano-xgmac-control-20261110/`, indexed by `validation.json`.
All six affected checks pass, including parent Python regressions and generated
consumer consistency. The complete XGMAC/network race suites pass independently;
selected source snapshots and BSP references remain unchanged across validation.
The full Nano ARM64 launcher cross-build passes from committed source
`762efbfc11696289a761f69772c4235ed308462b`, based on
`e2137b54bbedb34cefd78405758b5a491918bdcc`. All 2121 compiler input hashes
match the current source. ELF SHA-256:
`c801ff31904fdfcf52332a2a1d9d4ded579a67489f069da48e5edc024ce3a156`.
Network stays disabled, SD read-only and preferences volatile. No matched
entry/BL31/FIT/SOF was rebuilt or deployed; cross-building is not physical proof.
No kit/card/programmer or remote builder was touched. The source-bound firmware
MAC lease, private peripheral generation, active-core coexistence and independent
DMA fence are still required before selecting native Ethernet. Existing boot,
reset, memory and producer checks were not weakened. Physical SD writes,
keyboard/HDMI and DE10-equivalent appliance core-load acceptance remain open.
Hardware testing stays paused until the operator returns.
