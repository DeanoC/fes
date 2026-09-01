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

VERSION_DIRTY = $(shell test -z "$$(git status --porcelain --untracked-files=normal)" || printf '%s' -dirty)
MISTER_RUNTIME_VERSION ?= git-$(shell git rev-parse --short=12 HEAD)$(VERSION_DIRTY)

CPPFLAGS := -Iinclude -Isrc
TEST_CPPFLAGS := $(CPPFLAGS) -Itests/support
CXXFLAGS ?= -std=c++14 -Wall -Wextra -Werror -pthread -MMD -MP

LIB_SOURCES := \
	src/runtime.cpp \
	src/profile.cpp \
	src/native/artifacts.cpp \
	src/native/core_loader.cpp \
	src/native/hardware.cpp \
	src/native/linux/fpga_manager.cpp \
	src/native/linux/mmio.cpp \
	src/native/linux/spi.cpp \
	src/linux/production_hardware.cpp
LIB_OBJECTS := $(patsubst %.cpp,$(BUILD_DIR)/%.o,$(LIB_SOURCES))
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
	$(BUILD_DIR)/tests/unit/artifacts_test \
	$(BUILD_DIR)/tests/unit/native_hardware_test \
	$(BUILD_DIR)/tests/unit/core_loader_test \
	$(BUILD_DIR)/tests/unit/fpga_manager_test \
	$(BUILD_DIR)/tests/unit/mmio_test \
	$(BUILD_DIR)/tests/unit/spi_test \
	$(BUILD_DIR)/tests/unit/protocol_test \
	$(BUILD_DIR)/tests/integration/daemon_server_test

.PHONY: all clean test run-tests sanitize tsan archive-audit active-tree-test target

all: $(ARCHIVE) $(DAEMON)

$(BUILD_DIR)/src/daemon/main.o: CPPFLAGS += \
	-DMISTER_RUNTIME_VERSION=\"$(MISTER_RUNTIME_VERSION)\"

$(BUILD_DIR)/%.o: %.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) -c "$<" -o "$@"

$(ARCHIVE): $(LIB_OBJECTS)
	@mkdir -p "$(dir $@)"
	ZERO_AR_DATE=1 $(AR) rcsD "$@" $(LIB_OBJECTS)

$(DAEMON): $(DAEMON_OBJECTS) $(ARCHIVE)
	$(CXX) $(CXXFLAGS) $(DAEMON_OBJECTS) \
		-Wl,--whole-archive $(ARCHIVE) -Wl,--no-whole-archive \
		$(LDFLAGS) $(LDLIBS) -o "$@"

$(BUILD_DIR)/tests/unit/profile_test: tests/unit/profile_test.cpp \
		tests/support/test_profiles.hpp src/profile.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/profile_test.cpp \
		src/profile.cpp -o "$@"

$(BUILD_DIR)/tests/unit/runtime_test: tests/unit/runtime_test.cpp \
		tests/support/fake_hardware.cpp tests/support/capture_log.cpp \
		src/profile.cpp src/runtime.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/runtime_test.cpp \
		tests/support/fake_hardware.cpp tests/support/capture_log.cpp \
		src/profile.cpp src/runtime.cpp -o "$@"

$(BUILD_DIR)/tests/unit/artifacts_test: tests/unit/artifacts_test.cpp \
		src/native/artifacts.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/artifacts_test.cpp \
		src/native/artifacts.cpp -o "$@"

$(BUILD_DIR)/tests/unit/core_loader_test: tests/unit/core_loader_test.cpp \
		tests/support/fake_spi.cpp src/native/artifacts.cpp src/native/core_loader.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/core_loader_test.cpp \
		tests/support/fake_spi.cpp src/native/artifacts.cpp \
		src/native/core_loader.cpp -o "$@"

$(BUILD_DIR)/tests/unit/spi_test: tests/unit/spi_test.cpp \
		tests/support/fake_mmio.cpp src/native/linux/spi.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/spi_test.cpp \
		tests/support/fake_mmio.cpp src/native/linux/spi.cpp -o "$@"

$(BUILD_DIR)/tests/unit/fpga_manager_test: tests/unit/fpga_manager_test.cpp \
		tests/support/fake_mmio.cpp src/native/artifacts.cpp \
		src/native/linux/fpga_manager.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/fpga_manager_test.cpp \
		tests/support/fake_mmio.cpp src/native/artifacts.cpp \
		src/native/linux/fpga_manager.cpp -o "$@"

$(BUILD_DIR)/tests/unit/mmio_test: tests/unit/mmio_test.cpp \
		src/native/linux/mmio.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) -DMISTER_RUNTIME_TESTING $(CXXFLAGS) \
		tests/unit/mmio_test.cpp src/native/linux/mmio.cpp -o "$@"

$(BUILD_DIR)/tests/unit/native_hardware_test: tests/unit/native_hardware_test.cpp \
		tests/support/capture_log.cpp src/native/artifacts.cpp \
		src/native/core_loader.cpp src/native/hardware.cpp src/profile.cpp \
		src/linux/production_hardware.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/native_hardware_test.cpp \
		tests/support/capture_log.cpp src/native/artifacts.cpp \
		src/native/core_loader.cpp src/native/hardware.cpp src/profile.cpp \
		src/linux/production_hardware.cpp -o "$@"

$(BUILD_DIR)/tests/unit/protocol_test: tests/unit/protocol_test.cpp \
		src/daemon/json.cpp src/daemon/protocol.cpp src/profile.cpp src/runtime.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) tests/unit/protocol_test.cpp \
		src/daemon/json.cpp src/daemon/protocol.cpp src/profile.cpp src/runtime.cpp -o "$@"

$(BUILD_DIR)/tests/integration/daemon_server_test: \
		tests/integration/daemon_server_test.cpp \
		tests/support/fake_hardware.cpp \
		src/daemon/controller.cpp src/daemon/server.cpp \
		src/daemon/json.cpp src/daemon/protocol.cpp \
		src/linux/stderr_log.cpp src/profile.cpp src/runtime.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(TEST_CPPFLAGS) $(CXXFLAGS) \
		tests/integration/daemon_server_test.cpp \
		tests/support/fake_hardware.cpp \
		src/daemon/controller.cpp src/daemon/server.cpp \
		src/daemon/json.cpp src/daemon/protocol.cpp \
		src/linux/stderr_log.cpp src/profile.cpp src/runtime.cpp -o "$@"

run-tests: $(TEST_BINS)
	@set -euo pipefail; \
	for test_binary in $(TEST_BINS); do \
		"$$test_binary"; \
	done

active-tree-test: all
	@tests/active_tree_test.sh "$(CURDIR)"

test: run-tests
	@$(MAKE) active-tree-test

sanitize:
	@$(MAKE) BUILD_DIR="$(BUILD_DIR)/sanitize" \
		CXXFLAGS="$(CXXFLAGS) -fsanitize=address,undefined -fno-omit-frame-pointer" \
		run-tests

tsan:
	@$(MAKE) BUILD_DIR="$(BUILD_DIR)/tsan" \
		CXXFLAGS="$(CXXFLAGS) -fsanitize=thread -fno-omit-frame-pointer" \
		"$(BUILD_DIR)/tsan/tests/unit/runtime_test" \
		"$(BUILD_DIR)/tsan/tests/integration/daemon_server_test"
	@"$(BUILD_DIR)/tsan/tests/unit/runtime_test"
	@"$(BUILD_DIR)/tsan/tests/integration/daemon_server_test"

archive-audit: $(ARCHIVE)
	@set -euo pipefail; \
	expected_members="$$(printf '%s\n' $(notdir $(LIB_OBJECTS)) | LC_ALL=C sort)"; \
	actual_members="$$(ar t "$(ARCHIVE)" | LC_ALL=C sort)"; \
	[[ "$$actual_members" == "$$expected_members" ]] || { \
		echo "archive members differ from the production object list" >&2; \
		diff -u <(printf '%s\n' "$$expected_members") \
			<(printf '%s\n' "$$actual_members") >&2 || true; \
		exit 1; \
	}; \
	duplicate_members="$$(ar t "$(ARCHIVE)" | LC_ALL=C sort | uniq -d)"; \
	[[ -z "$$duplicate_members" ]] || { \
		echo "archive contains duplicate members" >&2; \
		printf '%s\n' "$$duplicate_members" >&2; \
		exit 1; \
	}; \
	archive_list="$$(find "$(BUILD_DIR)" -maxdepth 1 -type f -name '*.a' -printf '%P\n' | LC_ALL=C sort)"; \
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
	for required in runtime.o profile.o artifacts.o core_loader.o hardware.o \
		fpga_manager.o mmio.o spi.o production_hardware.o; do \
		grep -Fx "$$required" <<<"$$actual_members" >/dev/null || { \
			echo "archive omits required native member: $$required" >&2; \
			exit 1; \
		}; \
	done; \
	compiled_sources=""; \
	while IFS= read -r object; do \
		relative_object=$${object#"$(BUILD_DIR)/"}; \
		source="$${relative_object%.o}.cpp"; \
		[[ -f "$$source" ]] || { echo "object has no production source: $$relative_object" >&2; exit 1; }; \
		case "$$source" in \
			src/runtime.cpp|src/profile.cpp|src/native/artifacts.cpp|src/native/core_loader.cpp|src/native/hardware.cpp|src/native/linux/fpga_manager.cpp|src/native/linux/mmio.cpp|src/native/linux/spi.cpp|src/linux/production_hardware.cpp) ;; \
			*) echo "archive contains non-production source: $$source" >&2; exit 1 ;; \
		esac; \
		compiled_sources+="$$source"$$'\n'; \
	done < <(printf '%s\n' $(LIB_OBJECTS) | LC_ALL=C sort); \
	raw_owners="$$(while IFS= read -r object; do \
		if $(NM) -u "$$object" | grep -E '(^|[[:space:]])_?(close|ioctl|mmap|munmap|open|open64|openat|pread|pread64|pwrite|pwrite64)(@.*)?$$' >/dev/null; then \
			printf '%s\n' "$${object#"$(BUILD_DIR)/"}"; \
		fi; \
	done < <(printf '%s\n' $(LIB_OBJECTS) | LC_ALL=C sort))"; \
	expected_raw_owners=$$'src/native/artifacts.o\nsrc/native/core_loader.o\nsrc/native/linux/fpga_manager.o\nsrc/native/linux/mmio.o'; \
	[[ "$$raw_owners" == "$$expected_raw_owners" ]] || { \
		echo "raw I/O ownership differs from the canonical native boundary" >&2; \
		diff -u <(printf '%s\n' "$$expected_raw_owners") \
			<(printf '%s\n' "$$raw_owners") >&2 || true; \
		exit 1; \
	}; \
	undefined_symbols="$$( $(NM) -u "$(ARCHIVE)" | $(CXXFILT) )"; \
	if grep -E '(^|[^[:alnum:]_])(fpga_load_rbf|user_io_|scheduler_|offload_|reboot|reexec|execl|system)($$|[^[:alnum:]_])' \
		<<<"$$undefined_symbols" >/dev/null; then \
		echo "archive references superseded mutation authority" >&2; \
		exit 1; \
	fi; \
	if $(NM) -g "$(ARCHIVE)" | $(CXXFILT) | \
		grep -E 'LinuxMmioTestOperations|FakeHardware|FakeMmio|FakeSpi|CartProfile|BiosProfile|mister_test' >/dev/null; then \
		echo "archive exports test-only hardware or profile symbols" >&2; \
		exit 1; \
	fi; \
	grep -F 'src/native/artifacts.hpp' "$(BUILD_DIR)/src/native/hardware.d" >/dev/null || { \
		echo "native hardware dependency closure omits artifacts" >&2; \
		exit 1; \
	}; \
	grep -F 'src/native/linux/mmio.hpp' "$(BUILD_DIR)/src/native/linux/spi.d" >/dev/null || { \
		echo "SPI dependency closure omits MMIO" >&2; \
		exit 1; \
	}

target:
	$(MAKE) BUILD_DIR=build/target CXX="$(TARGET_CXX)" \
		AR="$(TARGET_AR)" all

clean:
	rm -rf -- "$(BUILD_DIR)"

-include $(DEPENDENCIES)
