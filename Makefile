VERSION ?= 0.1.0
LDFLAGS = -s -w -X github.com/clawzai2-tech/mister-remote/internal/version.Version=$(VERSION)

.PHONY: fmt test vet check build build-cli build-agent

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

test:
	go test -race ./...

vet:
	go vet ./...

check: fmt test vet

build: build-cli build-agent

build-cli:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/misterctl ./cmd/misterctl

build-agent:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/mister-agent-linux-armv7 ./cmd/mister-agent
