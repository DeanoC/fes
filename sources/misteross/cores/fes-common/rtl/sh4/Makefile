VERILATOR ?= verilator
QUARTUS_ROOTDIR ?= /data/altera_pro/26.3/quartus
BUILD_DIR ?= ../../../../../../out/sh4
BUILD := $(abspath $(BUILD_DIR))
TOP := fes_sh4_bram_system
RTL := fes_sh4_icache.sv fes_sh4_dcache.sv fes_sh4_mmu.sv fes_sh4_integer.sv fes_sh4_multiplier.sv fes_sh4_fpu_regs.sv fes_sh4_fp_add.sv fes_sh4.sv $(TOP).sv
PROJECT := $(TOP).qpf $(TOP).qsf $(TOP).sdc
QUARTUS_JOBS := $(abspath ../../../../scripts/quartus_jobs.py)

.PHONY: test fit
test:
	mkdir -p "$(BUILD)"
	$(VERILATOR) --binary --timing --assert -Wall --top-module fes_sh4_tb \
		--Mdir "$(BUILD)/sim" $(RTL) fes_sh4_icache_tb.sv fes_sh4_mmu_tb.sv fes_sh4_fp_add_tb.sv fes_sh4_tb.sv
	"$(BUILD)/sim/Vfes_sh4_tb"

fit:
	mkdir -p "$(BUILD)/fit"
	cp $(RTL) $(PROJECT) "$(BUILD)/fit"
	cd "$(BUILD)/fit" && FES_QUARTUS_RUN_DIR="$(BUILD)/fit" \
		python3 "$(QUARTUS_JOBS)" run --edition pro --label agilex5-sh4 --cap-minutes 20 -- \
		"$(QUARTUS_ROOTDIR)/bin/quartus_sh" --flow compile $(TOP)
