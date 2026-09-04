GO ?= go
PLATFORM ?= packages/platform/de10_nano.yaml
ORACLE ?= testdata/oracles/libmister-runtime-fpga.yaml
SYSTEM ?= packages/system/megadrive.yaml
SYSTEM_ORACLE ?= testdata/oracles/libmister-runtime-megadrive.yaml
CORE_SOURCE ?= packages/source/megadrive_mister.yaml
CORE_SOURCE_ORACLE ?= testdata/oracles/megadrive-core-source.yaml

.PHONY: all test vet validate report emit-cpp

all: test

test:
	$(GO) test ./...
	$(GO) run ./cmd/mister-packages validate $(PLATFORM)
	$(GO) run ./cmd/mister-packages diff-oracle $(PLATFORM) $(ORACLE)
	$(GO) run ./cmd/mister-packages validate $(SYSTEM)
	$(GO) run ./cmd/mister-packages diff-oracle $(SYSTEM) $(SYSTEM_ORACLE)
	$(GO) run ./cmd/mister-packages validate $(CORE_SOURCE)
	$(GO) run ./cmd/mister-packages diff-oracle $(CORE_SOURCE) $(CORE_SOURCE_ORACLE)

vet:
	$(GO) vet ./...
	git diff --check

validate:
	$(GO) run ./cmd/mister-packages validate $(PLATFORM)

report:
	$(GO) run ./cmd/mister-packages report $(PLATFORM)

emit-cpp:
	$(GO) run ./cmd/mister-packages emit-cpp $(PLATFORM)
