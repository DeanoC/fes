.DEFAULT_GOAL := help

EXP ?= 010_blinky
BUILD ?= oss
CORE ?= megadrive
ARTIFACT ?= rebuild
PYTHON ?= python3
VERILATOR ?= verilator
PONG_FRAMEWORK ?= $(CURDIR)/build/frameworks/template
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
PACKAGE_MANIFEST ?=
PACKAGE_RBF ?=
PACKAGE_OUTPUT ?= $(CURDIR)/build/packages

# Pass operator-selected programming settings through the environment.  This
# avoids interpolating host/user values into a shell command; program.py does
# the strict validation before creating any subprocess.
export EXP BUILD CORE ARTIFACT PYTHON PROGRAM_TRANSPORT MISTER_HOST MISTER_USER PROGRAMMER PROGRAM_SSH PROGRAM_SCP PROGRAM_CABLE PROGRAM_CABLE_INDEX PROGRAM_EXPECTED_BOARD PROGRAM_EXPECTED_MAIN_SHA256 PROGRAM_DRY_RUN

help:
	@printf '%s\n' \
		"Open MiSTer OSS Cyclone V toolchain" \
		"" \
		"Public targets:" \
		"  toolchain  Build or locate the pinned repository-local OSS tools" \
		"  doctor     Report host, toolchain, oracle, and hardware readiness" \
		"  doctor-strict  Require host and OSS readiness (Quartus/hardware optional)" \
		"  sim        Simulate an experiment with the Verilator lane" \
		"  sim-pong   Test the standalone Pong game logic (no board wrapper)" \
		"  sim-fes-pong  Test the FES GP mailbox and fixed 720p Pong shell" \
		"  stage-pong Stage pinned MiSTer framework and local Pong sources" \
		"  build-pong Build Pong with explicit Quartus 17.0.2 (no deployment)" \
		"  oss        Build an experiment with the open-source FPGA lane" \
		"  oracle     Build an experiment with the explicit Quartus oracle lane" \
		"  compare    Compare OSS and oracle build results" \
		"  fetch-core Check out a pinned core tree and hash its upstream RBF" \
		"  rebuild-core  Compile a fetched core with Quartus 17.0.2" \
		"  select-core  Copy upstream or rebuild RBF to build/current/" \
		"  export-core-bundle  Seal a Mega Drive rebuild for FogCast handoff" \
		"  export-core-package  Seal a format-2 package directory and .fcore archive" \
		"  program    Load one artifact volatile-only (mister default; jtag optional)" \
		"  clean      Remove generated output for an experiment" \
		"" \
		"Variables: EXP=010_blinky BUILD=oss CORE=megadrive ARTIFACT=rebuild PYTHON=python3" \
		"  PACKAGE_MANIFEST/PACKAGE_RBF required for export-core-package; PACKAGE_OUTPUT defaults to build/packages" \
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

.PHONY: toolchain toolchain-check doctor doctor-strict sim sim-pong sim-fes-pong stage-pong build-pong oss oracle compare fetch-core rebuild-core select-core export-core-bundle export-core-package program clean

stage-pong:
	$(PYTHON) scripts/build_pong.py --framework "$(PONG_FRAMEWORK)" --stage-only

build-pong:
	$(PYTHON) scripts/build_pong.py --framework "$(PONG_FRAMEWORK)"

sim-pong:
	@mkdir -p build/sim/pong
	$(VERILATOR) --cc --exe --build --top-module pong_game -Wall -GCLOCK_HZ=8000 \
		--Mdir "$(CURDIR)/build/sim/pong" cores/pong/rtl/pong_game.sv "$(CURDIR)/cores/pong/sim/tb.cpp"
	@build/sim/pong/Vpong_game
	@mkdir -p build/sim/pong-video
	$(VERILATOR) --cc --exe --build --top-module pong_video -Wall \
		--Mdir "$(CURDIR)/build/sim/pong-video" cores/pong/rtl/pong_video.sv "$(CURDIR)/cores/pong/sim/video_tb.cpp"
	@build/sim/pong-video/Vpong_video

sim-fes-pong:
	@mkdir -p build/sim/fes-pong-gp
	$(VERILATOR) --cc --exe --build --top-module fes_gp -Wall \
		-Icores/fes-pong/generated \
		--Mdir "$(CURDIR)/build/sim/fes-pong-gp" \
		cores/fes-pong/rtl/fes_gp.v "$(CURDIR)/cores/fes-pong/sim/gp_tb.cpp"
	@build/sim/fes-pong-gp/Vfes_gp "$(CURDIR)/cores/fes-pong/generated/exchanges.json"
	@mkdir -p build/sim/fes-pong-video
	$(VERILATOR) --cc --exe --build --top-module fes_pong_core -Wall \
		-Icores/fes-pong/generated \
		--Mdir "$(CURDIR)/build/sim/fes-pong-video" \
		cores/fes-pong/rtl/top.v cores/fes-pong/rtl/video_720p.v \
		cores/pong/rtl/pong_game.sv "$(CURDIR)/cores/fes-pong/sim/video_tb.cpp"
	@build/sim/fes-pong-video/Vfes_pong_core
	@mkdir -p build/sim/fes-pong-board
	$(VERILATOR) --cc --exe --build --top-module top -Wall --public-flat-rw \
		-Icores/fes-pong/generated \
		--Mdir "$(CURDIR)/build/sim/fes-pong-board" \
		cores/fes-pong/sim/board_models.v cores/fes-pong/rtl/top.v \
		cores/fes-pong/rtl/fes_gp.v cores/fes-pong/rtl/video_720p.v \
		cores/pong/rtl/pong_game.sv "$(CURDIR)/cores/fes-pong/sim/board_tb.cpp"
	@build/sim/fes-pong-board/Vtop

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
	@bash -c 'set -euo pipefail; source scripts/env.sh; exec scripts/run_sim.sh --experiment "$$EXP"'

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

export-core-bundle:
	@$(PYTHON) scripts/export_core_bundle.py --core "$(CORE)" --root "$(CURDIR)"

export-core-package:
	@test -n "$(PACKAGE_MANIFEST)" || { printf '%s\n' 'PACKAGE_MANIFEST is required' >&2; exit 2; }
	@test -n "$(PACKAGE_RBF)" || { printf '%s\n' 'PACKAGE_RBF is required' >&2; exit 2; }
	@$(PYTHON) scripts/export_core_package.py --manifest "$(PACKAGE_MANIFEST)" --rbf "$(PACKAGE_RBF)" --output "$(PACKAGE_OUTPUT)"

program:
	@scripts/program.py

clean:
	$(require_exp)
	@printf 'target not implemented in this task\n' >&2
	@exit 2

.PHONY: kit-session
kit-session:
	$(PYTHON) scripts/kit.py session --owner "$${KIT_OWNER:?set KIT_OWNER}" --purpose "$${KIT_PURPOSE:?set KIT_PURPOSE}"
