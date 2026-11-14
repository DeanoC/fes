# Explicit native launcher autoboot candidate

Code `303771739a9ad18d824d35ca245b633e9d74f6b4`, based on `a0a53cb62bdbb2013523de9395815ba3e1bc19a3`.
FES owns this boot-producer/package-admission change. No shared wire ABI,
Go runtime or FPGA source changes.

## Result

The selected Tenfoot launcher and paired menu can now explicitly request
`build_terasic_uboot.py --native-autoboot --native-entry-build ...`.
The matched U-Boot defaults to `fesboot` after a two-second UART abort window.
The default diagnostic selection and separate network recovery loader remain
manual. The environment remains volatile; alternate autoboot, menu-key and
retry paths are disabled.

Startup policy contributes to the boot ID. Builder and package admission
verify its exact configuration, source identity and the boot delay in the
compiled device tree. Admission reuses the repository's FDT parser, whose source
is included in the cold producer inventory. Missing, changed or contradictory
policies reject. Automatic startup requires the selected paired Tenfoot image;
a serial probe rejects before export mutation.

The complete entry/BL31/SPL/FIT/SOF chain passes off-board verification as boot
`548caa50395f3d94`. FIT size is 8,379,072 bytes against the
unchanged 8,388,608-byte bound (9,536 bytes spare).
The SDK ELF and entry payload remain byte-identical to the preceding candidate;
current selected source hashes were rechecked. FPGA core payload preservation
passes. Exact hashes and source identity are in `matched-chain.json`.

## Validation

- Final focused tests: 41 pass, including real compiled device-tree checks,
  package-policy/configuration/tree/identity rejection, export, SPL/firmware
  ownership and file-composition paths.
- Parent suite: 1,973 tests, passing with three skips.
- Generated consumer consistency, native firmware/FPGA launch integration,
  producer integration and embedded firmware/identity-drain checks pass.
- FPGA tooling suite: 1,595 tests, passing with 79 skips, run separately after the aggregate
  850-second host wrapper timed out in that final lane. The failed wrapper and
  partial log are retained. Product/native test deadlines were unchanged.
- Fresh matched chain builds and file/source/hash/size verification pass.

The initial direct focused command lacked this project's private test-helper
and DT-tool environment; its setup failure was followed by the configured
focused run. A final focused run also covers the completed FDT-parser change.
See `software-proof.json` for exact commands, results and captured-log hashes.
Private raw logs/artifacts are under `out/nano-autoboot-20261111/`.
Retention destination: `/home/deano/fes-retained/de25-native-autoboot-chain-20261111-r2`.

## Hardware boundary and next step

This turn made no UART/JTAG, SD-write, flash or programming operation.
The earlier two-core installation and independent reboot discovery are separate
hardware results for boot `6f54fe7c04d34128`; they do not qualify this new image.
Factory flash and the installed test card are unchanged.

`next-trial-plan.json` proposes a boot-FIT-only refresh through the existing
recovery path, followed by a fresh native programming epoch, receive-only
startup capture with no serial `fesboot`, and both installed-core launch/return
checks. Its automatic-start collector/lifecycle operator is not implemented
or admitted yet. It must be adapted and model-checked before a physical run;
existing manual-start operators cannot be reused unchanged. Fresh owned live
admission is required before every hardware operation.

Autonomous SD power-on boot, power-loss persistence, this image's physical
HDMI/key actions, native Ethernet and general appliance acceptance remain open.
The overall DE10-equivalent native core-loading goal remains active.
