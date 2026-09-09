GO ?= go
PYTHON ?= python3
PLATFORM ?= packages/platform/de10_nano.yaml
ORACLE ?= testdata/oracles/libmister-runtime-fpga.yaml
SYSTEM ?= packages/system/megadrive.yaml
SNES_SYSTEM ?= packages/system/snes.yaml
PONG_SYSTEM ?= packages/system/pong.yaml
NES_SYSTEM ?= packages/system/nes.yaml
SYSTEM_ORACLE ?= testdata/oracles/libmister-runtime-megadrive.yaml
CORE_SOURCE ?= packages/source/megadrive_mister.yaml
SNES_CORE_SOURCE ?= packages/source/snes_mister.yaml
NES_CORE_SOURCE ?= packages/source/nes_mister.yaml
FES_SIMPLE_GAME_ABI ?= packages/abi/fes_simple_game.yaml
MISTER_ABI ?= packages/abi/mister.yaml
PROGRAMMING_PROFILES ?= packages/programming/de10_nano.yaml
CORE_SOURCE_ORACLE ?= testdata/oracles/megadrive-core-source.yaml

.PHONY: all test vet validate report emit-cpp emit-go emit-verilog fixtures check-fixtures

all: test

test:
	$(PYTHON) -m unittest discover -s tests -p 'test_core_bundle_fixtures.py' -v
	$(GO) test ./...
	$(GO) run ./cmd/mister-packages validate $(PLATFORM)
	$(GO) run ./cmd/mister-packages diff-oracle $(PLATFORM) $(ORACLE)
	$(GO) run ./cmd/mister-packages validate $(SYSTEM)
	$(GO) run ./cmd/mister-packages validate $(PONG_SYSTEM)
	$(GO) run ./cmd/mister-packages validate $(SNES_SYSTEM)
	$(GO) run ./cmd/mister-packages validate $(NES_SYSTEM)
	$(GO) run ./cmd/mister-packages diff-oracle $(SYSTEM) $(SYSTEM_ORACLE)
	$(GO) run ./cmd/mister-packages validate $(CORE_SOURCE)
	$(GO) run ./cmd/mister-packages validate $(SNES_CORE_SOURCE)
	$(GO) run ./cmd/mister-packages validate $(NES_CORE_SOURCE)
	$(GO) run ./cmd/mister-packages validate $(FES_SIMPLE_GAME_ABI)
	$(GO) run ./cmd/mister-packages validate $(MISTER_ABI)
	$(GO) run ./cmd/mister-packages validate $(PROGRAMMING_PROFILES)
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

emit-go:
	$(GO) run ./cmd/mister-packages emit-go $(FES_SIMPLE_GAME_ABI)

emit-verilog:
	$(GO) run ./cmd/mister-packages emit-verilog $(FES_SIMPLE_GAME_ABI)

fixtures:
	$(PYTHON) scripts/core_bundle_fixtures.py

check-fixtures:
	$(PYTHON) scripts/core_bundle_fixtures.py --check
