PYTHON ?= python3
VERILATOR ?= verilator
OUTPUT ?=
QUARTUS_ROOTDIR ?=
VENDOR_ARCHIVE ?=
MISTEROSS_ROOT := $(abspath ../../..)
SIM_OUTPUT := $(MISTEROSS_ROOT)/build/sim/de25nano-hps-mailbox
RESET_SIM_OUTPUT := $(MISTEROSS_ROOT)/build/sim/de25nano-hps-reset
PROBE_SIM_OUTPUT := $(MISTEROSS_ROOT)/build/sim/de25nano-hps-ddr-probe

.PHONY: help project generate build sim sim-mailbox sim-reset sim-probe test
help:
	@printf '%s\n' 'project: extract the locked vendor GHRD into a new private output' 'generate: add the mailbox and regenerate project IP with Quartus Pro 26.3' 'build: regenerate and compile the HPS mailbox diagnostic SOF; never program it' 'sim: exercise the real mailbox and top reset RTL with Verilator' 'test: test vendor extraction and project preparation without Quartus'

project generate build:
	$(PYTHON) build.py $@ --quartus-root "$(QUARTUS_ROOTDIR)" --vendor-archive "$(VENDOR_ARCHIVE)" $(if $(OUTPUT),--output "$(OUTPUT)")

sim: sim-mailbox sim-reset sim-probe

sim-mailbox:
	@mkdir -p "$(SIM_OUTPUT)"
	$(VERILATOR) --cc --exe --build -O2 --top-module fes_de25_mailbox \
		-Igenerated --Mdir "$(SIM_OUTPUT)" fes_de25_mailbox.v "$(CURDIR)/mailbox_tb.cpp"
	"$(SIM_OUTPUT)/Vfes_de25_mailbox"

sim-reset:
	@mkdir -p "$(RESET_SIM_OUTPUT)"
	$(VERILATOR) --cc --exe --build -O2 --top-module fes_de25_top_reset \
		--Mdir "$(RESET_SIM_OUTPUT)" fes_de25_top_reset.v "$(CURDIR)/reset_tb.cpp"
	"$(RESET_SIM_OUTPUT)/Vfes_de25_top_reset"

sim-probe:
	@mkdir -p "$(PROBE_SIM_OUTPUT)"
	$(VERILATOR) --cc --exe --build -O2 --top-module fes_de25_ddr_probe \
		-I. --Mdir "$(PROBE_SIM_OUTPUT)" fes_de25_ddr_probe.v "$(CURDIR)/probe_tb.cpp"
	"$(PROBE_SIM_OUTPUT)/Vfes_de25_ddr_probe"

test:
	$(PYTHON) -m unittest -v test_build
