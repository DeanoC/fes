# Explicit native image selection applies consistently to fetch/build/verify/QEMU.
export NATIVE_RUNTIME_SYSTEMS PONG_RBF_BUNDLE SNES_RBF_BUNDLE NES_RBF_BUNDLE FES_PACKAGE_IDS FES_PONG_PACKAGE_DIR FES_PONG_PACKAGE_SELECTION FES_ZX81_PACKAGE_DIR FES_ZX81_PACKAGE_SELECTION FES_COLECO_PACKAGE_DIR FES_COLECO_PACKAGE_SELECTION

VERSION ?= 0.1.0
REVISION ?= $(shell git rev-parse --verify HEAD 2>/dev/null || printf unknown)
CONTAINER_RUNTIME ?= docker
LIBMISTER_RUNTIME_DIR ?= $(abspath ../libmister-runtime)
MEGADRIVE_RBF_SOURCE ?= source-built
MEGADRIVE_RBF_BUNDLE ?=
LDFLAGS = -s -w -X github.com/DeanoC/FogCast/internal/version.Version=$(VERSION)
FOGCAST_LDFLAGS = $(LDFLAGS) -X github.com/DeanoC/FogCast/internal/version.Revision=$(REVISION)
FOGCAST_GOOS ?= $(shell go env GOOS)
FOGCAST_GOARCH ?= $(shell go env GOARCH)
FOGCAST_OUTPUT ?= bin/fogcast
FOGCAST_API_OUTPUT ?= bin/fogcast-api
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

.PHONY: fmt test test-ui test-ui-boundary test-ui-browser test-ui-browser-required vet check build build-fogcast build-fogcast-api build-fogcast-host build-fogcast-tenfoot tenfoot-cgo-env tenfoot-smoke build-tenfoot-linuxfb-spike build-tenfoot-linuxfb-grid build-cli build-remote-play-sender build-remote-play-receiver build-remote-play-impair build-remote-play-audiobridge build-fogcast-kit build-agent build-bridge build-target-image-lock build-target-image-lock-container target-image-deploy target-smoke target-native-smoke

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test: build-agent test-ui
	$(NATIVE_GO_ENV) go test -race ./...
	sh scripts/tests/fogcast-build_test.sh
	sh scripts/tests/native-megadrive-support-truth_test.sh
	sh scripts/tests/native-development-rbf-support-truth_test.sh
	sh scripts/tests/deploy-target-image_test.sh
	sh scripts/tests/target-smoke_test.sh
	sh scripts/tests/native-runtime-smoke_test.sh

test-ui:
	$(MAKE) test-ui-boundary
	node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
	node --test internal/hostapi/ui_browser_test.js

test-ui-boundary:
	sh scripts/tests/ui-boundary_test.sh

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
	mkdir -p "$(dir $(FOGCAST_API_OUTPUT))"
	CGO_ENABLED=0 GOOS=$(FOGCAST_GOOS) GOARCH=$(FOGCAST_GOARCH) go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o "$(FOGCAST_API_OUTPUT)" ./cmd/fogcast-api

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

# CGO-free ARMv7 linuxfb spike: software rasterizer Present-blits to /dev/fb0
# and reads evdev/joystick input (move cursor, quit on Start/ESC/Q).
# No SDL3 tag. Same GOOS/GOARCH/GOARM lane as mister-agent-linux-armv7.
build-tenfoot-linuxfb-spike:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/tenfoot-linuxfb-spike-linux-armv7 ./cmd/tenfoot-linuxfb-spike

# CGO-free ARMv7 linuxfb fake cover-grid: software Present-blits to /dev/fb0,
# navigates a hardcoded title grid, confirms with South/Enter, quits on Start.
# No SDL3 tag. Same GOOS/GOARCH/GOARM lane as mister-agent-linux-armv7.
build-tenfoot-linuxfb-grid:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/tenfoot-linuxfb-grid-linux-armv7 ./cmd/tenfoot-linuxfb-grid

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

build-fogcast-kit:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/fogcast-kit-linux-armv7 ./cmd/fogcast-kit

.PHONY: build-fes-boot build-fes-update
build-fes-update:
	mkdir -p bin
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/fes-update ./cmd/fes-update

build-fes-boot:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -buildvcs=false -trimpath -ldflags '$(LDFLAGS)' -o bin/fes-boot-linux-armv7 ./cmd/fes-boot

build-agent: build-fogcast-kit
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/mister-agent-linux-armv7 ./cmd/mister-agent

build-bridge:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/mister-bridge-linux-armv7 ./cmd/mister-bridge

build-target-image-lock:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/target-image-lock ./cmd/target-image-lock

build-target-image-lock-container:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/target-image-lock-linux-amd64 ./cmd/target-image-lock

# Native Buildroot/rootfs assembly lives in the FES image/ recipe.
# These targets remain FogCast inputs: agent, kit, and the lock selector.

target-image-deploy:
	scripts/deploy-target-image.sh $(TARGET_IMAGE)

target-smoke:
	scripts/target-smoke.sh "$(GAME_ID)" "$(EXPECTED_CORE)"

target-native-smoke:
	scripts/native-runtime-smoke.sh $(NATIVE_RUNTIME_SELECTION)
