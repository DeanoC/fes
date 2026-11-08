# Scan precondition reporting

The preceding scan-capture diagnostic found the adapter and obtained its name
but refused the compound exact-name/JTAG-chain-type predicate before locking or
scanning. It reported neither individual outcome. Its diagnostic reservation
and the earlier native programming reservation remain consumed and retired.

This distinct host probe adds bounded, hex-encoded returned-name bytes and
explicit valid-length/name-match/chain-type fields before the unchanged
predicate. Tests cover wrong names, unknown chain types and unterminated names;
all must refuse before locking. It does not relax any scan or programming gate.

The otherwise unchanged isolated top-TRWU8CBL operator still permits at most
one controlled6MHz forced scan, uses adapter-only discovery, cannot create a
programming grant, and independently restores original15MHz readback, exact
USB permissions and its own private service. No FPGA programming, CPU reset,
SD/UART commands or bottom-kit operations are included. The previous board
state was recovery U-Boot; this diagnostic does not boot or launch a core.

The prepared retention requires the preceding diagnostic's fully read-back
hardware archive and verified source update bundle. Every selected operator,
probe, source header and vendor tool is hash-bound again. Source/controlled API
checks and failure-injected host cleanup tests remain software evidence.

Its result will distinguish a precondition refusal from an actual scan result.
Neither observation establishes the historical87 cause, fixes the native tape
deadline, or constitutes ordinary DE10-equivalent appliance acceptance.

The physical observation now distinguishes the predicates: exact name matched,
valid length44, returned chain type2. The pinned public AJI header defines
unknown0 and JTAG1; it does not describe2. The unchanged type gate refused
before lock/scan, so actual forced scans0. This establishes the immediate
host-probe refusal, not the historical87 cause or a physical chain failure.
Independent cleanup again passed, with original15MHz readback, same exact USB
permissions, own server gone and UART unused. This reservation is consumed.

Read-only inspection found matching AJI_HARDWARE conversion stores in both
installed vendor variants (chain type output offset0x28, features0x38). This
reduces the specific output-offset hypothesis but is not complete ABI proof.
The installed Quartus tree exposes no AJI header at the searched locations.
Intel's pinned jtagconfig enumeration path locks and reads/scans chains without
rejecting the pre-scan AJI_HARDWARE.chain_type. That is relevant to a separate
scan-only observer; the existing programming-grant gate remains unchanged.
