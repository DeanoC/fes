#!/bin/sh
set -eu
temp_task=$(mktemp -d /tmp/de25-runtime-rootfs.XXXXXX)
trap 'rm -rf "$temp_task"' EXIT
tar -xf /image/rootfs.tar -C "$temp_task" --no-same-owner --exclude='./dev/*' --exclude='dev/*'
qemu-aarch64-static -cpu cortex-a53 -L "$temp_task" /build/1bc89e9c0f8172a3de80133c0e14996d179c0a3ae1620c9248d977fd374335fb/runtime/tests/unit/production_hardware_test
