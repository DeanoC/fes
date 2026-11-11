#!/bin/sh
set -eu
mkdir /work/sdk
 tar -xzf /sdk/aarch64-sdk.tar.gz --strip-components=1 -C /work/sdk
/work/sdk/relocate-sdk.sh
cp /input/vendor.config /work/kernel-input/.config
make -C /work/kernel-input ARCH=arm64 CROSS_COMPILE=/work/sdk/bin/aarch64-buildroot-linux-gnu- olddefconfig
make -C /work/kernel-input ARCH=arm64 CROSS_COMPILE=/work/sdk/bin/aarch64-buildroot-linux-gnu- -j8 prepare scripts
make -C /work/kernel-input ARCH=arm64 CROSS_COMPILE=/work/sdk/bin/aarch64-buildroot-linux-gnu- -j8 drivers/firmware/stratix10-svc.o
/work/sdk/bin/aarch64-buildroot-linux-gnu-gcc --version
file /work/kernel-input/drivers/firmware/stratix10-svc.o
sha256sum /work/kernel-input/drivers/firmware/stratix10-svc.o
