VERSION ?= 0.1.0
REVISION ?= $(shell git rev-parse --verify HEAD 2>/dev/null || printf unknown)
CONTAINER_RUNTIME ?= docker
LDFLAGS = -s -w -X github.com/DeanoC/FogCast-POC/internal/version.Version=$(VERSION)
FOGCAST_LDFLAGS = $(LDFLAGS) -X github.com/DeanoC/FogCast-POC/internal/version.Revision=$(REVISION)
FOGCAST_GOOS ?= darwin
FOGCAST_GOARCH ?= arm64
FOGCAST_OUTPUT ?= bin/fogcast

.PHONY: fmt test vet check build build-fogcast build-fogcast-api build-cli build-hil build-fogcast-hil build-remote-play-receiver build-remote-play-impair build-agent build-bridge build-lock build-lock-container build-poc2-lock package-poc1a package-test poc1b-resolve poc1b-fetch poc1b-image-test poc1b-image-fetch poc1b-images poc1b-dev-image poc1b-verify-images poc1b-qemu-smoke poc1b-kernel-test poc1b-kernel poc1b-verify-kernel poc1b-deploy-test poc2-rootfs-test poc2-deploy-test build-stage-a0 build-stage-a0-firstbuild stage-a0-test stage-a0-firstbuild-test stage-a0-check

build-stage-a0:
	mkdir -p bin
	CGO_ENABLED=0 mise exec go@1.26.5 -- go build -buildvcs=false -trimpath -o bin/stage-a0 ./cmd/stage-a0

build-stage-a0-firstbuild:
	mkdir -p bin
	CGO_ENABLED=0 mise exec go@1.26.5 -- go build -buildvcs=false -trimpath -o bin/stage-a0-firstbuild ./cmd/stage-a0-firstbuild

stage-a0-test:
	mise exec go@1.26.5 -- go test ./internal/stagea0 ./cmd/stage-a0
	sh scripts/tests/stage-a0-init_test.sh

stage-a0-firstbuild-test:
	mise exec go@1.26.5 -- go test ./internal/stagea0/firstbuild ./cmd/stage-a0-firstbuild

stage-a0-check: build-stage-a0 build-stage-a0-firstbuild stage-a0-test stage-a0-firstbuild-test
	mise exec go@1.26.5 -- go test -race ./internal/stagea0 ./cmd/stage-a0
	mise exec go@1.26.5 -- go test -race ./internal/stagea0/firstbuild ./cmd/stage-a0-firstbuild
	mise exec go@1.26.5 -- go vet ./internal/stagea0 ./cmd/stage-a0
	mise exec go@1.26.5 -- go vet ./internal/stagea0/firstbuild ./cmd/stage-a0-firstbuild
	test -z "$$(gofmt -l internal/stagea0 cmd/stage-a0 cmd/stage-a0-firstbuild)"
	sh -n scripts/stage-a0-init-main.sh scripts/tests/stage-a0-init_test.sh
	shellcheck -x scripts/stage-a0-init-main.sh scripts/tests/stage-a0-init_test.sh

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test: build-agent
	go test -race ./...
	sh scripts/tests/fogcast-build_test.sh
	sh scripts/tests/inventory_test.sh
	sh scripts/tests/start-agent_test.sh
	sh scripts/tests/poc1b-sources_test.sh
	sh scripts/tests/poc1b-rootfs_test.sh
	sh scripts/tests/poc2-rootfs_test.sh
	sh scripts/tests/poc1b-image_test.sh
	sh scripts/tests/poc1b-dev_test.sh
	sh scripts/tests/poc1b-dev-container_test.sh
	sh scripts/tests/poc1b-kernel_test.sh
	sh scripts/tests/install-poc1b-target_test.sh
	sh scripts/tests/install-poc2-target_test.sh
	sh scripts/tests/restore-poc1b-sd_test.sh

vet:
	go vet ./...

check: fmt test vet

build: build-fogcast build-fogcast-api build-cli build-hil build-fogcast-hil build-remote-play-receiver build-remote-play-impair build-agent build-bridge build-lock build-lock-container build-poc2-lock

build-fogcast:
	mkdir -p "$(dir $(FOGCAST_OUTPUT))"
	CGO_ENABLED=0 GOOS=$(FOGCAST_GOOS) GOARCH=$(FOGCAST_GOARCH) go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o "$(FOGCAST_OUTPUT)" ./cmd/fogcast

build-fogcast-api:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -buildvcs=false -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/fogcast-api ./cmd/fogcast-api

build-cli:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/misterctl ./cmd/misterctl

build-hil:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-hil ./cmd/mister-hil

build-fogcast-hil:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/fogcast-hil ./cmd/fogcast-hil

build-remote-play-receiver:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/remote-play-receiver ./cmd/remote-play-receiver

build-remote-play-impair:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(FOGCAST_LDFLAGS)' -o bin/remote-play-impair ./cmd/remote-play-impair

build-agent:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-agent-linux-armv7 ./cmd/mister-agent

build-bridge:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-bridge-linux-armv7 ./cmd/mister-bridge

build-lock:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/poc1b-lock ./cmd/poc1b-lock

build-lock-container:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/poc1b-lock-linux-amd64 ./cmd/poc1b-lock

build-poc2-lock:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/poc2-lock ./cmd/poc2-lock

poc1b-resolve: build-lock
	@command -v "$(CONTAINER_RUNTIME)" >/dev/null 2>&1 || { echo 'poc1b-resolve: install a Docker-compatible container runtime first' >&2; exit 2; }
	$(CONTAINER_RUNTIME) pull --platform linux/amd64 docker.io/library/debian:12.11-slim
	bin/poc1b-lock resolve --container-runtime "$(CONTAINER_RUNTIME)" --output build/sources.poc1b.lock.toml

poc1b-fetch: build-lock-container
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/poc1b-container.sh fetch /work/scripts/fetch-poc1b-sources.sh

poc1b-image-test:
	sh scripts/tests/poc1b-image_test.sh

poc1b-image-fetch: build-agent
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-poc1b-image.sh --fetch prod
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-poc1b-image.sh --fetch dev

poc1b-images: build-agent poc1b-image-fetch
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-poc1b-image.sh prod
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-poc1b-image.sh dev

# Fast development path: only the dev root, one persistent Buildroot output,
# and no reproducibility comparison. Use poc1b-images for release evidence.
poc1b-dev-image: build-agent
	POC1B_DEV_CONTAINER=1 POC1B_OUTPUT_VOLUME=mister-remote-poc1b-dev-output POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-poc1b-image.sh --fast-dev

poc1b-verify-images:
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/verify-poc1b-image.sh prod build/output/poc1b/prod/linux.img build/output/poc1b/prod/manifest.tsv build/output/poc1b/prod/library-report.tsv
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/verify-poc1b-image.sh dev build/output/poc1b/dev/linux.img build/output/poc1b/dev/manifest.tsv build/output/poc1b/dev/library-report.tsv

poc1b-qemu-smoke:
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/qemu-smoke-poc1b.sh prod build/output/poc1b/prod/linux.img
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/qemu-smoke-poc1b.sh dev build/output/poc1b/dev/linux.img

poc1b-kernel-test:
	sh scripts/tests/poc1b-kernel_test.sh

poc1b-kernel: poc1b-images
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/build-poc1b-kernel.sh

poc1b-verify-kernel: poc1b-kernel
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/verify-poc1b-kernel.sh build/output/poc1b/kernel

poc1b-deploy-test:
	sh scripts/tests/install-poc1b-target_test.sh

poc2-rootfs-test:
	sh scripts/tests/poc2-rootfs_test.sh

poc2-deploy-test:
	sh scripts/tests/install-poc2-target_test.sh
	sh scripts/tests/restore-poc1b-sd_test.sh

package-poc1a:
	MISTER_TOKEN="$${MISTER_TOKEN:?MISTER_TOKEN is required}" VERSION="$(VERSION)" ./scripts/package-poc1a.sh

package-test:
	@set -eu; \
	sh -n scripts/package-poc1a.sh scripts/install-poc1a.sh scripts/install-poc1a-target.sh deploy/poc1a/start-agent.sh deploy/poc1a/user-startup.snippet.sh; \
	sh scripts/tests/install-poc1a-target_test.sh; \
	MISTER_TOKEN=abcdefghijklmnopqrstuvwxyzABCDEF VERSION="$(VERSION)" ./scripts/package-poc1a.sh; \
	archive="dist/mister-remote-poc1a-$(VERSION).tar.gz"; \
	expected=$$(printf '%s\n' 'mister-remote/MiSTer.ini.fragment' 'mister-remote/agent.toml' 'mister-remote/mister-agent' 'mister-remote/start-agent.sh'); \
	actual=$$(tar -tzf "$$archive"); \
	test "$$actual" = "$$expected"; \
	! tar -tzf "$$archive" | rg -i '\.(rom|bin|gen|md|sfc|smc|rbf|map)$$'; \
	first_archive=$$(mktemp -t mister-remote-poc1a.XXXXXX); \
	trap 'rm -f "$$first_archive"' EXIT INT TERM; \
	cp "$$archive" "$$first_archive"; \
	MISTER_TOKEN=abcdefghijklmnopqrstuvwxyzABCDEF VERSION="$(VERSION)" ./scripts/package-poc1a.sh; \
	cmp "$$first_archive" "$$archive"
