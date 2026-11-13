// SPDX-License-Identifier: GPL-2.0
/* Verify the compiled candidate DTB with the pinned kernel's own libfdt. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <libfdt.h>
#include "../de25-dma/generated/fes_de25_scanout.h"

static void require(int condition, const char *message)
{
	if (!condition) {
		fprintf(stderr, "DE25 owner DTB refused: %s\n", message);
		exit(1);
	}
}

static const fdt32_t *cells(const void *tree, int node, const char *name, int count)
{
	int length;
	const fdt32_t *p = fdt_getprop(tree, node, name, &length);
	require(p && length == count * (int)sizeof(*p), name);
	return p;
}

int main(int argc, char **argv)
{
	FILE *file;
	void *tree;
	long size;
	int owner, smmu, region, manager, symbols, length;
	const fdt32_t *p;
	const char *value;
	char path[256];

	require(argc == 2, "one DTB filename required");
	file = fopen(argv[1], "rb");
	require(file != NULL, "open");
	require(fseek(file, 0, SEEK_END) == 0, "seek");
	size = ftell(file);
	require(size >= (long)sizeof(struct fdt_header) && size <= 16 * 1024 * 1024, "size");
	require(fseek(file, 0, SEEK_SET) == 0, "rewind");
	tree = malloc((size_t)size);
	require(tree != NULL, "allocation");
	require(fread(tree, 1, (size_t)size, file) == (size_t)size, "read");
	require(fclose(file) == 0, "close");
	require(fdt_totalsize(tree) <= (unsigned long)size && fdt_check_header(tree) == 0, "FDT bounds/header");
	require(fdt_node_check_compatible(tree, 0, "intel,socfpga-agilex5") == 0, "board");
	owner = fdt_path_offset(tree, "/dma-owner@20010200");
	require(owner >= 0 && fdt_parent_offset(tree, owner) == 0, "permanent root owner");
	require(fdt_node_check_compatible(tree, owner, "fes,de25-dma") == 0, "owner compatible");
	p = cells(tree, owner, "reg", 4);
	require(fdt32_to_cpu(p[0]) == 0 && fdt32_to_cpu(p[1]) ==
		FES_DE25_SCANOUT_ARCHITECTURAL_LW_BASE + FES_DE25_SCANOUT_PERIPHERAL_OFFSET &&
		fdt32_to_cpu(p[2]) == 0 && fdt32_to_cpu(p[3]) == FES_DE25_SCANOUT_CONTROL_SPAN,
		"control resource");
	require(!fdt_getprop(tree, owner, "dma-coherent", &length), "unproven coherence declaration");
	p = cells(tree, owner, "iommus", 2);
	require(fdt32_to_cpu(p[1]) == FES_DE25_SCANOUT_SMMU_STREAM_ID, "stream ID");
	smmu = fdt_node_offset_by_phandle(tree, fdt32_to_cpu(p[0]));
	require(smmu >= 0 && fdt_node_check_compatible(tree, smmu, "arm,smmu-v3") == 0, "SMMU");
	p = cells(tree, smmu, "#iommu-cells", 1);
	require(fdt32_to_cpu(*p) == 1, "SMMU cells");
	value = fdt_getprop(tree, smmu, "status", &length);
	require(value && length == 5 && !memcmp(value, "okay", 5), "SMMU enabled");
	p = cells(tree, owner, "fes,fpga-region", 1);
	region = fdt_node_offset_by_phandle(tree, fdt32_to_cpu(*p));
	require(region == fdt_path_offset(tree, "/fpga-region") && fdt_parent_offset(tree, region) == 0,
		"root FPGA region");
	require(fdt_node_check_compatible(tree, region, "fpga-region") == 0, "region compatible");
	p = cells(tree, region, "fpga-mgr", 1);
	manager = fdt_node_offset_by_phandle(tree, fdt32_to_cpu(*p));
	require(manager >= 0 && fdt_node_check_compatible(tree, manager,
		"intel,agilex5-soc-fpga-mgr") == 0, "Agilex5 manager");
	require(!fdt_getprop(tree, region, "fpga-bridges", &length), "vendor SDM topology");
	symbols = fdt_path_offset(tree, "/__symbols__");
	require(symbols >= 0, "symbols");
	value = fdt_getprop(tree, symbols, "fes_de25_scanout", &length);
	require(value && fdt_get_path(tree, owner, path, sizeof(path)) == 0 &&
		length == (int)strlen(path) + 1 && !memcmp(value, path, (size_t)length), "owner symbol");
	free(tree);
	puts("DE25_OWNER_DTB_PASS");
	return 0;
}
