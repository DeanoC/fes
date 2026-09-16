# Native image recipe. FogCast supplies agent, kit and the lock selector via
# FOGCAST_DIR; this tree owns Buildroot, the builder container and assembly.
CONTAINER_RUNTIME ?= docker
FOGCAST_DIR ?=
NATIVE_RUNTIME_MODE ?= package-only
TARGET_IMAGE_OUTPUT_VOLUME ?= fogcast-target-image-output

export NATIVE_RUNTIME_MODE FES_PACKAGE_IDS \
	FES_PONG_PACKAGE_DIR FES_PONG_PACKAGE_SELECTION \
	FES_ZX81_PACKAGE_DIR FES_ZX81_PACKAGE_SELECTION \
	FES_COLECO_PACKAGE_DIR FES_COLECO_PACKAGE_SELECTION \
	FES_SG1000_PACKAGE_DIR FES_SG1000_PACKAGE_SELECTION \
	FOGCAST_DIR TARGET_IMAGE_OUTPUT_VOLUME
require_fogcast = @test -n "$(FOGCAST_DIR)" || { echo 'image: FOGCAST_DIR is required' >&2; exit 2; }

.PHONY: test build-target-image-lock-container target-image-native-fetch \
	target-image-native target-image-native-verify target-image-native-qemu-smoke \
	target-image-fetch target-images target-image-dev target-image-verify \
	target-image-qemu-smoke target-kernel-test target-kernel target-kernel-verify

test:
	sh scripts/tests/native-package-only_test.sh
	sh scripts/tests/native-runtime-inputs_test.sh
	sh scripts/tests/native-development-rbf-support-truth_test.sh
	sh scripts/tests/target-image-sources_test.sh
	sh scripts/tests/kit-init_test.sh
	sh scripts/tests/target-image-rootfs_test.sh
	sh scripts/tests/rootfs-package-cleanup_test.sh
	sh scripts/tests/verify-target-image-cleanup_test.sh
	sh scripts/tests/target-image_test.sh
	sh scripts/tests/target-image-dev_test.sh
	sh scripts/tests/target-image-dev-container_test.sh
	sh scripts/tests/target-kernel_test.sh

build-target-image-lock-container:
	$(require_fogcast)
	$(MAKE) -C "$(FOGCAST_DIR)" CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" build-target-image-lock-container
	mkdir -p bin
	cp "$(FOGCAST_DIR)/bin/target-image-lock-linux-amd64" bin/target-image-lock-linux-amd64

ifeq ($(shell uname -s),Darwin)
build-target-image-lock:
	$(require_fogcast)
	$(MAKE) -C "$(FOGCAST_DIR)" build-target-image-lock
	mkdir -p bin
	cp "$(FOGCAST_DIR)/bin/target-image-lock" bin/target-image-lock

target-image-native-fetch: build-target-image-lock
endif

target-image-native-fetch: build-target-image-lock-container

target-image-native-fetch:
	$(require_fogcast)
	NATIVE_RUNTIME_MODE="$(NATIVE_RUNTIME_MODE)" \
	  LIBMISTER_RUNTIME_DIR= \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/target-image-container.sh fetch \
	  /work/scripts/fetch-native-runtime-inputs.sh
	NATIVE_RUNTIME_MODE="$(NATIVE_RUNTIME_MODE)" \
	  LIBMISTER_RUNTIME_DIR="$(LIBMISTER_RUNTIME_DIR)" \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/build-target-image.sh --fetch native-dev

target-image-native: target-image-native-fetch

target-image-native:
	$(require_fogcast)
	NATIVE_RUNTIME_MODE="$(NATIVE_RUNTIME_MODE)" \
	  LIBMISTER_RUNTIME_DIR="$(LIBMISTER_RUNTIME_DIR)" \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/build-target-image.sh native-dev

target-image-native-verify:
	$(require_fogcast)
	NATIVE_RUNTIME_MODE="$(NATIVE_RUNTIME_MODE)" \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/verify-target-image.sh native-dev build/output/target-image/native-dev/linux.img build/output/target-image/native-dev/manifest.tsv build/output/target-image/native-dev/library-report.tsv

target-image-native-qemu-smoke:
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/qemu-smoke-target-image.sh native-dev \
	  build/output/target-image/native-dev/linux.img

target-image-fetch: build-target-image-lock-container
	$(require_fogcast)
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/target-image-container.sh fetch /work/scripts/fetch-target-image-sources.sh
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/build-target-image.sh --fetch prod
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/build-target-image.sh --fetch dev

target-images: target-image-fetch
	$(require_fogcast)
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/build-target-image.sh prod
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/build-target-image.sh dev

target-image-dev: target-image-fetch
	$(require_fogcast)
	TARGET_IMAGE_DEV_CONTAINER=1 TARGET_IMAGE_OUTPUT_VOLUME=fogcast-target-image-output \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/build-target-image.sh --fast-dev

target-image-verify:
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/verify-target-image.sh prod build/output/target-image/prod/linux.img build/output/target-image/prod/manifest.tsv build/output/target-image/prod/library-report.tsv
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/verify-target-image.sh dev build/output/target-image/dev/linux.img build/output/target-image/dev/manifest.tsv build/output/target-image/dev/library-report.tsv

target-image-qemu-smoke:
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/qemu-smoke-target-image.sh prod build/output/target-image/prod/linux.img
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/qemu-smoke-target-image.sh dev build/output/target-image/dev/linux.img

target-kernel-test:
	sh scripts/tests/target-kernel_test.sh

target-kernel: target-images
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/build-target-kernel.sh

target-kernel-verify: target-kernel
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  FOGCAST_DIR="$(FOGCAST_DIR)" \
	  scripts/verify-target-kernel.sh build/output/target-image/kernel
