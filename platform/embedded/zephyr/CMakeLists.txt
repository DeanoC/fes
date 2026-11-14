cmake_minimum_required(VERSION 3.20.0)
find_package(Zephyr REQUIRED HINTS $ENV{ZEPHYR_BASE})
project(fes_embedded_base LANGUAGES C)
target_sources(app PRIVATE src/main.c)
target_compile_options(app PRIVATE -Wall -Wextra -Werror)

# Zephyr 4.2.0's NVMe controller includes an unused <soc.h>; qemu_cortex_a53
# provides no such header. Compile a local copy with only that include removed.
# The pinned upstream checkout remains untouched and all driver logic is kept.
get_target_property(disk_sources drivers__disk SOURCES)
list(FILTER disk_sources EXCLUDE REGEX "nvme_controller\\.c$")
set_property(TARGET drivers__disk PROPERTY SOURCES "${disk_sources}")
set(nvme_dir "${ZEPHYR_BASE}/drivers/disk/nvme")
file(SHA256 "${nvme_dir}/nvme_controller.c" nvme_original_sha256)
if(NOT nvme_original_sha256 STREQUAL "35b50200a5d391860096b2ae340e60f6e8dc82f12e8c34b6a12a9bb3e5b573e6")
  message(FATAL_ERROR "Pinned NVMe source changed; review the AArch64 adaptation")
endif()
file(READ "${nvme_dir}/nvme_controller.c" nvme_source)
string(REGEX MATCHALL "#include <soc.h>" nvme_soc_includes "${nvme_source}")
list(LENGTH nvme_soc_includes nvme_soc_include_count)
if(NOT nvme_soc_include_count EQUAL 1)
  message(FATAL_ERROR "Review the pinned NVMe source adaptation before changing Zephyr")
endif()
string(REPLACE "#include <soc.h>" "/* Unused SoC header omitted for AArch64. */"
       nvme_source "${nvme_source}")
# QEMU virt has no PCI-enumerating firmware to leave endpoint decode enabled.
# The upstream NVMe driver assumes that state; the E1000 driver sets it itself.
string(REPLACE "\t/* Allocating vectors */"
       "\tpcie_set_cmd(nvme_ctrlr_cfg->pcie->bdf, PCIE_CONF_CMDSTAT_MEM | PCIE_CONF_CMDSTAT_MASTER, true);\n\n\t/* Allocating vectors */"
       nvme_source "${nvme_source}")
file(WRITE "${CMAKE_CURRENT_BINARY_DIR}/nvme_controller_aarch64.c" "${nvme_source}")
target_sources(drivers__disk PRIVATE "${CMAKE_CURRENT_BINARY_DIR}/nvme_controller_aarch64.c")
target_include_directories(drivers__disk PRIVATE "${nvme_dir}")
