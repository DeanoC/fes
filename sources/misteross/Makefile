.DEFAULT_GOAL := help

EXP ?= 010_blinky
BUILD ?= oss
PYTHON ?= python3
VERILATOR ?= verilator
# Older supported Verilator releases do not define these warning categories.
VERILATOR_PROCASSINIT = $(shell $(VERILATOR) -Wno-PROCASSINIT --version >/dev/null 2>&1 && echo -Wno-PROCASSINIT)
VERILATOR_SIMILARNAME = $(shell $(VERILATOR) -Wno-SIMILARNAME --version >/dev/null 2>&1 && echo -Wno-SIMILARNAME)
PROGRAM_TRANSPORT ?= jtag
PROGRAMMER ?=
PROGRAM_CABLE ?=
PROGRAM_CABLE_INDEX ?=
PROGRAM_EXPECTED_BOARD ?=
PROGRAM_DRY_RUN ?=
PACKAGE_MANIFEST ?=
PACKAGE_RBF ?=
PACKAGE_OUTPUT ?= $(CURDIR)/build/packages
CACHE_ROOT ?=
GPU_DEVICES ?= 0

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
export EXP BUILD   PROGRAM_TRANSPORT   PROGRAMMER   PROGRAM_CABLE PROGRAM_CABLE_INDEX PROGRAM_EXPECTED_BOARD  PROGRAM_DRY_RUN

help:
	@printf '%s\n' \
		"Open MiSTer OSS Cyclone V toolchain" \
		"" \
		"Two jobs (do not mix them):" \
		"  OSS place-and-route experiments   docs/oss-pnr.md" \
		"  FES core update or new core       docs/cores.md" \
		"" \
		"Public targets:" \
		"  toolchain  Build or locate the pinned repository-local OSS tools" \
		"  toolchain-fes  Build the repository-local HIP toolchain from toolchain.lock" \
		"  toolchain-fes-pcxt  Provision the PC/XT diagnostic toolchain (PCXT_GPU_ROUTER=HIP or CUDA)" \
		"  toolchain-fes-menu  Provision the FES menu HIP toolchain (per-core lock)" \
		"  toolchain-fes-catch  Provision the FES Catch HIP toolchain (per-core lock)" \
		"  toolchain-fes-pong  Provision the FES Pong HIP toolchain (per-core lock)" \
		"  toolchain-fes-zx81  Build the repository-local HIP toolchain for the standard FES ZX81 socket" \
		"  toolchain-fes-coleco  Build the Coleco v2 SGM toolchain" \
		"  toolchain-fes-sms  Provision the FES SMS HIP toolchain (per-core lock)" \
		"  toolchain-fes-sg1000  Provision the FES SG-1000 HIP toolchain (per-core lock)" \
		"  toolchain-fes-apple2  Provision the FES Apple II HIP toolchain (per-core lock)" \
		"  toolchain-fes-c64  Provision the FES C64 HIP toolchain (per-core lock)" \
		"  toolchain-fes-spectrum  Provision the FES Spectrum HIP toolchain (per-core lock)" \
		"  toolchain-fes-riscv  Provision the FES RISC-V HIP toolchain (per-core lock)" \
		"  toolchain-fes-pc386  Provision the pc386 HIP toolchain (per-core lock)" \
		"  doctor     Report host, toolchain, optional Quartus, and hardware readiness" \
		"  doctor-strict  Require host and OSS readiness (Quartus/hardware optional)" \
		"  sim        Simulate an experiment with the Verilator lane" \
		"  sim-pong   Test the standalone Pong game logic (no board wrapper)" \
		"  sim-fes-demo  Test composable application endpoints and reference demos" \
		"  sim-fes-video-parts  Test direct and scanline video parts through the physical socket" \
		"  build-fes-coleco-video  Seal the separate Coleco video development shell" \
		"  build-fes-video-part  Build VIDEO_VARIANT=direct|scanlines against VIDEO_SHELL and VIDEO_PACKAGE" \
		"  sim-fes-ramtest  Test the RAM tester against SDRAM and HPS DDR models, holds and identity" \
		"  build-fes-ramtest-100  Seal the OSS 100 MHz SDRAM and HPS DDR tester with HIP nextpnr" \
		"  build-fes-ramtest-130  Seal the OSS 130 MHz SDRAM and HPS DDR tester with HIP nextpnr" \
		"  diagnose-fes-ramtest-timing  Measure the 130 MHz RAM tester on the shared toolchain" \
		"  toolchain-fes-ramtest  Provision the FES RAM Tester HIP toolchain (per-core lock)" \
		"  toolchain-fes-atari-st  Provision the Atari ST OSS toolchain (per-core lock)" \
		"  build-fes-ramtest-quartus  Quartus 17.0.2 SDRAM and HPS DDR tester at 130 MHz (RAMTEST_MHZ=100 for 100 MHz)" \
		"  sim-fes-splash  Test HDMI splash logo, motion and ADV7513 I2C board path" \
		"  build-fes-splash  Produce splash RBF + native-inputs provenance (OSS, not a play package)" \
		"  build-fes-demo  Seal autonomous video application with HIP nextpnr" \
		"  build-fes-demo-media  Seal gamepad/palette-media application with HIP nextpnr" \
		"  build-fes-demo-audio  Seal gamepad/stereo-tone application with HIP nextpnr" \
		"  sim-fes-pong  Test the FES GP mailbox and fixed 720p Pong shell" \
		"  sim-fes-zx81  Test the ZX81 machine, GP mailbox, raster and live DDR session display" \
		"  sim-fes-zx81-rom  Read the ZX81 machine ROM port for the 8 KiB BASIC image" \
		"  sim-fes-zx81-expansion  Test vacant 1 KiB and validation-cart RAMTOP on the Z80-like edge" \
		"  sim-fes-zx81-variants  Probe family variants and verify the ZX80 tape load address" \
		"  zx81-openrom  Assemble the open ZX81 ROM: build/zx81-openrom/zx81-openrom.bin (8192 bytes)" \
		"  sim-fes-zx81-openrom  Run the open ZX81 ROM on the 1 KiB and 16 KiB machines: display, keys, SAVE, LOAD" \
		"  sim-fes-coleco  Test the FES application ColecoVision slice and native controller ports" \
		"  sim-fes-native-video  Test native frame capture, Direct/Scanlines and clock crossing" \
		"  sim-fes-display-plane  Test the shared session-display blend, latency, raster lock and fault return" \
		"  sim-fes-native-socket  Test both native parts through their physical socket wrapper" \
		"  build-fes-coleco-native-video  Seal the native-pixel shell requiring a linked video part" \
		"  sim-fes-coleco-native  Test actual registered Coleco VDP native pixels and inline board path" \
		"  sim-fes-coleco-session  Test the Coleco mailbox, native part raster and live DDR session display" \
		"  synth-fes-native-video  Diagnostic locked synthesis, requires CACHE_ROOT; no route/seal" \
		"  sim-fes-z80  Test original NMOS and documented fast Z80 RTL" \
		"  sim-fes-riscv  Test the original RV32I CPU, the fes.riscv system and its board shell" \
		"  build-fes-riscv  Seal FES RISC-V with HIP nextpnr/Mistral (CACHE_ROOT= for shared cache)" \
		"  sim-fes-tms9918-timing  Check the shared ~60 Hz NTSC raster enable across Coleco clock rates" \
		"  sim-fes-coleco-oss  Test the OSS-conditional ColecoVision RAM and shell paths" \
		"  sim-fes-coleco-sgm-integrated  Test the v2 SGM module and shell RAM through CPU/socket cycles" \
		"  sim-fes-coleco-unit[-oss]  Run focused Coleco unit/CPU simulations" \
		"  sim-fes-coleco-board-CASE[-oss]  Run one board case: graphics stream interactive controllers vdp-io sprites" \
		"  sim-fes-coleco-quartus  Test RAM/media with supplied Quartus 17 models and Icarus" \
		"  coleco-diagnostic  Generate the open Coleco Graphics I cartridge and reference image" \
		"  coleco-sprite-diagnostic  Generate the open Coleco Graphics II sprite cartridge and reference image" \
		"  build-fes-zx81-quartus  Quartus 17.0.2 oracle package for FES ZX81" \
		"  build-fes-zx81  Seal FES ZX81 HIP (CACHE_ROOT=; BEST_FMAX=1 GPU_DEVICES=1 uses one selected GPU)" \
		"  build-fes-zx80  Seal experimental Sinclair ZX80 (package-only; same lock as fes.zx81)" \
		"  build-fes-ts1000  Seal experimental Timex Sinclair 1000 (package-only; same lock as fes.zx81)" \
		"  build-fes-ts1500  Seal experimental Timex Sinclair 1500 (package-only; same lock as fes.zx81)" \
		"  build-fes-coleco-quartus  Quartus 17.0.2 oracle package for FES ColecoVision" \
		"  build-fes-coleco  Seal FES ColecoVision v2 socket with HIP (CACHE_ROOT=)" \
		"  sim-fes-sg1000  Test the FES simple-computer SG-1000 machine (Coleco sibling)" \
		"  sim-fes-sg1000-oss  Test OSS-conditional SG-1000 registered media, RAM and VDP" \
		"  sim-fes-sg1000-rom-link  Test the sealed 16 KiB SG-1000 ROM path" \
		"  sim-fes-sg1000-quartus  Test SG-1000 RAM/media with supplied Quartus 17 models and Icarus" \
		"  sg1000-diagnostic  Generate the open SG-1000 Graphics I cartridge and reference image" \
		"  build-fes-sg1000-quartus  Quartus 17.0.2 oracle package for FES SG-1000" \
		"  build-fes-sg1000  Seal FES SG-1000 with HIP nextpnr/Mistral (Coleco lock; CACHE_ROOT= for shared cache)" \
		"  sim-fes-sms  Test the FES SMS blob-stream mailbox, 32KiB map, SN76489 and HDMI I2S (fes.sms)" \
		"  sim-fes-sms-oss  Test SMS ROM-link mailbox capabilities/rejections and OSS-conditional media, RAM and VDP" \
		"  sim-fes-sms-rom-link-mailbox  Test SMS ROM-link mailbox capabilities and rejected media commands" \
		"  sim-fes-sms-quartus  Test SMS RAM/media with supplied Quartus 17 models and Icarus" \
		"  sms-diagnostic  Generate the open Master System Mode 4 cartridge and reference image" \
		"  build-fes-sms-quartus  Quartus 17.0.2 oracle package for FES Master System (fes.sms)" \
		"  build-fes-sms  Seal FES Master System with HIP nextpnr/Mistral (Coleco lock; CACHE_ROOT= for shared cache)" \
		"  build-fes-pong  Seal FES Pong with HIP nextpnr/Mistral (CACHE_ROOT= for shared cache)" \
		"  apple2-diagnostic  Generate the open Apple II firmware, synthetic disk and reference frames" \
		"  sim-fes-apple2  Test the fes.computer mailbox, Apple II machine/Disk II and board shell" \
		"  sim-fes-apple2-mailbox  Replay the fes.computer golden exchanges against the shared endpoint" \
		"  sim-fes-apple2-machine  Boot the open diagnostic and synthetic disk; compare every frame" \
		"  sim-fes-apple2-board  Drive top.v through the mailbox: HID keys, live disk insert/eject, reset" \
		"  sim-fes-spectrum  Test the Spectrum machine, tape pilot, probe sockets and board mailbox" \
		"  build-fes-apple2  Seal FES Apple II (format 3 firmware map, four slot sockets) with HIP (CACHE_ROOT=)" \
		"  build-fes-spectrum  Seal FES ZX Spectrum (format 3 firmware map, four edge sockets) with HIP (CACHE_ROOT=)" \
		"  c64-diagnostic  Generate the open C64 firmware, font and synthetic D64" \
		"  sim-fes-c64-session  Check actual C64 VIC raster and the shared DDR session plane" \
		"  sim-fes-c64  Boot the open C64 diagnostic: cartridges, keyboard, joystick and D64" \
		"  sim-fes-c64-sid  Check digital SID state and staged MOS6581 audio" \
		"  sim-fes-c64-ba-timing  Check CPU retirement at VIC BA phase boundaries" \
		"  sim-fes-pcxt  Test the PC/XT ISA fabric, waits, IRQ and DMA transactions" \
		"  sim-fes-pcxt-fdc  Test the XT floppy adapter over ISA/DMA" \
		"  sim-fes-pcxt-media  Test runtime XT floppy upload, replacement and diagnostic" \
		"  sim-fes-pcxt-mailbox  Test the PC/XT media identity and mailbox handshake" \
		"  sim-fes-pcxt-dos  Test XT BIOS POST and boot the pinned FreeDOS floppy (downloads fixtures)" \
		"  sim-fes-pc386  Check pc386 devices, the fes.computer 1.1 exchanges, audio, OPL2, presenter and display plane on port 2" \
		"  sim-fes-pc386-boot  Boot the Bochs BIOS and FreeDOS on the pc386 machine with the oracle CPU; replay its VGA trace" \
		"  sim-fes-pc386-fesenv  Boot the games image and check FESENV sets BLASTER from CMOS" \
		"  sim-fes-pc386-system  Boot FreeDOS on pc386_system, the M5 machine with fes_i386_cpu, to the prompt (about an hour)" \
		"  sim-fes-pc386-sound  Boot FreeDOS and run the audio start-up exercise; writes sound.wav" \
		"  sim-fes-pc386-vga  Check the pc386 presenter and plane, run the VGA test disk, replay its VGA trace against Bochs" \
		"  synth-fes-pc386  Synthesize and pack the pc386 machine without the CPU (needs CACHE_ROOT)" \
		"  build-fes-pc386-quartus  Seal fes.pc386, the machine with fes_i386_cpu, with Quartus 17.0.2 (CACHE_ROOT=, SEED=)" \
		"  build-fes-pcxt  Quartus PC/XT diagnostic package (QUARTUS_ROOTDIR and pinned Yosys)" \
		"  sim-fes-atari-st  Test the Atari 520ST CPU, memory, peripherals, media and video" \
		"  build-fes-atari-st  Seal the Atari 520ST with HIP nextpnr (ST_VIDEO_OUTPUT=direct|scanlines)" \
		"  build-fes-atari-st-card  Seal a probe in ST_SHELL/ST_PACKAGE socket 1" \
		"  fetch-fes-atari-st-emutos  Fetch the pinned free 192 KiB EmuTOS test ROM" \
		"  sim-fes-atari-st-emutos-memory  Boot EMUTOS_ROM through SDRAM and capture HDMI RGB" \
		"  atari-st-diagnostic  Generate two original 192 KiB 68000 firmware inputs" \
		"  build-fes-c64  Seal FES Commodore 64 (format 3 firmware map, two cartridge sockets) with HIP (CACHE_ROOT=)" \
		"  oss        Build an experiment with the open-source FPGA lane" \
		"  export-core-package  Seal a format-2 package directory and .fcore archive" \
		"  program    Explicit JTAG SRAM maintenance diagnostic" \
		"  clean      Remove generated output for an experiment" \
		"" \
		"Variables: EXP=010_blinky BUILD=oss PYTHON=python3" \
		"  CACHE_ROOT= optional shared toolchain cache root for build-fes-pong/zx81/coleco/sg1000/sms" \
		"  PACKAGE_MANIFEST/PACKAGE_RBF required for export-core-package; PACKAGE_OUTPUT defaults to build/packages" \
		"  PROGRAM_TRANSPORT=jtag; normal package loads use FogCast" \
		"  PROGRAM_EXPECTED_BOARD is required for every non-dry action (de10nano)" \
		"  PROGRAM_CABLE_INDEX is rejected for USB-Blaster II" \
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
_SHARED_LEGACY_SIM_GOALS := $(filter sim sim-pong sim-fes-riscv sim-fes-pong sim-fes-zx81 sim-fes-zx81-session sim-fes-display-plane sim-fes-zx81-rom sim-fes-zx81-bus sim-fes-zx81-expansion sim-fes-zx81-variants sim-fes-zx81-openrom sim-fes-coleco sim-fes-coleco-% sim-fes-tms9918-timing sim-fes-sg1000 sim-fes-sg1000-oss sim-fes-sg1000-rom-link sim-fes-sg1000-quartus sim-fes-sms sim-fes-sms-oss sim-fes-sms-rom-link-mailbox sim-fes-sms-quartus sim-fes-splash sim-fes-ramtest sim-fes-apple2 sim-fes-apple2-% sim-fes-c64 sim-fes-c64-% sim-fes-z80 sim-fes-z80-timing sim-fes-spectrum sim-fes-spectrum-% sim-fes-atari-st sim-fes-atari-st-%,$(MAKECMDGOALS))
ifneq ($(strip $(_SHARED_LEGACY_SIM_GOALS)),)
$(error misteross: shared toolchain cache is unsupported for Make simulation targets; unset FES_TOOLCHAIN_CACHE_ROOT for the local simulation lane)
endif
endif

.PHONY: toolchain toolchain-fes toolchain-fes-menu toolchain-fes-catch toolchain-fes-pong toolchain-fes-zx81 toolchain-fes-coleco toolchain-fes-sg1000 toolchain-fes-sms toolchain-fes-apple2 toolchain-fes-c64 toolchain-fes-spectrum toolchain-fes-riscv toolchain-fes-pc386 toolchain-fes-ramtest toolchain-check doctor doctor-strict sim sim-pong sim-fes-pong sim-fes-zx81 sim-fes-zx81-session sim-fes-zx81-rom sim-fes-zx81-bus sim-fes-zx81-expansion sim-fes-coleco sim-fes-coleco-oss sim-fes-tms9918-timing sim-fes-sg1000 sim-fes-sg1000-oss sim-fes-sg1000-rom-link sim-fes-sg1000-quartus sg1000-diagnostic sim-fes-sms sim-fes-sms-oss sim-fes-sms-rom-link-mailbox sim-fes-sms-quartus sms-diagnostic sim-fes-c64 c64-diagnostic sim-fes-spectrum spectrum-diagnostic sim-fes-splash build-fes-splash build-fes-zx81-quartus build-fes-zx81 build-fes-coleco-quartus build-fes-coleco build-fes-sg1000-quartus build-fes-sg1000 build-fes-sms-quartus build-fes-sms build-fes-pong build-fes-c64 build-fes-spectrum oss export-core-package program clean

sim-pong:
	$(require_local_sim)
	@mkdir -p build/sim/pong
	$(VERILATOR) --cc --exe --build --top-module pong_game -Wall -GCLOCK_HZ=8000 \
		--Mdir "$(CURDIR)/build/sim/pong" cores/pong/rtl/pong_game.sv "$(CURDIR)/cores/pong/sim/tb.cpp"
	@build/sim/pong/Vpong_game

.PHONY: sim-fes-demo build-fes-demo build-fes-demo-media sim-fes-splash build-fes-splash sim-fes-ramtest build-fes-ramtest-100 build-fes-ramtest-130 diagnose-fes-ramtest-timing build-fes-ramtest-quartus
sim-fes-ramtest:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_ramtest.py

build-fes-ramtest-100:
	$(FES_SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_ramtest.py --memory-mhz 100 $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),--cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",)

build-fes-ramtest-130:
	$(FES_SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_ramtest.py --memory-mhz 130 $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),--cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",)

diagnose-fes-ramtest-timing:
	$(FES_SHARED_MAKE_ENV)$(PYTHON) scripts/diagnose_fes_ramtest_timing.py $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),--cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",) $(if $(GPU_DEVICE),--gpu-device "$(GPU_DEVICE)",)

build-fes-ramtest-quartus:
	$(PYTHON) scripts/build_fes_ramtest_quartus.py

sim-fes-demo:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_demo.py --verilator "$(VERILATOR)"

.PHONY: sim-fes-video-parts build-fes-coleco-video build-fes-video-part
sim-fes-video-parts:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_video_parts.py --verilator "$(VERILATOR)"

.PHONY: sim-fes-display-plane
sim-fes-display-plane:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_display_plane.py --verilator "$(VERILATOR)"

.PHONY: sim-fes-native-video
sim-fes-native-video:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_native_video.py --verilator "$(VERILATOR)"

.PHONY: sim-fes-coleco-native synth-fes-native-video
sim-fes-coleco-native:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_coleco_native.py --verilator "$(VERILATOR)"

.PHONY: sim-fes-coleco-session
sim-fes-coleco-session:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_coleco_session.py --verilator "$(VERILATOR)"

synth-fes-native-video:
	@test -n "$(CACHE_ROOT)" || { echo 'Set CACHE_ROOT to the authenticated compiler cache'; exit 1; }
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/synth_fes_native_video.py --cache-root "$(CACHE_ROOT)"

build-fes-coleco-video:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_coleco_socket_v2.py --root "$(CURDIR)" --video-socket$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

.PHONY: build-fes-coleco-native-video sim-fes-native-socket
build-fes-coleco-native-video:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_coleco_socket_v2.py --root "$(CURDIR)" --native-video-socket$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

sim-fes-native-socket:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_native_socket.py --verilator "$(VERILATOR)"

VIDEO_VARIANT ?= direct
build-fes-video-part:
	@test -n "$(VIDEO_SHELL)" -a -n "$(VIDEO_PACKAGE)" || { echo 'Set VIDEO_SHELL and VIDEO_PACKAGE to the exact frozen producer output and sealed package'; exit 1; }
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_video_part.py --root "$(CURDIR)" --shell "$(VIDEO_SHELL)" --package "$(VIDEO_PACKAGE)" --variant "$(VIDEO_VARIANT)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

sim-fes-splash:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_splash.py --verilator "$(VERILATOR)"

build-fes-splash:
	$(PYTHON) scripts/build_fes_splash.py

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

.PHONY: sim-fes-zx81-bus sim-fes-zx81-expansion
sim-fes-zx81-bus:
	$(require_local_sim)
	@for cart in ram16k zonx qs_chrs; do \
		name=$$cart; if [ $$cart = qs_chrs ]; then name=qs; fi; \
		mkdir -p build/sim/fes-zx81-bus-$$name; \
		$(VERILATOR) --cc --exe --build --top-module expansion_bus_harness -Wall \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-WIDTHTRUNC -Wno-WIDTHEXPAND \
		-Wno-PINCONNECTEMPTY -Wno-DECLFILENAME -Wno-UNUSEDPARAM \
		$(VERILATOR_SIMILARNAME) \
		-Icores/fes-zx81/rtl -Icores/fes-zx81/expansions \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-bus-$$name" \
		cores/fes-zx81/sim/expansion_bus_harness.v \
		cores/fes-zx81/expansions/$$cart.v cores/fes-zx81/expansions/zonx_ay.v \
		experiments/901_plugged_base/sim/mistral_ff_model.v \
		cores/fes-zx81/rtl/zx81_expansion_socket.v cores/fes-zx81/rtl/zx81_dpram.v \
		cores/fes-zx81/rtl/zx81_ram_pack.v \
		"$(CURDIR)/cores/fes-zx81/sim/bus_tb.cpp" || exit $$?; \
		build/sim/fes-zx81-bus-$$name/Vexpansion_bus_harness $$name || exit $$?; \
	done

sim-fes-zx81-expansion: sim-fes-zx81-bus
	$(require_local_sim)
	@for cart in 0 1; do \
		mkdir -p build/sim/fes-zx81-expansion-$$cart; \
		$(VERILATOR) --cc --exe --build --top-module expansion_machine -Wall \
		-GCART_PRESENT=$$cart \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_SIMILARNAME) $(VERILATOR_PROCASSINIT) \
		-Icores/fes-zx81/rtl -Icores/fes-common/rtl/z80 \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-expansion-$$cart" \
		cores/fes-zx81/sim/expansion_machine.v \
		cores/fes-zx81/expansions/ram16k.v \
		experiments/901_plugged_base/sim/mistral_ff_model.v \
		cores/fes-zx81/rtl/zx81_machine_clock.v cores/fes-zx81/rtl/zx81_machine.sv \
		$(FES_Z80_NMOS_RTL) \
		cores/fes-zx81/rtl/zx81_dpram.v cores/fes-zx81/rtl/zx81_expansion_socket.v \
		cores/fes-zx81/rtl/zx81_ram_pack.v \
		"$(CURDIR)/cores/fes-zx81/sim/expansion_tb.cpp" || exit $$?; \
		if [ $$cart = 0 ]; then ramtop=0x4400; else ramtop=0x8000; fi; \
		build/sim/fes-zx81-expansion-$$cart/Vexpansion_machine $$ramtop || exit $$?; \
	done

.PHONY: sim-fes-zx81-rom
sim-fes-zx81-rom:
	$(require_local_sim)
	@mkdir -p build/sim/fes-zx81-rom
	$(VERILATOR) --cc --exe --build --top-module rom_init_window -Wall \
		-Wno-UNUSEDSIGNAL -Wno-PINCONNECTEMPTY \
		-Icores/fes-zx81/rtl \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-rom" \
		cores/fes-zx81/sim/rom_init_window.v \
		cores/fes-zx81/rtl/zx81_dpram.v \
		"$(CURDIR)/cores/fes-zx81/sim/rom_init_tb.cpp"
	@build/sim/fes-zx81-rom/Vrom_init_window "$(CURDIR)/cores/fes-zx81/rtl/zx8x.hex"

.PHONY: sim-fes-zx81-variants
sim-fes-zx81-variants:
	$(require_local_sim)
	@set -e; \
	for spec in "zx81:" "zx80:-DZX81_MODEL_ZX80" "ts1000:-DZX81_MODEL_TS1000" "ts1500:-DZX81_MODEL_TS1500"; do \
		name=$${spec%%:*}; \
		define=$${spec#*:}; \
		mkdir -p build/sim/fes-zx81-variant-$$name; \
		extra=""; \
		if [ -n "$$define" ]; then extra="$$define"; fi; \
		$(VERILATOR) --cc --exe --build --top-module zx81_machine -Wall \
			-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
			-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
			-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
			-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
			-Icores/fes-zx81/generated -Icores/fes-common/rtl/z80 -Icores/fes-zx81/rtl \
			-GEXTERNAL_RAM=1 \
			-GFIRMWARE_INIT='"$(CURDIR)/cores/fes-zx81/sim/variant_probe.hex"' \
			$$extra \
			--Mdir "$(CURDIR)/build/sim/fes-zx81-variant-$$name" \
			cores/fes-zx81/rtl/zx81_machine_clock.v cores/fes-zx81/rtl/zx81_machine.sv \
			$(FES_Z80_NMOS_RTL) \
			cores/fes-zx81/rtl/zx81_dpram.v \
			"$(CURDIR)/cores/fes-zx81/sim/variant_tb.cpp" || exit $$?; \
		build/sim/fes-zx81-variant-$$name/Vzx81_machine $$name || exit $$?; \
	done
	@mkdir -p build/sim/fes-zx81-variant-zx80-load
	$(VERILATOR) --cc --exe --build --top-module zx81_machine -Wall \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-zx81/generated -Icores/fes-common/rtl/z80 -Icores/fes-zx81/rtl \
		-GEXTERNAL_RAM=1 \
		-GFIRMWARE_INIT='"$(CURDIR)/cores/fes-zx81/sim/zx80_load_probe.hex"' \
		-DZX81_MODEL_ZX80 \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-variant-zx80-load" \
		cores/fes-zx81/rtl/zx81_machine_clock.v cores/fes-zx81/rtl/zx81_machine.sv \
		$(FES_Z80_NMOS_RTL) \
		cores/fes-zx81/rtl/zx81_dpram.v \
		"$(CURDIR)/cores/fes-zx81/sim/tape_load_tb.cpp"
	@build/sim/fes-zx81-variant-zx80-load/Vzx81_machine

.PHONY: sim-fes-zx81-machine
sim-fes-zx81-machine:
	$(require_local_sim)
	@mkdir -p build/sim/fes-zx81-machine
	$(VERILATOR) --cc --exe --build --top-module zx81_machine -Wall \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-zx81/generated -Icores/fes-common/rtl/z80 -Icores/fes-zx81/rtl \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-machine" \
		cores/fes-zx81/rtl/zx81_machine_clock.v cores/fes-zx81/rtl/zx81_machine.sv \
		$(FES_Z80_NMOS_RTL) \
		cores/fes-zx81/rtl/zx81_dpram.v \
		"$(CURDIR)/cores/fes-zx81/sim/machine_tb.cpp"
	@build/sim/fes-zx81-machine/Vzx81_machine

.PHONY: zx81-openrom sim-fes-zx81-openrom
zx81-openrom:
	@python3 scripts/build_zx81_openrom.py

# The open ROM on the real machine RTL, as the 1 KiB fes.zx81 and with 16 KiB.
sim-fes-zx81-openrom: zx81-openrom
	$(require_local_sim)
	@for ram in 1 0; do \
		dir=build/sim/fes-zx81-openrom-$$ram; mkdir -p $$dir; \
		$(VERILATOR) --cc --exe --build -O3 --top-module zx81_machine -Wall \
			-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
			-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
			-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
			-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
			-GEXTERNAL_RAM=$$ram '-GFIRMWARE_INIT="build/zx81-openrom/zx81-openrom.hex"' \
			-Icores/fes-zx81/generated -Icores/fes-common/rtl/z80 -Icores/fes-zx81/rtl \
			--Mdir "$(CURDIR)/$$dir" \
			cores/fes-zx81/rtl/zx81_machine_clock.v cores/fes-zx81/rtl/zx81_machine.sv \
			$(FES_Z80_NMOS_RTL) \
			cores/fes-zx81/rtl/zx81_dpram.v \
			"$(CURDIR)/cores/fes-zx81/sim/openrom_tb.cpp" || exit 1; \
	done
	@build/sim/fes-zx81-openrom-1/Vzx81_machine check & one=$$!; \
		build/sim/fes-zx81-openrom-0/Vzx81_machine check; sixteen=$$?; \
		wait $$one && test $$sixteen = 0

sim-fes-zx81: sim-fes-zx81-rom sim-fes-zx81-machine sim-fes-zx81-expansion sim-fes-display-plane sim-fes-zx81-session sim-fes-zx81-variants sim-fes-zx81-openrom
	$(require_local_sim)
	@mkdir -p build/sim/fes-zx81-save
	$(VERILATOR) --cc --exe --build --top-module zx81_machine -Wall \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-zx81/generated -Icores/fes-common/rtl/z80 -Icores/fes-zx81/rtl \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-save" \
		cores/fes-zx81/rtl/zx81_machine_clock.v cores/fes-zx81/rtl/zx81_machine.sv \
		$(FES_Z80_NMOS_RTL) \
		cores/fes-zx81/rtl/zx81_dpram.v \
		"$(CURDIR)/cores/fes-zx81/sim/save_tb.cpp"
	@build/sim/fes-zx81-save/Vzx81_machine
	@mkdir -p build/sim/fes-zx81-video
	$(VERILATOR) --cc --exe --build --top-module zx81_video_720p -Wall \
		-Wno-UNUSEDSIGNAL -Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-UNUSEDPARAM \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-video" \
		cores/fes-zx81/rtl/zx81_video_720p.v \
		"$(CURDIR)/cores/fes-zx81/sim/video_tb.cpp"
	@build/sim/fes-zx81-video/Vzx81_video_720p

.PHONY: sim-fes-zx81-session
sim-fes-zx81-session:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_zx81_session.py --verilator "$(VERILATOR)"

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

.PHONY: sim-fes-tms9918-timing
sim-fes-tms9918-timing:
	$(require_local_sim)
	@mkdir -p build/sim/$@
	$(VERILATOR) --cc --exe --build --top-module raster_ce_tb -Wall \
		--Mdir "$(CURDIR)/build/sim/$@" \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-coleco/sim/raster_ce_tb.sv \
		"$(CURDIR)/cores/fes-coleco/sim/raster_ce_tb.cpp"
	@build/sim/$@/Vraster_ce_tb

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
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/vdp_machine_tb.cpp"
	@build/sim/$@/Vcoleco_machine build/diagnostics/fes-coleco/vdp-io.rom \
		build/diagnostics/fes-coleco/vdp-io-16k.rom

# The coherent output check is consumed by Coleco, SG-1000 and SMS.
.PHONY: sim-fes-audio-output
sim-fes-audio-output:
	$(require_local_sim)
	@mkdir -p build/sim/fes-audio-output
	$(VERILATOR) --cc --exe --build --top-module fes_audio_output -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-audio-output" \
		cores/fes-common/rtl/fes_audio_output.v cores/fes-common/rtl/fes_audio_i2s.v \
		"$(CURDIR)/cores/fes-coleco/sim/audio_output_tb.cpp"
	@build/sim/fes-audio-output/Vfes_audio_output

# Each board case is a separate CI shard. The shared output prerequisite runs
# before the Coleco PSG check.
.PHONY: sim-fes-coleco-audio
sim-fes-coleco-audio: sim-fes-audio-output
	$(require_local_sim)
	@mkdir -p build/sim/fes-psg
	$(VERILATOR) --cc --exe --build --top-module fes_sn76489 -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-psg" cores/fes-common/rtl/fes_sn76489.sv \
		"$(CURDIR)/cores/fes-coleco/sim/psg_tb.cpp"
	@build/sim/fes-psg/Vfes_sn76489

# Each board case is a separate CI shard. The shared build prerequisite runs
# once per lane in a Make invocation; diagnostics finish before their readers.
COLECO_BOARD_CASES := graphics stream interactive controllers vdp-io sprites
COLECO_BOARD_TARGETS := $(addprefix sim-fes-coleco-board-,$(COLECO_BOARD_CASES))
.PHONY: sim-fes-coleco-unit sim-fes-coleco-unit-oss sim-fes-coleco-board-build sim-fes-coleco-board-build-oss $(COLECO_BOARD_TARGETS) $(addsuffix -oss,$(COLECO_BOARD_TARGETS))
sim-fes-coleco: sim-fes-coleco-unit $(COLECO_BOARD_TARGETS) sim-fes-coleco-oss
sim-fes-coleco-oss: sim-fes-coleco-unit-oss $(addsuffix -oss,$(COLECO_BOARD_TARGETS)) sim-fes-coleco-expansion sim-fes-coleco-diagnostic sim-fes-coleco-sgm-socket sim-fes-coleco-sgm-shell-ram sim-fes-coleco-sgm-ay sim-fes-coleco-sgm-module sim-fes-coleco-sgm-audio sim-fes-coleco-sgm-probe

.PHONY: sim-fes-coleco-sgm-socket
sim-fes-coleco-megacart:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-megacart
	$(VERILATOR) --cc --exe --build --top-module coleco_megacart_rom -Wall \
		-Wno-UNUSEDSIGNAL -Wno-DECLFILENAME -Wno-WIDTHEXPAND \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-megacart" \
		cores/fes-coleco/rtl/coleco_megacart_rom.v \
		"$(CURDIR)/cores/fes-coleco/sim/megacart_tb.cpp"
	@build/sim/fes-coleco-megacart/Vcoleco_megacart_rom
	@$(MAKE) sim-fes-coleco-megacart-machine

.PHONY: sim-fes-coleco-megacart

.PHONY: sim-fes-coleco-megacart-machine
sim-fes-coleco-megacart-machine:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-megacart-machine
	$(VERILATOR) --cc --exe --build --top-module sgm_shell_machine -Wall \
		-DTV80_REFRESH=1 -DFES_COLECO_OSS=1 -DFES_COLECO_EXPANSION_V2_DEV=1 \
		-DFES_COLECO_MEGACART_LINK=1 -DSGM_INTEGRATED=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-coleco/rtl -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-megacart-machine" \
		cores/fes-coleco/sim/sgm_shell_machine.sv \
		cores/fes-coleco/rtl/coleco_expansion_socket_v2.v \
		cores/fes-coleco/rtl/coleco_expansion_ram.v \
		cores/fes-coleco/rtl/coleco_audio_mix.v \
		cores/fes-coleco/expansions/sgm.v \
		cores/fes-coleco/expansions/sgm_control.v \
		cores/fes-coleco/expansions/sgm_ay.v \
		cores/fes-coleco/rtl/coleco_megacart_rom.v \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-common/rtl/coleco_dpram.v \
		cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/megacart_machine_tb.cpp"
	@build/sim/fes-coleco-megacart-machine/Vsgm_shell_machine

sim-fes-coleco-sgm-socket:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-sgm-socket
	$(VERILATOR) --cc --exe --build --top-module coleco_expansion_socket_v2 -Wall \
		-Wno-UNUSEDSIGNAL -Icores/fes-coleco/rtl \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-sgm-socket" \
		cores/fes-coleco/rtl/coleco_expansion_socket_v2.v \
		"$(CURDIR)/cores/fes-coleco/sim/sgm_socket_tb.cpp"
	@build/sim/fes-coleco-sgm-socket/Vcoleco_expansion_socket_v2

.PHONY: sim-fes-coleco-sgm-shell-ram
sim-fes-coleco-sgm-shell-ram:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-sgm-shell-ram
	$(VERILATOR) --cc --exe --build --top-module sgm_shell_machine -Wall \
		-DTV80_REFRESH=1 -DFES_COLECO_OSS=1 -DFES_COLECO_EXPANSION_V2_DEV=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-coleco/rtl -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-sgm-shell-ram" \
		cores/fes-coleco/sim/sgm_shell_machine.sv \
		cores/fes-coleco/rtl/coleco_expansion_socket_v2.v \
		cores/fes-coleco/rtl/coleco_expansion_ram.v \
		cores/fes-coleco/rtl/coleco_audio_mix.v \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-common/rtl/coleco_dpram.v \
		cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/sgm_shell_ram_tb.cpp"
	@build/sim/fes-coleco-sgm-shell-ram/Vsgm_shell_machine

.PHONY: sim-fes-coleco-sgm-ay
sim-fes-coleco-sgm-ay:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-sgm-ay
	$(VERILATOR) --cc --exe --build --top-module sgm_ay -Wall \
		-Wno-UNUSEDSIGNAL -Wno-DECLFILENAME -Wno-WIDTHTRUNC \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-sgm-ay" \
		cores/fes-coleco/expansions/sgm_ay.v \
		"$(CURDIR)/cores/fes-coleco/sim/sgm_ay_tb.cpp"
	@build/sim/fes-coleco-sgm-ay/Vsgm_ay

.PHONY: sim-fes-coleco-sgm-module sim-fes-coleco-sgm-audio
sim-fes-coleco-sgm-module:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-sgm-module
	$(VERILATOR) --cc --exe --build --top-module cart -Wall \
		-Wno-UNUSEDSIGNAL -Wno-DECLFILENAME -Wno-WIDTHTRUNC \
		-Icores/fes-coleco/rtl --Mdir "$(CURDIR)/build/sim/fes-coleco-sgm-module" \
		cores/fes-coleco/expansions/sgm.v cores/fes-coleco/expansions/sgm_control.v \
		cores/fes-coleco/expansions/sgm_ay.v \
		"$(CURDIR)/cores/fes-coleco/sim/sgm_module_tb.cpp"
	@build/sim/fes-coleco-sgm-module/Vcart

sim-fes-coleco-sgm-audio:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-sgm-audio
	$(VERILATOR) --cc --exe --build --top-module sgm_audio_path -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-sgm-audio" \
		cores/fes-coleco/sim/sgm_audio_path.sv \
		cores/fes-coleco/rtl/coleco_audio_mix.v \
		cores/fes-common/rtl/fes_audio_output.v cores/fes-common/rtl/fes_audio_i2s.v \
		"$(CURDIR)/cores/fes-coleco/sim/sgm_audio_tb.cpp"
	@build/sim/fes-coleco-sgm-audio/Vsgm_audio_path

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
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/firmware_tb.cpp"
	@build/sim/$@/Vfirmware_model

sim-fes-coleco-unit: sim-fes-z80-timing sim-fes-tms9918-timing sim-fes-coleco-audio sim-fes-coleco-graphics sim-fes-coleco-stream sim-fes-coleco-vdp-io sim-fes-coleco-firmware
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
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-common/rtl/coleco_vdp.sv \
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

.PHONY: sim-fes-coleco-expansion
sim-fes-coleco-expansion:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-expansion
	$(VERILATOR) --cc --exe --build --top-module coleco_machine -Wall \
		-DTV80_REFRESH=1 -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_COLECO_OSS=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-expansion" \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-common/rtl/coleco_dpram.v \
		cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/expansion_tb.cpp"
	@build/sim/fes-coleco-expansion/Vcoleco_machine

.PHONY: sim-fes-coleco-diagnostic
sim-fes-coleco-diagnostic:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-diagnostic
	$(VERILATOR) --cc --exe --build --top-module cart -Wall \
		-Wno-UNUSEDSIGNAL -Wno-DECLFILENAME -Wno-UNUSEDPARAM \
		-Icores/fes-coleco/rtl \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-diagnostic" \
		cores/fes-coleco/expansions/diagnostic.v \
		"$(CURDIR)/cores/fes-coleco/sim/diagnostic_tb.cpp"
	@build/sim/fes-coleco-diagnostic/Vcart
	@$(MAKE) sim-fes-coleco-diagnostic-machine

.PHONY: sim-fes-coleco-diagnostic-machine
sim-fes-coleco-diagnostic-machine:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-diagnostic-machine
	$(VERILATOR) --cc --exe --build --top-module diagnostic_machine -Wall \
		-DTV80_REFRESH=1 -DFES_COLECO_OSS=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-coleco/rtl -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-diagnostic-machine" \
		cores/fes-coleco/sim/diagnostic_machine.sv \
		cores/fes-coleco/rtl/coleco_expansion_socket.v \
		cores/fes-coleco/expansions/diagnostic.v \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-common/rtl/coleco_dpram.v \
		cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/diagnostic_machine_tb.cpp"
	@build/sim/fes-coleco-diagnostic-machine/Vdiagnostic_machine

.PHONY: sim-fes-coleco-sgm-integrated
sim-fes-coleco-sgm-integrated:
	$(require_local_sim)
	@mkdir -p build/sim/fes-coleco-sgm-integrated
	$(VERILATOR) --cc --exe --build --top-module sgm_shell_machine -Wall \
		-DTV80_REFRESH=1 -DFES_COLECO_OSS=1 -DFES_COLECO_EXPANSION_V2_DEV=1 \
		-DSGM_INTEGRATED=1 -CFLAGS "-DSGM_INTEGRATED=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
		-Icores/fes-coleco/rtl -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-coleco-sgm-integrated" \
		cores/fes-coleco/sim/sgm_shell_machine.sv \
		cores/fes-coleco/rtl/coleco_expansion_socket_v2.v \
		cores/fes-coleco/rtl/coleco_expansion_ram.v \
		cores/fes-coleco/rtl/coleco_audio_mix.v \
		cores/fes-coleco/expansions/sgm.v \
		cores/fes-coleco/expansions/sgm_control.v \
		cores/fes-coleco/expansions/sgm_ay.v \
		cores/fes-common/rtl/fes_sn76489.sv cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-common/rtl/coleco_dpram.v \
		cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/sgm_shell_ram_tb.cpp"
	@build/sim/fes-coleco-sgm-integrated/Vsgm_shell_machine

.PHONY: sim-fes-coleco-sgm-probe
sim-fes-coleco-sgm-probe: sim-fes-coleco-sgm-integrated
	@mkdir -p build/diagnostics/fes-coleco
	$(PYTHON) cores/fes-coleco/diagnostic/sgm_probe.py --output build/diagnostics/fes-coleco/sgm-probe.rom
	@build/sim/fes-coleco-sgm-integrated/Vsgm_shell_machine build/diagnostics/fes-coleco/sgm-probe.rom

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
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv \
		cores/fes-common/rtl/coleco_vdp.sv cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/coleco_video_720p.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-coleco/sim/board_tb.cpp"

sim-fes-coleco-unit-oss: sim-fes-z80-timing sim-fes-coleco-graphics-oss sim-fes-coleco-stream-oss sim-fes-coleco-vdp-io-oss sim-fes-coleco-firmware-oss
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
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-common/rtl/coleco_vdp.sv \
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
		cores/fes-coleco/rtl/coleco_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-common/rtl/coleco_vdp.sv \
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

.PHONY: build-fes-zx80 build-fes-ts1000 build-fes-ts1500
build-fes-zx80:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_zx81_oss.py --root "$(CURDIR)" --model zx80$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

build-fes-ts1000:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_zx81_oss.py --root "$(CURDIR)" --model ts1000$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

build-fes-ts1500:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_zx81_oss.py --root "$(CURDIR)" --model ts1500$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

build-fes-coleco-quartus:
	$(PYTHON) scripts/build_fes_coleco.py --root "$(CURDIR)"

build-fes-coleco:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_coleco_socket_v2.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

.PHONY: sg1000-diagnostic
APPLE2_DIAGNOSTIC := build/diagnostics/fes-apple2
APPLE2_VERILATOR_FLAGS := -Wall -Wno-UNUSEDSIGNAL -Wno-DECLFILENAME -Wno-WIDTHEXPAND \
	-Wno-WIDTHTRUNC -Wno-CASEX -Wno-PINCONNECTEMPTY -Wno-UNUSEDPARAM \
	-Wno-CASEINCOMPLETE -Wno-CASEOVERLAP -Wno-SYNCASYNCNET \
	-Icores/fes-apple2/rtl -Icores/fes-common/generated
APPLE2_RTL := cores/fes-apple2/rtl/apple2_machine.sv cores/fes-apple2/rtl/apple2_ram.v \
	cores/fes-apple2/rtl/apple2_rom.v cores/fes-apple2/rtl/apple2_video.v \
	cores/fes-apple2/rtl/apple2_disk2_card.v cores/fes-apple2/rtl/apple2_disk2_drive.v \
	cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v

.PHONY: apple2-diagnostic sim-fes-apple2 sim-fes-apple2-mailbox sim-fes-apple2-machine sim-fes-apple2-board
apple2-diagnostic:
	@mkdir -p $(APPLE2_DIAGNOSTIC)
	$(PYTHON) cores/fes-apple2/diagnostic/font.py --verilog $(APPLE2_DIAGNOSTIC)/font.vh
	@cmp -s $(APPLE2_DIAGNOSTIC)/font.vh cores/fes-apple2/rtl/apple2_font.vh || \
		{ echo 'cores/fes-apple2/rtl/apple2_font.vh is stale; regenerate it with diagnostic/font.py' >&2; exit 1; }
	$(PYTHON) cores/fes-apple2/diagnostic/probe_card.py --verilog $(APPLE2_DIAGNOSTIC)/probe_rom.vh
	@cmp -s $(APPLE2_DIAGNOSTIC)/probe_rom.vh cores/fes-apple2/expansions/probe_rom.vh || \
		{ echo 'cores/fes-apple2/expansions/probe_rom.vh is stale; regenerate it with diagnostic/probe_card.py' >&2; exit 1; }
	$(PYTHON) cores/fes-apple2/diagnostic/firmware.py --output-dir $(APPLE2_DIAGNOSTIC)
	$(PYTHON) cores/fes-apple2/diagnostic/render.py --output-dir $(APPLE2_DIAGNOSTIC)

sim-fes-apple2: sim-fes-apple2-mailbox sim-fes-apple2-machine sim-fes-apple2-board

sim-fes-apple2-mailbox:
	$(require_local_sim)
	@mkdir -p build/sim/fes-computer-mailbox-input build/sim/fes-computer-mailbox-media
	$(VERILATOR) --cc --exe --build --top-module fes_computer_mailbox -Wall -Wno-DECLFILENAME \
		-Icores/fes-common/generated -GENABLE_KEYBOARD=1\'b1 -GENABLE_PORTS=1\'b1 -GENABLE_AUDIO=1\'b1 \
		--Mdir "$(CURDIR)/build/sim/fes-computer-mailbox-input" \
		cores/fes-common/rtl/fes_computer_mailbox.v "$(CURDIR)/cores/fes-common/sim/computer_mailbox_tb.cpp"
	@build/sim/fes-computer-mailbox-input/Vfes_computer_mailbox cores/fes-common/generated/computer-exchanges.json 0
	$(VERILATOR) --cc --exe --build --top-module fes_computer_mailbox -Wall -Wno-DECLFILENAME \
		-Icores/fes-common/generated -GENABLE_APPLE2_FLOPPY=1\'b1 -GUNIT0_MIN=1 -GUNIT0_MAX=1030 \
		--Mdir "$(CURDIR)/build/sim/fes-computer-mailbox-media" \
		cores/fes-common/rtl/fes_computer_mailbox.v "$(CURDIR)/cores/fes-common/sim/computer_mailbox_tb.cpp"
	@build/sim/fes-computer-mailbox-media/Vfes_computer_mailbox cores/fes-common/generated/computer-exchanges.json 1

sim-fes-apple2-machine: apple2-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-apple2-machine
	$(VERILATOR) --cc --exe --build -O2 --top-module apple2_sim_top $(APPLE2_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-apple2-machine" \
		-Icores/fes-apple2/expansions \
		cores/fes-apple2/sim/apple2_sim_top.sv $(APPLE2_RTL) \
		cores/fes-apple2/rtl/apple2_slot_sockets.v cores/fes-apple2/expansions/probe.v \
		"$(CURDIR)/cores/fes-apple2/sim/machine_tb.cpp"
	@build/sim/fes-apple2-machine/Vapple2_sim_top --require-refs

sim-fes-apple2-board: apple2-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-apple2-board
	$(VERILATOR) --cc --exe --build -O2 --top-module top $(APPLE2_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-apple2-board" \
		cores/fes-apple2/sim/board_models.v cores/fes-apple2/rtl/top.v $(APPLE2_RTL) \
		cores/fes-common/rtl/fes_computer_mailbox.v cores/fes-common/rtl/fes_audio_output.v \
		cores/fes-common/rtl/fes_audio_i2s.v cores/fes-apple2/rtl/apple2_disk_store.v \
		cores/fes-apple2/rtl/apple2_keyboard.v cores/fes-apple2/rtl/apple2_audio.v \
		cores/fes-apple2/rtl/apple2_slot_sockets.v \
		"$(CURDIR)/cores/fes-apple2/sim/board_tb.cpp"
	@build/sim/fes-apple2-board/Vtop $(APPLE2_DIAGNOSTIC)/disk.dsk build/sim/fes-apple2-board

FES_Z80_NMOS_RTL := cores/fes-common/rtl/z80/fes_z80_alu.sv \
	cores/fes-common/rtl/z80/fes_z80_engine.sv cores/fes-common/rtl/z80/fes_z80_bus.sv \
	cores/fes-common/rtl/z80/fes_z80_nmos.sv

SPECTRUM_DIAGNOSTIC := build/diagnostics/fes-spectrum
SPECTRUM_VERILATOR_FLAGS := -Wall -Wno-UNUSEDSIGNAL -Wno-DECLFILENAME -Wno-WIDTHEXPAND \
	-Wno-WIDTHTRUNC -Wno-CASEX -Wno-PINCONNECTEMPTY -Wno-UNUSEDPARAM \
	-Wno-CASEINCOMPLETE -Wno-CASEOVERLAP -Wno-SYNCASYNCNET -Wno-UNOPTFLAT \
	-Wno-BLKSEQ $(VERILATOR_PROCASSINIT) \
	-Icores/fes-spectrum/rtl -Icores/fes-common/generated -Icores/fes-common/rtl
SPECTRUM_RTL := cores/fes-spectrum/rtl/spectrum_machine.sv cores/fes-spectrum/rtl/spectrum_ula.sv \
	cores/fes-spectrum/rtl/spectrum_contention.sv cores/fes-spectrum/rtl/spectrum_ram.v \
	cores/fes-spectrum/rtl/spectrum_rom.v cores/fes-spectrum/rtl/spectrum_video.v \
	cores/fes-spectrum/rtl/spectrum_tape.v cores/fes-spectrum/rtl/spectrum_snapshot.v \
	cores/fes-spectrum/rtl/spectrum_beta.sv cores/fes-spectrum/rtl/spectrum_fdd.sv \
	cores/fes-spectrum/rtl/spectrum_upd765.sv \
	cores/fes-spectrum/rtl/spectrum_keyboard.v \
	cores/fes-spectrum/rtl/spectrum_audio.v cores/fes-spectrum/rtl/spectrum_slot_sockets.v \
	cores/fes-zx81/expansions/zonx_ay.v $(FES_Z80_NMOS_RTL)
SPECTRUM_PLANE_RTL := cores/fes-common/rtl/fes_display_cdc.v cores/fes-common/rtl/fes_display_plane.v \
	cores/fes-menu/rtl/fes_menu_control.v cores/fes-menu/rtl/fes_menu_video.v \
	cores/fes-menu/rtl/fes_menu_reader.v cores/fes-common/rtl/fes_hps_ddr.v \
	cores/fes-common/rtl/fes_hps_ddr_guard.v cores/fes-common/rtl/fes_media_ddr.v \
	cores/fes-common/sim/hps_ddr_media_model.v

.PHONY: spectrum-diagnostic sim-fes-spectrum sim-fes-spectrum-machine sim-fes-spectrum-board sim-fes-spectrum-tape
.PHONY: sim-fes-spectrum-contention sim-fes-spectrum-rom
spectrum-diagnostic:
	@mkdir -p $(SPECTRUM_DIAGNOSTIC)
	$(PYTHON) cores/fes-spectrum/diagnostic/firmware.py --output-dir $(SPECTRUM_DIAGNOSTIC)

sim-fes-spectrum: sim-fes-spectrum-machine sim-fes-spectrum-board sim-fes-spectrum-tape

sim-fes-spectrum-rom:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_spectrum_rom.py --verilator "$(VERILATOR)" --mem-sim "$(ROM_MEM_SIM)"

# Every model's machine with the open diagnostic firmware.
sim-fes-spectrum-machine:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_spectrum.py --verilator "$(VERILATOR)"

# Fuse's per-instruction contention points; fetches the pinned fixtures.
sim-fes-spectrum-contention:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_spectrum.py --verilator "$(VERILATOR)" --model 48 --download

# The PZX deck against a reference expansion; the 48K build also stops at
# STOP blocks flagged 48K-only.
sim-fes-spectrum-tape:
	$(require_local_sim)
	@mkdir -p build/sim/fes-spectrum-tape build/sim/fes-spectrum-tape-48k
	$(VERILATOR) --cc --exe --build -O2 --top-module spectrum_tape -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-spectrum-tape" \
		cores/fes-spectrum/rtl/spectrum_tape.v \
		"$(CURDIR)/cores/fes-spectrum/sim/tape_tb.cpp"
	@build/sim/fes-spectrum-tape/Vspectrum_tape
	$(VERILATOR) --cc --exe --build -O2 --top-module spectrum_tape -Wall -GIS_48K=1\'b1 \
		-CFLAGS -DTAPE_IS_48K=1 --Mdir "$(CURDIR)/build/sim/fes-spectrum-tape-48k" \
		cores/fes-spectrum/rtl/spectrum_tape.v \
		"$(CURDIR)/cores/fes-spectrum/sim/tape_tb.cpp"
	@build/sim/fes-spectrum-tape-48k/Vspectrum_tape

sim-fes-spectrum-board: spectrum-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-spectrum-board
	$(VERILATOR) --cc --exe --build -O2 --top-module top $(SPECTRUM_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-spectrum-board" \
		cores/fes-spectrum/sim/board_models.v cores/fes-spectrum/rtl/top.v $(SPECTRUM_RTL) \
		$(SPECTRUM_PLANE_RTL) cores/fes-common/rtl/fes_computer_mailbox.v \
		cores/fes-common/rtl/fes_audio_output.v cores/fes-common/rtl/fes_audio_i2s.v \
		"$(CURDIR)/cores/fes-spectrum/sim/board_tb.cpp"
	@build/sim/fes-spectrum-board/Vtop

C64_SID_AUDIO_SOURCES := cores/fes-c64/rtl/c64_sid_svf.sv cores/fes-c64/rtl/c64_sid_filter_rom.sv cores/fes-c64/rtl/c64_sid_filter_table.sv cores/fes-c64/rtl/c64_sid_vcr_shared.sv cores/fes-c64/rtl/c64_sid_integrator.sv cores/fes-c64/rtl/c64_sid_filter.sv cores/fes-c64/rtl/c64_sid_output_filter.sv cores/fes-c64/rtl/c64_sid_dac.sv cores/fes-c64/rtl/c64_sid_audio.sv
C64_DIAGNOSTIC := build/diagnostics/fes-c64
C64_VERILATOR_FLAGS := -DFES_SIMULATION -Wall -Wno-UNUSEDSIGNAL -Wno-DECLFILENAME -Wno-WIDTHEXPAND \
	-Wno-WIDTHTRUNC -Wno-CASEX -Wno-PINCONNECTEMPTY -Wno-UNUSEDPARAM \
	-Wno-CASEINCOMPLETE -Wno-CASEOVERLAP -Wno-SYNCASYNCNET -Wno-UNOPTFLAT \
	$(VERILATOR_PROCASSINIT) \
	-Icores/fes-c64/rtl -Icores/fes-c64/expansions -Icores/fes-common/generated
# Full writable visibility remains available for deep internal diagnostics.
C64_BOOT_FULL_VISIBILITY ?= 0
C64_BOOT_VISIBILITY := $(if $(filter 1,$(C64_BOOT_FULL_VISIBILITY)),--public-flat-rw,cores/fes-c64/sim/observe.vlt)
C64_RTL := cores/fes-c64/rtl/c64_machine.sv cores/fes-c64/rtl/c64_ram.v \
	cores/fes-c64/rtl/c64_color_ram.v \
	cores/fes-c64/rtl/c64_rom.v cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv \
	cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv cores/fes-c64/rtl/c64_frame_store.sv cores/fes-c64/rtl/c64_sid_envelope.sv cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv cores/fes-c64/rtl/c64_sid.v $(C64_SID_AUDIO_SOURCES) \
	cores/fes-c64/rtl/c64_cia.v cores/fes-c64/rtl/c64_disk_store.v \
	cores/fes-c64/rtl/c64_drive1541.sv cores/fes-c64/rtl/c64_via.v cores/fes-c64/rtl/c64_gcr_read.v cores/fes-c64/rtl/c64_d64_medium.sv \
	cores/fes-c64/rtl/c64_keyboard.v cores/fes-c64/rtl/c64_slot_sockets.v \
	cores/fes-c64/expansions/probe.v \
	cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v

.PHONY: c64-diagnostic sim-fes-c64 sim-fes-c64-cia sim-fes-c64-cia-reference sim-fes-c64-color-ram
c64-diagnostic:
	@mkdir -p $(C64_DIAGNOSTIC)
	$(PYTHON) cores/fes-c64/diagnostic/firmware.py --output-dir $(C64_DIAGNOSTIC)
	$(PYTHON) cores/fes-c64/diagnostic/disk.py --output $(C64_DIAGNOSTIC)/disk.d64 \
		--hex-output $(C64_DIAGNOSTIC)/disk.hex

sim-fes-c64-cia:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-cia
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_cia $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-cia" \
		cores/fes-c64/rtl/c64_cia.v "$(CURDIR)/cores/fes-c64/sim/cia_tb.cpp"
	@build/sim/fes-c64-cia/Vc64_cia

sim-fes-c64-cia-reference:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-cia-reference
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_cia $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-cia-reference" \
		cores/fes-c64/rtl/c64_cia.v "$(CURDIR)/cores/fes-c64/sim/cia_timing_tb.cpp"
	@build/sim/fes-c64-cia-reference/Vc64_cia

.PHONY: sim-fes-c64-d021
sim-fes-c64-d021:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-d021
	$(VERILATOR) --cc --exe --build -O2 --top-module d021_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-d021" \
		cores/fes-c64/sim/d021_sim_top.sv $(C64_RTL) "$(CURDIR)/cores/fes-c64/sim/d021_tb.cpp" \
		-CFLAGS '-O2 -std=c++17' -MAKEFLAGS 'OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3'
	@for fixture in 0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23; do \
		build/sim/fes-c64-d021/Vd021_sim_top $$fixture \
		build/sim/fes-c64-d021/firmware.hex build/sim/fes-c64-d021/disk.hex || exit $$?; \
	done

.PHONY: sim-fes-c64-vic-timing
sim-fes-c64-vic-timing:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-vic-timing
	$(VERILATOR) --cc --exe --build -O2 --top-module vic_timing_sim_top $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-vic-timing" \
		cores/fes-c64/sim/vic_timing_sim_top.sv cores/fes-c64/rtl/c64_vic.v \
		cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv \
		cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv \
		cores/fes-c64/rtl/c64_vic_sprite_fetch.sv cores/fes-c64/rtl/c64_frame_store.sv \
		cores/fes-c64/rtl/c64_color_ram.v "$(CURDIR)/cores/fes-c64/sim/vic_timing_tb.cpp" \
		-CFLAGS '-O2 -std=c++17'
	@build/sim/fes-c64-vic-timing/Vvic_timing_sim_top

.PHONY: sim-fes-c64-vic
.PHONY: sim-fes-c64-so sim-fes-c64-drive-so sim-fes-c64-via sim-fes-c64-drive sim-fes-c64-drive-build
sim-fes-c64-drive-so:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-drive-so
	$(VERILATOR) --cc --exe --build -O2 --top-module drive_so_sim_top $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-drive-so" \
		cores/fes-c64/sim/drive_so_sim_top.sv cores/fes-c64/rtl/c64_drive1541.sv \
		cores/fes-c64/rtl/c64_via.v cores/fes-common/rtl/cpu6502/cpu6502.v \
		cores/fes-common/rtl/cpu6502/cpu6502_alu.v "$(CURDIR)/cores/fes-c64/sim/drive_so_tb.cpp"
	@build/sim/fes-c64-drive-so/Vdrive_so_sim_top

sim-fes-c64-so:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-so build/sim/fes-c64-so-disabled
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) --public-flat-rw -GENABLE_SO=1 \
		--Mdir "$(CURDIR)/build/sim/fes-c64-so" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/so_tb.cpp"
	@build/sim/fes-c64-so/Vcpu6502
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-so-disabled" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/so_tb.cpp"
	@build/sim/fes-c64-so-disabled/Vcpu6502 +SO_DISABLED=1

sim-fes-c64-via:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-via
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_via $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-via" \
		cores/fes-c64/rtl/c64_via.v "$(CURDIR)/cores/fes-c64/sim/via_tb.cpp"
	@build/sim/fes-c64-via/Vc64_via

sim-fes-c64-drive-build:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-drive
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_drive_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-drive" \
		cores/fes-c64/sim/c64_drive_sim_top.sv cores/fes-c64/rtl/c64_rom.v \
		cores/fes-c64/rtl/c64_drive1541.sv cores/fes-c64/rtl/c64_via.v \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/drive_tb.cpp"

sim-fes-c64-drive: sim-fes-c64-so sim-fes-c64-drive-so sim-fes-c64-via sim-fes-c64-drive-build
	$(PYTHON) cores/fes-c64/diagnostic/drive_firmware.py --output-dir build/diagnostics/fes-c64-drive
	@cd build/sim/fes-c64-drive && ./Vc64_drive_sim_top \
		+C64_FIRMWARE_HEX=$(CURDIR)/build/diagnostics/fes-c64-drive/combined-firmware.hex \
		+C64_DRIVE_DIAGNOSTIC=1 +C64_DRIVE_CYCLES=1000000

sim-fes-c64-vic:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-vic
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_vic $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-vic" \
		cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv cores/fes-c64/rtl/c64_frame_store.sv cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/vic_tb.cpp"
	@build/sim/fes-c64-vic/Vc64_vic

.PHONY: sim-fes-c64-keyboard
sim-fes-c64-keyboard:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-keyboard
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_keyboard $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-keyboard" \
		cores/fes-c64/rtl/c64_keyboard.v "$(CURDIR)/cores/fes-c64/sim/keyboard_tb.cpp"
	@build/sim/fes-c64-keyboard/Vc64_keyboard

sim-fes-c64-color-ram:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-color-ram
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_color_ram $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-color-ram" \
		cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/color_ram_tb.cpp"
	@build/sim/fes-c64-color-ram/Vc64_color_ram

.PHONY: sim-fes-c64-boot-build
sim-fes-c64-boot-build:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-boot
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_boot_sim_top $(C64_VERILATOR_FLAGS) $(C64_BOOT_VISIBILITY) --savable \
		-MAKEFLAGS 'OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3' \
		--Mdir "$(CURDIR)/build/sim/fes-c64-boot" \
		cores/fes-c64/sim/c64_boot_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/boot_tb.cpp"

.PHONY: sim-fes-c64-boot-oracle-build sim-fes-c64-gcr
sim-fes-c64-boot-oracle-build:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-boot-oracle
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_boot_sim_top $(C64_VERILATOR_FLAGS) $(C64_BOOT_VISIBILITY) --savable \
		-MAKEFLAGS 'OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3' \
		-DFES_C64_EXTERNAL_MEDIUM_SIM -CFLAGS -DFES_C64_EXTERNAL_MEDIUM_SIM \
		--Mdir "$(CURDIR)/build/sim/fes-c64-boot-oracle" \
		cores/fes-c64/sim/c64_boot_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/boot_tb.cpp"

C64_MEDIUM_FIXTURE := build/sim/fes-c64-medium-fixture
.PHONY: c64-medium-fixture sim-fes-c64-medium sim-fes-c64-medium-mailbox
c64-medium-fixture:
	$(PYTHON) cores/fes-c64/sim/medium_fixture.py --output-dir $(C64_MEDIUM_FIXTURE)

sim-fes-c64-medium: c64-medium-fixture
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-medium
	$(VERILATOR) --cc --exe --build -O2 --top-module medium_top $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-medium" \
		cores/fes-c64/sim/medium_top.sv cores/fes-c64/rtl/c64_d64_medium.sv \
		cores/fes-c64/rtl/c64_disk_store.v "$(CURDIR)/cores/fes-c64/sim/medium_tb.cpp"
	@build/sim/fes-c64-medium/Vmedium_top $(C64_MEDIUM_FIXTURE)/disk1.g64 \
		$(C64_MEDIUM_FIXTURE)/disk2.g64 $(C64_MEDIUM_FIXTURE)/disk2.d64 \
		+C64_DISK_HEX=$(C64_MEDIUM_FIXTURE)/disk1.hex

sim-fes-c64-medium-mailbox: c64-medium-fixture
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-medium-mailbox
	$(VERILATOR) --cc --exe --build -O2 --top-module mailbox_medium_top $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-medium-mailbox" \
		cores/fes-c64/sim/mailbox_medium_top.sv cores/fes-c64/rtl/c64_d64_medium.sv \
		cores/fes-c64/rtl/c64_disk_store.v cores/fes-common/rtl/fes_computer_mailbox.v \
		"$(CURDIR)/cores/fes-c64/sim/mailbox_medium_tb.cpp"
	@build/sim/fes-c64-medium-mailbox/Vmailbox_medium_top $(C64_MEDIUM_FIXTURE)/disk1.d64 \
		$(C64_MEDIUM_FIXTURE)/disk1.g64 $(C64_MEDIUM_FIXTURE)/disk2.d64 \
		$(C64_MEDIUM_FIXTURE)/disk2.g64 +C64_DISK_HEX=$(C64_MEDIUM_FIXTURE)/empty.hex


.PHONY: sim-fes-c64-sprite
sim-fes-c64-sprite:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sprite
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_vic_sprite_fetch $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sprite" \
		cores/fes-c64/rtl/c64_vic_sprite_fetch.sv \
		"$(CURDIR)/cores/fes-c64/sim/sprite_tb.cpp"
	@build/sim/fes-c64-sprite/Vc64_vic_sprite_fetch

.PHONY: sim-fes-c64-ba-timing
sim-fes-c64-ba-timing:
	$(require_local_sim)
	$(PYTHON) cores/fes-c64/sim/ba_timing_cases.py --verilator "$(VERILATOR)" \
		--build-dir build/sim/fes-c64-ba-timing -- $(C64_VERILATOR_FLAGS) $(C64_RTL)

.PHONY: sim-fes-c64-ready
sim-fes-c64-ready: c64-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-ready
	$(VERILATOR) --cc --exe --build -O2 --top-module ready_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-ready" \
		cores/fes-c64/sim/ready_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/ready_tb.cpp"
	@build/sim/fes-c64-ready/Vready_sim_top "$(CURDIR)/build/sim/fes-c64-ready/firmware.hex"

.PHONY: sim-fes-c64-push
sim-fes-c64-push: c64-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-push-bus build/sim/fes-c64-push
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-push-bus" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/push_bus_tb.cpp"
	@build/sim/fes-c64-push-bus/Vcpu6502
	$(VERILATOR) --cc --exe --build -O2 --top-module ready_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-push" \
		cores/fes-c64/sim/ready_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/push_tb.cpp"
	@build/sim/fes-c64-push/Vready_sim_top "$(CURDIR)/build/sim/fes-c64-push/firmware.hex"

.PHONY: sim-fes-c64-jsr
sim-fes-c64-jsr: c64-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-jsr-bus build/sim/fes-c64-jsr
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-jsr-bus" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/jsr_bus_tb.cpp"
	@build/sim/fes-c64-jsr-bus/Vcpu6502
	$(VERILATOR) --cc --exe --build -O2 --top-module ready_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-jsr" \
		cores/fes-c64/sim/ready_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/jsr_tb.cpp"
	@build/sim/fes-c64-jsr/Vready_sim_top "$(CURDIR)/build/sim/fes-c64-jsr/firmware.hex"

.PHONY: sim-fes-c64-nmi
sim-fes-c64-nmi:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-nmi
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-nmi" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/nmi_tb.cpp"
	@build/sim/fes-c64-nmi/Vcpu6502

.PHONY: sim-fes-c64-irq
sim-fes-c64-irq:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-irq
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-irq" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/irq_tb.cpp"
	@build/sim/fes-c64-irq/Vcpu6502

.PHONY: sim-fes-c64-clock
sim-fes-c64-clock: c64-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-clock
	$(VERILATOR) --cc --exe --build -O2 --top-module clock_sim_top $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-clock" \
		cores/fes-c64/sim/clock_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/clock_tb.cpp"
	@build/sim/fes-c64-clock/Vclock_sim_top

.PHONY: sim-fes-c64-cadence
sim-fes-c64-cadence:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-cadence
	$(VERILATOR) --cc --exe --build -O2 --top-module cadence_sim_top $(C64_VERILATOR_FLAGS) \
		-CFLAGS "-O2" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-cadence" \
		cores/fes-c64/sim/cadence_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/cadence_tb.cpp"
	@build/sim/fes-c64-cadence/Vcadence_sim_top

.PHONY: sim-fes-c64-sax
sim-fes-c64-sax:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sax
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sax" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/sax_tb.cpp"
	@build/sim/fes-c64-sax/Vcpu6502

.PHONY: sim-fes-c64-axs
sim-fes-c64-axs:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-axs
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-axs" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/axs_tb.cpp"
	@build/sim/fes-c64-axs/Vcpu6502

.PHONY: sim-fes-c64-axs-oracle
sim-fes-c64-axs-oracle:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-axs-oracle
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-axs-oracle" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/axs_oracle_tb.cpp"
	@build/sim/fes-c64-axs-oracle/Vcpu6502

.PHONY: sim-fes-c64-rmw
sim-fes-c64-rmw:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-rmw
	$(VERILATOR) --cc --exe --build -O2 --top-module cpu6502 $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-rmw" \
		cores/fes-common/rtl/cpu6502/cpu6502.v cores/fes-common/rtl/cpu6502/cpu6502_alu.v \
		"$(CURDIR)/cores/fes-c64/sim/rmw_tb.cpp"
	@build/sim/fes-c64-rmw/Vcpu6502

sim-fes-c64-gcr:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-gcr
	$(PYTHON) cores/fes-c64/sim/gcr_fixture.py --output build/sim/fes-c64-gcr
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_gcr_read $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-gcr" cores/fes-c64/rtl/c64_gcr_read.v \
		"$(CURDIR)/cores/fes-c64/sim/gcr_read_tb.cpp"
	@build/sim/fes-c64-gcr/Vc64_gcr_read build/sim/fes-c64-gcr/track-1.hex build/sim/fes-c64-gcr/blocks-1.hex 3 21
	@build/sim/fes-c64-gcr/Vc64_gcr_read build/sim/fes-c64-gcr/track-18.hex build/sim/fes-c64-gcr/blocks-18.hex 2 19
	@build/sim/fes-c64-gcr/Vc64_gcr_read build/sim/fes-c64-gcr/track-25.hex build/sim/fes-c64-gcr/blocks-25.hex 1 18
	@build/sim/fes-c64-gcr/Vc64_gcr_read build/sim/fes-c64-gcr/track-31.hex build/sim/fes-c64-gcr/blocks-31.hex 0 17

.PHONY: sim-fes-c64-checkpoint
sim-fes-c64-checkpoint: sim-fes-c64-boot-build sim-fes-c64-boot-oracle-build
	$(PYTHON) cores/fes-c64/sim/checkpoint_tb.py
	$(PYTHON) cores/fes-c64/sim/observer_tb.py

.PHONY: sim-fes-c64-video sim-fes-c64-rom sim-fes-c64-fetch sim-fes-c64-frame
sim-fes-c64-video:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-video
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_video_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-video" \
		cores/fes-c64/sim/video_sim_top.sv cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv \
		cores/fes-c64/rtl/c64_frame_store.sv \
		cores/fes-c64/rtl/c64_ram.v cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/video_tb.cpp"
	@build/sim/fes-c64-video/Vc64_video_sim_top

.PHONY: sim-fes-c64-rom-primitive
sim-fes-c64-rom-primitive:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-rom-primitive
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_rom $(C64_VERILATOR_FLAGS) -UVERILATOR \
		--Mdir "$(CURDIR)/build/sim/fes-c64-rom-primitive" cores/fes-c64/rtl/c64_rom.v \
		cores/fes-c64/sim/rom_primitives.sv "$(CURDIR)/cores/fes-c64/sim/rom_tb.cpp"
	@build/sim/fes-c64-rom-primitive/Vc64_rom

sim-fes-c64-rom: c64-diagnostic sim-fes-c64-rom-primitive
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-rom
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_rom $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-rom" cores/fes-c64/rtl/c64_rom.v \
		"$(CURDIR)/cores/fes-c64/sim/rom_tb.cpp"
	@build/sim/fes-c64-rom/Vc64_rom +C64_FIRMWARE_HEX=build/diagnostics/fes-c64/rom-test.hex

.PHONY: sim-fes-c64-fetch sim-fes-c64-frame
sim-fes-c64-fetch:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-fetch build/sim/fes-c64-fetch-dynamic
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_vic_fetch $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-fetch" cores/fes-c64/rtl/c64_vic_fetch.sv \
		"$(CURDIR)/cores/fes-c64/sim/fetch_tb.cpp"
	@build/sim/fes-c64-fetch/Vc64_vic_fetch
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_vic_fetch $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-fetch-dynamic" cores/fes-c64/rtl/c64_vic_fetch.sv \
		"$(CURDIR)/cores/fes-c64/sim/fetch_dynamic_tb.cpp"
	@build/sim/fes-c64-fetch-dynamic/Vc64_vic_fetch

sim-fes-c64-frame:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-frame
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_frame_store $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-frame" cores/fes-c64/rtl/c64_frame_store.sv \
		"$(CURDIR)/cores/fes-c64/sim/frame_tb.cpp"
	@build/sim/fes-c64-frame/Vc64_frame_store

# The full c64 suite in four independent groups, so CI runs them as parallel
# matrix rows; sim-fes-c64 still runs everything.
.PHONY: sim-fes-c64-cpu-group sim-fes-c64-video-a-group sim-fes-c64-video-b-group sim-fes-c64-system-group sim-fes-c64-machine
sim-fes-c64-cpu-group: sim-fes-c64-rmw sim-fes-c64-sax sim-fes-c64-axs sim-fes-c64-axs-oracle sim-fes-c64-jsr sim-fes-c64-push sim-fes-c64-irq sim-fes-c64-nmi sim-fes-c64-ready sim-fes-c64-clock sim-fes-c64-cadence
sim-fes-c64-video-a-group: sim-fes-c64-d021 sim-fes-c64-ba-timing sim-fes-c64-vic-timing sim-fes-c64-sprite-pal sim-fes-c64-open-graphics sim-fes-c64-graphics sim-fes-c64-border-position sim-fes-c64-scroll sim-fes-c64-graphics-modes
sim-fes-c64-video-b-group: sim-fes-c64-collision-cpu sim-fes-c64-collisions sim-fes-c64-sprite-video sim-fes-c64-border sim-fes-c64-readback sim-fes-c64-vic sim-fes-c64-sprite sim-fes-c64-video sim-fes-c64-fetch sim-fes-c64-frame
sim-fes-c64-system-group: sim-fes-c64-session sim-fes-c64-medium sim-fes-c64-medium-mailbox sim-fes-c64-sid c64-diagnostic sim-fes-c64-cia sim-fes-c64-cia-reference sim-fes-c64-color-ram sim-fes-c64-keyboard sim-fes-c64-drive sim-fes-c64-gcr sim-fes-c64-checkpoint sim-fes-c64-rom sim-fes-c64-machine
sim-fes-c64: sim-fes-c64-cpu-group sim-fes-c64-video-a-group sim-fes-c64-video-b-group sim-fes-c64-system-group
sim-fes-c64-machine: c64-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-machine
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_sim_top $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-machine" \
		cores/fes-c64/sim/c64_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/machine_tb.cpp"
	$(PYTHON) cores/fes-c64/sim/diagnostic_cases.py \
		--model build/sim/fes-c64-machine/Vc64_sim_top --diagnostic-dir $(C64_DIAGNOSTIC)

sg1000-diagnostic:
	@mkdir -p build/diagnostics/fes-sg1000
	$(PYTHON) cores/fes-sg1000/diagnostic/generate.py \
		--output build/diagnostics/fes-sg1000/graphics-i.rom \
		--preview build/diagnostics/fes-sg1000/graphics-i.ppm
	$(PYTHON) cores/fes-sg1000/diagnostic/generate.py --pad-to 16384 \
		--output build/diagnostics/fes-sg1000/graphics-i-16k.rom \
		--hex-output build/diagnostics/fes-sg1000/graphics-i-16k.hex
	$(PYTHON) cores/fes-sg1000/diagnostic/generate.py --controllers \
		--output build/diagnostics/fes-sg1000/controller.rom \
		--preview build/diagnostics/fes-sg1000/controller.ppm
	$(PYTHON) cores/fes-sg1000/diagnostic/generate.py --controllers --pad-to 16384 \
		--output build/diagnostics/fes-sg1000/controller-16k.rom
	$(PYTHON) cores/fes-sg1000/diagnostic/generate.py --sound --pad-to 16384 \
		--output build/diagnostics/fes-sg1000/sound-16k.rom

sim-fes-sg1000: sim-fes-z80-timing sg1000-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-sg1000-machine
	$(VERILATOR) --cc --exe --build --top-module sg1000_machine -Wall \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		-Icores/fes-sg1000/generated -Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-sg1000-machine" \
		cores/fes-sg1000/rtl/sg1000_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/fes_sn76489.sv \
		$(FES_Z80_NMOS_RTL) \
		"$(CURDIR)/cores/fes-sg1000/sim/machine_tb.cpp"
	@build/sim/fes-sg1000-machine/Vsg1000_machine \
		build/diagnostics/fes-sg1000/graphics-i.rom \
		build/diagnostics/fes-sg1000/controller.rom

sim-fes-sg1000-oss: sim-fes-z80-timing sg1000-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-sg1000-machine-oss
	$(VERILATOR) --cc --exe --build --top-module sg1000_machine -Wall \
		-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		-Icores/fes-sg1000/generated -Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-sg1000-machine-oss" \
		cores/fes-sg1000/rtl/sg1000_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/fes_sn76489.sv \
		$(FES_Z80_NMOS_RTL) \
		"$(CURDIR)/cores/fes-sg1000/sim/machine_tb.cpp"
	@build/sim/fes-sg1000-machine-oss/Vsg1000_machine \
		build/diagnostics/fes-sg1000/graphics-i.rom \
		build/diagnostics/fes-sg1000/controller.rom

sim-fes-sg1000-rom-link: sim-fes-z80-timing sg1000-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-sg1000-rom-link
	$(VERILATOR) --cc --exe --build --top-module sg1000_machine -Wall \
		-DFES_SG1000_OSS=1 -DFES_SG1000_ROM_LINK=1 -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_SG1000_ROM_LINK=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		-Icores/fes-sg1000/generated -Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-sg1000-rom-link" \
		cores/fes-sg1000/rtl/sg1000_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-sg1000/rtl/sg1000_rom_link.v \
		cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/fes_sn76489.sv \
		$(FES_Z80_NMOS_RTL) \
		"$(CURDIR)/cores/fes-sg1000/sim/machine_tb.cpp"
	@build/sim/fes-sg1000-rom-link/Vsg1000_machine \
		build/diagnostics/fes-sg1000/graphics-i-16k.rom

.PHONY: sim-fes-sg1000-gp-audio
sim-fes-sg1000-gp-audio:
	$(require_local_sim)
	@mkdir -p build/sim/fes-sg1000-gp-audio
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall \
		-GENABLE_MEDIA_BLOB=0 -GENABLE_AUDIO=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNUSEDPARAM -Wno-PINCONNECTEMPTY \
		-Icores/fes-sg1000/generated \
		--Mdir "$(CURDIR)/build/sim/fes-sg1000-gp-audio" \
		cores/fes-common/rtl/fes_computer_gp.v cores/fes-common/rtl/coleco_dpram.v \
		"$(CURDIR)/cores/fes-sg1000/sim/gp_audio_tb.cpp"
	@build/sim/fes-sg1000-gp-audio/Vfes_computer_gp

.PHONY: sim-fes-sg1000-quartus
sim-fes-sg1000-quartus:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_sg1000_quartus.py

build-fes-sg1000-quartus:
	$(PYTHON) scripts/build_fes_sg1000.py --root "$(CURDIR)"

build-fes-sg1000:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_sg1000_oss.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

.PHONY: build-fes-apple2
build-fes-apple2:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_apple2_oss.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

.PHONY: build-fes-c64
build-fes-c64:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_c64_oss.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

.PHONY: build-fes-spectrum build-fes-spectrum128 build-fes-spectrum-plus3 build-fes-pentagon
build-fes-spectrum:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_spectrum_oss.py --root "$(CURDIR)" --model 48$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

build-fes-spectrum128:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_spectrum_oss.py --root "$(CURDIR)" --model 128$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

build-fes-spectrum-plus3:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_spectrum_oss.py --root "$(CURDIR)" --model plus3$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

build-fes-pentagon:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_spectrum_oss.py --root "$(CURDIR)" --model pentagon$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

.PHONY: sms-diagnostic
sms-diagnostic:
	@mkdir -p build/diagnostics/fes-sms
	$(PYTHON) cores/fes-sms/diagnostic/generate.py \
		--output build/diagnostics/fes-sms/mode4.rom \
		--preview build/diagnostics/fes-sms/mode4.ppm
	$(PYTHON) cores/fes-sms/diagnostic/generate.py --pad-to 32768 \
		--output build/diagnostics/fes-sms/mode4-32k.rom \
		--hex-output build/diagnostics/fes-sms/mode4-32k.hex
	$(PYTHON) cores/fes-sms/diagnostic/generate.py --interactive \
		--output build/diagnostics/fes-sms/mode4-hil.rom \
		--preview build/diagnostics/fes-sms/mode4-hil.ppm
	$(PYTHON) cores/fes-sms/diagnostic/generate.py --interactive --pad-to 32768 \
		--output build/diagnostics/fes-sms/mode4-hil-32k.rom

sim-fes-sms: sim-fes-z80-timing sms-diagnostic sim-fes-audio-output
	$(require_local_sim)
	@mkdir -p build/sim/fes-sms-gp
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall --public \
		-Wno-PINCONNECTEMPTY -Wno-UNUSEDPARAM -Wno-UNUSEDSIGNAL \
		-Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-BLKSEQ \
		-GENABLE_MEDIA_STREAM=1 -GENABLE_AUDIO=1 \
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
	@mkdir -p build/sim/fes-sms-machine
	$(VERILATOR) --cc --exe --build --top-module sms_machine -Wall \
		-DTV80_REFRESH=1 \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		-Icores/fes-sms/generated -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-sms-machine" \
		cores/fes-sms/rtl/sms_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-sms/rtl/sms_vdp.sv \
		cores/fes-sms/rtl/sms_psg.sv \
		cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-sms/sim/machine_tb.cpp"
	@build/sim/fes-sms-machine/Vsms_machine \
		build/diagnostics/fes-sms/mode4.rom

sim-fes-sms-rom-link-mailbox:
	$(require_local_sim)
	@mkdir -p build/sim/fes-sms-gp-rom-link
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall \
		-DFES_COLECO_OSS=1 -CFLAGS "-DFES_COLECO_OSS=1" \
		-Wno-PINCONNECTEMPTY -Wno-UNUSEDPARAM -Wno-UNUSEDSIGNAL \
		-Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-BLKSEQ \
		-GENABLE_MEDIA_BLOB=0 -GENABLE_AUDIO=1 \
		-Icores/fes-sms/generated -Icores/fes-coleco/generated \
		--Mdir "$(CURDIR)/build/sim/fes-sms-gp-rom-link" \
		cores/fes-common/rtl/fes_computer_gp.v cores/fes-common/rtl/coleco_dpram.v \
		"$(CURDIR)/cores/fes-sms/sim/no_media_tb.cpp"
	@build/sim/fes-sms-gp-rom-link/Vfes_computer_gp

sim-fes-sms-oss: sim-fes-z80-timing sms-diagnostic sim-fes-sms-rom-link-mailbox sim-fes-audio-output
	$(require_local_sim)
	@mkdir -p build/sim/fes-sms-gp-oss
	$(VERILATOR) --cc --exe --build --top-module fes_computer_gp -Wall --public \
		-DFES_COLECO_OSS=1 -CFLAGS "-DFES_COLECO_OSS=1" \
		-Wno-PINCONNECTEMPTY -Wno-UNUSEDPARAM -Wno-UNUSEDSIGNAL \
		-Wno-WIDTHTRUNC -Wno-WIDTHEXPAND -Wno-BLKSEQ \
		-GENABLE_MEDIA_STREAM=1 -GENABLE_AUDIO=1 \
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
		-DTV80_REFRESH=1 -DFES_SMS_OSS=1 -DFES_SMS_ROM_LINK=1 -DFES_COLECO_OSS=1 \
		-CFLAGS "-DFES_SMS_OSS=1 -DFES_SMS_ROM_LINK=1 -DFES_COLECO_OSS=1" \
		-Wno-UNUSEDSIGNAL -Wno-UNOPTFLAT -Wno-CASEINCOMPLETE -Wno-WIDTHTRUNC \
		-Wno-WIDTHEXPAND -Wno-SYNCASYNCNET -Wno-PINCONNECTEMPTY \
		-Wno-DECLFILENAME -Wno-IMPLICITSTATIC -Wno-VARHIDDEN -Wno-UNUSEDPARAM \
		-Wno-CASEX -Wno-BLKSEQ \
		-Icores/fes-sms/generated -Icores/fes-coleco/generated -Icores/fes-common/rtl/tv80 \
		--Mdir "$(CURDIR)/build/sim/fes-sms-machine-oss" \
		cores/fes-sms/rtl/sms_machine.sv cores/fes-common/rtl/fes_z80_ce.sv cores/fes-sms/rtl/sms_rom_link.v cores/fes-sms/rtl/sms_vdp.sv \
		cores/fes-sms/rtl/sms_psg.sv \
		cores/fes-common/rtl/coleco_vdp.sv \
		cores/fes-common/rtl/coleco_dpram.v cores/fes-common/rtl/coleco_video_dpram.v \
		cores/fes-common/rtl/t80pa.v cores/fes-common/rtl/tv80/tv80_core.v \
		cores/fes-common/rtl/tv80/tv80_alu.v cores/fes-common/rtl/tv80/tv80_mcode.v \
		cores/fes-common/rtl/tv80/tv80_reg.v \
		"$(CURDIR)/cores/fes-sms/sim/machine_tb.cpp"
	@build/sim/fes-sms-machine-oss/Vsms_machine \
		build/diagnostics/fes-sms/mode4-32k.rom

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

toolchain-fes-menu:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-menu/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-menu") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-catch:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-catch/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-catch") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-pong:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-pong/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-pong") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

.PHONY: toolchain-fes-atari-st
toolchain-fes-atari-st:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-atari-st/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-atari-st") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

.PHONY: toolchain-fes-ramtest
toolchain-fes-ramtest:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-ramtest/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-ramtest") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-zx81: toolchain-zx81-expansion

.PHONY: toolchain-zx81-expansion
toolchain-zx81-expansion:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-zx81/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/zx81-expansion") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-coleco: toolchain-fes-coleco-sgm

.PHONY: toolchain-fes-coleco-sgm
toolchain-fes-coleco-sgm:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-coleco/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-coleco-socket-v2") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-sg1000:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-sg1000/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-sg1000") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-apple2:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-apple2/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-apple2") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-c64:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-c64/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-c64") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-spectrum:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-spectrum/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-spectrum") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-sms:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-sms/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-sms") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-riscv:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-riscv/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-riscv") \
	FES_TOOLCHAIN_GPU_ROUTER=HIP \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

toolchain-fes-pc386:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-pc386/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-pc386") \
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

.PHONY: sim-fes-menu
sim-fes-menu:
	$(PYTHON) scripts/sim_fes_menu.py --verilator $(VERILATOR)

.PHONY: build-fes-menu-pattern
build-fes-menu-pattern:
	$(FES_SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_menu.py $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),--cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",)

.PHONY: build-fes-menu-ddr
build-fes-menu-ddr:
	$(FES_SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_menu.py --mode ddr $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),--cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",)

.PHONY: build-fes-menu-package
build-fes-menu-package:
	$(FES_SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_menu_package.py $(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE),--cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",)

.PHONY: sim-fes-zx81-clock sim-fes-zx81-ay
sim-fes-zx81-clock:
	$(require_local_sim)
	@mkdir -p build/sim/fes-zx81-clock
	@$(VERILATOR) --cc --exe --build --top-module zx81_machine_clock -Wall -Wno-UNUSEDSIGNAL \
		--Mdir "$(CURDIR)/build/sim/fes-zx81-clock" \
		cores/fes-zx81/rtl/zx81_machine_clock.v "$(CURDIR)/cores/fes-zx81/sim/clock_tb.cpp"
	@build/sim/fes-zx81-clock/Vzx81_machine_clock
sim-fes-zx81-ay:
	@python3 -m unittest tests.test_zonx_ay tests.test_zonx_firmware -v

.PHONY: sim-fes-z80-timing
sim-fes-z80-timing:
	$(require_local_sim)
	@mkdir -p build/sim/fes-z80-timing
	$(VERILATOR) --cc --exe --build -O2 --top-module fes_z80_ce -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-z80-timing" \
		cores/fes-common/rtl/fes_z80_ce.sv "$(CURDIR)/cores/fes-coleco/sim/cpu_ce_tb.cpp"
	@build/sim/fes-z80-timing/Vfes_z80_ce

.PHONY: sim-fes-z80-pin-trace
sim-fes-z80-pin-trace:
	$(require_local_sim)
	$(PYTHON) scripts/test_fes_z80_pin_trace.py --self-test --verilator "$(VERILATOR)"

.PHONY: sim-fes-z80
sim-fes-z80: sim-fes-z80-pin-trace
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_z80.py --verilator "$(VERILATOR)"

# Original RV32I CPU lane and the fes.riscv package built on it.
.PHONY: sim-fes-riscv build-fes-riscv
sim-fes-riscv:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_riscv.py --verilator "$(VERILATOR)"

build-fes-riscv:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_riscv.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

# Atari 520ST motherboard, shared memory and optional stock-firmware boot.
ST_CORE := cores/fes-atari-st
ST_CPU := cores/fes-common/rtl/fx68k
ST_MFP_WAIT_STATES ?= 4
ST_DIAGNOSTIC := $(CURDIR)/build/diagnostics/fes-atari-st
ST_CPU_RTL := $(ST_CPU)/fx68k.sv $(ST_CPU)/fx68kAlu.sv $(ST_CPU)/uaddrPla.sv
ST_VIDEO_RTL := $(ST_CORE)/rtl/st_native_border.sv $(ST_CORE)/rtl/st_native_low_video.sv $(ST_CORE)/rtl/st_video.sv cores/fes-common/rtl/fes_video_part_direct.v \
	cores/fes-common/rtl/fes_video_part_scanlines.v
ST_MACHINE_RTL := $(ST_CORE)/rtl/st_cpu.sv $(ST_CORE)/rtl/st_machine.sv \
	$(ST_CORE)/expansions/st_probe.sv $(ST_VIDEO_RTL) $(ST_CPU_RTL)
# Upstream FX68K uses unpacked structures with independently assigned fields.
# Keep that preserved source; these suppressions match the qualified simulator.
ST_CPU_FLAGS := -Wno-WIDTH -Wno-TIMESCALEMOD -Wno-MULTIDRIVEN -Wno-INITIALDLY \
	-Wno-ALWCOMBORDER -Wno-UNOPTFLAT -Wno-BLKANDNBLK -Wno-CASEINCOMPLETE -Wno-CASEOVERLAP
ST_IO_RTL := $(ST_CORE)/rtl/st_io.sv $(ST_CORE)/rtl/st_mfp.sv \
	$(ST_CORE)/rtl/st_acia.sv $(ST_CORE)/rtl/st_ikbd.sv $(ST_CORE)/rtl/st_ym2149.sv $(ST_CORE)/rtl/st_ym_filter.sv $(ST_CORE)/rtl/st_ym_mixer.sv $(ST_CORE)/rtl/st_ym_mix_rom.sv \
	$(ST_CORE)/rtl/st_floppy.sv $(ST_CORE)/rtl/st_floppy_writer.sv cores/fes-zx81/expansions/zonx_ay.v

.PHONY: atari-st-sources atari-st-diagnostic sim-fes-atari-st sim-fes-atari-st-machine sim-fes-atari-st-video
atari-st-sources:
	$(PYTHON) $(ST_CORE)/sim/check_sources.py

atari-st-diagnostic: atari-st-sources
	$(PYTHON) $(ST_CORE)/diagnostic/firmware.py --out $(ST_DIAGNOSTIC)

sim-fes-atari-st: sim-fes-atari-st-ym-counter sim-fes-atari-st-ym-filter sim-fes-atari-st-ym-mixer sim-fes-atari-st-machine sim-fes-atari-st-video sim-fes-atari-st-mfp sim-fes-atari-st-mfp-bus sim-fes-atari-st-mfp-iack-timing sim-fes-atari-st-ram-bus sim-fes-atari-st-input-audio sim-fes-atari-st-io sim-fes-atari-st-psg-wait sim-fes-atari-st-floppy sim-fes-atari-st-floppy-write sim-fes-atari-st-media-port sim-fes-atari-st-media-lifecycle sim-fes-computer-interaction sim-fes-atari-st-mailbox-direct sim-fes-atari-st-memory sim-fes-atari-st-video-adapter sim-fes-atari-st-native-video sim-fes-atari-st-cache-write-coherence sim-fes-atari-st-native-border sim-fes-atari-st-shifter-output sim-fes-atari-st-short-line-video sim-fes-atari-st-media sim-fes-atari-st-media-geometry

sim-fes-atari-st-machine: atari-st-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-machine
	@cp $(ST_CPU)/microrom.mem $(ST_CPU)/nanorom.mem build/sim/fes-atari-st-machine/
	$(VERILATOR) --cc --exe --build -O2 --top-module st_sim_top $(ST_CPU_FLAGS) \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-machine" \
		$(ST_CORE)/sim/st_sim_top.sv $(ST_MACHINE_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/machine_tb.cpp" -CFLAGS '-O2'
	@cd build/sim/fes-atari-st-machine && ./Vst_sim_top "$(ST_DIAGNOSTIC)"

sim-fes-atari-st-video:
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-video
	$(VERILATOR) --cc --exe --build -O2 --top-module st_video_sim_top -Wall \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-video" \
		$(ST_CORE)/sim/st_video_sim_top.sv $(ST_VIDEO_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/video_tb.cpp" -CFLAGS '-O2'
	@build/sim/fes-atari-st-video/Vst_video_sim_top

.PHONY: sim-fes-atari-st-mfp sim-fes-atari-st-input-audio sim-fes-atari-st-floppy sim-fes-atari-st-emutos
sim-fes-atari-st-mfp:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build -O2 --top-module st_mfp -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-mfp" \
		$(ST_CORE)/rtl/st_mfp.sv "$(CURDIR)/$(ST_CORE)/sim/mfp_tb.cpp" -CFLAGS '-std=c++17'
	@build/sim/fes-atari-st-mfp/Vst_mfp

sim-fes-atari-st-input-audio:
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-input-audio
	$(VERILATOR) --cc --exe --build -O2 --top-module st_input_audio_sim_top -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-input-audio" \
		$(ST_CORE)/sim/st_input_audio_sim_top.sv $(ST_CORE)/rtl/st_acia.sv \
		$(ST_CORE)/rtl/st_ikbd.sv $(ST_CORE)/rtl/st_ym2149.sv $(ST_CORE)/rtl/st_ym_filter.sv $(ST_CORE)/rtl/st_ym_mixer.sv $(ST_CORE)/rtl/st_ym_mix_rom.sv cores/fes-zx81/expansions/zonx_ay.v \
		"$(CURDIR)/$(ST_CORE)/sim/input_audio_tb.cpp" -CFLAGS '-std=c++17'
	@build/sim/fes-atari-st-input-audio/Vst_input_audio_sim_top

sim-fes-atari-st-floppy:
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-floppy
	$(VERILATOR) --cc --exe --build -O2 --top-module st_floppy -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-floppy" \
		$(ST_CORE)/rtl/st_floppy.sv $(ST_CORE)/rtl/st_floppy_writer.sv "$(CURDIR)/$(ST_CORE)/sim/floppy_tb.cpp" -CFLAGS '-std=c++17'
	@build/sim/fes-atari-st-floppy/Vst_floppy

.PHONY: sim-fes-atari-st-floppy-write sim-fes-computer-interaction sim-fes-atari-st-mailbox-direct sim-fes-atari-st-media-port sim-fes-atari-st-media-lifecycle
sim-fes-atari-st-floppy-write:
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-floppy-write
	$(VERILATOR) --cc --exe --build -O2 --top-module st_floppy -Wall "-GENABLE_WRITE=1'b1" \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-floppy-write" \
		$(ST_CORE)/rtl/st_floppy.sv $(ST_CORE)/rtl/st_floppy_writer.sv \
		"$(CURDIR)/$(ST_CORE)/sim/floppy_write_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-floppy-write/Vst_floppy

sim-fes-computer-interaction:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build --top-module computer_interaction_sim_top -Wall -Wno-DECLFILENAME \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/computer-interaction" \
		cores/fes-common/sim/computer_interaction_sim_top.sv cores/fes-common/rtl/fes_computer_mailbox_direct.v \
		$(ST_CORE)/rtl/st_ikbd.sv $(ST_CORE)/rtl/st_acia.sv \
		"$(CURDIR)/cores/fes-common/sim/computer_interaction_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/computer-interaction/Vcomputer_interaction_sim_top

sim-fes-atari-st-mailbox-direct:
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-mailbox-direct
	$(VERILATOR) --cc --exe --build --top-module mailbox_direct_compare_top -Wall -Wno-DECLFILENAME \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-mailbox-direct" \
		cores/fes-common/sim/mailbox_direct_compare_top.sv \
		cores/fes-common/rtl/fes_computer_mailbox.v cores/fes-common/rtl/fes_computer_mailbox_direct.v \
		"$(CURDIR)/cores/fes-common/sim/mailbox_direct_compare_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-mailbox-direct/Vmailbox_direct_compare_top \
		"$(CURDIR)/cores/fes-common/generated/computer-exchanges.json"

sim-fes-atari-st-media-port:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build --top-module st_media_port -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-media-port" \
		$(ST_CORE)/rtl/st_media_port.sv "$(CURDIR)/$(ST_CORE)/sim/media_port_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-media-port/Vst_media_port

sim-fes-atari-st-media-lifecycle:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build --top-module st_media_lifecycle_sim_top -Wall \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-media-lifecycle" \
		$(ST_CORE)/sim/st_media_lifecycle_sim_top.sv cores/fes-common/rtl/fes_computer_mailbox_direct.v \
		$(ST_CORE)/rtl/st_floppy_writer.sv $(ST_CORE)/rtl/st_media_writer.sv $(ST_CORE)/rtl/st_media_port.sv \
		"$(CURDIR)/$(ST_CORE)/sim/media_lifecycle_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-media-lifecycle/Vst_media_lifecycle_sim_top

sim-fes-atari-st-emutos: atari-st-sources
	$(require_local_sim)
	@test -n "$(EMUTOS_ROM)" || (echo 'Set EMUTOS_ROM to a 192 KiB ST image'; exit 1)
	@mkdir -p build/sim/fes-atari-st-emutos
	@cp $(ST_CPU)/microrom.mem $(ST_CPU)/nanorom.mem build/sim/fes-atari-st-emutos/
	$(VERILATOR) --cc --exe --build -O2 --top-module st_boot_sim_top $(ST_CPU_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-emutos" \
		$(ST_CORE)/sim/st_boot_sim_top.sv $(ST_CORE)/rtl/st_system.sv \
		$(ST_CORE)/rtl/st_cpu.sv $(ST_CORE)/rtl/st_machine.sv $(ST_IO_RTL) \
		$(ST_CORE)/rtl/st_native_border.sv $(ST_CORE)/rtl/st_native_low_video.sv $(ST_CPU_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/boot_tb.cpp" -CFLAGS '-O3 -std=c++17'
	@cd build/sim/fes-atari-st-emutos && ./Vst_boot_sim_top "$(abspath $(EMUTOS_ROM))"

.PHONY: sim-fes-atari-st-memory sim-fes-atari-st-video-adapter sim-fes-atari-st-native-video sim-fes-atari-st-native-border sim-fes-atari-st-media sim-fes-atari-st-media-geometry
.PHONY: sim-fes-atari-st-memory-slots
sim-fes-atari-st-memory-slots:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build -O2 --top-module st_memory_sim_top -Wall \
		"-GPHASE_SLOTS=1'b1" "-GPOSTED_CPU_WRITES=1'b1" "-GSINGLE_RANK_REFRESH=1'b1" "-GSLOT_REFRESH=1'b1" \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-memory-slots" \
		$(ST_CORE)/sim/st_memory_sim_top.sv $(ST_CORE)/rtl/st_memory.sv \
		cores/fes-ramtest/rtl/sdram_addon_port.v "$(CURDIR)/$(ST_CORE)/sim/memory_tb.cpp" \
		-CFLAGS '-O2 -std=c++17 -DST_SLOT_POLICY=1'
	@build/sim/fes-atari-st-memory-slots/Vst_memory_sim_top

sim-fes-atari-st-memory: sim-fes-atari-st-memory-slots
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-memory build/sim/fes-atari-st-memory-legacy-refresh
	$(VERILATOR) --cc --exe --build -O2 --top-module st_memory_sim_top -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-memory" \
		$(ST_CORE)/sim/st_memory_sim_top.sv $(ST_CORE)/rtl/st_memory.sv \
		cores/fes-ramtest/rtl/sdram_addon_port.v "$(CURDIR)/$(ST_CORE)/sim/memory_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-memory/Vst_memory_sim_top
	@mkdir -p build/sim/fes-atari-st-memory-late-completion
	$(VERILATOR) --cc --exe --build -O2 --top-module st_memory_sim_top -Wall -GEARLY_COMPLETION=0 \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-memory-late-completion" \
		$(ST_CORE)/sim/st_memory_sim_top.sv $(ST_CORE)/rtl/st_memory.sv \
		cores/fes-ramtest/rtl/sdram_addon_port.v "$(CURDIR)/$(ST_CORE)/sim/memory_tb.cpp" -CFLAGS '-O2 -std=c++17 -DST_EARLY_COMPLETION=0'
	@build/sim/fes-atari-st-memory-late-completion/Vst_memory_sim_top
	$(VERILATOR) --cc --exe --build -O2 --top-module st_memory_sim_top -Wall \
		-GEARLY_COMPLETION=0 "-GREFRESH_WAIT_CYCLES=14'd16" --Mdir "$(CURDIR)/build/sim/fes-atari-st-memory-legacy-refresh" \
		$(ST_CORE)/sim/st_memory_sim_top.sv $(ST_CORE)/rtl/st_memory.sv \
		cores/fes-ramtest/rtl/sdram_addon_port.v "$(CURDIR)/$(ST_CORE)/sim/memory_tb.cpp" \
		-CFLAGS '-O2 -std=c++17 -DST_REFRESH_WAIT_CYCLES=16 -DST_EARLY_COMPLETION=0'
	@build/sim/fes-atari-st-memory-legacy-refresh/Vst_memory_sim_top
	@mkdir -p build/sim/fes-atari-st-memory-registered-input
	$(VERILATOR) --cc --exe --build -O2 --top-module st_memory_sim_top -Wall "-GREGISTERED_READ_INPUT=1'b1" \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-memory-registered-input" \
		$(ST_CORE)/sim/st_memory_sim_top.sv $(ST_CORE)/rtl/st_memory.sv \
		cores/fes-ramtest/rtl/sdram_addon_port.v "$(CURDIR)/$(ST_CORE)/sim/memory_tb.cpp" \
		-CFLAGS '-O2 -std=c++17 -DST_REGISTERED_READ_INPUT=1'
	@build/sim/fes-atari-st-memory-registered-input/Vst_memory_sim_top

sim-fes-atari-st-video-adapter:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build -O2 --top-module st_video_adapter_sim_top -Wall \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-video-adapter" \
		$(ST_CORE)/sim/st_video_adapter_sim_top.sv $(ST_CORE)/rtl/st_video_adapter.sv $(ST_VIDEO_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/video_adapter_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-video-adapter/Vst_video_adapter_sim_top

.PHONY: sim-fes-atari-st-native-border
sim-fes-atari-st-native-border:
	$(require_local_sim)
	@mkdir -p "$(CURDIR)/build/sim/fes-atari-st-native-border"
	$(VERILATOR) --cc --exe --build -O2 --top-module st_native_border -Wall -GEVENT_BITS=5 \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-native-border" \
		$(ST_CORE)/rtl/st_native_border.sv "$(CURDIR)/$(ST_CORE)/sim/native_border_tb.cpp" \
		-CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-native-border/Vst_native_border

.PHONY: sim-fes-atari-st-short-line-video
sim-fes-atari-st-short-line-video:
	$(require_local_sim)
	@mkdir -p "$(CURDIR)/build/sim/fes-atari-st-short-line-video"
	$(VERILATOR) --cc --exe --build -O2 --top-module st_shifter_output_sim_top -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-short-line-video" \
		$(ST_CORE)/sim/st_shifter_output_sim_top.sv $(ST_CORE)/rtl/st_native_low_video.sv $(ST_CORE)/rtl/st_native_border.sv \
		"$(CURDIR)/$(ST_CORE)/sim/short_line_video_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-short-line-video/Vst_shifter_output_sim_top

.PHONY: sim-fes-atari-st-cache-write-coherence
sim-fes-atari-st-cache-write-coherence: sim-fes-atari-st-native-video
	@build/sim/fes-atari-st-native-video/Vst_native_video_sim_top --cache-write-coherence
	@for plane in 0 1 2 3; do \
		for phase in 221 223 225 227 229 231 233 235; do \
			build/sim/fes-atari-st-native-video/Vst_native_video_sim_top --cache-word-boundary $$plane $$phase || exit $$?; \
		done; \
	done

sim-fes-atari-st-native-video:
	$(require_local_sim)
	@mkdir -p "$(CURDIR)/build/sim/fes-atari-st-native-video"
	$(VERILATOR) --cc --exe --build -O2 --top-module st_native_video_sim_top -Wall \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-native-video" \
		$(ST_CORE)/sim/st_native_video_sim_top.sv $(ST_CORE)/rtl/st_video_adapter.sv $(ST_VIDEO_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/native_video_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-native-video/Vst_native_video_sim_top

sim-fes-atari-st-media:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build -O2 --top-module st_media_sim_top -Wall \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-media" \
		$(ST_CORE)/sim/st_media_sim_top.sv $(ST_CORE)/rtl/st_media_writer.sv \
		cores/fes-common/rtl/fes_computer_mailbox_direct.v "$(CURDIR)/$(ST_CORE)/sim/media_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-media/Vst_media_sim_top

sim-fes-atari-st-media-geometry:
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-media-geometry
	$(VERILATOR) --cc --exe --build -O2 --top-module st_media_sim_top -Wall "-GGEOMETRY=1'b1" \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-media-geometry" \
		$(ST_CORE)/sim/st_media_sim_top.sv $(ST_CORE)/rtl/st_media_writer.sv \
		cores/fes-common/rtl/fes_computer_mailbox_direct.v "$(CURDIR)/$(ST_CORE)/sim/media_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-media-geometry/Vst_media_sim_top geometry

.PHONY: fetch-fes-atari-st-emutos sim-fes-atari-st-io sim-fes-atari-st-emutos-memory build-fes-atari-st
fetch-fes-atari-st-emutos:
	$(PYTHON) scripts/fetch_atari_st_emutos.py

sim-fes-atari-st-io:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build -O2 --top-module st_io_sim_top -Wall \
		-GMFP_WAIT_STATES=$(ST_MFP_WAIT_STATES) --Mdir "$(CURDIR)/build/sim/fes-atari-st-io" \
		$(ST_CORE)/sim/st_io_sim_top.sv $(ST_IO_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/io_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-io/Vst_io_sim_top

.PHONY: sim-fes-atari-st-mfp-bus
sim-fes-atari-st-mfp-bus: atari-st-sources
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-mfp-bus
	@cp $(ST_CPU)/microrom.mem $(ST_CPU)/nanorom.mem build/sim/fes-atari-st-mfp-bus/
	$(VERILATOR) --cc --exe --build -O2 --top-module st_boot_sim_top $(ST_CPU_FLAGS) \
		-Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-mfp-bus" \
		$(ST_CORE)/sim/st_boot_sim_top.sv $(ST_CORE)/rtl/st_system.sv \
		$(ST_CORE)/rtl/st_cpu.sv $(ST_CORE)/rtl/st_machine.sv $(ST_IO_RTL) \
		$(ST_CORE)/rtl/st_native_border.sv $(ST_CORE)/rtl/st_native_low_video.sv $(ST_CPU_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/mfp_bus_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@cd build/sim/fes-atari-st-mfp-bus && ./Vst_boot_sim_top
	$(VERILATOR) --cc --exe --build -O2 --top-module st_io_sim_top -Wall \
		-GMFP_WAIT_STATES=4 --Mdir "$(CURDIR)/build/sim/fes-atari-st-mfp-wait" \
		$(ST_CORE)/sim/st_io_sim_top.sv $(ST_IO_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/mfp_wait_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-mfp-wait/Vst_io_sim_top

EMUTOS_SECONDS ?= 8
EMUTOS_CAPTURE ?= $(CURDIR)/build/sim/fes-atari-st-boot-memory/emutos-sdram
sim-fes-atari-st-emutos-memory: atari-st-sources
	$(require_local_sim)
	@test -n "$(EMUTOS_ROM)" || (echo 'Set EMUTOS_ROM to a 192 KiB ST image'; exit 1)
	@mkdir -p build/sim/fes-atari-st-boot-memory
	@cp $(ST_CPU)/microrom.mem $(ST_CPU)/nanorom.mem build/sim/fes-atari-st-boot-memory/
	$(VERILATOR) --cc --exe --build -O2 --top-module st_boot_memory_sim_top $(ST_CPU_FLAGS) \
		-GMFP_WAIT_STATES=$(ST_MFP_WAIT_STATES) -Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-boot-memory" \
		$(ST_CORE)/sim/st_boot_memory_sim_top.sv $(ST_CORE)/sim/st_memory_sim_top.sv \
		$(ST_CORE)/rtl/st_system.sv $(ST_CORE)/rtl/st_cpu.sv $(ST_CORE)/rtl/st_machine.sv \
		$(ST_CORE)/rtl/st_memory.sv cores/fes-ramtest/rtl/sdram_addon_port.v \
		$(ST_CORE)/rtl/st_video_adapter.sv $(ST_VIDEO_RTL) $(ST_IO_RTL) $(ST_CPU_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/boot_memory_tb.cpp" -CFLAGS '-O3 -std=c++17'
	@cd build/sim/fes-atari-st-boot-memory && ./Vst_boot_memory_sim_top \
		"$(abspath $(EMUTOS_ROM))" "$(EMUTOS_SECONDS)" "$(abspath $(EMUTOS_CAPTURE))"

.PHONY: sim-fes-atari-st-ram-bus sim-fes-atari-st-ram-bus-0 sim-fes-atari-st-ram-bus-1
sim-fes-atari-st-ram-bus: sim-fes-atari-st-ram-bus-0 sim-fes-atari-st-ram-bus-1

sim-fes-atari-st-ram-bus-0 sim-fes-atari-st-ram-bus-1: sim-fes-atari-st-ram-bus-%: atari-st-sources
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-ram-bus-$*
	@cp $(ST_CPU)/microrom.mem $(ST_CPU)/nanorom.mem build/sim/fes-atari-st-ram-bus-$*/
	$(VERILATOR) --cc --exe --build -O2 --top-module st_boot_memory_sim_top $(ST_CPU_FLAGS) \
		-GEARLY_COMPLETION=$* -GMFP_WAIT_STATES=$(ST_MFP_WAIT_STATES) -Icores/fes-common/generated --Mdir "$(CURDIR)/build/sim/fes-atari-st-ram-bus-$*" \
		$(ST_CORE)/sim/st_boot_memory_sim_top.sv $(ST_CORE)/sim/st_memory_sim_top.sv \
		$(ST_CORE)/rtl/st_system.sv $(ST_CORE)/rtl/st_cpu.sv $(ST_CORE)/rtl/st_machine.sv \
		$(ST_CORE)/rtl/st_memory.sv cores/fes-ramtest/rtl/sdram_addon_port.v \
		$(ST_CORE)/rtl/st_video_adapter.sv $(ST_VIDEO_RTL) $(ST_IO_RTL) $(ST_CPU_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/boot_memory_tb.cpp" -CFLAGS '-O3 -std=c++17 -DST_RAM_BUS_PROBE -DST_EARLY_COMPLETION=$*'
	@cd build/sim/fes-atari-st-ram-bus-$* && ./Vst_boot_memory_sim_top

ST_VIDEO_OUTPUT ?= direct
build-fes-atari-st:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_atari_st_oss.py --root "$(CURDIR)" \
		--video-output "$(ST_VIDEO_OUTPUT)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)

.PHONY: build-fes-atari-st-card
ST_SHELL ?= build/fes-atari-st-oss
build-fes-atari-st-card:
	@test -n "$(ST_PACKAGE)" || (echo 'Set ST_PACKAGE to the exact sealed shell package'; exit 1)
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_atari_st_slot_card.py --root "$(CURDIR)" \
		--shell "$(ST_SHELL)" --package "$(ST_PACKAGE)" --slot 1 --card probe \
		$(if $(strip $(CACHE_ROOT)),--cache-root "$(CACHE_ROOT)",) $(if $(GPU_DEVICE),--gpu "$(GPU_DEVICE)",)

.PHONY: build-fes-atari-st-video-part sim-fes-atari-st-video-parts
ST_VIDEO_PART ?= direct
ST_VIDEO_PART_SEED ?= 4
build-fes-atari-st-video-part:
	@test -n "$(ST_PACKAGE)" || (echo 'Set ST_PACKAGE to the exact sealed ST shell package'; exit 1)
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_atari_st_video_part.py --shell "$(ST_SHELL)" --package "$(ST_PACKAGE)" \
		--variant "$(ST_VIDEO_PART)" --seed "$(ST_VIDEO_PART_SEED)" \
		$(if $(strip $(CACHE_ROOT)),--cache-root "$(CACHE_ROOT)",) $(if $(GPU_DEVICE),--gpu "$(GPU_DEVICE)",)

sim-fes-atari-st-video-parts:
	$(require_local_sim)
	$(PYTHON) scripts/sim_atari_st_video_parts.py

.PHONY: sim-fes-atari-st-ym-counter
sim-fes-atari-st-ym-counter:
	$(require_local_sim)
	@mkdir -p build/sim/fes-atari-st-ym-counter
	@$(VERILATOR) --cc --exe --build -O2 --top-module st_ym_counter_sim_top -Wall -Wno-UNUSEDSIGNAL \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-ym-counter" \
		$(ST_CORE)/sim/st_ym_counter_sim_top.sv cores/fes-zx81/expansions/zonx_ay.v \
		"$(CURDIR)/$(ST_CORE)/sim/ym_counter_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-ym-counter/Vst_ym_counter_sim_top

.PHONY: sim-fes-atari-st-ym-mixer
sim-fes-atari-st-ym-mixer:
	$(require_local_sim)
	$(PYTHON) scripts/generate_st_ym_mix_rom.py --check
	@mkdir -p build/sim/fes-atari-st-ym-mixer
	@$(VERILATOR) --cc --exe --build -O2 --top-module st_ym_mixer -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-ym-mixer" \
		$(ST_CORE)/rtl/st_ym_mixer.sv $(ST_CORE)/rtl/st_ym_mix_rom.sv \
		"$(CURDIR)/$(ST_CORE)/sim/ym_mixer_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-ym-mixer/Vst_ym_mixer

.PHONY: sim-fes-atari-st-ym-filter
sim-fes-atari-st-ym-filter:
	$(require_local_sim)
	@set -e; for profile in scaled nominal; do \
		if [ "$$profile" = scaled ]; then chip=500000; fabric=8000000; else chip=2005311; fabric=52224000; fi; \
		mkdir -p build/sim/fes-atari-st-ym-filter-$$profile; \
		$(VERILATOR) --cc --exe --build -O2 --top-module st_ym_filter -Wall \
			-GCHIP_CLOCK_HZ=$$chip --Mdir "$(CURDIR)/build/sim/fes-atari-st-ym-filter-$$profile" \
			$(ST_CORE)/rtl/st_ym_filter.sv "$(CURDIR)/$(ST_CORE)/sim/ym_filter_tb.cpp" \
			-CFLAGS "-O2 -std=c++17 -DST_YM_FILTER_TEST_CHIP_HZ=$$chip -DST_YM_FILTER_TEST_SYS_HZ=$$fabric"; \
		build/sim/fes-atari-st-ym-filter-$$profile/Vst_ym_filter; \
	done

.PHONY: sim-fes-atari-st-shifter-output
sim-fes-atari-st-shifter-output:
	$(require_local_sim)
	@mkdir -p "$(CURDIR)/build/sim/fes-atari-st-shifter-output"
	$(VERILATOR) --cc --exe --build -O2 --top-module st_shifter_output_sim_top -Wall \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-shifter-output" \
		$(ST_CORE)/sim/st_shifter_output_sim_top.sv $(ST_CORE)/rtl/st_native_low_video.sv \
		$(ST_CORE)/rtl/st_native_border.sv "$(CURDIR)/$(ST_CORE)/sim/shifter_output_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-shifter-output/Vst_shifter_output_sim_top

.PHONY: sim-fes-atari-st-psg-wait
sim-fes-atari-st-psg-wait:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build -O2 --top-module st_io_sim_top -Wall \
		-GPSG_WAIT_STATES=4 --Mdir "$(CURDIR)/build/sim/fes-atari-st-psg-wait" \
		$(ST_CORE)/sim/st_io_sim_top.sv $(ST_IO_RTL) \
		"$(CURDIR)/$(ST_CORE)/sim/psg_wait_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-psg-wait/Vst_io_sim_top

.PHONY: sim-fes-atari-st-mfp-iack-timing
sim-fes-atari-st-mfp-iack-timing:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build -O2 --top-module st_mfp_iack_timing \
		--Mdir "$(CURDIR)/build/sim/fes-atari-st-mfp-iack-timing" \
		$(ST_CORE)/rtl/st_system.sv \
		"$(CURDIR)/$(ST_CORE)/sim/mfp_iack_timing_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-atari-st-mfp-iack-timing/Vst_mfp_iack_timing

.PHONY: sim-fes-c64-sprite-video
sim-fes-c64-sprite-video:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sprite-video
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_video_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sprite-video" \
		cores/fes-c64/sim/video_sim_top.sv cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv \
		cores/fes-c64/rtl/c64_frame_store.sv \
		cores/fes-c64/rtl/c64_ram.v cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/sprite_video_tb.cpp"
	@build/sim/fes-c64-sprite-video/Vc64_video_sim_top

.PHONY: sim-fes-c64-border
sim-fes-c64-border:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-border
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_video_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-border" \
		cores/fes-c64/sim/video_sim_top.sv cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv \
		cores/fes-c64/rtl/c64_frame_store.sv \
		cores/fes-c64/rtl/c64_ram.v cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/border_tb.cpp"
	@build/sim/fes-c64-border/Vc64_video_sim_top

.PHONY: sim-fes-c64-readback
sim-fes-c64-readback:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-readback
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_video_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-readback" \
		cores/fes-c64/sim/video_sim_top.sv cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv \
		cores/fes-c64/rtl/c64_frame_store.sv \
		cores/fes-c64/rtl/c64_ram.v cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/readback_tb.cpp"
	@build/sim/fes-c64-readback/Vc64_video_sim_top

.PHONY: sim-fes-c64-collisions
sim-fes-c64-collisions:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-collisions
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_vic_collisions $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-collisions" cores/fes-c64/rtl/c64_vic_collisions.sv \
		"$(CURDIR)/cores/fes-c64/sim/collision_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-c64-collisions/Vc64_vic_collisions

.PHONY: sim-fes-c64-collision-cpu
sim-fes-c64-collision-cpu:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-collision-cpu
	$(PYTHON) cores/fes-c64/sim/collision_cpu_program.py --output build/sim/fes-c64-collision-cpu/program.bin
	$(VERILATOR) --cc --exe --build -O2 --top-module ready_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-collision-cpu" \
		cores/fes-c64/sim/ready_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/collision_cpu_tb.cpp"
	@build/sim/fes-c64-collision-cpu/Vready_sim_top "$(CURDIR)/build/sim/fes-c64-collision-cpu/firmware.hex" "$(CURDIR)/build/sim/fes-c64-collision-cpu/program.bin"

.PHONY: sim-fes-c64-border-position
sim-fes-c64-border-position:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-border-position
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_video_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-border-position" \
		cores/fes-c64/sim/video_sim_top.sv cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv \
		cores/fes-c64/rtl/c64_frame_store.sv \
		cores/fes-c64/rtl/c64_ram.v cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/border_position_tb.cpp"
	@build/sim/fes-c64-border-position/Vc64_video_sim_top

.PHONY: sim-fes-c64-scroll
sim-fes-c64-scroll:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-scroll
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_video_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-scroll" \
		cores/fes-c64/sim/video_sim_top.sv cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv \
		cores/fes-c64/rtl/c64_frame_store.sv \
		cores/fes-c64/rtl/c64_ram.v cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/scroll_tb.cpp"
	@build/sim/fes-c64-scroll/Vc64_video_sim_top

.PHONY: sim-fes-c64-graphics-modes
sim-fes-c64-graphics-modes:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-graphics-modes
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_video_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-graphics-modes" \
		cores/fes-c64/sim/video_sim_top.sv cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv \
		cores/fes-c64/rtl/c64_frame_store.sv \
		cores/fes-c64/rtl/c64_ram.v cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/graphics_modes_tb.cpp"
	@build/sim/fes-c64-graphics-modes/Vc64_video_sim_top

.PHONY: sim-fes-c64-graphics
sim-fes-c64-graphics:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-graphics
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_vic_graphics_pixels $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-graphics" cores/fes-c64/rtl/c64_vic_graphics_pixels.sv \
		"$(CURDIR)/cores/fes-c64/sim/graphics_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-c64-graphics/Vc64_vic_graphics_pixels

.PHONY: sim-fes-c64-open-graphics
sim-fes-c64-open-graphics:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-open-graphics
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_video_sim_top $(C64_VERILATOR_FLAGS) --public-flat-rw \
		--Mdir "$(CURDIR)/build/sim/fes-c64-open-graphics" \
		cores/fes-c64/sim/video_sim_top.sv cores/fes-c64/rtl/c64_vic.v cores/fes-c64/rtl/c64_vic_graphics_pixels.sv cores/fes-c64/rtl/c64_vic_sprite_pixels.sv cores/fes-c64/rtl/c64_vic_collisions.sv cores/fes-c64/rtl/c64_vic_fetch.sv cores/fes-c64/rtl/c64_vic_sprite_fetch.sv \
		cores/fes-c64/rtl/c64_frame_store.sv \
		cores/fes-c64/rtl/c64_ram.v cores/fes-c64/rtl/c64_color_ram.v \
		"$(CURDIR)/cores/fes-c64/sim/open_graphics_tb.cpp"
	@build/sim/fes-c64-open-graphics/Vc64_video_sim_top

.PHONY: sim-fes-c64-sid-audio
sim-fes-c64-sid-audio:
	$(require_local_sim)
	@for prepared in 0 1; do \
		mkdir -p "$(CURDIR)/build/sim/fes-c64-sid-audio-$$prepared"; \
		$(VERILATOR) --cc --exe --build -j 2 -O2 $(C64_VERILATOR_FLAGS) \
			-CFLAGS "-std=c++17 -O3" -MAKEFLAGS "OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3" \
			--top-module sid_audio_sim_top -GPREPARED=$$prepared \
			--Mdir "$(CURDIR)/build/sim/fes-c64-sid-audio-$$prepared" \
			cores/fes-c64/sim/sid_audio_sim_top.sv \
			cores/fes-c64/rtl/c64_sid.v cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv \
			cores/fes-c64/rtl/c64_sid_envelope.sv $(C64_SID_AUDIO_SOURCES) \
			"$(CURDIR)/cores/fes-c64/sim/sid_audio_tb.cpp" || exit $$?; \
		build/sim/fes-c64-sid-audio-$$prepared/Vsid_audio_sim_top || exit $$?; \
	done


.PHONY: sim-fes-c64-sid
sim-fes-c64-sid: sim-fes-c64-sid-audio sim-fes-c64-sid-envelope-prepared sim-fes-c64-sid-prepared sim-fes-c64-sid-envelope sim-fes-c64-sid-oscillator sim-fes-c64-sid-osc3 sim-fes-c64-sid-coupling sim-fes-c64-sid-phase sim-fes-c64-sid-sequenced sim-fes-c64-sid-sequenced-osc3 sim-fes-c64-sid-sequenced-mix sim-fes-c64-sid-svf
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_sid_sim_top $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid" \
		cores/fes-c64/sim/sid_sim_top.sv cores/fes-c64/rtl/c64_sid.v cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv cores/fes-c64/rtl/c64_sid_envelope.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_tb.cpp"
	@build/sim/fes-c64-sid/Vc64_sid_sim_top

.PHONY: sim-fes-c64-sid-oscillator sim-fes-c64-sid-osc3
sim-fes-c64-sid-oscillator:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-oscillator
	$(VERILATOR) --cc --exe --build -O2 --top-module oscillator_sim_top $(C64_VERILATOR_FLAGS) \
		-CFLAGS "-O3" -MAKEFLAGS "OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-oscillator" \
		cores/fes-c64/sim/sid_oscillator_sim_top.sv cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_oscillator_tb.cpp"
	@build/sim/fes-c64-sid-oscillator/Voscillator_sim_top

sim-fes-c64-sid-osc3:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-osc3
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_sid_sim_top $(C64_VERILATOR_FLAGS) \
		-CFLAGS "-O3" -MAKEFLAGS "OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-osc3" \
		cores/fes-c64/sim/sid_sim_top.sv cores/fes-c64/rtl/c64_sid.v \
		cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv cores/fes-c64/rtl/c64_sid_envelope.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_oscillator_bus_tb.cpp"
	@build/sim/fes-c64-sid-osc3/Vc64_sid_sim_top

.PHONY: sim-fes-c64-sid-coupling
sim-fes-c64-sid-coupling:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-coupling
	$(VERILATOR) --cc --exe --build -O2 --top-module sid_coupling_sim_top $(C64_VERILATOR_FLAGS) \
		-CFLAGS "-O3" -MAKEFLAGS "OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-coupling" \
		cores/fes-c64/sim/sid_coupling_sim_top.sv cores/fes-c64/rtl/c64_sid.v \
		cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv cores/fes-c64/rtl/c64_sid_envelope.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_coupling_tb.cpp"
	@build/sim/fes-c64-sid-coupling/Vsid_coupling_sim_top

.PHONY: sim-fes-c64-sid-phase
sim-fes-c64-sid-phase:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-phase
	$(VERILATOR) --cc --exe --build -O2 --top-module sid_coupling_sim_top -GPREPARE_PHASE=1 $(C64_VERILATOR_FLAGS) -CFLAGS "-DC64_PREPARED_PHASE" \
		-CFLAGS "-O3" -MAKEFLAGS "OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-phase" \
		cores/fes-c64/sim/sid_coupling_sim_top.sv cores/fes-c64/rtl/c64_sid.v \
		cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv cores/fes-c64/rtl/c64_sid_envelope.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_coupling_tb.cpp"
	@build/sim/fes-c64-sid-phase/Vsid_coupling_sim_top


.PHONY: sim-fes-c64-sid-svf
sim-fes-c64-sid-svf:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-svf
	$(VERILATOR) --cc --exe --build -O2 --top-module sid_svf_sim_top $(C64_VERILATOR_FLAGS) \
		-CFLAGS "-O3" -MAKEFLAGS "OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-svf" \
		cores/fes-c64/sim/sid_svf_sim_top.sv cores/fes-c64/rtl/c64_sid_svf.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_svf_tb.cpp"
	@build/sim/fes-c64-sid-svf/Vsid_svf_sim_top

.PHONY: sim-fes-c64-sid-sequenced sim-fes-c64-sid-sequenced-osc3 sim-fes-c64-sid-sequenced-mix
# Sequenced voices: the same pinned reSID coupling, OSC3 and mix fixtures with
# each voice's waveform stage one SYS edge after Phi (the motherboard build).
sim-fes-c64-sid-sequenced:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-sequenced
	$(VERILATOR) --cc --exe --build -O2 --top-module sid_coupling_sim_top -GPREPARE_PHASE=1 -GSEQUENCE_VOICES=1 $(C64_VERILATOR_FLAGS) \
		-CFLAGS "-DC64_PREPARED_PHASE -DC64_SEQUENCED_VOICES" \
		-CFLAGS "-O3" -MAKEFLAGS "OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-sequenced" \
		cores/fes-c64/sim/sid_coupling_sim_top.sv cores/fes-c64/rtl/c64_sid.v \
		cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv cores/fes-c64/rtl/c64_sid_envelope.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_coupling_tb.cpp"
	@build/sim/fes-c64-sid-sequenced/Vsid_coupling_sim_top

sim-fes-c64-sid-sequenced-osc3:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-sequenced-osc3
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_sid_sim_top -GPREPARE_PHASE=1 -GPREPARE_ENVELOPE=1 -GSEQUENCE_VOICES=1 $(C64_VERILATOR_FLAGS) \
		-CFLAGS "-DC64_SEQUENCED_VOICES -O3" -MAKEFLAGS "OPT_FAST=-O3 OPT_SLOW=-O3 OPT_GLOBAL=-O3" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-sequenced-osc3" \
		cores/fes-c64/sim/sid_sim_top.sv cores/fes-c64/rtl/c64_sid.v \
		cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv cores/fes-c64/rtl/c64_sid_envelope.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_oscillator_bus_tb.cpp"
	@build/sim/fes-c64-sid-sequenced-osc3/Vc64_sid_sim_top

sim-fes-c64-sid-sequenced-mix:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-sequenced-mix
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_sid_sim_top -GPREPARE_PHASE=1 -GPREPARE_ENVELOPE=1 -GSEQUENCE_VOICES=1 $(C64_VERILATOR_FLAGS) \
		-CFLAGS "-DC64_SEQUENCED_VOICES" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-sequenced-mix" \
		cores/fes-c64/sim/sid_sim_top.sv cores/fes-c64/rtl/c64_sid.v cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv cores/fes-c64/rtl/c64_sid_envelope.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_tb.cpp"
	@build/sim/fes-c64-sid-sequenced-mix/Vc64_sid_sim_top

.PHONY: sim-fes-c64-sid-envelope
sim-fes-c64-sid-envelope:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-envelope
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_sid_envelope $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-envelope" \
		cores/fes-c64/rtl/c64_sid_envelope.sv "$(CURDIR)/cores/fes-c64/sim/sid_envelope_tb.cpp"
	@build/sim/fes-c64-sid-envelope/Vc64_sid_envelope

.PHONY: sim-fes-c64-sid-envelope-prepared sim-fes-c64-sid-prepared
sim-fes-c64-sid-envelope-prepared:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-envelope-prepared
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_sid_envelope -GPREPARE_STATE=1 $(C64_VERILATOR_FLAGS) -CFLAGS "-DC64_PREPARED_ENVELOPE" \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-envelope-prepared" \
		cores/fes-c64/rtl/c64_sid_envelope.sv "$(CURDIR)/cores/fes-c64/sim/sid_envelope_tb.cpp"
	@build/sim/fes-c64-sid-envelope-prepared/Vc64_sid_envelope

sim-fes-c64-sid-prepared:
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-sid-prepared
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_sid_sim_top -GPREPARE_PHASE=1 -GPREPARE_ENVELOPE=1 $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sid-prepared" \
		cores/fes-c64/sim/sid_sim_top.sv cores/fes-c64/rtl/c64_sid.v cores/fes-c64/rtl/c64_sid_oscillator.sv cores/fes-c64/rtl/c64_sid_wave_tables.sv cores/fes-c64/rtl/c64_sid_envelope.sv \
		"$(CURDIR)/cores/fes-c64/sim/sid_tb.cpp"
	@build/sim/fes-c64-sid-prepared/Vc64_sid_sim_top


.PHONY: sim-fes-c64-sprite-pal
sim-fes-c64-sprite-pal:
	$(require_local_sim)
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_vic_sprite_pixels $(C64_VERILATOR_FLAGS) -GROW_DATA_DOT=5 \
		--Mdir "$(CURDIR)/build/sim/fes-c64-sprite-pal" cores/fes-c64/rtl/c64_vic_sprite_pixels.sv \
		"$(CURDIR)/cores/fes-c64/sim/sprite_pal_tb.cpp" -CFLAGS '-O2 -std=c++17'
	@build/sim/fes-c64-sprite-pal/Vc64_vic_sprite_pixels

.PHONY: sim-fes-pcxt
sim-fes-pcxt:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pcxt.py --verilator "$(VERILATOR)"

.PHONY: sim-fes-pcxt-fdc sim-fes-pcxt-media sim-fes-pcxt-mailbox sim-fes-pcxt-dos
sim-fes-pcxt-fdc:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pcxt_fdc.py --verilator "$(VERILATOR)"

sim-fes-pcxt-media:
	$(require_local_sim)
	VERILATOR="$(VERILATOR)" $(PYTHON) scripts/sim_fes_pcxt_media.py
	VERILATOR="$(VERILATOR)" $(PYTHON) scripts/sim_fes_pcxt_media_diag.py

sim-fes-pcxt-mailbox:
	$(require_local_sim)
	VERILATOR="$(VERILATOR)" $(PYTHON) scripts/sim_fes_pcxt_mailbox.py

sim-fes-pcxt-dos:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pcxt_dos.py --download --verilator "$(VERILATOR)"

.PHONY: sim-fes-pc386 sim-fes-pc386-boot sim-fes-pc386-fesenv sim-fes-pc386-sound sim-fes-pc386-vga sim-fes-pc386-system synth-fes-pc386 build-fes-pc386-quartus
sim-fes-pc386:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pc386.py --suite devices --verilator "$(VERILATOR)"
	$(PYTHON) scripts/sim_fes_pc386.py --suite storage --verilator "$(VERILATOR)"
	$(PYTHON) scripts/sim_fes_pc386.py --suite audio --verilator "$(VERILATOR)"
	$(PYTHON) scripts/sim_fes_pc386.py --suite opl2 --verilator "$(VERILATOR)"
	$(PYTHON) scripts/sim_fes_pc386.py --suite benches --verilator "$(VERILATOR)"

sim-fes-pc386-boot:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pc386.py --suite boot --download --verilator "$(VERILATOR)"

sim-fes-pc386-fesenv:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pc386.py --suite boot --image games --type 'set|' \
		--expect 'BLASTER=A220 I7 D1 T3' --limit-seconds 10 --download --verilator "$(VERILATOR)"

sim-fes-pc386-sound:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pc386.py --suite sound --download --verilator "$(VERILATOR)"

sim-fes-pc386-vga:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pc386.py --suite vga --download --verilator "$(VERILATOR)"

sim-fes-pc386-system:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pc386.py --suite system --download --verilator "$(VERILATOR)"

synth-fes-pc386:
	@test -n "$(CACHE_ROOT)" || { echo 'Set CACHE_ROOT to the authenticated compiler cache'; exit 1; }
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/synth_fes_pc386.py --cache-root "$(CACHE_ROOT)"

build-fes-pc386-quartus:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_pc386_quartus.py --root "$(CURDIR)"$(if $(strip $(CACHE_ROOT)), --cache-root "$(CACHE_ROOT)",)$(if $(strip $(SEED)), --seed "$(SEED)",)

.PHONY: synth-fes-pcxt-isa
synth-fes-pcxt-isa:
	@test -n "$(CACHE_ROOT)" || { echo 'Set CACHE_ROOT to the authenticated compiler cache'; exit 1; }
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/synth_fes_pcxt.py --cache-root "$(CACHE_ROOT)"

# Quartus diagnostic package; OSS tools remain available for advisory probes.
PCXT_GPU_ROUTER ?= HIP
.PHONY: build-fes-pcxt toolchain-fes-pcxt
toolchain-fes-pcxt:
	@$(FES_SHARED_MAKE_ENV)$(FES_TOOLCHAIN_CACHE_ENV) FES_TOOLCHAIN_LOCKFILE="$(CURDIR)/cores/fes-pcxt/toolchain.lock" $(if $(strip $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)),,FES_TOOLCHAIN_ROOT="$(CURDIR)/build/toolchain/fes-pcxt") \
	FES_TOOLCHAIN_GPU_ROUTER="$(PCXT_GPU_ROUTER)" \
	FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' scripts/bootstrap.sh

build-fes-pcxt:
	$(SHARED_MAKE_ENV)$(PYTHON) scripts/build_fes_pcxt_quartus.py$(if $(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE), --cache-root "$(FES_TOOLCHAIN_CACHE_ROOT_EFFECTIVE)",) $(PCXT_BUILD_ARGS)

.PHONY: sim-fes-c64-session
sim-fes-c64-session:
	$(require_local_sim)
	$(PYTHON) -m scripts.sim_fes_c64_session --verilator "$(VERILATOR)"

# PR-tier smoke simulations (scripts/ci_simulations.py SMOKE_TARGETS): each
# proves boot or a few frames in a couple of minutes on a CI runner. The full
# targets above run in the nightly/manual regression; pushes use smoke.
.PHONY: sim-fes-zx81-smoke sim-fes-c64-smoke sim-fes-atari-st-smoke sim-fes-spectrum-smoke sim-fes-pcxt-smoke
sim-fes-zx81-smoke: sim-fes-zx81-rom sim-fes-zx81-machine

sim-fes-c64-smoke: c64-diagnostic
	$(require_local_sim)
	@mkdir -p build/sim/fes-c64-machine
	$(VERILATOR) --cc --exe --build -O2 --top-module c64_sim_top $(C64_VERILATOR_FLAGS) \
		--Mdir "$(CURDIR)/build/sim/fes-c64-machine" \
		cores/fes-c64/sim/c64_sim_top.sv $(C64_RTL) \
		"$(CURDIR)/cores/fes-c64/sim/machine_tb.cpp"
	$(PYTHON) cores/fes-c64/sim/diagnostic_cases.py --case positive \
		--model build/sim/fes-c64-machine/Vc64_sim_top --diagnostic-dir $(C64_DIAGNOSTIC)

sim-fes-atari-st-smoke: sim-fes-atari-st-machine

sim-fes-spectrum-smoke:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_spectrum.py --verilator "$(VERILATOR)" --model 48 --smoke

sim-fes-pcxt-smoke:
	$(require_local_sim)
	$(PYTHON) scripts/sim_fes_pcxt.py --suite system --verilator "$(VERILATOR)"
