# Copyright 2026 FogCast contributors
# SPDX-License-Identifier: GPL-3.0-or-later

SHELL := /bin/bash

CXX ?= c++
AR ?= ar

BUILD_DIR := build
ARCHIVE := $(BUILD_DIR)/libmister-runtime.a

CPPFLAGS := -Iinclude -Isrc
TEST_CPPFLAGS := $(CPPFLAGS) -Itests/support/include
CXXFLAGS := -std=c++14 -Wall -Wextra -Werror -pthread -MMD -MP

LIB_SOURCES := src/runtime.cpp \
	$(wildcard src/profile.cpp) \
	$(sort $(wildcard src/native/*.cpp)) \
	$(sort $(wildcard src/native/linux/*.cpp)) \
	$(wildcard src/linux/production_hardware.cpp)
LIB_OBJECTS := $(patsubst %.cpp,$(BUILD_DIR)/%.o,$(LIB_SOURCES))
DEPENDENCIES := $(LIB_OBJECTS:.o=.d)

.PHONY: all clean archive-audit

all: $(ARCHIVE)

$(BUILD_DIR)/%.o: %.cpp
	@mkdir -p "$(dir $@)"
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) -c "$<" -o "$@"

$(ARCHIVE): $(LIB_OBJECTS)
	@mkdir -p "$(dir $@)"
	ZERO_AR_DATE=1 $(AR) rcsD "$@" $(LIB_OBJECTS)

archive-audit: $(ARCHIVE)
	@set -euo pipefail; \
		expected_members="$$(printf '%s\n' $(notdir $(LIB_OBJECTS)) | LC_ALL=C sort)"; \
		actual_members="$$(ar t "$(ARCHIVE)" | LC_ALL=C sort)"; \
		if [[ "$$actual_members" != "$$expected_members" ]]; then \
			echo "archive members differ from the production object list" >&2; \
			diff -u <(printf '%s\n' "$$expected_members") <(printf '%s\n' "$$actual_members") >&2 || true; \
			exit 1; \
		fi; \
		duplicate_members="$$(ar t "$(ARCHIVE)" | LC_ALL=C sort | uniq -d)"; \
		if [[ -n "$$duplicate_members" ]]; then \
			echo "archive contains duplicate members" >&2; \
			printf '%s\n' "$$duplicate_members" >&2; \
			exit 1; \
		fi

clean:
	rm -rf -- "$(BUILD_DIR)"

-include $(DEPENDENCIES)
