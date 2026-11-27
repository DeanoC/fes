/* SPDX-License-Identifier: GPL-2.0-or-later */
/* Agilex 5 CS/R1 register map RC28_B0. Only normal-value registers appear
 * here. Clock enable SET/CLEAR aliases, ECC event status and DMA stay out. */
struct mac_spl_register { const char *name; unsigned address; };
enum mac_spl_word {
	MAC_CLK_STAT, MAC_MAIN_EN, MAC_MAIN_BYPASS, MAC_NOC_CLK, MAC_NOC_DIV,
	MAC_PER_EN, MAC_PER_BYPASS, MAC_EMAC_SEL, MAC_COUNTER_RESET,
	MAC_PER_RESET, MAC_TSN0_CFG, MAC_TBU_CTRL, MAC_TBU_IDS, MAC_L4_FW,
	MAC_NOC_IDLE, MAC_OBSERVATION_WORDS
};

static const struct mac_spl_register mac_spl_registers[] = {
	[MAC_CLK_STAT]      = {"clock_stat",   0x10d10004},
	[MAC_MAIN_EN]       = {"main_en",      0x10d10024},
	[MAC_MAIN_BYPASS]   = {"main_bypass",  0x10d10030},
	[MAC_NOC_CLK]       = {"noc_clk",      0x10d10040},
	[MAC_NOC_DIV]       = {"noc_div",      0x10d10044},
	[MAC_PER_EN]        = {"per_en",       0x10d1007c},
	[MAC_PER_BYPASS]    = {"per_bypass",   0x10d10088},
	[MAC_EMAC_SEL]      = {"emac_sel",     0x10d10094},
	[MAC_COUNTER_RESET] = {"counter_rst",  0x10d100f4},
	[MAC_PER_RESET]     = {"per0_reset",   0x10d11024},
	[MAC_TSN0_CFG]      = {"tsn0",         0x10d12044},
	[MAC_TBU_CTRL]      = {"tsn0_sidctrl", 0x10d1219c},
	[MAC_TBU_IDS]       = {"tsn0_sids",    0x10d121c8},
	[MAC_L4_FW]         = {"l4_fw_emac0",  0x10d2102c},
	[MAC_NOC_IDLE]      = {"noc_idle",     0x10d120d4}
};

/* MAC_Version at 0x10810110 is read-only. Its CSR clock is L4_SP.
 * Keep both TSN0 resets deasserted, both PLLs settled, backed by a
 * selected PLL with NoC bypass off, with the relevant gates open. */
static const char *mac_spl_skip_reason(const unsigned v[MAC_OBSERVATION_WORDS])
{
	const unsigned locked = (1U << 16) | (1U << 8);
	const unsigned changing = (1U << 24) | (1U << 17) | (1U << 9) | 1U;
	const unsigned emac_clock = 1U << ((v[MAC_EMAC_SEL] >> 26) & 1U);
	if (v[MAC_PER_RESET] & 0x101U)
		return "tsn0-reset";
	if ((v[MAC_CLK_STAT] & (locked | changing)) != locked)
		return "clock-state";
	if ((v[MAC_MAIN_EN] & 0xeU) != 0xeU || !(v[MAC_PER_EN] & 1U))
		return "clock-gate";
	if (((v[MAC_NOC_CLK] >> 16) & 7U) > 1 || (v[MAC_MAIN_BYPASS] & 2U) ||
	    ((v[MAC_PER_BYPASS] | v[MAC_COUNTER_RESET]) & emac_clock))
		return "clock-source";
	return 0;
}
