PYTHON ?= python3
VERILATOR ?= verilator
JOBS ?= 8

.PHONY: help sim
help:
	@printf '%s\n' 'sim: verify masked x87 arithmetic and pipeline protocol against host x87 (x86 host)' 'Use VERILATOR=/path/to/verilator and JOBS=N to select the simulator and build parallelism.'

sim:
	$(PYTHON) run.py --verilator "$(VERILATOR)" --jobs "$(JOBS)"
