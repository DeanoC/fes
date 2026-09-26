GO ?= go
PYTHON ?= python3
PLATFORM ?= packages/platform/de10_nano.yaml
ORACLE ?= testdata/oracles/libmister-runtime-fpga.yaml
FES_SIMPLE_GAME_ABI ?= packages/abi/fes_simple_game.yaml
FES_SIMPLE_COMPUTER_ABI ?= packages/abi/fes_simple_computer.yaml
FES_APPLICATION_ABI ?= packages/abi/fes_application.yaml
PROGRAMMING_PROFILES ?= packages/programming/de10_nano.yaml

.PHONY: all test vet validate report emit-cpp emit-go emit-verilog fixtures check-fixtures

all: test

test:
	$(PYTHON) -m unittest discover -s tests -p 'test_core_bundle*fixtures.py' -v
	$(PYTHON) -m unittest discover -s tests -p 'test_core_persistence_fixtures.py' -v
	$(PYTHON) scripts/core_persistence_fixtures.py --check
	$(GO) test ./...
	$(GO) run ./cmd/mister-packages validate $(PLATFORM)
	$(GO) run ./cmd/mister-packages diff-oracle $(PLATFORM) $(ORACLE)
	$(GO) run ./cmd/mister-packages validate $(FES_SIMPLE_GAME_ABI)
	$(GO) run ./cmd/mister-packages validate $(FES_SIMPLE_COMPUTER_ABI)
	$(GO) run ./cmd/mister-packages validate $(FES_APPLICATION_ABI)
	$(GO) run ./cmd/mister-packages validate $(PROGRAMMING_PROFILES)

vet:
	$(GO) vet ./...
	git diff --check

validate:
	$(GO) run ./cmd/mister-packages validate $(PLATFORM)

report:
	$(GO) run ./cmd/mister-packages report $(PLATFORM)

emit-cpp:
	$(GO) run ./cmd/mister-packages emit-cpp $(PLATFORM)

emit-go:
	$(GO) run ./cmd/mister-packages emit-go $(FES_SIMPLE_GAME_ABI)

emit-verilog:
	$(GO) run ./cmd/mister-packages emit-verilog $(FES_SIMPLE_GAME_ABI)

fixtures:
	$(PYTHON) scripts/core_bundle_fixtures.py
	$(PYTHON) scripts/core_bundle_v3_fixtures.py
	$(PYTHON) scripts/core_bundle_v4_fixtures.py
	$(PYTHON) scripts/core_persistence_fixtures.py

check-fixtures:
	$(PYTHON) scripts/core_bundle_fixtures.py --check
	$(PYTHON) scripts/core_bundle_v3_fixtures.py --check
	$(PYTHON) scripts/core_bundle_v4_fixtures.py --check
	$(PYTHON) scripts/core_persistence_fixtures.py --check
