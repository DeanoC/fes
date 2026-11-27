/* SPDX-License-Identifier: GPL-2.0-or-later */
/* Included in the qualified CBL SPL report after its bounded early console.
 * No DDR, storage, MAC configuration, descriptors or network device open. */
#include "fes_cpu_diag_config.h"
#include "mac_spl_snapshot.h"

__attribute__((__noinline__)) static void fes_nano_mac_observe(void)
{
	unsigned sample[MAC_OBSERVATION_WORDS];
	unsigned changed = 0;
	unsigned el = current_el();
	printf("FES_NANO_MAC_SPL BEGIN id=%s el=%u\n", FES_CPU_DIAG_ID, el);
	if (el != 3) {
		puts("FES_NANO_MAC_SPL END state=el-rejected\n");
		return;
	}
	for (unsigned pass = 0; pass < 2; ++pass) {
		for (unsigned i = 0; i < MAC_OBSERVATION_WORDS; ++i) {
			const struct mac_spl_register *r = &mac_spl_registers[i];
			unsigned value = readl((void *)(unsigned long)r->address);
			if (pass && value != sample[i])
				changed = 1;
			sample[i] = value;
			printf("FES_NANO_MAC_SPL WORD pass=%u name=%s value=%08x\n",
			       pass, r->name, value);
		}
	}
	const char *reason = changed ? "unstable" : mac_spl_skip_reason(sample);
	if (reason)
		printf("FES_NANO_MAC_SPL VERSION skipped=%s\n", reason);
	else {
		puts("FES_NANO_MAC_SPL VERSION read\n");
		printf("FES_NANO_MAC_SPL VERSION value=%08x\n",
		       readl((void *)0x10810110UL));
	}
	puts("FES_NANO_MAC_SPL END state=observed\n");
}
