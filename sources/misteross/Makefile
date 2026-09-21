.DEFAULT_GOAL := help

EXP ?= 010_blinky
BUILD ?= oss
CORE ?= megadrive
ARTIFACT ?= rebuild
PYTHON ?= python3
VERILATOR ?= verilator
# Older supported Verilator releases do not define this warning category.
VERILATOR_PROCASSINIT = $(shell $(VERILATOR) -Wno-PROCASSINIT --version >/dev/null 2>&1 && echo -Wno-PROCASSINIT)
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
CACHE_ROOT ?=
GPU_DEVICES ?= 0,1

FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE := $(strip $(if $(FES_TOOLCHAIN_CACHE_ROOT),$(FES_TOOLCHAIN_CACHE_ROOT),$(CACHE_ROOT)))
FES_TOOLCHAIN_CACHE_ENV = $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),FES_TOOLCHAIN_CACHE_ROOT="$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",)

ifneq ($(strip $(FES_TOOLCHAIN_CACHE_ROOT)),)
ifneq ($(strip $(CACHE_ROOT)),)
ifneq ($(strip $(FES_TOOLCHAIN_CACHE_ROOT)),$(strip $(CACHE_ROOT)))
$(error misteross: CACHE_ROOT and FES_TOOLCHAIN_CACHE_ROOT must agree when both are set)
endif
endif
endif

# Pass operator-selected programming settings through the environment.  This
# avoids interpolating host/user values into a shell command; program.py does
# the strict validation before creating any subprocess.
export EXP BUILD CORE ARTIFACT PROGRAM_TRANSPORT MISTER_HOST MISTER_USER PROGRAMMER PROGRAM_SSH PROGRAM_SCP PROGRAM_CABLE PROGRAM_CABLE_INDEX PROGRAM_EXPECTED_BOARD PROGRAM_EXPECTED_MAIN_SHA256 PROGRAM_DRY_RUN

help:
	@printf '%s\n' \
		"Open MiSTer OSS Cyclone V toolchain" \
		"" \
		"Public targets:" \
		"  toolchain  Build or locate the pinned repository-local OSS tools" \
		"  toolchain-fes  Build the repository-local HIP toolchain for FES Pong/ZX81" \
		"  doctor     Report host, toolchain, oracle, and hardware readiness" \
		"  doctor-strict  Require host and OSS readiness (Quartus/hardware optional)" \
		"  sim        Simulate an experiment with the Verilator lane" \
		"  sim-pong   Test the standalone Pong game logic (no board wrapper)" \
		"  sim-fes-demo  Test composable application endpoints and reference demos" \
		"  build-fes-demo  Seal autonomous video application with HIP nextpnr" \
		"  build-fes-demo-media  Seal gamepad/palette-media application with HIP nextpnr" \
		"  build-fes-demo-audio  Seal gamepad/stereo-tone application with HIP nextpnr" \
		"  sim-fes-pong  Test the FES GP mailbox and fixed 720p Pong shell" \
		"  sim-fes-zx81  Test the FES simple-computer GP mailbox, ZX81 machine and 720p raster" \
		"  sim-fes-coleco  Test the FES application ColecoVision slice and native controller ports" \
		"  sim-fes-coleco-oss  Test the OSS-conditional ColecoVision RAM and shell paths" \
		"  sim-fes-coleco-unit[-oss]  Run focused Coleco unit/CPU simulations" \
		"  sim-fes-coleco-board-CASE[-oss]  Run one board case: graphics stream interactive controllers vdp-io sprites" \
		"  sim-fes-coleco-quartus  Test RAM/media with supplied Quartus 17 models and Icarus" \
		"  coleco-diagnostic  Generate the open Coleco Graphics I cartridge and reference image" \
		"  coleco-sprite-diagnostic  Generate the open Coleco Graphics II sprite cartridge and reference image" \
		"  build-fes-zx81-quartus  Quartus 17.0.2 oracle package for FES ZX81" \
		"  build-fes-zx81  Seal FES ZX81 HIP (CACHE_ROOT=; BEST_FMAX=1 GPU_DEVICES=0,1 searches weight/seed)" \
		"  build-fes-coleco-quartus  Quartus 17.0.2 oracle package for FES ColecoVision" \
		"  build-fes-coleco  Seal FES ColecoVision HIP (CACHE_ROOT=; BEST_FMAX=1 GPU_DEVICES=0,1 searches weight/seed)" \
		"  sim-fes-sg1000  Test the FES simple-computer SG-1000 machine (Coleco sibling)" \
		"  sim-fes-sg1000-oss  Test OSS-conditional SG-1000 registered media, RAM and VDP" \
		"  sim-fes-sg1000-quartus  Test SG-1000 RAM/media with supplied Quartus 17 models and Icarus" \
		"  sg1000-diagnostic  Generate the open SG-1000 Graphics I cartridge and reference image" \
		"  build-fes-sg1000-quartus  Quartus 17.0.2 oracle package for FES SG-1000" \
		"  build-fes-sg1000  Seal FES SG-1000 with HIP nextpnr/Mistral (Coleco lock; CACHE_ROOT= for shared cache)" \
		"  sim-fes-sms  Test the FES SMS blob-stream mailbox, 32KiB map, SN76489 and HDMI I2S (fes.sms)" \
		"  sim-fes-sms-oss  Test OSS-conditional SMS registered media, RAM and VDP" \
		"  sim-fes-sms-quartus  Test SMS RAM/media with supplied Quartus 17 models and Icarus" \
		"  sms-diagnostic  Generate the open Master System Mode 4 cartridge and reference image" \
		"  build-fes-sms-quartus  Quartus 17.0.2 oracle package for FES Master System (fes.sms)" \
		"  build-fes-sms  Seal FES Master System with HIP nextpnr/Mistral (Coleco lock; CACHE_ROOT= for shared cache)" \
		"  build-fes-pong  Seal FES Pong with HIP nextpnr/Mistral (CACHE_ROOT= for shared cache)" \
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
		"  CACHE_ROOT= optional shared toolchain cache root for build-fes-pong/zx81/coleco/sg1000/sms" \
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

define require_local_sim
	@if [ -n "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)" ]; then \
		printf '%s\n' 'misteross: shared toolchain cache is unsupported for Make simulation targets; use an FES Python recipe or unset FES_TOOLCHAIN_CACHE_ROOT for the local simulation lane' >&2; \
		exit 2; \
	fi
endef

# GNU Make adds jobserver and verbosity controls to MAKEFLAGS/MFLAGS.  They
# describe this parent invocation, not the immutable shared compiler lane;
# clear them only for shared subprocesses while preserving the requested JOBS
# setting used by bootstrap.  CACHE_ROOT selects the FES producer shared
# path via --cache-root; FES_TOOLCHAIN_CACHE_ROOT remains the bootstrap
# shared-cache selector.  Either one must drop parent Make flags.
SHARED_MAKE_ENV = $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT)$(CACHE_ROOT)),MAKEFLAGS= MFLAGS= ,)
FES_SHARED_MAKE_ENV = $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),MAKEFLAGS= MFLAGS= ,)

# Reject an explicitly requested legacy simulation goal while Make is still
# parsing the graph.  This runs before any prerequisite (including diagnostic
# ROM generation) can write output; the recipe guard above also covers an
# indirect invocation through a future aggregate target.
ifneq ($(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),)
_SHARED_LEGACY_SIM_GOALS := $(filter sim sim-pong sim-fes-pong sim-fes-zx81 sim-fes-coleco sim-fes-coleco-% sim-fes-sg1000 sim-fes-sg1000-oss sim-fes-sg1000-quartus sim-fes-sms sim-fes-sms-oss sim-fes-sms-quartus,$(MAKECMDGOALS))
ifneq ($(strip $(_SHARED_LEGACY_SIM_GOALS)),)
$(error misteross: shared toolchain cache is unsupported for Make simulation targets; unset FES_TOOLCHAIN_CACHE_ROOT for the local simulation lane)
endif
endif

.PHONY: toolchain toolchain-fes toolchain-fes-coleco toolchain-fes-sg1000 toolchain-fes-sms toolchain-check doctor doctor-strict sim sim-pong sim-fes-pong sim-fes-zx81 sim-fes-coleco sim-fes-coleco-oss sim-fes-sg1000 sim-fes-sg1000-oss sim-fes-sg1000-quartus sg1000-diagnostic sim-fes-sms sim-fes-sms-oss sim-fes-sms-quartus sms-diagnostic build-fes-zx81-quartus build-fes-zx81 build-fes-coleco-quartus build-fes-coleco build-fes-sg1000-quartus build-fes-sg1000 build-fes-sms-quartus build-fes-sms build-fes-pong stage-pong build-pong oss oracle compare fetch-core rebuild-core select-core export-core-bundle export-core-package program clean

stage-pong:
	$(PYTHON) scripts/build_pong.py --framework "$(PONG_FRAMEWORK)" --stage-only

build-pong:
	$(PYTHON) scripts/build_pong.py --framework "$(PONG_FRAMEWORK)"

sim-pong:
	$(require_local_sim)
	@mkdir -p build/sim/pong
	$(VERILATOR) --cc --exe --build --top-module pong_game -Wall -GCLOCK_HZ=8000 \
		--Mdir "$(CURDIR)/build/sim/pong" cores/pong/rtl/pong_game.sv "$(CURDIR)/cores/pong/sim/tb.cpp"
	@build/sim/pong/Vpong_game
	@mkdir -p build/sim/pong-video
	$(VERILATOR) --cc --exe --build --top-module pong_video -Wall \
		--Mdir "$(CURDIR)/build/sim/pong-video" cores/pong/rtl/pong_video.sv "$(CURDIR)/cores/pong/sim/video_tb.cpp"
	@build/sim/pong-video/Vpong_video

.PHONY: sim-fes-demo build-fes-demo build-fes-demo-media
sim-fes-demo:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_demo.py --verilator "$(VERILATOR)"

build-fes-demo:
	$(FES_SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_demo.py $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),--cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",)

build-fes-demo-media:
	$(FES_SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_demo.py --media $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),--cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",)

.PHONY: build-fes-demo-audio
build-fes-demo-audio:
	$(FES_SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_demo.py --audio $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),--cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",)

sim-fes-pong:
	$(require_local_sim)
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
		cores/fes-pong/rtl/top.v cores/fes-common/rtl/fes_video_720p.v \
		cores/pong/rtl/pong_game.sv "$(CURDIR)/cores/fes-pong/sim/video_tb.cpp"
	@build/sim/fes-pong-video/Vfes_pong_core
	@mkdir -p build/sim/fes-pong-board
	$(VERILATOR) --cc --exe --build --top-module top -Wall --public-flat-rw \
		-Icores/fes-pong/generated \
		--Mdir "$(CURDIR)/build/sim/fes-pong-board" \
		cores/fes-pong/sim/board_models.v cores/fes-pong/rtl/top.v \
		cores/fes-pong/rtl/fes_gp.v cores/fes-common/rtl/fes_video_720p.v \
		cores/pong/rtl/pong_game.sv "$(CURDIR)/cores/fes-pong/sim/board_tb.cpp"
	@build/sim/fes-pong-board/Vtop

.PHONY: sim-fes-zx81-expansion
sim-fes-zx81-expansion:
	$(require_local_sim)
	@for pack in 0 1; do \
		mkdir -p build/sim/fes-zx81-expansion-$$pack; \
		$(VERILATOR) --cc --exe --build --top-module expansion_machine -Wall \
		-GPACK_PRESENT=$$pack -DTV80_REFRESH=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-zx81/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-expansion-$$pack" \
		cores/fes-zx81/sim/expansion_machine.v \
		cores/fes-zx81/expansions/ram16k.v \
		experiments/901_plugged_base/sim/mistral_ff_model.v \
		cores/fes-zx81/rtl/zx81_machine.sv cores/fes-zx81/rtl/t80pa.v \
		cores/fes-zx81/rtl/zx81_dpram.v cores/fes-zx81/rtl/zx81_ram_socket.v \
		cores/fes-zx81/rtl/zx81_ram_pack.v cores/fes-zx81/rtl/tv80/tv80_core.v \
		cores/fes-zx81/rtl/tv80/tv80_alu.v cores/fes-zx81/rtl/tv80/tv80_mcode.v \
		cores/fes-zx81/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-zx81/sim/expansion_tb.cpp" || exit $$?; \
		if [ $$pack = 0 ]; then ramtop=0x4400; else ramtop=0x8000; fi; \
		build/sim/fes-zx81-expansion-$$pack/Vexpansion_machine $$ramtop || exit $$?; \
	done

sim-fes-zx81: sim-fes-zx81-expansion
	$(require_local_sim)
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
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
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
	$(require_local_sim)
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

.PHONY: coleco-vdp-diagnostic sim-fes-coleco-vdp-io sim-fes-coleco-vdp-io-oss sim-fes-coleco-graphics sim-fes-coleco-graphics-oss
sim-fes-coleco-graphics sim-fes-coleco-graphics-oss:
	$(require_local_sim)
	@mkdir -p build/sim/$@
	$(VERILATOR) --cc --exe --build --top-module coleco_vdp -Wall \
		$(if $(filter %-oss,$@),-DFES_COLECO_OSS=1,) \
		-Wno-UNUSEDSIGNAL -Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-UNUSEDPARAM \
		--Mdir "$(CURDIR)/build/sim/$@" \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-common/rtl/coleco_dpram.v \
		cores/fes-common/rtl/coleco_video_dpram.v \
		"$(CURDIR)/cores/fes-coleco/sim/graphics_tb.cpp"
	@build/sim/$@/Vcoleco_vdp

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
	$(require_local_sim)
	@mkdir -p build/sim/$@
	$(VERILATOR) --cc --exe --build --top-module coleco_machine -Wall \
		-DTV80_REFRESH=1 $(if $(filter %-oss,$@),-DFES_COLECO_OSS=1 -CFLAGS "-DFES_COLECO_OSS=1") \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/$@" \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-common/rtl/fes_audio_i2s.v cores/fes-common/rtl/fes_audio_output.v \
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/vdp_machine_tb.cpp"
	@build/sim/$@/Vcoleco_machine build/diagnostics/fes-coleco/vdp-io.rom \
		build/diagnostics/fes-coleco/vdp-io-16k.rom

# Each board case is a separate CI shard. The shared build prerequisite runs
.PHONY: sim-fes-coleco-audio
sim-fes-coleco-audio:
	$(require_local_sim)
	@mkdir -p build/sim/fes-psg build/sim/fes-audio-output
	$(VERILATOR) --cc --exe --build --top-module fes_sn76489 -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-psg" cores/fes-common/rtl/fes_sn76489.sv \
		"$(CURDIR)/cores/fes-coleco/sim/psg_tb.cpp"
	@build/sim/fes-psg/Vfes_sn76489
	$(VERILATOR) --cc --exe --build --top-module fes_audio_output -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-audio-output" \
		cores/fes-common/rtl/fes_audio_output.v cores/fes-common/rtl/fes_audio_i2s.v \
		"$(CURDIR)/cores/fes-coleco/sim/audio_output_tb.cpp"
	@build/sim/fes-audio-output/Vfes_audio_output

# Each board case is a separate CI shard. The shared build prerequisite runs
# once per lane in a Make invocation; diagnostics finish before their readers.
COLECO_BOARD_CASES := graphics stream interactive controllers vdp-io sprites
COLECO_BOARD_TARGETS := $(addprefix sim-fes-coleco-board-,$(COLECO_BOARD_CASES))
.PHONY: sim-fes-coleco-unit sim-fes-coleco-unit-oss sim-fes-coleco-board-build sim-fes-coleco-board-build-oss $(COLECO_BOARD_TARGETS) $(addsuffix -oss,$(COLECO_BOARD_TARGETS))
sim-fes-coleco: sim-fes-coleco-unit $(COLECO_BOARD_TARGETS) sim-fes-coleco-oss
sim-fes-coleco-oss: sim-fes-coleco-unit-oss $(addsuffix -oss,$(COLECO_BOARD_TARGETS))

$(COLECO_BOARD_TARGETS): sim-fes-coleco-board-build
$(addsuffix -oss,$(COLECO_BOARD_TARGETS)): sim-fes-coleco-board-build-oss

.PHONY: sim-fes-coleco-firmware sim-fes-coleco-firmware-oss
sim-fes-coleco-firmware-oss: COLECO_FIRMWARE_FLAGS = -DFES_COLECO_OSS=1
sim-fes-coleco-firmware sim-fes-coleco-firmware-oss:
	$(require_local_sim)
	@mkdir -p build/sim/$@
	$(VERILATOR) --cc --exe --build --top-module firmware_model -Wall \
		-DTV80_REFRESH=1 $(COLECO_FIRMWARE_FLAGS) \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-coleco/generated -Icores/fes-common/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/$@" \
		cores/fes-coleco/sim/firmware_model.sv \
		cores/fes-common/rtl/fes_sn76489.sv \
		cores/fes-coleco/rtl/coleco_application_gp.v cores/fes-common/rtl/fes_application_gp.v \
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/firmware_tb.cpp"
	@build/sim/$@/Vfirmware_model

sim-fes-coleco-unit: sim-fes-coleco-audio sim-fes-coleco-graphics sim-fes-coleco-stream sim-fes-coleco-vdp-io sim-fes-coleco-firmware
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-gp
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall \
		-Wno-PINCONNECTEMPTY \
		-Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-gp" \
		cores/fes-common/rtl/fes_computer_gp.v cores/fes-common/rtl/coleco_dpram.v \
		"$(CURDIR)/cores/fes-coleco/sim/gp_tb.cpp"
	@build/sim/fes-coleco-gp/Vfes_computer_gp
	@mkdir -p build/sim/fes-coleco-vdp
	$(VERILATOR) --cc --exe --build --top-module coleco_vdp -Wall \
		-Wno-UNUSEDSIGNAL -Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-UNUSEDPARAM \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-vdp" \
		cores/fes-common/rtl/coleco_vdp.sv \
		"$(CURDIR)/cores/fes-coleco/sim/vdp_tb.cpp"
	@build/sim/fes-coleco-vdp/Vcoleco_vdp
	@mkdir -p build/sim/fes-coleco-machine
	$(VERILATOR) --cc --exe --build --top-module coleco_machine -Wall \
		-DTV80_REFRESH=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-machine" \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-common/rtl/fes_audio_i2s.v cores/fes-common/rtl/fes_audio_output.v \
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/machine_tb.cpp"
	@build/sim/fes-coleco-machine/Vcoleco_machine
	@mkdir -p build/sim/fes-coleco-video
	$(VERILATOR) --cc --exe --build --top-module coleco_video_720p -Wall \
		-Wno-UNUSEDSIGNAL -Wno-WIDTHTRUNC -Wno-UNUSEDPARAM \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-video" \
		cores/fes-common/rtl/coleco_video_dpram.v cores/fes-common/rtl/coleco_video_720p.v \
		"$(CURDIR)/cores/fes-coleco/sim/video_tb.cpp"
	@build/sim/fes-coleco-video/Vcoleco_video_720p

sim-fes-coleco-board-build:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-board
	$(VERILATOR) --cc --exe --build --top-module top -Wall \
		-DTV80_REFRESH=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) --public-flat-rw \
		-Icores/fes-common/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-board" \
		cores/fes-coleco/sim/board_models.v cores/fes-coleco/rtl/top.v \
		cores/fes-common/rtl/fes_application_gp.v cores/fes-coleco/rtl/coleco_application_gp.v cores/fes-common/rtl/coleco_dpram.v \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-common/rtl/fes_audio_i2s.v cores/fes-common/rtl/fes_audio_output.v \
		cores/fes-coleco/rtl/coleco_machine.sv \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/coleco_video_720p.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/board_tb.cpp"

sim-fes-coleco-unit-oss: sim-fes-coleco-graphics-oss sim-fes-coleco-stream-oss sim-fes-coleco-vdp-io-oss sim-fes-coleco-firmware-oss
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-gp-oss
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall \
		-Wno-PINCONNECTEMPTY -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_COLECO_OSS=1" \
		-Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-gp-oss" \
		cores/fes-common/rtl/fes_computer_gp.v cores/fes-common/rtl/coleco_dpram.v \
		"$(CURDIR)/cores/fes-coleco/sim/gp_tb.cpp"
	@build/sim/fes-coleco-gp-oss/Vfes_computer_gp
	@mkdir -p build/sim/fes-coleco-vdp-oss
	$(VERILATOR) --cc --exe --build --top-module coleco_vdp -Wall \
		-DFES_COLECO_OSS=1 -Wno-UNUSEDSIGNAL -Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-UNUSEDPARAM \
		-CFLAGS "-DFES_COLECO_OSS=1" \
		-Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-vdp-oss" \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-common/rtl/coleco_dpram.v \
		cores/fes-common/rtl/coleco_video_dpram.v \
		"$(CURDIR)/cores/fes-coleco/sim/vdp_tb.cpp"
	@build/sim/fes-coleco-vdp-oss/Vcoleco_vdp
	@mkdir -p build/sim/fes-coleco-machine-oss
	$(VERILATOR) --cc --exe --build --top-module coleco_machine -Wall \
		-DTV80_REFRESH=1 -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_COLECO_OSS=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-machine-oss" \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-common/rtl/fes_audio_i2s.v cores/fes-common/rtl/fes_audio_output.v \
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/machine_tb.cpp"
	@build/sim/fes-coleco-machine-oss/Vcoleco_machine

sim-fes-coleco-board-build-oss:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-board-oss
	$(VERILATOR) --cc --exe --build --top-module top -Wall \
		-DTV80_REFRESH=1 -DFES_COLECO_OSS=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-CFLAGS "-DFES_COLECO_OSS=1" \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) --public-flat-rw \
		-Icores/fes-common/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-board-oss" \
		cores/fes-coleco/sim/board_models.v cores/fes-coleco/rtl/top.v \
		cores/fes-common/rtl/fes_application_gp.v cores/fes-coleco/rtl/coleco_application_gp.v cores/fes-common/rtl/coleco_dpram.v \
		cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-common/rtl/fes_audio_i2s.v cores/fes-common/rtl/fes_audio_output.v \
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_video_720p.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/board_tb.cpp"

sim-fes-coleco-board-graphics sim-fes-coleco-board-graphics-oss: coleco-diagnostic
	@build/sim/fes-coleco-board$(if $(filter %-oss,$@),-oss)/Vtop build/diagnostics/fes-coleco/graphics-i.rom \
		build/diagnostics/fes-coleco/graphics-i-16k.rom build/diagnostics/fes-coleco/graphics-i.rom

sim-fes-coleco-board-stream sim-fes-coleco-board-stream-oss: coleco-stream-diagnostic coleco-diagnostic
	@build/sim/fes-coleco-board$(if $(filter %-oss,$@),-oss)/Vtop build/diagnostics/fes-coleco/stream-32k.rom \
		build/diagnostics/fes-coleco/stream-24k.rom build/diagnostics/fes-coleco/stream-odd.rom build/diagnostics/fes-coleco/graphics-i.rom

sim-fes-coleco-board-interactive sim-fes-coleco-board-interactive-oss: coleco-diagnostic
	@build/sim/fes-coleco-board$(if $(filter %-oss,$@),-oss)/Vtop --interactive build/diagnostics/fes-coleco/input.rom \
		build/diagnostics/fes-coleco/input-16k.rom build/diagnostics/fes-coleco/input.rom

sim-fes-coleco-board-controllers sim-fes-coleco-board-controllers-oss: coleco-diagnostic
	@build/sim/fes-coleco-board$(if $(filter %-oss,$@),-oss)/Vtop --controllers build/diagnostics/fes-coleco/controller.rom \
		build/diagnostics/fes-coleco/controller-16k.rom build/diagnostics/fes-coleco/controller.rom

sim-fes-coleco-board-vdp-io sim-fes-coleco-board-vdp-io-oss: coleco-vdp-diagnostic
	@build/sim/fes-coleco-board$(if $(filter %-oss,$@),-oss)/Vtop --vdp-io build/diagnostics/fes-coleco/vdp-io.rom \
		build/diagnostics/fes-coleco/vdp-io-16k.rom build/diagnostics/fes-coleco/vdp-io.rom

sim-fes-coleco-board-sprites sim-fes-coleco-board-sprites-oss: coleco-sprite-diagnostic
	@build/sim/fes-coleco-board$(if $(filter %-oss,$@),-oss)/Vtop --sprites build/diagnostics/fes-coleco/sprites.rom \
		build/diagnostics/fes-coleco/sprites-16k.rom build/diagnostics/fes-coleco/sprites.rom

build-fes-zx81-quartus:
	$(PYTHON) scripts/build_fes_zx81.py --root "$(CURDIR)"

build-fes-zx81:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_zx81_oss.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)$(if $(filter 1,$(BEST_FMAX)), --best-fmax$(if $(strip $(GPU_DEVICES)), --gpu-devices "$(GPU_DEVICES)",),)

build-fes-coleco-quartus:
	$(PYTHON) scripts/build_fes_coleco.py --root "$(CURDIR)"

build-fes-coleco:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_coleco_oss.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)$(if $(filter 1,$(BEST_FMAX)), --best-fmax$(if $(strip $(GPU_DEVICES)), --gpu-devices "$(GPU_DEVICES)",),)

.PHONY: sg1000-diagnostic
sg1000-diagnostic:
	@mkdir -p build/diagnostics/fes-sg1000
	$(PYTHON) cores/fes-sg1000/diagnostic/generate.py \
		--output build/diagnostics/fes-sg1000/graphics-i.rom \
		--preview build/diagnostics/fes-sg1000/graphics-i.ppm
	$(PYTHON) cores/fes-sg1000/diagnostic/generate.py --pad-to 16384 \
		--output build/diagnostics/fes-sg1000/graphics-i-16k.rom
	$(PYTHON) cores/fes-sg1000/diagnostic/generate.py --controllers \
		--output build/diagnostics/fes-sg1000/controller.rom \
		--preview build/diagnostics/fes-sg1000/controller.ppm
	$(PYTHON) cores/fes-sg1000/diagnostic/generate.py --controllers --pad-to 16384 \
		--output build/diagnostics/fes-sg1000/controller-16k.rom

sim-fes-sg1000: sg1000-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-sg1000-machine
	$(VERILATOR) --cc --exe --build --top-module sg1000_machine -Wall \
		-DTV80_REFRESH=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		-Icores/fes-sg1000/generated -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-sg1000-machine" \
		cores/fes-sg1000/rtl/sg1000_machine.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-sg1000/sim/machine_tb.cpp"
	@build/sim/fes-sg1000-machine/Vsg1000_machine \
		build/diagnostics/fes-sg1000/graphics-i.rom \
		build/diagnostics/fes-sg1000/controller.rom

sim-fes-sg1000-oss: sg1000-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-sg1000-machine-oss
	$(VERILATOR) --cc --exe --build --top-module sg1000_machine -Wall \
		-DTV80_REFRESH=1 -DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		-Icores/fes-sg1000/generated -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-sg1000-machine-oss" \
		cores/fes-sg1000/rtl/sg1000_machine.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-sg1000/sim/machine_tb.cpp"
	@build/sim/fes-sg1000-machine-oss/Vsg1000_machine \
		build/diagnostics/fes-sg1000/graphics-i.rom \
		build/diagnostics/fes-sg1000/controller.rom

.PHONY: sim-fes-sg1000-quartus
sim-fes-sg1000-quartus:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_sg1000_quartus.py

build-fes-sg1000-quartus:
	$(PYTHON) scripts/build_fes_sg1000.py --root "$(CURDIR)"

build-fes-sg1000:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_sg1000_oss.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

.PHONY: sms-diagnostic
sms-diagnostic:
	@mkdir -p build/diagnostics/fes-sms
	$(PYTHON) cores/fes-sms/diagnostic/generate.py \
		--output build/diagnostics/fes-sms/mode4.rom \
		--preview build/diagnostics/fes-sms/mode4.ppm
	$(PYTHON) cores/fes-sms/diagnostic/generate.py --pad-to 32768 \
		--output build/diagnostics/fes-sms/mode4-32k.rom
	$(PYTHON) cores/fes-sms/diagnostic/generate.py --interactive \
		--output build/diagnostics/fes-sms/mode4-hil.rom \
		--preview build/diagnostics/fes-sms/mode4-hil.ppm
	$(PYTHON) cores/fes-sms/diagnostic/generate.py --interactive --pad-to 32768 \
		--output build/diagnostics/fes-sms/mode4-hil-32k.rom

sim-fes-sms: sms-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-sms-gp
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall --public \
		-Wno-PINCONNECTEMPTY -Wno-UNUSEDPARAM -Wno-UNUSEDSIGNAL \
		-Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-BLKSEQ \
		-GENABLE_MEDIA_STREAM=1 \
		-Icores/fes-sms/generated -Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-sms-gp" \
		cores/fes-common/rtl/fes_computer_gp.v cores/fes-common/rtl/coleco_dpram.v \
		"$(CURDIR)/cores/fes-sms/sim/stream_tb.cpp"
	@build/sim/fes-sms-gp/Vfes_computer_gp \
		"$(CURDIR)/cores/fes-sms/generated/stream-exchanges.json"
	@mkdir -p build/sim/fes-sms-vdp
	$(VERILATOR) --cc --exe --build --top-module sms_vdp -Wall \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		--Mdir "$(CURDIR)/build/sim/fes-sms-vdp" \
		cores/fes-sms/rtl/sms_vdp.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		"$(CURDIR)/cores/fes-sms/sim/vdp_tb.cpp"
	@build/sim/fes-sms-vdp/Vsms_vdp
	@mkdir -p build/sim/fes-sms-psg
	$(VERILATOR) --cc --exe --build --top-module sms_psg -Wall \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-BLKSEQ \
		--Mdir "$(CURDIR)/build/sim/fes-sms-psg" \
		cores/fes-sms/rtl/sms_psg.sv \
		"$(CURDIR)/cores/fes-sms/sim/psg_tb.cpp"
	@build/sim/fes-sms-psg/Vsms_psg
	@mkdir -p build/sim/fes-sms-i2s
	$(VERILATOR) --cc --exe --build --top-module sms_hdmi_i2s -Wall \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-BLKSEQ \
		--Mdir "$(CURDIR)/build/sim/fes-sms-i2s" \
		cores/fes-sms/rtl/sms_hdmi_i2s.v \
		"$(CURDIR)/cores/fes-sms/sim/i2s_tb.cpp"
	@build/sim/fes-sms-i2s/Vsms_hdmi_i2s
	@mkdir -p build/sim/fes-sms-machine
	$(VERILATOR) --cc --exe --build --top-module sms_machine -Wall \
		-DTV80_REFRESH=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		-Icores/fes-sms/generated -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-sms-machine" \
		cores/fes-sms/rtl/sms_machine.sv cores/fes-sms/rtl/sms_vdp.sv \
		cores/fes-sms/rtl/sms_psg.sv \
		cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-sms/sim/machine_tb.cpp"
	@build/sim/fes-sms-machine/Vsms_machine \
		build/diagnostics/fes-sms/mode4.rom

sim-fes-sms-oss: sms-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-sms-gp-oss
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall --public \
		-DFES_COLECO_OSS=1 -CFLAGS "-DFES_COLECO_OSS=1" \
		-Wno-PINCONNECTEMPTY -Wno-UNUSEDPARAM -Wno-UNUSEDSIGNAL \
		-Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-BLKSEQ \
		-GENABLE_MEDIA_STREAM=1 \
		-Icores/fes-sms/generated -Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-sms-gp-oss" \
		cores/fes-common/rtl/fes_computer_gp.v cores/fes-common/rtl/coleco_dpram.v \
		"$(CURDIR)/cores/fes-sms/sim/stream_tb.cpp"
	@build/sim/fes-sms-gp-oss/Vfes_computer_gp \
		"$(CURDIR)/cores/fes-sms/generated/stream-exchanges.json"
	@mkdir -p build/sim/fes-sms-vdp-oss
	$(VERILATOR) --cc --exe --build --top-module sms_vdp -Wall \
		-DFES_COLECO_OSS=1 -CFLAGS "-DFES_COLECO_OSS=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		--Mdir "$(CURDIR)/build/sim/fes-sms-vdp-oss" \
		cores/fes-sms/rtl/sms_vdp.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		"$(CURDIR)/cores/fes-sms/sim/vdp_tb.cpp"
	@build/sim/fes-sms-vdp-oss/Vsms_vdp
	@mkdir -p build/sim/fes-sms-machine-oss
	$(VERILATOR) --cc --exe --build --top-module sms_machine -Wall \
		-DTV80_REFRESH=1 -DFES_SMS_OSS=1 -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_SMS_OSS=1 -DFES_COLECO_OSS=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		-Icores/fes-sms/generated -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-sms-machine-oss" \
		cores/fes-sms/rtl/sms_machine.sv cores/fes-sms/rtl/sms_vdp.sv \
		cores/fes-sms/rtl/sms_psg.sv \
		cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-sms/sim/machine_tb.cpp"
	@build/sim/fes-sms-machine-oss/Vsms_machine \
		build/diagnostics/fes-sms/mode4.rom

.PHONY: sim-fes-sms-quartus
sim-fes-sms-quartus:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_sms_quartus.py

build-fes-sms-quartus:
	$(PYTHON) scripts/build_fes_sms.py --root "$(CURDIR)"

build-fes-sms:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_sms_oss.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)


build-fes-pong:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_pong.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

toolchain:
	@$(SHARED_MAKE_ENV)scripts/bootstrap.sh

toolchain-fes:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

.PHONY: toolchain-zx81-expansion
toolchain-zx81-expansion:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/toolchains/zx81-expansion.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/zx81-expansion") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-coleco:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/toolchains/registered-memory.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-coleco") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-sg1000:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/toolchains/registered-memory.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-sg1000") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-sms:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/toolchains/registered-memory.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-sms") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-check:
	@$(SHARED_MAKE_ENV)scripts/bootstrap.sh --check-prereqs

doctor:
	@if [ -n "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)" ]; then \
		$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) "$(PYTHON)" scripts/doctor.py; \
	else \
		bash -c '. scripts/env.sh; exec "$$1" scripts/doctor.py' _ "$(PYTHON)"; \
	fi

doctor-strict:
	@if [ -n "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)" ]; then \
		$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) "$(PYTHON)" scripts/doctor.py --strict oss; \
	else \
		bash -c '. scripts/env.sh; exec "$$1" scripts/doctor.py --strict oss' _ "$(PYTHON)"; \
	fi

sim:
	$(require_exp)
	$(require_local_sim)
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

.PHONY: sim-fes-coleco-stream sim-fes-coleco-stream-oss
sim-fes-coleco-stream sim-fes-coleco-stream-oss:
	$(require_local_sim)
	@mkdir -p build/sim/$@
	$(VERILATOR) --cc --exe --build --top-module coleco_application_gp -Wall \
		$(if $(findstring -oss,$@),-DFES_COLECO_OSS=1,) \
		-Wno-PINCONNECTEMPTY -Wno-UNUSEDSIGNAL --public-flat-rw \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/$@" \
		cores/fes-common/rtl/fes_application_gp.v cores/fes-coleco/rtl/coleco_application_gp.v \
		cores/fes-common/rtl/coleco_dpram.v "$(CURDIR)/cores/fes-coleco/sim/stream_tb.cpp"
	@build/sim/$@/Vcoleco_application_gp cores/fes-common/generated/stream-exchanges.json

.PHONY: coleco-stream-diagnostic
coleco-stream-diagnostic:
	python3 cores/fes-coleco/diagnostic/generate.py --stream-size 24576 --output build/diagnostics/fes-coleco/stream-24k.rom
	python3 cores/fes-coleco/diagnostic/generate.py --stream-size 32767 --output build/diagnostics/fes-coleco/stream-odd.rom
	python3 cores/fes-coleco/diagnostic/generate.py --stream-size 32768 --output build/diagnostics/fes-coleco/stream-32k.rom --preview build/diagnostics/fes-coleco/stream-pass.ppm
