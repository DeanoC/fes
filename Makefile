.DEFAULT_GOAL := help

EXP ?= 010_blinky
BUILD ?= oss
PYTHON ?= python3

help:
	@printf '%s\n' \
		"Open MiSTer OSS Cyclone V toolchain" \
		"" \
		"Public targets:" \
		"  toolchain  Build or locate the pinned repository-local OSS tools" \
		"  doctor     Report host, toolchain, oracle, and hardware readiness" \
		"  sim        Simulate an experiment with the Verilator lane" \
		"  oss        Build an experiment with the open-source FPGA lane" \
		"  oracle     Build an experiment with the explicit Quartus oracle lane" \
		"  compare    Compare OSS and oracle build results" \
		"  program    Program one volatile OSS artifact explicitly" \
		"  clean      Remove generated output for an experiment" \
		"" \
		"Variables: EXP=$(EXP) BUILD=$(BUILD) PYTHON=$(PYTHON)"

define require_exp
	@if ! printf '%s\n' '$(EXP)' | grep -Eq '^[0-9][0-9][0-9]_[a-z0-9_]+$$'; then \
		printf 'invalid EXP: %s\n' '$(EXP)' >&2; \
		exit 2; \
	fi
endef

.PHONY: toolchain toolchain-check doctor sim oss oracle compare program clean

toolchain:
	@scripts/bootstrap.sh

toolchain-check:
	@scripts/bootstrap.sh --check-prereqs

doctor:
	@printf 'target not implemented in this task\n' >&2
	@exit 2

sim oss oracle compare program clean:
	$(require_exp)
	@printf 'target not implemented in this task\n' >&2
	@exit 2
