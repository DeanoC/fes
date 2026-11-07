#!/bin/sh
set -eu
cp /work/kernel-input/vmlinux.symvers /work/kernel-input/Module.symvers
make -C /work/kernel-input ARCH=arm64 CROSS_COMPILE=/work/sdk/bin/aarch64-buildroot-linux-gnu- -j8 M=drivers/fpga modules
make -s -C /work/kernel-input ARCH=arm64 CROSS_COMPILE=/work/sdk/bin/aarch64-buildroot-linux-gnu- kernelrelease
sha256sum /work/kernel-input/arch/arm64/boot/Image /work/kernel-input/drivers/fpga/stratix10-soc.ko /work/kernel-input/drivers/fpga/fpga-bridge.ko /work/kernel-input/drivers/fpga/fpga-region.ko /work/kernel-input/drivers/fpga/of-fpga-region.ko
