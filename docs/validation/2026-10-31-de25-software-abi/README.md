# DE25 software base ABI validation

FES bundle packer scope, base0da63d1044cc7ecf9908efba52af17b3e55e6271.
No runtime, agent, FPGA, kernel or shared-contract changes; no installation.

The actual approved FES bottom-board userland was copied read-only after a
status-file claim and trusted UART/SSH identity check. The boot UUID remains
e8963972-c3e9-405c-a2ef-93eb09f691e6. Busybox and Dropbear hashes match the
approved baseline. Libraries and those executables pass real AArch64 ELF,
dynamic dependency and required-symbol closure checks. The resulting contract
is `fes-kit-base-v1-fab9b86e78feaffc706c8098ebda7661d9d5ccf074a90710b35e6e27ad3c9e90`.
[base-abi-proof.json](base-abi-proof.json) binds the archive, executables and
result. This is a dereferenced library/userland file view, not a full rootfs or
SD attestation. The view remains local; no library binaries are published here.

The actual filesystem exposed a GNU libgcc linker script and libstdc++ GDB
helper under names matching *.so*. The validator now inventories ELF objects
only. A dynamic reference to an excluded non-ELF file still fails, and wrong
ELF architectures are refused. Thirteen focused packer tests include a real
Go-cross-compiled AArch64 executable, wrong/mixed architecture rejection,
board-identity refusal, linker-script/helper exclusion, unresolved dependency
refusal and an unchanged DE10 contract digest control. Seventeen standalone
producer tests also pass.

The board received no upload, driver load, service restart, FPGA operation,
reboot, flash or SD/boot change. UART logged out and closed, all SSH reads ended,
and the idle bottom board was released on its unchanged HPS-first/FES baseline.
This proves software ABI validation only. A sealed DE25 factory bundle, SDK
selection for the normal producer, managed activation/update and production
runtime/MENU integration remain required for the full normal-kit goal.
