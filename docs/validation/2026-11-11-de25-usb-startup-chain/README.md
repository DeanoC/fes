# Matched native chain with USB startup outcomes

Built from `da33a94cc56ca7a975eb275ce04d1d3cf30c0890` into the separate
`out/nano-usb-startup-chain-20261111` directory. SDK, entry, BL31, U-Boot/FIT,
Quartus composition, matched-source verification and staged-file plan all pass.
The original tested installer candidate is preserved.

The native ELF includes the current `usbcontrol/startup.go` source hash and
its `stage/stop_attempted/stop_verified` failure formatter. The actual target
code retains the existing fatal startup behavior, partial-resource retention,
one bounded cleanup and prohibition on retry. This rebuild makes the failure
outcome available for a future native test; it does not fix the physical USB
connection or implement optional input/hotplug.

Boot ID is `b3ed4389ad43d91b`. FIT is 8,374,952 bytes with 13,656 bytes spare
under the unchanged 8 MiB bound. SDK/entry/firmware source and metadata match,
including CPU mapping, secure MMU, ownership and both menu/computer contexts.
The 25 MHz SD setting and writable spare-card plan are preserved. Networking
remains disabled in native startup. All nine staged installer/core files match
the previous candidate byte for byte; only the boot-chain files change.
The ten-file upload plan has `execute=false` and no live authority. No upload,
SD write, FPGA programming or kit operation is performed.

Composition acquires `/tmp/lander-quartus-3090.lock` through the existing bounded
flock command. It uses Quartus Pro 26.3 PFG to inject the matched SPL, inspect
and split the output, and compare the unchanged FPGA core payload. There is
no RTL synthesis, constraint/clock relaxation or timing signoff claim.

`publication.json` pins the SDK, entry, BL31, FIT and SOF hashes.
`build-results.json` records bounded-job results and log hashes. The seven
software gates from the [startup implementation](../2026-11-11-de25-usb-startup-outcome/README.md)
remain separately identified at their tested commit; only documentation changed
between it and this compiled commit, and all integration source hashes still
match. They are not relabelled as a new hardware or source-commit test pass.
No additional parent-suite run is substituted for these actual artifact builds.

The complete candidate and selected source closure are retained at
`/home/deano/fes-retained/de25-usb-startup-chain-prepared-20261111-r1`.
Archive readback checks all 2,420 file hashes/modes. An independent restore of
the source delta with its named base bundle passes strict Git fsck and pack
verification; 2,274 selected source files are retained and hash-checked.
External tools/vendor repositories and installed recovery still need current
admission before use. No claim, stale programming grant or old boot receipt
becomes live authority through this archive.

The hardware controller still needs rebinding and a fresh complete admission
to this exact new chain. The outstanding keyboard connection confirmation is
also unresolved. First physical package installation, subsequent independent
boot/discovery without restaging, and ordinary DE10-equivalent core loading
remain unverified. No push, PR, merge or cleanup of old outputs is performed.
