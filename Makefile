# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

SHELL := /bin/bash

CXX ?= c++
AR ?= ar
NM ?= nm
CXXFILT ?= c++filt
TARGET_CXX ?= arm-none-linux-gnueabihf-g++
TARGET_AR ?= arm-none-linux-gnueabihf-ar

BUILD_DIR ?= build
ARCHIVE := $(BUILD_DIR)/libmister-runtime.a
DAEMON := $(BUILD_DIR)/mister-runtime
VERSION_INPUT := $(BUILD_DIR)/.mister-runtime-version

VERSION_DIRTY = $(shell test -z "$$(git status --porcelain --untracked-files=normal)" || printf '%s' -dirty)
MISTER_RUNTIME_VERSION ?= git-$(shell git rev-parse --short=12 HEAD)$(VERSION_DIRTY)

CPPFLAGS := -D_FILE_OFFSET_BITS=64 -Iinclude -Isrc -isystem third_party/toml11/include
TEST_CPPFLAGS := $(CPPFLAGS) -Itests/support
CXXFLAGS ?= -std=c++14 -Wall -Wextra -Werror -pthread -MMD -MP

# GNU ar accepts -D (deterministic). BSD/Apple ar does not; ZERO_AR_DATE=1
# already zeros timestamps on those implementations.
ifeq ($(shell $(AR) --help 2>&1 | grep -c -- '-D'),0)
AR_CREATE_FLAGS := rcs
else
AR_CREATE_FLAGS := rcsD
endif

# GNU ld uses --whole-archive so omitted native members fail at link.
# Apple ld uses -force_load for the same archive closure.
ifeq ($(shell $(CXX) --version 2>/dev/null | grep -c 'Apple clang'),0)
DAEMON_ARCHIVE_LINK = -Wl,--whole-archive $(ARCHIVE) -Wl,--no-whole-archive
else
DAEMON_ARCHIVE_LINK = -Wl,-force_load,$(ARCHIVE)
endif

LIB_SOURCES := \
	src/runtime.cpp \
	src/profile.cpp \
	src/native/artifacts.cpp \
	src/native/core_package.cpp \
	src/native/core_data.cpp \
	src/native/core_driver.cpp \
	src/native/fes_gp.cpp \
	src/native/core_loader.cpp \
	src/native/diagnostic.cpp \
	src/native/sha256.cpp \
	src/native/input.cpp \
	src/native/video_recipe.cpp \
	src/native/video.cpp \
	src/native/hardware.cpp \
	src/native/linux/fpga_manager.cpp \
	src/native/linux/framebuffer.cpp \
	src/native/linux/i2c.cpp \
	src/native/linux/mmio.cpp \
	src/native/linux/spi.cpp \
	src/linux/production_hardware.cpp
LIB_OBJECTS := $(patsubst %.cpp,$(BUILD_DIR)/%.o,$(LIB_SOURCES)) \
	$(BUILD_DIR)/src/native/linux/linux_input.o
DAEMON_SOURCES := \
	src/daemon/json.cpp \
	src/daemon/protocol.cpp \
	src/daemon/controller.cpp \
	src/daemon/server.cpp \
	src/daemon/main.cpp \
	src/linux/stderr_log.cpp
DAEMON_OBJECTS := $(patsubst %.cpp,$(BUILD_DIR)/%.o,$(DAEMON_SOURCES))
DEPENDENCIES := $(LIB_OBJECTS:.o=.d) $(DAEMON_OBJECTS:.o=.d)

TEST_BINS := \
	$(BUILD_DIR)/tests/unit/profile_test \
	$(BUILD_DIR)/tests/unit/runtime_test \
	$(BUILD_DIR)/tests/unit/diagnostic_test \
	$(BUILD_DIR)/tests/unit/artifacts_test \
	$(BUILD_DIR)/tests/unit/core_package_test \
	$(BUILD_DIR)/tests/unit/core_data_test \
	$(BUILD_DIR)/tests/unit/fes_gp_test \
	$(BUILD_DIR)/tests/unit/native_hardware_test \
	$(BUILD_DIR)/tests/unit/core_loader_test \
	$(BUILD_DIR)/tests/unit/fpga_manager_test \
	$(BUILD_DIR)/tests/unit/mmio_test \
	$(BUILD_DIR)/tests/unit/off_t_test \
	$(BUILD_DIR)/tests/unit/spi_test \
	$(BUILD_DIR)/tests/unit/video_recipe_test \
	$(BUILD_DIR)/tests/unit/adv7513_test \
	$(BUILD_DIR)/tests/unit/i2c_test \
	$(BUILD_DIR)/tests/unit/input_test \
	$(BUILD_DIR)/tests/unit/video_test \
	$(BUILD_DIR)/tests/unit/framebuffer_test \
	$(BUILD_DIR)/tests/unit/protocol_test \
	$(BUILD_DIR)/tests/integration/daemon_server_test
TEST_HEADERS := $(wildcard \
	include/libmister-runtime/*.h \
	src/*.hpp \
	src/native/*.hpp \
	src/native/generated/*.hpp \
	src/native/linux/*.hpp \
	src/daemon/*.hpp \
	src/linux/*.hpp \
	third_party/toml11/include/*.hpp \
	third_party/toml11/include/toml/*.hpp \
	tests/support/*.hpp)

.PHONY: all clean test run-tests incremental-build-test version-build-test \
	force-version sanitize tsan archive-audit active-tree-test \
	profile-provenance-test hardware-support-truth-test target

all: $(ARCHIVE) $(DAEMON)

$(BUILD_DIR)/src/daemon/main.o: CPPFLAGS += \
	-DMISTER_RUNTIME_VERSION=\"$(MISTER_RUNTIME_VERSION)\"
$(BUILD_DIR)/src/daemon/main.o: $(VERSION_INPUT)

$(VERSION_INPUT): force-version
	@mkdir -p "$(dir $@)"
	@temporary="$@.tmp"; \
	printf '%s\n' '$(MISTER_RUNTIME_VERSION)' >"$$temporary"; \
	if ! cmp -s "$$temporary" "$@"; then mv -f "$$temporary" "$@"; \
	else rm -f -- "$$temporary"; fi

$(BUILD_DIR)/%.o: %.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) -c "$<" -o "$@"

$(BUILD_DIR)/src/native/linux/linux_input.o: src/native/linux/input.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) -c "$<" -o "$@"

$(ARCHIVE): $(LIB_OBJECTS)
	@mkdir -p "$(dir $@)"
	ZERO_AR_DATE=1 $(AR) $(AR_CREATE_FLAGS) "$@" $(LIB_OBJECTS)

$(DAEMON): $(DAEMON_OBJECTS) $(ARCHIVE)
	$(CXX) $(CXXFLAGS) $(DAEMON_OBJECTS) \
		$(DAEMON_ARCHIVE_LINK) \
		$(LDFLAGS) $(LDLIBS) -o "$@"

$(BUILD_DIR)/tests/unit/profile_test: tests/unit/profile_test.cpp \
		tests/support/test_profiles.hpp src/profile.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/profile_test.cpp \
		src/profile.cpp -o "$@"

$(BUILD_DIR)/tests/unit/runtime_test: tests/unit/runtime_test.cpp \
		tests/support/fake_hardware.cpp tests/support/capture_log.cpp \
		src/profile.cpp src/runtime.cpp src/native/diagnostic.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/runtime_test.cpp \
		tests/support/fake_hardware.cpp tests/support/capture_log.cpp \
		src/profile.cpp src/runtime.cpp src/native/diagnostic.cpp -o "$@"

$(BUILD_DIR)/tests/unit/diagnostic_test: tests/unit/diagnostic_test.cpp \
		src/native/diagnostic.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/diagnostic_test.cpp \
		src/native/diagnostic.cpp -o "$@"

$(BUILD_DIR)/tests/unit/artifacts_test: tests/unit/artifacts_test.cpp \
		src/native/artifacts.cpp src/native/diagnostic.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/artifacts_test.cpp \
		src/native/artifacts.cpp src/native/diagnostic.cpp -o "$@"

$(BUILD_DIR)/tests/unit/core_package_test: tests/unit/core_package_test.cpp \
		src/native/core_package.cpp src/native/sha256.cpp src/native/artifacts.cpp \
		src/native/diagnostic.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/core_package_test.cpp \
		src/native/core_package.cpp src/native/sha256.cpp \
		src/native/artifacts.cpp src/native/diagnostic.cpp -o "$@"

$(BUILD_DIR)/tests/unit/core_data_test: tests/unit/core_data_test.cpp src/native/core_data.cpp src/native/sha256.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/core_data_test.cpp src/native/core_data.cpp src/native/sha256.cpp -o "$@"

$(BUILD_DIR)/tests/unit/fes_gp_test: tests/unit/fes_gp_test.cpp \
		tests/support/fake_mmio.cpp src/native/fes_gp.cpp src/native/artifacts.cpp src/native/diagnostic.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/fes_gp_test.cpp \
		tests/support/fake_mmio.cpp src/native/fes_gp.cpp src/native/artifacts.cpp src/native/diagnostic.cpp -o "$@"

$(BUILD_DIR)/tests/unit/core_loader_test: tests/unit/core_loader_test.cpp \
		tests/support/fake_spi.cpp src/native/artifacts.cpp src/native/core_loader.cpp \
		src/native/diagnostic.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/core_loader_test.cpp \
		tests/support/fake_spi.cpp src/native/artifacts.cpp \
		src/native/core_loader.cpp src/native/diagnostic.cpp -o "$@"

$(BUILD_DIR)/tests/unit/spi_test: tests/unit/spi_test.cpp \
		tests/support/fake_mmio.cpp src/native/linux/spi.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/spi_test.cpp \
		tests/support/fake_mmio.cpp src/native/linux/spi.cpp -o "$@"

$(BUILD_DIR)/tests/unit/video_recipe_test: tests/unit/video_recipe_test.cpp \
		src/native/video_recipe.hpp src/native/video_recipe.cpp \
		src/native/adv7513.hpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/video_recipe_test.cpp \
		src/native/video_recipe.cpp -o "$@"

$(BUILD_DIR)/tests/unit/adv7513_test: tests/unit/adv7513_test.cpp \
		src/native/adv7513.hpp src/native/video_recipe.hpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/adv7513_test.cpp -o "$@"

$(BUILD_DIR)/tests/unit/i2c_test: tests/unit/i2c_test.cpp \
		src/native/linux/i2c.hpp src/native/linux/i2c.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) -DMISTER_RUNTIME_TESTING $(CXXFLAGS) \
		tests/unit/i2c_test.cpp src/native/linux/i2c.cpp -o "$@"

$(BUILD_DIR)/tests/unit/input_test: tests/unit/input_test.cpp \
		tests/support/fake_input.cpp src/native/input.cpp \
		src/native/linux/input.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) -DMISTER_RUNTIME_TESTING $(CXXFLAGS) \
		tests/unit/input_test.cpp tests/support/fake_input.cpp \
		src/native/input.cpp src/native/linux/input.cpp -o "$@"

$(BUILD_DIR)/tests/unit/video_test: tests/unit/video_test.cpp \
		tests/support/fake_i2c.cpp tests/support/capture_log.cpp \
		src/native/artifacts.cpp src/native/core_loader.cpp \
		src/native/diagnostic.cpp \
		src/native/video_recipe.cpp \
		src/native/video.hpp src/native/video.cpp src/native/linux/framebuffer.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/video_test.cpp \
		tests/support/fake_i2c.cpp tests/support/capture_log.cpp \
		src/native/artifacts.cpp src/native/core_loader.cpp \
		src/native/diagnostic.cpp \
		src/native/video_recipe.cpp \
		src/native/video.cpp src/native/linux/framebuffer.cpp -o "$@"

$(BUILD_DIR)/tests/unit/framebuffer_test: tests/unit/framebuffer_test.cpp src/native/linux/framebuffer.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) -DMISTER_RUNTIME_TESTING $(CXXFLAGS) \
		tests/unit/framebuffer_test.cpp src/native/linux/framebuffer.cpp -o "$@"

$(BUILD_DIR)/tests/unit/fpga_manager_test: tests/unit/fpga_manager_test.cpp \
		tests/support/fake_mmio.cpp src/native/artifacts.cpp \
		src/native/diagnostic.cpp \
		src/native/linux/fpga_manager.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/fpga_manager_test.cpp \
		tests/support/fake_mmio.cpp src/native/artifacts.cpp \
		src/native/diagnostic.cpp \
		src/native/linux/fpga_manager.cpp -o "$@"

$(BUILD_DIR)/tests/unit/mmio_test: tests/unit/mmio_test.cpp \
		src/native/linux/mmio.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) -DMISTER_RUNTIME_TESTING $(CXXFLAGS) \
		tests/unit/mmio_test.cpp src/native/linux/mmio.cpp -o "$@"

$(BUILD_DIR)/tests/unit/off_t_test: tests/unit/off_t_test.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) tests/unit/off_t_test.cpp -o "$@"

$(BUILD_DIR)/tests/unit/native_hardware_test: tests/unit/native_hardware_test.cpp \
		tests/support/capture_log.cpp tests/support/fake_input.cpp \
		tests/support/fake_mmio.cpp \
		src/native/artifacts.cpp src/native/core_package.cpp src/native/core_data.cpp src/native/sha256.cpp \
		src/native/core_driver.cpp src/native/core_loader.cpp src/native/input.cpp \
		src/native/fes_gp.cpp src/native/diagnostic.cpp \
		src/native/video_recipe.cpp src/native/video.cpp \
		src/native/hardware.cpp src/profile.cpp src/runtime.cpp \
		src/linux/production_hardware.cpp \
		src/native/linux/mmio.cpp src/native/linux/fpga_manager.cpp \
		src/native/linux/framebuffer.cpp \
		src/native/linux/i2c.cpp src/native/linux/input.cpp \
		src/native/linux/spi.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) \
		-DMISTER_RUNTIME_IDLE_RBF=\"/definitely-missing/libmister-runtime/idle.rbf\" \
		tests/unit/native_hardware_test.cpp \
		tests/support/capture_log.cpp tests/support/fake_input.cpp \
		tests/support/fake_mmio.cpp \
		src/native/artifacts.cpp src/native/core_package.cpp src/native/core_data.cpp src/native/sha256.cpp \
		src/native/core_driver.cpp src/native/core_loader.cpp src/native/input.cpp \
		src/native/fes_gp.cpp src/native/diagnostic.cpp \
		src/native/video_recipe.cpp src/native/video.cpp \
		src/native/hardware.cpp src/profile.cpp src/runtime.cpp \
		src/linux/production_hardware.cpp \
		src/native/linux/mmio.cpp src/native/linux/fpga_manager.cpp \
		src/native/linux/framebuffer.cpp \
		src/native/linux/i2c.cpp src/native/linux/input.cpp \
		src/native/linux/spi.cpp -o "$@"

$(BUILD_DIR)/tests/unit/protocol_test: tests/unit/protocol_test.cpp \
		src/daemon/json.cpp src/daemon/protocol.cpp src/profile.cpp src/runtime.cpp \
		src/native/diagnostic.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/protocol_test.cpp \
		src/daemon/json.cpp src/daemon/protocol.cpp src/profile.cpp src/runtime.cpp \
		src/native/diagnostic.cpp -o "$@"

$(BUILD_DIR)/tests/integration/daemon_server_test: \
		tests/integration/daemon_server_test.cpp \
		tests/support/fake_hardware.cpp \
		src/daemon/controller.cpp src/daemon/server.cpp \
		src/daemon/json.cpp src/daemon/protocol.cpp \
		src/linux/stderr_log.cpp src/profile.cpp src/runtime.cpp \
		src/native/diagnostic.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) \
		tests/integration/daemon_server_test.cpp \
		tests/support/fake_hardware.cpp \
		src/daemon/controller.cpp src/daemon/server.cpp \
		src/daemon/json.cpp src/daemon/protocol.cpp \
		src/linux/stderr_log.cpp src/profile.cpp src/runtime.cpp \
		src/native/diagnostic.cpp -o "$@"

$(TEST_BINS): $(TEST_HEADERS)

run-tests: $(TEST_BINS)
	@set -euo pipefail; \
	for test_binary in $(TEST_BINS); do \
		"$$test_binary"; \
	done

active-tree-test: all
	@tests/active_tree_test.sh "$(CURDIR)"

profile-provenance-test:
	@tests/profile_provenance_test.sh "$(CURDIR)"

hardware-support-truth-test:
	@tests/hardware_support_truth_test.sh "$(CURDIR)"

incremental-build-test: run-tests
	@tests/incremental_build_test.sh "$(CURDIR)" $(TEST_BINS)

version-build-test: incremental-build-test
	@tests/version_build_test.sh "$(CURDIR)"

test: version-build-test
	@$(MAKE) active-tree-test
	@$(MAKE) hardware-support-truth-test

sanitize:
	@$(MAKE) BUILD_DIR="$(BUILD_DIR)/sanitize" \
		CXXFLAGS="$(CXXFLAGS) -fsanitize=address,undefined -fno-omit-frame-pointer" \
		run-tests

tsan:
	@$(MAKE) BUILD_DIR="$(BUILD_DIR)/tsan" \
		CXXFLAGS="$(CXXFLAGS) -fsanitize=thread -fno-omit-frame-pointer" \
		"$(BUILD_DIR)/tsan/tests/unit/input_test" \
		"$(BUILD_DIR)/tsan/tests/unit/runtime_test" \
		"$(BUILD_DIR)/tsan/tests/integration/daemon_server_test"
	@"$(BUILD_DIR)/tsan/tests/unit/input_test"
	@"$(BUILD_DIR)/tsan/tests/unit/runtime_test"
	@"$(BUILD_DIR)/tsan/tests/integration/daemon_server_test"

archive-audit: $(ARCHIVE)
	@set -euo pipefail; \
	expected_members="$$(printf '%s\n' $(notdir $(LIB_OBJECTS)) | LC_ALL=C sort)"; \
	actual_members="$$(ar t "$(ARCHIVE)" | grep -v '^__\.SYMDEF' | LC_ALL=C sort)"; \
	[[ "$$actual_members" == "$$expected_members" ]] || { \
		echo "archive members differ from the production object list" >&2; \
		diff -u <(printf '%s\n' "$$expected_members") \
			<(printf '%s\n' "$$actual_members") >&2 || true; \
		exit 1; \
	}; \
	duplicate_members="$$(ar t "$(ARCHIVE)" | grep -v '^__\.SYMDEF' | LC_ALL=C sort | uniq -d)"; \
	[[ -z "$$duplicate_members" ]] || { \
		echo "archive contains duplicate members" >&2; \
		printf '%s\n' "$$duplicate_members" >&2; \
		exit 1; \
	}; \
	member_count="$$(printf '%s\n' "$$actual_members" | sed '/^$$/d' | wc -l | tr -d ' ')"; \
	[[ "$$member_count" == 21 ]] || { \
		echo "canonical archive must contain exactly 21 production members" >&2; \
		exit 1; \
	}; \
	archive_list="$$(find "$(BUILD_DIR)" -maxdepth 1 -type f -name '*.a' | sed 's|^.*/||' | LC_ALL=C sort)"; \
	[[ "$$archive_list" == "libmister-runtime.a" ]] || { \
		echo "canonical build did not produce exactly one archive" >&2; \
		printf '%s\n' "$$archive_list" >&2; \
		exit 1; \
	}; \
	object_members="$$(printf '%s\n' $(notdir $(LIB_OBJECTS)) | LC_ALL=C sort)"; \
	[[ "$$actual_members" == "$$object_members" ]] || { \
		echo "archive members differ from production objects" >&2; \
		exit 1; \
	}; \
	for required in runtime.o profile.o artifacts.o core_data.o core_package.o core_driver.o fes_gp.o core_loader.o \
		diagnostic.o sha256.o input.o hardware.o \
		fpga_manager.o framebuffer.o mmio.o spi.o production_hardware.o video_recipe.o \
		video.o i2c.o linux_input.o; do \
		grep -Fx "$$required" <<<"$$actual_members" >/dev/null || { \
			echo "archive omits required native member: $$required" >&2; \
			exit 1; \
		}; \
	done; \
	compiled_sources=""; \
	while IFS= read -r object; do \
		relative_object=$${object#"$(BUILD_DIR)/"}; \
		if [[ "$$relative_object" == "src/native/linux/linux_input.o" ]]; then \
			source="src/native/linux/input.cpp"; \
		else source="$${relative_object%.o}.cpp"; fi; \
		[[ -f "$$source" ]] || { echo "object has no production source: $$relative_object" >&2; exit 1; }; \
		case "$$source" in \
			src/runtime.cpp|src/profile.cpp|src/native/artifacts.cpp|src/native/core_package.cpp|src/native/core_data.cpp|src/native/core_driver.cpp|src/native/fes_gp.cpp|src/native/core_loader.cpp|src/native/diagnostic.cpp|src/native/sha256.cpp|src/native/input.cpp|src/native/video_recipe.cpp|src/native/video.cpp|src/native/hardware.cpp|src/native/linux/fpga_manager.cpp|src/native/linux/framebuffer.cpp|src/native/linux/i2c.cpp|src/native/linux/input.cpp|src/native/linux/mmio.cpp|src/native/linux/spi.cpp|src/linux/production_hardware.cpp) ;; \
			*) echo "archive contains non-production source: $$source" >&2; exit 1 ;; \
		esac; \
		compiled_sources+="$$source"$$'\n'; \
	done < <(printf '%s\n' $(LIB_OBJECTS) | LC_ALL=C sort); \
	raw_owners="$$(while IFS= read -r object; do \
		if $(NM) -u "$$object" | grep -E '(^|[[:space:]])_?(close|ioctl|mmap|munmap|open|open64|openat|pread|pread64|pwrite|pwrite64)(@.*)?$$' >/dev/null; then \
			printf '%s\n' "$${object#"$(BUILD_DIR)/"}"; \
		fi; \
	done < <(printf '%s\n' $(LIB_OBJECTS) | LC_ALL=C sort))"; \
	expected_raw_owners=$$'src/native/artifacts.o\nsrc/native/core_data.o\nsrc/native/core_loader.o\nsrc/native/core_package.o\nsrc/native/diagnostic.o\nsrc/native/linux/fpga_manager.o\nsrc/native/linux/framebuffer.o\nsrc/native/linux/i2c.o\nsrc/native/linux/linux_input.o\nsrc/native/linux/mmio.o'; \
	[[ "$$raw_owners" == "$$expected_raw_owners" ]] || { \
		echo "raw I/O ownership differs from the canonical native boundary" >&2; \
		diff -u <(printf '%s\n' "$$expected_raw_owners") \
			<(printf '%s\n' "$$raw_owners") >&2 || true; \
		exit 1; \
	}; \
	undefined_symbols="$$( $(NM) -u "$(ARCHIVE)" | $(CXXFILT) )"; \
	if grep -E '(^|[^[:alnum:]_])(fpga_load_rbf|user_io_|video_mode_adjust|scheduler_|offload_|reboot|reexec|execl|system)($$|[^[:alnum:]_])' \
		<<<"$$undefined_symbols" >/dev/null; then \
		echo "archive references superseded mutation authority" >&2; \
		exit 1; \
	fi; \
	if $(NM) -g "$(ARCHIVE)" | $(CXXFILT) | \
		grep -E 'LinuxFramebufferTestOperations|LinuxMmioTestOperations|LinuxI2cTestOperations|LinuxInputTestOperations|FakeHardware|FakeMmio|FakeSpi|FakeI2c|FakeInput|CartProfile|BiosProfile|mister_test' >/dev/null; then \
		echo "archive exports test-only hardware or profile symbols" >&2; \
		exit 1; \
	fi; \
	grep -F 'src/native/artifacts.hpp' "$(BUILD_DIR)/src/native/hardware.d" >/dev/null || { \
		echo "native hardware dependency closure omits artifacts" >&2; \
		exit 1; \
	}; \
	grep -F 'src/native/video.hpp' "$(BUILD_DIR)/src/native/hardware.d" >/dev/null || { \
		echo "native hardware dependency closure omits video" >&2; \
		exit 1; \
	}; \
	for header in src/native/video_recipe.hpp src/native/adv7513.hpp \
		src/native/core_loader.hpp \
		src/native/linux/spi.hpp src/native/linux/i2c.hpp; do \
		grep -F "$$header" "$(BUILD_DIR)/src/native/video.d" >/dev/null || { \
			echo "video dependency closure omits $$header" >&2; \
			exit 1; \
		}; \
	done; \
	grep -F 'src/native/adv7513.hpp' "$(BUILD_DIR)/src/native/video_recipe.d" \
		>/dev/null || { \
		echo "video recipe dependency closure omits adv7513" >&2; \
		exit 1; \
	}; \
	for header in src/native/fes_gp.hpp src/native/linux/i2c.hpp src/native/video.hpp \
		src/native/video_recipe.hpp; do \
		grep -F "$$header" "$(BUILD_DIR)/src/linux/production_hardware.d" >/dev/null || { \
			echo "production hardware dependency closure omits $$header" >&2; \
			exit 1; \
		}; \
	done; \
	grep -F 'src/native/linux/mmio.hpp' "$(BUILD_DIR)/src/native/linux/spi.d" >/dev/null || { \
		echo "SPI dependency closure omits MMIO" >&2; \
		exit 1; \
	}; \
	grep -F 'src/native/generated/de10_nano.hpp' "$(BUILD_DIR)/src/native/linux/fpga_manager.d" \
		>/dev/null || { \
		echo "FPGA manager dependency closure omits generated de10_nano" >&2; \
		exit 1; \
	}; \
	grep -F 'src/native/generated/de10_nano.hpp' "$(BUILD_DIR)/src/native/linux/spi.d" \
		>/dev/null || { \
		echo "SPI dependency closure omits generated de10_nano" >&2; \
		exit 1; \
	}; \
	grep -F 'src/native/generated/megadrive.hpp' "$(BUILD_DIR)/src/linux/production_hardware.d" \
		>/dev/null || { \
		echo "production hardware dependency closure omits generated megadrive" >&2; \
		exit 1; \
	}

target:
	$(MAKE) BUILD_DIR=build/target CXX="$(TARGET_CXX)" \
		AR="$(TARGET_AR)" all

clean:
	rm -rf -- "$(BUILD_DIR)"

-include $(DEPENDENCIES)
