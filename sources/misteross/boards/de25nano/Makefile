PYTHON ?= python3
VERILATOR ?= verilator
OUTPUT ?=
QUARTUS_ROOTDIR ?=
MISTEROSS_ROOT := $(abspath ../..)
SIM_OUTPUT := $(MISTEROSS_ROOT)/build/sim/de25nano

.PHONY: help project build sim test
help:
	@printf '%s\n' 'project: copy the standalone Quartus Pro project into a new build directory' 'build: compile a diagnostic SOF with explicit QUARTUS_ROOTDIR (25.1.1 Pro)' 'sim: test the unmodified 50 MHz LED divider with Verilator' 'test: test project preparation and compiler version gating without Quartus'

project:
	$(PYTHON) build.py project $(if $(OUTPUT),--output "$(OUTPUT)")

build:
	$(PYTHON) build.py build --quartus-root "$(QUARTUS_ROOTDIR)" $(if $(OUTPUT),--output "$(OUTPUT)")

sim:
	@mkdir -p "$(SIM_OUTPUT)"
	$(VERILATOR) --cc --exe --build -O2 --top-module de25nano_blinky \
		--Mdir "$(SIM_OUTPUT)" blinky.v reset_release_model.v "$(CURDIR)/blinky_tb.cpp"
	"$(SIM_OUTPUT)/Vde25nano_blinky"

test:
	$(PYTHON) -m unittest -v test_build
