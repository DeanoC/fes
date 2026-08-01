VERSION ?= 0.1.0
CONTAINER_RUNTIME ?= docker
LDFLAGS = -s -w -X github.com/clawzai2-tech/mister-remote/internal/version.Version=$(VERSION)

.PHONY: fmt test vet check build build-cli build-hil build-agent build-lock build-lock-container package-poc1a package-test poc1b-resolve poc1b-fetch

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test:
	go test -race ./...
	sh scripts/tests/inventory_test.sh
	sh scripts/tests/start-agent_test.sh
	sh scripts/tests/poc1b-sources_test.sh
	sh scripts/tests/poc1b-rootfs_test.sh

vet:
	go vet ./...

check: fmt test vet

build: build-cli build-hil build-agent build-lock build-lock-container

build-cli:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/misterctl ./cmd/misterctl

build-hil:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-hil ./cmd/mister-hil

build-agent:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-agent-linux-armv7 ./cmd/mister-agent

build-lock:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/poc1b-lock ./cmd/poc1b-lock

build-lock-container:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/poc1b-lock-linux-amd64 ./cmd/poc1b-lock

poc1b-resolve: build-lock
	@command -v "$(CONTAINER_RUNTIME)" >/dev/null 2>&1 || { echo 'poc1b-resolve: install a Docker-compatible container runtime first' >&2; exit 2; }
	$(CONTAINER_RUNTIME) pull --platform linux/amd64 docker.io/library/debian:12.11-slim
	bin/poc1b-lock resolve --container-runtime "$(CONTAINER_RUNTIME)" --output build/sources.poc1b.lock.toml

poc1b-fetch: build-lock-container
	POC1B_CONTAINER_RUNTIME="$(CONTAINER_RUNTIME)" scripts/poc1b-container.sh fetch /work/scripts/fetch-poc1b-sources.sh

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
