# Forced live JTAG scan on TRWU8CBL

A single forced scan at 6 MHz passed through a new, top-only private server.
The installed Quartus 26.3 client returned one device, `4362C0DD`, with a
10-bit instruction register. The probe unlocked the chain, restored the
original 15 MHz clock and stopped its private server. An independent audit
verified the saved command sequence, exact original USB permissions and that
the UART was free. It opened no UART and performed no image programming,
CPU reset or SD write. The continuing claim remains held.

Intel's [pinned AJI header](https://github.com/intel/libaji_client/blob/5d9c7824204d1de343071ec28a4363d65156e61e/src/h/aji.h#L154-L164)
maps error 87 to `AJI_BAD_JTAG_CHAIN`; it is distinct from permission and
lock errors. The public
[client implementation](https://github.com/intel/libaji_client/blob/5d9c7824204d1de343071ec28a4363d65156e61e/src/jtag/jtag_client_chain.cpp#L925-L1030)
distinguishes a forced `SCAN_CHAIN` request from a cached-capable `READ_CHAIN`
request. This justified a separate live-scan diagnostic. It does not establish
which internal API the closed-source programmer used in the earlier attempt.
That failed attempt already had a successful `quartus_pgm -a` immediately before
programming; another device-list gate alone would not repair it.

The first probe incorrectly required a cached-chain read after changing the
clock. That read returned 38 (`AJI_CHAIN_NOT_CONFIGURED`); it stopped and cleaned
up before reaching the forced scan. Its original failed result is preserved.
The corrected probe requests the scan directly, then reads its result without
automatic scanning. Seventeen injected API cases check success, first-error
termination, unlock, device identity/geometry rejection and wrong-cable or
environment refusal. Both original source snapshots and the corrected source
were frozen before their respective device access.

[result.json](result.json) binds the physical capture and independent audit.
The [probe source](probe/scan_probe.cpp) calls the installed client library;
its mapped library path and contemporaneous file hash are recorded. Source
copies under `probe/` describe this diagnostic, not a production loader. The
complete executable stages, API headers and primary-source hashes are retained
from `out/nano-jtag-live-scan-20261108` and
`out/nano-jtag-live-scan-v2-20261108` for replay.

The result rules out a persistent failure of this particular live scan at the
time tested. It does **not** explain the earlier programming-time error 87,
qualify image programming, identify current CPU/FPGA application state, or
pass the native launch/return cycle. The two prior image attempts remain
consumed. No additional programming attempt was made.

Next: prepare a programming procedure that records the actual programmer
executable/library identity and phase boundaries, and uses an explicit live
chain check rather than treating a listing as live-scan evidence. A concrete
transport repair is still required before another image-programming trial;
this successful diagnostic alone does not authorize replaying the consumed
procedure.
