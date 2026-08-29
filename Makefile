.DEFAULT_GOAL := help

EXP ?= 010_blinky
BUILD ?= oss
RUN_ID ?=
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
FOGCAST_DEV_HOST ?=
FOGCAST_DEV_USER ?=
FOGCAST_DEV_EXPECTED_BOARD ?=
FOGCAST_DEV_EXPECTED_MAIN_SHA256 ?=
FOGCAST_DEV_TOOL_SHA256 ?=
FOGCAST_DEV_SSH ?=
FOGCAST_DEV_SCP ?=
FOGCAST_DEV_DRY_RUN ?= 1

# Pass operator-selected programming settings through the environment.  This
# avoids interpolating host/user values into a shell command; program.py does
# the strict validation before creating any subprocess.
export EXP BUILD RUN_ID PYTHON PROGRAM_TRANSPORT MISTER_HOST MISTER_USER PROGRAMMER PROGRAM_SSH PROGRAM_SCP PROGRAM_CABLE PROGRAM_CABLE_INDEX PROGRAM_EXPECTED_BOARD PROGRAM_EXPECTED_MAIN_SHA256 PROGRAM_DRY_RUN
export FOGCAST_DEV_HOST FOGCAST_DEV_USER FOGCAST_DEV_EXPECTED_BOARD FOGCAST_DEV_EXPECTED_MAIN_SHA256 FOGCAST_DEV_TOOL_SHA256 FOGCAST_DEV_SSH FOGCAST_DEV_SCP FOGCAST_DEV_DRY_RUN

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
		"  dev-bundle Build a private unsigned OSS/oracle development bundle" \
		"  dev-load   Stage, run, recover, and retrieve one FogCast development result" \
		"  dev-preflight  Stage and preflight one bundle without reboot/result retrieval" \
		"  dev-fault-inject  Run the deterministic fenced kill/recovery workflow" \
		"  program    Load one artifact volatile-only (mister default; jtag optional)" \
		"  clean      Remove generated output for an experiment" \
		"" \
		"Variables: EXP=010_blinky BUILD=oss RUN_ID= PYTHON=python3" \
		"  PROGRAM_TRANSPORT=mister MISTER_HOST/MISTER_USER required for mister" \
		"  PROGRAM_EXPECTED_BOARD is required for every non-dry action (misterpi or de10nano)" \
		"  PROGRAM_EXPECTED_MAIN_SHA256 is required for non-dry mister; PROGRAM_CABLE_INDEX is rejected for USB-Blaster II" \
		"  PROGRAMMER/PROGRAM_SSH/PROGRAM_SCP/PROGRAM_CABLE are optional; PROGRAM_DRY_RUN=1 is the safe default (0/false for live)" \
		"  FogCast targets require FOGCAST_DEV_HOST/USER/EXPECTED_BOARD/EXPECTED_MAIN_SHA256/TOOL_SHA256" \
		"  FOGCAST_DEV_SSH/FOGCAST_DEV_SCP are optional pinned clients; FOGCAST_DEV_DRY_RUN=1 is the safe default"

define require_exp
	@if ! printf '%s\n' "$$EXP" | grep -Eq '^[0-9][0-9][0-9]_[a-z0-9_]+$$'; then \
		printf 'invalid EXP: %s\n' "$$EXP" >&2; \
		exit 2; \
	fi
endef

.PHONY: toolchain toolchain-check doctor doctor-strict sim oss oracle compare dev-bundle dev-load dev-preflight dev-fault-inject program clean

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

dev-bundle:
	@if [ "$$EXP" != "020_linux_mailbox" ]; then \
		printf 'dev-bundle requires EXP=020_linux_mailbox\n' >&2; \
		exit 2; \
	fi
	@if [ "$$BUILD" != "oss" ] && [ "$$BUILD" != "oracle" ]; then \
		printf 'dev-bundle requires BUILD=oss or BUILD=oracle\n' >&2; \
		exit 2; \
	fi
	@if [ -n "$$RUN_ID" ]; then \
		exec "$(PYTHON)" scripts/dev_bundle.py --experiment "$$EXP" --lane "$$BUILD" --run-id "$$RUN_ID"; \
	else \
		exec "$(PYTHON)" scripts/dev_bundle.py --experiment "$$EXP" --lane "$$BUILD"; \
	fi

define require_fogcast_dev
	@if [ "$$EXP" != "020_linux_mailbox" ]; then \
		printf 'FogCast development transport requires EXP=020_linux_mailbox\n' >&2; \
		exit 2; \
	fi
	@if [ "$$BUILD" != "oss" ] && [ "$$BUILD" != "oracle" ]; then \
		printf 'FogCast development transport requires BUILD=oss or BUILD=oracle\n' >&2; \
		exit 2; \
	fi
	@if ! printf '%s\n' "$$RUN_ID" | grep -Eq '^[0-9a-f]{32}$$'; then \
		printf 'FogCast development transport requires RUN_ID=<32-lower-hex>\n' >&2; \
		exit 2; \
	fi
endef

dev-load:
	$(require_fogcast_dev)
	@exec "$$PYTHON" scripts/fogcast_dev.py load --experiment "$$EXP" --build "$$BUILD" --run-id "$$RUN_ID"

dev-preflight:
	$(require_fogcast_dev)
	@exec "$$PYTHON" scripts/fogcast_dev.py preflight --experiment "$$EXP" --build "$$BUILD" --run-id "$$RUN_ID"

dev-fault-inject:
	$(require_fogcast_dev)
	@exec "$$PYTHON" scripts/fogcast_dev.py fault-inject --experiment "$$EXP" --build "$$BUILD" --run-id "$$RUN_ID"

program:
	@scripts/program.py

clean:
	$(require_exp)
	@printf 'target not implemented in this task\n' >&2
	@exit 2
