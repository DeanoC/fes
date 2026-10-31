#!/bin/sh
set -eu
temp_task=$(mktemp -d /tmp/de25-sdk-probe.XXXXXX)
trap 'rm -rf "$temp_task"' EXIT
tar -xzf /artifacts/aarch64-sdk.tar.gz -C "$temp_task"
sdk_task="$temp_task/aarch64-buildroot-linux-gnu_sdk-buildroot"
"$sdk_task/relocate-sdk.sh"
cat > "$temp_task/probe.c" <<'EOF'
#include <stdio.h>
#ifndef __aarch64__
#error Expected AArch64 target
#endif
int main(void) { puts("FES DE25 AArch64 SDK probe passed"); return 0; }
EOF
"$sdk_task/bin/aarch64-buildroot-linux-gnu-gcc" "$temp_task/probe.c" -o "$temp_task/probe"
sysroot_task=$("$sdk_task/bin/aarch64-buildroot-linux-gnu-gcc" -print-sysroot)
qemu-aarch64-static -L "$sysroot_task" "$temp_task/probe"
