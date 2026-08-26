# Quartus Oracle Method

Quartus Prime Lite 17.0.2 is an optional reference compiler, not an open-tool
dependency. The oracle is accepted only after its exact version is reported;
the identical blinky RTL, target device, pinout, and 50 MHz timing intent are
used in both lanes. Oracle outputs remain under `build/oracle/` and include
logs, the RBF, fitter and timing reports, resource summary, and a manifest.

The official complete Update 2 installer is
`Quartus-lite-17.0.2.602-linux.tar` (SHA-1
`02aebab728d54e3ca8660d2646fdf93bc669b0ac`). The base
`Quartus-lite-17.0.0.595-linux.tar` bundle has SHA-1
`e71eeca4c8e1efaca902a58a37544c0572c6f45e` and is not itself version 17.0.2.
The stable official download page is
<https://www.altera.com/downloads/fpga-development-tools/quartus-prime-lite-edition-design-software-version-17-0-linux>.
The repository documents names and checksums there but never downloads
automatically, redistributes, or commits proprietary files.

Quartus must be installed in a path without spaces. Detection is explicit via
`QUARTUS_ROOTDIR` (or the documented equivalent) inside oracle-specific code;
missing Quartus stops only the oracle request and does not affect simulation or
OSS diagnostics. Comparison is structural and behavioral rather than an
expectation that Quartus and OSS resource counts or RBF bytes match exactly.
