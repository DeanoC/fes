VERSION ?= 0.1.0
REVISION ?= $(shell git rev-parse --verify HEAD 2>/dev/null || printf unknown)
CONTAINER_RUNTIME ?= docker
LIBMISTER_RUNTIME_DIR ?= $(abspath ../libmister-runtime)
LDFLAGS = -s -w -X github.com/DeanoC/FogCast/internal/version.Version=$(VERSION)
FOGCAST_LDFLAGS = $(LDFLAGS) -X github.com/DeanoC/FogCast/internal/version.Revision=$(REVISION)
FOGCAST_GOOS ?= darwin
FOGCAST_GOARCH ?= arm64
FOGCAST_OUTPUT ?= bin/fogcast
FOGCAST_HOST_OUTPUT ?= bin/FogCastHost.app

# The Darwin capture helper weak-links AVFoundation/CoreAudio. Keep the
# generic repository checks cgo-enabled on Darwin with the same deployment
# target and linker allow-list used by the signed host build, while leaving
# non-Darwin contributors on the ordinary Go toolchain defaults.
ifeq ($(shell uname -s),Darwin)
NATIVE_GO_ENV = MACOSX_DEPLOYMENT_TARGET=11.0 CGO_CFLAGS=-mmacosx-version-min=11.0 CGO_CXXFLAGS=-mmacosx-version-min=11.0 CGO_LDFLAGS=-mmacosx-version-min=11.0 CGO_LDFLAGS_ALLOW=-Wl,-weak_framework,.*
else
NATIVE_GO_ENV =
endif

.PHONY: fmt test test-ui test-ui-browser test-ui-browser-required vet check build build-fogcast build-fogcast-api build-fogcast-host build-fogcast-tenfoot tenfoot-cgo-env tenfoot-smoke build-cli build-remote-play-sender build-remote-play-receiver build-remote-play-impair build-remote-play-audiobridge build-agent build-bridge build-target-image-lock build-target-image-lock-container target-image-resolve target-image-fetch target-image-test target-images target-image-dev target-image-verify target-image-qemu-smoke target-image-native-fetch target-image-native target-image-native-verify target-image-native-qemu-smoke target-image-deploy target-smoke target-native-smoke target-kernel-test target-kernel target-kernel-verify

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test: build-agent test-ui
	$(NATIVE_GO_ENV) go test -race ./...
	sh scripts/tests/fogcast-build_test.sh
	sh scripts/tests/native-runtime-inputs_test.sh
	sh scripts/tests/native-megadrive-support-truth_test.sh
	sh scripts/tests/target-image-sources_test.sh
	sh scripts/tests/target-image-rootfs_test.sh
	sh scripts/tests/target-image_test.sh
	sh scripts/tests/target-image-dev_test.sh
	sh scripts/tests/target-image-dev-container_test.sh
	sh scripts/tests/target-kernel_test.sh
	sh scripts/tests/deploy-target-image_test.sh
	sh scripts/tests/target-smoke_test.sh
	sh scripts/tests/native-runtime-smoke_test.sh

test-ui:
	node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
	node --test internal/hostapi/ui_browser_test.js

test-ui-browser:
	node --test internal/hostapi/ui_browser_test.js

test-ui-browser-required:
	FOGCAST_BROWSER_REQUIRED=1 node --test internal/hostapi/ui_browser_test.js

vet:
	$(NATIVE_GO_ENV) go vet ./...

check: fmt test vet

build: build-fogcast build-fogcast-api build-cli build-remote-play-sender build-remote-play-receiver build-remote-play-impair build-agent build-bridge build-target-image-lock build-target-image-lock-container

build-fogcast:
	mkdir -p "$(dir $(FOGCAST_OUTPUT))"
	CGO_ENABLED=0 GOOS=$(FOGCAST_GOOS) GOARCH=$(FOGCAST_GOARCH) go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o "$(FOGCAST_OUTPUT)" ./cmd/fogcast

build-fogcast-api:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/fogcast-api ./cmd/fogcast-api

build-fogcast-host:
	FOGCAST_SIGNING_IDENTITY="$(FOGCAST_SIGNING_IDENTITY)" VERSION="$(VERSION)" REVISION="$(REVISION)" scripts/build-fogcast-host.sh "$(FOGCAST_HOST_OUTPUT)"

# Native SDL3 10-foot launcher. Requires pkg-config sdl3 and a C toolchain
# (Homebrew sdl3 on Darwin; distro SDL3 devel on Linux — see
# docs/native-tenfoot-launcher/LINUX.md). Host API client only.
ifndef TENFOOT_CGO_ENV
ifeq ($(shell uname -s),Darwin)
TENFOOT_CGO_ENV = MACOSX_DEPLOYMENT_TARGET=11.0 CGO_ENABLED=1 CGO_CFLAGS=-mmacosx-version-min=11.0 CGO_LDFLAGS=-mmacosx-version-min=11.0 CGO_LDFLAGS_ALLOW=-Wl,-.*
else
TENFOOT_CGO_ENV = CGO_ENABLED=1
endif
endif

build-fogcast-tenfoot:
	mkdir -p bin
	$(TENFOOT_CGO_ENV) go build -tags sdl3 -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/fogcast-tenfoot ./cmd/fogcast-tenfoot

# Prints the CGO env the tenfoot target uses on this host (Darwin vs Linux).
tenfoot-cgo-env:
	@printf '%s\n' '$(TENFOOT_CGO_ENV)'

tenfoot-smoke: build-fogcast-tenfoot
	bin/fogcast-tenfoot -smoke -no-attract -api http://127.0.0.1:8787

build-cli:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/misterctl ./cmd/misterctl

build-remote-play-sender:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/remote-play-sender ./cmd/remote-play-sender

build-remote-play-receiver:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/remote-play-receiver ./cmd/remote-play-receiver

build-remote-play-impair:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/remote-play-impair ./cmd/remote-play-impair

build-remote-play-audiobridge:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/remote-play-audiobridge ./cmd/remote-play-audiobridge

build-agent:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-agent-linux-armv7 ./cmd/mister-agent

build-bridge:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-bridge-linux-armv7 ./cmd/mister-bridge

build-target-image-lock:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/target-image-lock ./cmd/target-image-lock

build-target-image-lock-container:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/target-image-lock-linux-amd64 ./cmd/target-image-lock

target-image-resolve: build-target-image-lock
	@command -v "$(CONTAINER_RUNTIME)" >/dev/null 2>&1 || { echo 'target-image-resolve: install a Docker-compatible container runtime first' >&2; exit 2; }
	$(CONTAINER_RUNTIME) pull --platform linux/amd64 docker.io/library/debian:12.11-slim
	bin/target-image-lock resolve --container-runtime "$(CONTAINER_RUNTIME)" --output build/target-image.sources.lock.toml

target-image-fetch: build-target-image-lock-container build-agent
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/target-image-container.sh fetch /work/scripts/fetch-target-image-sources.sh
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-target-image.sh --fetch prod
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-target-image.sh --fetch dev

target-image-test:
	sh scripts/tests/target-image_test.sh

target-images: build-agent target-image-fetch
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-target-image.sh prod
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-target-image.sh dev

# Fast development path: only the dev root, one persistent Buildroot output,
# and no reproducibility comparison. Use target-images for release evidence.
target-image-dev: build-agent target-image-fetch
	TARGET_IMAGE_DEV_CONTAINER=1 TARGET_IMAGE_OUTPUT_VOLUME=fogcast-target-image-output TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-target-image.sh --fast-dev

target-image-verify:
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/verify-target-image.sh prod build/output/target-image/prod/linux.img build/output/target-image/prod/manifest.tsv build/output/target-image/prod/library-report.tsv
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/verify-target-image.sh dev build/output/target-image/dev/linux.img build/output/target-image/dev/manifest.tsv build/output/target-image/dev/library-report.tsv

target-image-qemu-smoke:
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/qemu-smoke-target-image.sh prod build/output/target-image/prod/linux.img
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/qemu-smoke-target-image.sh dev build/output/target-image/dev/linux.img

target-image-native-fetch: build-target-image-lock-container build-agent
	LIBMISTER_RUNTIME_DIR= \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  scripts/target-image-container.sh fetch \
	  /work/scripts/fetch-native-runtime-inputs.sh
	LIBMISTER_RUNTIME_DIR="$(LIBMISTER_RUNTIME_DIR)" \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  scripts/build-target-image.sh --fetch native-dev

target-image-native: build-agent target-image-native-fetch
	LIBMISTER_RUNTIME_DIR="$(LIBMISTER_RUNTIME_DIR)" \
	  TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  scripts/build-target-image.sh native-dev

target-image-native-verify:
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/verify-target-image.sh native-dev build/output/target-image/native-dev/linux.img build/output/target-image/native-dev/manifest.tsv build/output/target-image/native-dev/library-report.tsv

target-image-native-qemu-smoke:
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" \
	  scripts/qemu-smoke-target-image.sh native-dev \
	  build/output/target-image/native-dev/linux.img

target-image-deploy:
	scripts/deploy-target-image.sh $(TARGET_IMAGE)

target-smoke:
	scripts/target-smoke.sh "$(GAME_ID)" "$(EXPECTED_CORE)"

target-native-smoke:
	scripts/native-runtime-smoke.sh

target-kernel-test:
	sh scripts/tests/target-kernel_test.sh

target-kernel: target-images
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-target-kernel.sh

target-kernel-verify: target-kernel
	TARGET_IMAGE_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/verify-target-kernel.sh build/output/target-image/kernel
