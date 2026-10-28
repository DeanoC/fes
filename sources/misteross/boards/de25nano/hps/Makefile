PYTHON ?= python3
VERILATOR ?= verilator
OUTPUT ?=
QUARTUS_ROOTDIR ?=
VENDOR_ARCHIVE ?=
MISTEROSS_ROOT := $(abspath ../../..)
SIM_OUTPUT := $(MISTEROSS_ROOT)/build/sim/de25nano-hps-mailbox

.PHONY: help project generate build sim test
help:
	@printf '%s\n' 'project: extract the locked vendor GHRD into a new private output' 'generate: add the mailbox and regenerate project IP with Quartus Pro 26.3' 'build: regenerate and compile the HPS mailbox diagnostic SOF; never program it' 'sim: exercise the real mailbox RTL with Verilator' 'test: test vendor extraction and project preparation without Quartus'

project generate build:
	$(PYTHON) build.py $@ --quartus-root "$(QUARTUS_ROOTDIR)" --vendor-archive "$(VENDOR_ARCHIVE)" $(if $(OUTPUT),--output "$(OUTPUT)")

sim:
	@mkdir -p "$(SIM_OUTPUT)"
	$(VERILATOR) --cc --exe --build -O2 --top-module fes_de25_mailbox \
		-Igenerated --Mdir "$(SIM_OUTPUT)" fes_de25_mailbox.v "$(CURDIR)/mailbox_tb.cpp"
	"$(SIM_OUTPUT)/Vfes_de25_mailbox"

test:
	$(PYTHON) -m unittest -v test_build
