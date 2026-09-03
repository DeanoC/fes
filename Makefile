GO ?= go
PLATFORM ?= packages/platform/de10_nano.yaml
ORACLE ?= testdata/oracles/libmister-runtime-fpga.yaml

.PHONY: all test vet validate report emit-cpp

all: test

test:
	$(GO) test ./...
	$(GO) run ./cmd/mister-packages validate $(PLATFORM)
	$(GO) run ./cmd/mister-packages diff-oracle $(PLATFORM) $(ORACLE)

vet:
	$(GO) vet ./...
	git diff --check

validate:
	$(GO) run ./cmd/mister-packages validate $(PLATFORM)

report:
	$(GO) run ./cmd/mister-packages report $(PLATFORM)

emit-cpp:
	$(GO) run ./cmd/mister-packages emit-cpp $(PLATFORM)
