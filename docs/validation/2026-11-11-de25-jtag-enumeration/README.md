# Programmer-style automatic JTAG enumeration

The preceding native programming stopped at scan error87 after a passing
locked forced-scan probe. Comparison with successful retained programming
shows the same wrapper, executable and client-library paths. The failed call
opened no SOF. This does not establish a physical cause or prove the chain
state after failure.

Disassembly of the installed programmer identifies a concrete API difference:
`QPGM_FRAMEWORK::create_chain(bool)` initializes device count to zero, locks the
chain, then calls `aji_read_device_chain` with a null list and automatic scan
true. It expects status47 and allocates a list before a second automatic read.
The existing admission probe explicitly forces a scan, then reads one cached
entry with automatic scan false. The [API comparison](api-comparison.json)
pins the executable and disassembly; it is an observation about this installed
binary, not a claim about every programmer path.

The separate C++ diagnostic reproduces the count/list shape, bounds the list
to one device and verifies exact4362C0DD/mask0/IR10 identity. It requires a
private top-only cable, locked3MHz/auto0, stops on an unexpected response and
unlocks without retry. Nine API models pass; three actual orchestration models
pass success, uncertain operation and cleanup failures. The vendor-linked
binary also compiles. It emits a distinct format rejected by the programming
grant decoder. No image, programmer, UART, CPU-reset or SD API is invoked.

The [one-attempt procedure](operator-source.json) retains the shared Quartus
lock, continuing TRWU8CBL claim, fresh stable USB identity, exact tool/source
hashes and clock/USB/private-server cleanup. The factory card and bottom kit
remain untouched. A passing diagnostic will establish neither a transport fix
nor permission to retry native programming. Full selected parent software
checks are running separately; this bounded scan-only experiment does not
claim native boot, installation or core-load acceptance.
