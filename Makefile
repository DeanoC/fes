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
		"  sim-fes-zx81  Test the FES simple-computer GP mailbox, ZX81 machine and 720p raster" \
		"  sim-fes-coleco  Test the FES simple-computer ColecoVision slice and 720p shell" \
		"  sim-fes-coleco-oss  Test the OSS-conditional ColecoVision RAM and shell paths" \
		"  sim-fes-coleco-quartus  Test RAM/media with supplied Quartus 17 models and Icarus" \
		"  coleco-diagnostic  Generate the open Coleco Graphics I cartridge and reference image" \
		"  coleco-sprite-diagnostic  Generate the open Coleco Graphics II sprite cartridge and reference image" \
		"  build-fes-zx81-quartus  Quartus 17.0.2 bring-up package for FES ZX81" \
		"  build-fes-zx81  Seal FES ZX81 with the pinned Yosys/nextpnr-mistral tools" \
		"  build-fes-coleco-quartus  Quartus 17.0.2 bring-up package for FES ColecoVision" \
		"  build-fes-coleco  Seal FES ColecoVision with the pinned Yosys/nextpnr-mistral tools" \
		"  build-fes-pong  Build and seal standalone FES Pong with the pinned OSS tools" \
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

.PHONY: toolchain toolchain-check doctor doctor-strict sim sim-pong sim-fes-pong sim-fes-zx81 sim-fes-coleco sim-fes-coleco-oss build-fes-zx81-quartus build-fes-zx81 build-fes-coleco-quartus build-fes-coleco build-fes-pong stage-pong build-pong oss oracle compare fetch-core rebuild-core select-core export-core-bundle export-core-package program clean

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
	@build/sim/fes-pong-gp/Vfes_gp "$(CURDIR)/cores/fes-pong/generated/persistence-exchanges.json"
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

sim-fes-zx81:
	@mkdir -p build/sim/fes-zx81-gp
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall \
		-Wno-PINCONNECTEMPTY \
		-Icores/fes-zx81/generated \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-gp" \
		cores/fes-zx81/rtl/fes_computer_gp.v cores/fes-zx81/rtl/zx81_dpram.v \
		"$(CURDIR)/cores/fes-zx81/sim/gp_tb.cpp"
	@build/sim/fes-zx81-gp/Vfes_computer_gp "$(CURDIR)/cores/fes-zx81/generated/exchanges.json"
	@mkdir -p build/sim/fes-zx81-machine
	$(VERILATOR) --cc --exe --build --top-module zx81_machine -Wall \
		-DTV80_REFRESH=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-PROCASSINIT \
		-Icores/fes-zx81/generated -Icores/fes-zx81/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-machine" \
		cores/fes-zx81/rtl/zx81_machine.sv cores/fes-zx81/rtl/t80pa.v \
		cores/fes-zx81/rtl/zx81_dpram.v cores/fes-zx81/rtl/tv80/tv80_core.v \
		cores/fes-zx81/rtl/tv80/tv80_alu.v cores/fes-zx81/rtl/tv80/tv80_mcode.v \
		cores/fes-zx81/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-zx81/sim/machine_tb.cpp"
	@build/sim/fes-zx81-machine/Vzx81_machine
	@mkdir -p build/sim/fes-zx81-video
	$(VERILATOR) --cc --exe --build --top-module zx81_video_720p -Wall \
		-Wno-UNUSEDSIGNAL -Wno-WIDTHTRUNC -Wno-UNUSEDPARAM \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-video" \
		cores/fes-zx81/rtl/zx81_video_720p.v \
		"$(CURDIR)/cores/fes-zx81/sim/video_tb.cpp"
	@build/sim/fes-zx81-video/Vzx81_video_720p

.PHONY: coleco-diagnostic coleco-sprite-diagnostic
.PHONY: sim-fes-coleco-quartus
sim-fes-coleco-quartus:
	$(PYTHON) scripts/sim_fes_coleco_quartus.py

coleco-diagnostic:
	$(PYTHON) cores/fes-coleco/diagnostic/generate.py \
		--output build/diagnostics/fes-coleco/graphics-i.rom \
		--preview build/diagnostics/fes-coleco/graphics-i.ppm
	$(PYTHON) cores/fes-coleco/diagnostic/generate.py --pad-to 16384 \
		--output build/diagnostics/fes-coleco/graphics-i-16k.rom
	$(PYTHON) cores/fes-coleco/diagnostic/generate.py --interactive \
		--output build/diagnostics/fes-coleco/input.rom \
		--preview build/diagnostics/fes-coleco/input.ppm
	$(PYTHON) cores/fes-coleco/diagnostic/generate.py --interactive --pad-to 16384 \
		--output build/diagnostics/fes-coleco/input-16k.rom
	$(PYTHON) cores/fes-coleco/diagnostic/generate.py --controllers \
		--output build/diagnostics/fes-coleco/controller.rom \
		--preview build/diagnostics/fes-coleco/controller.ppm
	$(PYTHON) cores/fes-coleco/diagnostic/generate.py --controllers --pad-to 16384 \
		--output build/diagnostics/fes-coleco/controller-16k.rom

.PHONY: coleco-vdp-diagnostic sim-fes-coleco-vdp-io sim-fes-coleco-vdp-io-oss
coleco-sprite-diagnostic:
	$(PYTHON) cores/fes-coleco/diagnostic/sprite_io.py \
		--output build/diagnostics/fes-coleco/sprites.rom \
		--preview build/diagnostics/fes-coleco/sprites.ppm
	$(PYTHON) cores/fes-coleco/diagnostic/sprite_io.py --pad-to 16384 \
		--output build/diagnostics/fes-coleco/sprites-16k.rom

coleco-vdp-diagnostic: coleco-sprite-diagnostic
	$(PYTHON) cores/fes-coleco/diagnostic/vdp_io.py --output build/diagnostics/fes-coleco/vdp-io.rom \
		--preview build/diagnostics/fes-coleco/vdp-io.ppm
	$(PYTHON) cores/fes-coleco/diagnostic/vdp_io.py --pad-to 16384 --output build/diagnostics/fes-coleco/vdp-io-16k.rom

sim-fes-coleco-vdp-io sim-fes-coleco-vdp-io-oss: coleco-vdp-diagnostic
	@mkdir -p build/sim/$@
	$(VERILATOR) --cc --exe --build --top-module coleco_machine -Wall \
		-DTV80_REFRESH=1 $(if $(filter %-oss,$@),-DFES_COLECO_OSS=1 -CFLAGS "-DFES_COLECO_OSS=1") \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-PROCASSINIT \
		-Icores/fes-coleco/generated -Icores/fes-coleco/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/$@" \
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-coleco/rtl/coleco_vdp.sv \
		cores/fes-coleco/rtl/coleco_dpram.v cores/fes-coleco/rtl/coleco_video_dpram.v \
		cores/fes-coleco/rtl/t80pa.v cores/fes-coleco/rtl/tv80/tv80_core.v \
		cores/fes-coleco/rtl/tv80/tv80_alu.v cores/fes-coleco/rtl/tv80/tv80_mcode.v \
		cores/fes-coleco/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/vdp_machine_tb.cpp"
	@build/sim/$@/Vcoleco_machine build/diagnostics/fes-coleco/vdp-io.rom \
		build/diagnostics/fes-coleco/vdp-io-16k.rom

sim-fes-coleco: sim-fes-coleco-oss sim-fes-coleco-vdp-io
	@mkdir -p build/sim/fes-coleco-gp
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall \
		-Wno-PINCONNECTEMPTY \
		-Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-gp" \
		cores/fes-coleco/rtl/fes_computer_gp.v cores/fes-coleco/rtl/coleco_dpram.v \
		"$(CURDIR)/cores/fes-coleco/sim/gp_tb.cpp"
	@build/sim/fes-coleco-gp/Vfes_computer_gp
	@mkdir -p build/sim/fes-coleco-vdp
	$(VERILATOR) --cc --exe --build --top-module coleco_vdp -Wall \
		-Wno-UNUSEDSIGNAL -Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-UNUSEDPARAM \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-vdp" \
		cores/fes-coleco/rtl/coleco_vdp.sv \
		"$(CURDIR)/cores/fes-coleco/sim/vdp_tb.cpp"
	@build/sim/fes-coleco-vdp/Vcoleco_vdp
	@mkdir -p build/sim/fes-coleco-machine
	$(VERILATOR) --cc --exe --build --top-module coleco_machine -Wall \
		-DTV80_REFRESH=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-PROCASSINIT \
		-Icores/fes-coleco/generated -Icores/fes-coleco/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-machine" \
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-coleco/rtl/coleco_vdp.sv \
		cores/fes-coleco/rtl/coleco_dpram.v cores/fes-coleco/rtl/coleco_video_dpram.v \
		cores/fes-coleco/rtl/t80pa.v cores/fes-coleco/rtl/tv80/tv80_core.v \
		cores/fes-coleco/rtl/tv80/tv80_alu.v cores/fes-coleco/rtl/tv80/tv80_mcode.v \
		cores/fes-coleco/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/machine_tb.cpp"
	@build/sim/fes-coleco-machine/Vcoleco_machine
	@mkdir -p build/sim/fes-coleco-video
	$(VERILATOR) --cc --exe --build --top-module coleco_video_720p -Wall \
		-Wno-UNUSEDSIGNAL -Wno-WIDTHTRUNC -Wno-UNUSEDPARAM \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-video" \
		cores/fes-coleco/rtl/coleco_video_dpram.v cores/fes-coleco/rtl/coleco_video_720p.v \
		"$(CURDIR)/cores/fes-coleco/sim/video_tb.cpp"
	@build/sim/fes-coleco-video/Vcoleco_video_720p
	@mkdir -p build/sim/fes-coleco-board
	$(VERILATOR) --cc --exe --build --top-module top -Wall \
		-DTV80_REFRESH=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-PROCASSINIT --public-flat-rw \
		-Icores/fes-coleco/generated -Icores/fes-coleco/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-board" \
		cores/fes-coleco/sim/board_models.v cores/fes-coleco/rtl/top.v \
		cores/fes-coleco/rtl/fes_computer_gp.v cores/fes-coleco/rtl/coleco_dpram.v \
		cores/fes-coleco/rtl/coleco_machine.sv \
		cores/fes-coleco/rtl/coleco_vdp.sv cores/fes-coleco/rtl/coleco_video_dpram.v \
		cores/fes-coleco/rtl/coleco_video_720p.v \
		cores/fes-coleco/rtl/t80pa.v cores/fes-coleco/rtl/tv80/tv80_core.v \
		cores/fes-coleco/rtl/tv80/tv80_alu.v cores/fes-coleco/rtl/tv80/tv80_mcode.v \
		cores/fes-coleco/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/board_tb.cpp"
	@build/sim/fes-coleco-board/Vtop build/diagnostics/fes-coleco/graphics-i.rom \
		build/diagnostics/fes-coleco/graphics-i-16k.rom build/diagnostics/fes-coleco/graphics-i.rom
	@build/sim/fes-coleco-board/Vtop --interactive build/diagnostics/fes-coleco/input.rom \
		build/diagnostics/fes-coleco/input-16k.rom build/diagnostics/fes-coleco/input.rom
	@build/sim/fes-coleco-board/Vtop --controllers build/diagnostics/fes-coleco/controller.rom \
		build/diagnostics/fes-coleco/controller-16k.rom build/diagnostics/fes-coleco/controller.rom
	@build/sim/fes-coleco-board/Vtop --vdp-io build/diagnostics/fes-coleco/vdp-io.rom \
		build/diagnostics/fes-coleco/vdp-io-16k.rom build/diagnostics/fes-coleco/vdp-io.rom
	@build/sim/fes-coleco-board/Vtop --sprites build/diagnostics/fes-coleco/sprites.rom \
		build/diagnostics/fes-coleco/sprites-16k.rom build/diagnostics/fes-coleco/sprites.rom

sim-fes-coleco-oss: coleco-diagnostic sim-fes-coleco-vdp-io-oss
	@mkdir -p build/sim/fes-coleco-gp-oss
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall \
		-Wno-PINCONNECTEMPTY -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_COLECO_OSS=1" \
		-Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-gp-oss" \
		cores/fes-coleco/rtl/fes_computer_gp.v cores/fes-coleco/rtl/coleco_dpram.v \
		"$(CURDIR)/cores/fes-coleco/sim/gp_tb.cpp"
	@build/sim/fes-coleco-gp-oss/Vfes_computer_gp
	@mkdir -p build/sim/fes-coleco-vdp-oss
	$(VERILATOR) --cc --exe --build --top-module coleco_vdp -Wall \
		-DFES_COLECO_OSS=1 -Wno-UNUSEDSIGNAL -Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-UNUSEDPARAM \
		-CFLAGS "-DFES_COLECO_OSS=1" \
		-Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-vdp-oss" \
		cores/fes-coleco/rtl/coleco_vdp.sv cores/fes-coleco/rtl/coleco_dpram.v \
		cores/fes-coleco/rtl/coleco_video_dpram.v \
		"$(CURDIR)/cores/fes-coleco/sim/vdp_tb.cpp"
	@build/sim/fes-coleco-vdp-oss/Vcoleco_vdp
	@mkdir -p build/sim/fes-coleco-machine-oss
	$(VERILATOR) --cc --exe --build --top-module coleco_machine -Wall \
		-DTV80_REFRESH=1 -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_COLECO_OSS=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-PROCASSINIT \
		-Icores/fes-coleco/generated -Icores/fes-coleco/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-machine-oss" \
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-coleco/rtl/coleco_vdp.sv \
		cores/fes-coleco/rtl/coleco_dpram.v cores/fes-coleco/rtl/coleco_video_dpram.v \
		cores/fes-coleco/rtl/t80pa.v cores/fes-coleco/rtl/tv80/tv80_core.v \
		cores/fes-coleco/rtl/tv80/tv80_alu.v cores/fes-coleco/rtl/tv80/tv80_mcode.v \
		cores/fes-coleco/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/machine_tb.cpp"
	@build/sim/fes-coleco-machine-oss/Vcoleco_machine
	@mkdir -p build/sim/fes-coleco-board-oss
	$(VERILATOR) --cc --exe --build --top-module top -Wall \
		-DTV80_REFRESH=1 -DFES_COLECO_OSS=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-CFLAGS "-DFES_COLECO_OSS=1" \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-PROCASSINIT --public-flat-rw \
		-Icores/fes-coleco/generated -Icores/fes-coleco/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-board-oss" \
		cores/fes-coleco/sim/board_models.v cores/fes-coleco/rtl/top.v \
		cores/fes-coleco/rtl/fes_computer_gp.v cores/fes-coleco/rtl/coleco_dpram.v \
		cores/fes-coleco/rtl/coleco_video_dpram.v \
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-coleco/rtl/coleco_vdp.sv \
		cores/fes-coleco/rtl/coleco_video_720p.v \
		cores/fes-coleco/rtl/t80pa.v cores/fes-coleco/rtl/tv80/tv80_core.v \
		cores/fes-coleco/rtl/tv80/tv80_alu.v cores/fes-coleco/rtl/tv80/tv80_mcode.v \
		cores/fes-coleco/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/board_tb.cpp"
	@build/sim/fes-coleco-board-oss/Vtop build/diagnostics/fes-coleco/graphics-i.rom \
		build/diagnostics/fes-coleco/graphics-i-16k.rom build/diagnostics/fes-coleco/graphics-i.rom
	@build/sim/fes-coleco-board-oss/Vtop --interactive build/diagnostics/fes-coleco/input.rom \
		build/diagnostics/fes-coleco/input-16k.rom build/diagnostics/fes-coleco/input.rom
	@build/sim/fes-coleco-board-oss/Vtop --controllers build/diagnostics/fes-coleco/controller.rom \
		build/diagnostics/fes-coleco/controller-16k.rom build/diagnostics/fes-coleco/controller.rom
	@build/sim/fes-coleco-board-oss/Vtop --vdp-io build/diagnostics/fes-coleco/vdp-io.rom \
		build/diagnostics/fes-coleco/vdp-io-16k.rom build/diagnostics/fes-coleco/vdp-io.rom
	@build/sim/fes-coleco-board-oss/Vtop --sprites build/diagnostics/fes-coleco/sprites.rom \
		build/diagnostics/fes-coleco/sprites-16k.rom build/diagnostics/fes-coleco/sprites.rom

build-fes-zx81-quartus:
	$(PYTHON) scripts/build_fes_zx81.py --root "$(CURDIR)"

build-fes-zx81:
	$(PYTHON) scripts/build_fes_zx81_oss.py --root "$(CURDIR)"

build-fes-coleco-quartus:
	$(PYTHON) scripts/build_fes_coleco.py --root "$(CURDIR)"

build-fes-coleco:
	$(PYTHON) scripts/build_fes_coleco_oss.py --root "$(CURDIR)"

build-fes-pong:
	$(PYTHON) scripts/build_fes_pong.py --root "$(CURDIR)"

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
