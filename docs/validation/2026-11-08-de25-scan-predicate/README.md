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
