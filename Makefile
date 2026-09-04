.DEFAULT_GOAL := help

EXP ?= 010_blinky
BUILD ?= oss
CORE ?= megadrive
ARTIFACT ?= rebuild
PYTHON ?= python3
PROGRAM_TRANSPORT ?= mister
MISTER_HOST ?=
MISTER_USER ?=
PROGRAMMER ?=
PROGRAM_SSH ?=
PROGRAM_SCP ?=
PROGRAM_CABLE ?=
PROGRAM_CABLE_INDEX ?=
PROGRAM_EXPECTED_BOARD ?=
PROGRAM_EXPECTED_MAIN_SHA256 ?=
PROGRAM_DRY_RUN ?=

# Pass operator-selected programming settings through the environment.  This
# avoids interpolating host/user values into a shell command; program.py does
# the strict validation before creating any subprocess.
export EXP BUILD PYTHON PROGRAM_TRANSPORT MISTER_HOST MISTER_USER PROGRAMMER PROGRAM_SSH PROGRAM_SCP PROGRAM_CABLE PROGRAM_CABLE_INDEX PROGRAM_EXPECTED_BOARD PROGRAM_EXPECTED_MAIN_SHA256 PROGRAM_DRY_RUN

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
		"  fetch-core Check out a pinned core tree and hash its upstream RBF" \
		"  rebuild-core  Compile a fetched core with Quartus 17.0.2" \
		"  select-core  Copy upstream or rebuild RBF to build/current/" \
		"  program    Load one artifact volatile-only (mister default; jtag optional)" \
		"  clean      Remove generated output for an experiment" \
		"" \
		"Variables: EXP=010_blinky BUILD=oss CORE=megadrive ARTIFACT=rebuild PYTHON=python3" \
		"  PROGRAM_TRANSPORT=mister MISTER_HOST/MISTER_USER required for mister" \
		"  PROGRAM_EXPECTED_BOARD is required for every non-dry action (misterpi or de10nano)" \
		"  PROGRAM_EXPECTED_MAIN_SHA256 is required for non-dry mister; PROGRAM_CABLE_INDEX is rejected for USB-Blaster II" \
		"  PROGRAMMER/PROGRAM_SSH/PROGRAM_SCP/PROGRAM_CABLE are optional; PROGRAM_DRY_RUN=1 is the safe default (0/false for live)"

define require_exp
	@if ! printf '%s\n' "$$EXP" | grep -Eq '^[0-9][0-9][0-9]_[a-z0-9_]+$$'; then \
		printf 'invalid EXP: %s\n' "$$EXP" >&2; \
		exit 2; \
	fi
endef

.PHONY: toolchain toolchain-check doctor doctor-strict sim oss oracle compare fetch-core rebuild-core select-core program clean

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
	@bash -c 'set -euo pipefail; source scripts/env.sh; case "$$EXP" in 010_blinky) rtl="$$OPEN_MISTER_ROOT/experiments/010_blinky/rtl/top.v"; tb="$$OPEN_MISTER_ROOT/experiments/010_blinky/sim/tb.cpp"; model=""; ;; 020_linux_mailbox) rtl="$$OPEN_MISTER_ROOT/experiments/020_linux_mailbox/rtl/top.v"; tb="$$OPEN_MISTER_ROOT/experiments/020_linux_mailbox/sim/tb.cpp"; model="$$OPEN_MISTER_ROOT/experiments/020_linux_mailbox/sim/hps_gp_model.v"; ;; *) printf "unknown simulation experiment: %s\\n" "$$EXP" >&2; exit 2 ;; esac; sim_dir="$$OPEN_MISTER_ROOT/build/sim/$$EXP"; verilator="$$TOOLCHAIN_INSTALL/bin/verilator"; mkdir -p "$$sim_dir"; run_logged() { local log="$$1"; shift; { printf '\''command:'\''; printf '\'' %q'\'' "$$@"; printf "\\n"; "$$@"; } >"$$log" 2>&1; }; "$$PYTHON" "$$OPEN_MISTER_ROOT/scripts/doctor.py" --check-tool verilator; if [[ "$$EXP" == 020_linux_mailbox ]]; then run_logged "$$sim_dir/verilator-lint.log" "$$verilator" --lint-only --top-module top --Mdir "$$sim_dir/lint" "$$rtl" "$$model"; run_logged "$$sim_dir/verilator-build.log" "$$verilator" --cc --exe --build --public --top-module top --Mdir "$$sim_dir/obj_dir" "$$rtl" "$$model" "$$tb"; run_logged "$$sim_dir/simulation.log" "$$sim_dir/obj_dir/Vtop"; cat "$$sim_dir/simulation.log"; run_logged "$$sim_dir/verilator-wrap-build.log" "$$verilator" --cc --exe --build --public --top-module mailbox_fsm "-GSTART_SEQUENCE=8'\''hff" -GMESSAGE_BYTES=2 --Mdir "$$sim_dir/wrap_obj_dir" "$$rtl" "$$tb" -CFLAGS -DMAILBOX_WRAP; run_logged "$$sim_dir/wrap-simulation.log" "$$sim_dir/wrap_obj_dir/Vmailbox_fsm"; cat "$$sim_dir/wrap-simulation.log"; else run_logged "$$sim_dir/verilator-lint.log" "$$verilator" --lint-only --top-module top --Mdir "$$sim_dir/lint" "$$rtl"; run_logged "$$sim_dir/verilator-build.log" "$$verilator" --cc --exe --build --public --top-module top -GCOUNTER_BITS=4 --Mdir "$$sim_dir/obj_dir" "$$rtl" "$$tb"; run_logged "$$sim_dir/simulation.log" "$$sim_dir/obj_dir/Vtop"; cat "$$sim_dir/simulation.log"; fi'

oss:
	$(require_exp)
	@scripts/build_oss.sh --experiment "$$EXP"


oracle:
	$(require_exp)
	@scripts/build_oracle.sh --experiment "$$EXP"

compare:
	$(require_exp)
	@$(PYTHON) scripts/compare_builds.py --experiment "$$EXP" \
		--oss-manifest "build/oss/$$EXP/manifest.json" \
		--oracle-manifest "build/oracle/$$EXP/manifest.json" \
		--output-dir "build/compare/$$EXP"

fetch-core:
	@$(PYTHON) scripts/fetch_core.py --core "$$CORE"

rebuild-core:
	@$(PYTHON) scripts/rebuild_core.py --core "$$CORE"

select-core:
	@$(PYTHON) scripts/select_core.py --core "$$CORE" --artifact "$$ARTIFACT"

program:
	@scripts/program.py

clean:
	$(require_exp)
	@printf 'target not implemented in this task\n' >&2
	@exit 2
