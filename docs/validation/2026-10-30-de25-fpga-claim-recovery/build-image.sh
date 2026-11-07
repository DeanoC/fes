#!/bin/sh
set -eu
/work/kernel-input/scripts/config --file /work/kernel-input/.config --set-str LOCALVERSION '-fes-de25-claim1' --disable LOCALVERSION_AUTO
make -C /work/kernel-input ARCH=arm64 CROSS_COMPILE=/work/sdk/bin/aarch64-buildroot-linux-gnu- olddefconfig
make -C /work/kernel-input ARCH=arm64 CROSS_COMPILE=/work/sdk/bin/aarch64-buildroot-linux-gnu- -j8 Image
make -C /work/kernel-input ARCH=arm64 CROSS_COMPILE=/work/sdk/bin/aarch64-buildroot-linux-gnu- -j8 M=drivers/fpga modules
make -s -C /work/kernel-input ARCH=arm64 CROSS_COMPILE=/work/sdk/bin/aarch64-buildroot-linux-gnu- kernelrelease
sha256sum /work/kernel-input/arch/arm64/boot/Image
