These are unchanged network sources from the pinned Terasic U-Boot commit
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`. Their own license headers apply.
The recovery-return tests combine them with `../nano-recovery-phy`, then apply
the existing PHY preparation before the separate return-observation layer.
`platform/embedded/nano/recovery_return_trace.py` pins all input hashes.

Tests compile actual success/return and XGMAC stop source blocks against host
callbacks. Matching callback order does not establish physical DMA quiescence,
timer behavior or the cause of the retained hardware timeout.
