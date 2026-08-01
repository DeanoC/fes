VERSION ?= 0.1.0
LDFLAGS = -s -w -X github.com/clawzai2-tech/mister-remote/internal/version.Version=$(VERSION)

.PHONY: fmt test vet check build build-cli build-hil build-agent package-poc1a package-test

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test:
	go test -race ./...
	sh scripts/tests/inventory_test.sh

vet:
	go vet ./...

check: fmt test vet

build: build-cli build-hil build-agent

build-cli:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/misterctl ./cmd/misterctl

build-hil:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-hil ./cmd/mister-hil

build-agent:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-agent-linux-armv7 ./cmd/mister-agent

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
