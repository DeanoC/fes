.DEFAULT_GOAL := help

EXP ?= 010_blinky
BUILD ?= oss
PYTHON ?= python3
PROGRAM_TRANSPORT ?= mister
MISTER_HOST ?=
MISTER_USER ?=
PROGRAMMER ?=
PROGRAM_SSH ?=
PROGRAM_SCP ?=
PROGRAM_CABLE ?=
PROGRAM_DRY_RUN ?=

# Pass operator-selected programming settings through the environment.  This
# avoids interpolating host/user values into a shell command; program.py does
# the strict validation before creating any subprocess.
export EXP BUILD PROGRAM_TRANSPORT MISTER_HOST MISTER_USER PROGRAMMER PROGRAM_SSH PROGRAM_SCP PROGRAM_CABLE PROGRAM_DRY_RUN

help:
	@printf '%s\n' \
		"Open MiSTer OSS Cyclone V toolchain" \
		"" \
		"Public targets:" \
		"  toolchain  Build or locate the pinned repository-local OSS tools" \
		"  doctor     Report host, toolchain, oracle, and hardware readiness" \
		"  doctor-strict  Require host and OSS readiness (Quartus/hardware optional)" \
		"  sim        Simulate an experiment with the Verilator lane" \
		"  oss        Build an experiment with the open-source FPGA lane" \
		"  oracle     Build an experiment with the explicit Quartus oracle lane" \
		"  compare    Compare OSS and oracle build results" \
		"  program    Load one artifact volatile-only (mister default; jtag optional)" \
		"  clean      Remove generated output for an experiment" \
		"" \
		"Variables: EXP=$(EXP) BUILD=$(BUILD) PYTHON=$(PYTHON)" \
		"  PROGRAM_TRANSPORT=$(PROGRAM_TRANSPORT) MISTER_HOST/MISTER_USER required for mister" \
		"  PROGRAMMER/PROGRAM_SSH/PROGRAM_SCP/PROGRAM_CABLE and PROGRAM_DRY_RUN=1 are optional"

define require_exp
	@if ! printf '%s\n' '$(EXP)' | grep -Eq '^[0-9][0-9][0-9]_[a-z0-9_]+$$'; then \
		printf 'invalid EXP: %s\n' '$(EXP)' >&2; \
		exit 2; \
	fi
endef

.PHONY: toolchain toolchain-check doctor doctor-strict sim oss oracle compare program clean

toolchain:
	@scripts/bootstrap.sh

toolchain-check:
	@scripts/bootstrap.sh --check-prereqs

doctor:
	@bash -c '. scripts/env.sh; exec "$$1" scripts/doctor.py' _ "$(PYTHON)"

doctor-strict:
	@bash -c '. scripts/env.sh; exec "$$1" scripts/doctor.py --strict oss' _ "$(PYTHON)"

sim:
	$(require_exp)
	@case "$(EXP)" in \
		010_blinky) ;; \
		*) printf 'target not implemented in this task\n' >&2; exit 2 ;; \
	esac
	@bash -c 'set -euo pipefail; source scripts/env.sh; $(PYTHON) "$$OPEN_MISTER_ROOT/scripts/doctor.py" --check-tool verilator; sim_dir="$$OPEN_MISTER_ROOT/build/sim/$(EXP)"; rtl="$$OPEN_MISTER_ROOT/experiments/$(EXP)/rtl/top.v"; tb="$$OPEN_MISTER_ROOT/experiments/$(EXP)/sim/tb.cpp"; verilator="$$TOOLCHAIN_INSTALL/bin/verilator"; mkdir -p "$$sim_dir"; run_logged() { local log="$$1"; shift; { printf '\''command:'\''; printf '\'' %q'\'' "$$@"; printf "\\n"; "$$@"; } >"$$log" 2>&1; }; run_logged "$$sim_dir/verilator-lint.log" "$$verilator" --lint-only --top-module top --Mdir "$$sim_dir/lint" "$$rtl"; run_logged "$$sim_dir/verilator-build.log" "$$verilator" --cc --exe --build --top-module top -GCOUNTER_BITS=4 --Mdir "$$sim_dir/obj_dir" "$$rtl" "$$tb"; run_logged "$$sim_dir/simulation.log" "$$sim_dir/obj_dir/Vtop"; cat "$$sim_dir/simulation.log"'

oss:
	$(require_exp)
	@scripts/build_oss.sh --experiment "$(EXP)"


oracle:
	$(require_exp)
	@scripts/build_oracle.sh --experiment "$(EXP)"

compare:
	$(require_exp)
	@$(PYTHON) scripts/compare_builds.py --experiment "$(EXP)" \
		--oss-manifest "build/oss/$(EXP)/manifest.json" \
		--oracle-manifest "build/oracle/$(EXP)/manifest.json" \
		--output-dir "build/compare/$(EXP)"

program:
	@$(PYTHON) scripts/program.py

clean:
	$(require_exp)
	@printf 'target not implemented in this task\n' >&2
	@exit 2
