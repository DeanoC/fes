/* Host fixture for the pinned vendor bridge function; no physical MMIO. */
#include <stdint.h>

#define PLAT_SOCFPGA_AGILEX5 5
#define PLATFORM_MODEL PLAT_SOCFPGA_AGILEX5
#define SOCFPGA_RSTMGR(x) 0U
#define SOCFPGA_SYSMGR(x) 0U
#define SOCFPGA_F2SDRAMMGR(x) 0U
#define RSTMGR_BRGMODRSTMASK_SOC2FPGA 1U
#define RSTMGR_BRGMODRSTMASK_LWHPS2FPGA 2U
#define RSTMGR_BRGMODRSTMASK_FPGA2SOC 4U
#define RSTMGR_BRGMODRSTMASK_F2SDRAM0 8U
#define RSTMGR_BRGMODRST_SOC2FPGA 1U
#define RSTMGR_BRGMODRST_LWHPS2FPGA 2U
#define RSTMGR_BRGMODRST_FPGA2SOC 4U
#define RSTMGR_BRGMODRST_F2SSDRAM0 8U
#define RSTMGR_HDSKREQ_SOC2FPGAREQ 1U
#define RSTMGR_HDSKREQ_LWSOC2FPGAREQ 1U
#define RSTMGR_HDSKREQ_FPGAHSREQ 1U
#define RSTMGR_HDSKREQ_F2S_FLUSH 1U
#define RSTMGR_HDSKREQ_F2SDRAM0REQ 1U
#define RSTMGR_HDSKACK_SOC2FPGAACK 1U
#define RSTMGR_HDSKACK_SOC2FPGAACK_DASRT 0U
#define RSTMGR_HDSKACK_LWSOC2FPGAACK 1U
#define RSTMGR_HDSKACK_LWSOC2FPGAACK_DASRT 0U
#define RSTMGR_HDSKACK_FPGAHSACK 1U
#define RSTMGR_HDSKACK_FPGAHSACK_DASRT 0U
#define RSTMGR_HDSKACK_F2S_FLUSH 1U
#define RSTMGR_HDSKACK_FPGA2SOCACK 1U
#define RSTMGR_HDSKACK_FPGA2SOCACK_DASRT 0U
#define RSTMGR_HDSKACK_FPGAHSREQ 1U
#define RSTMGR_HDSKACK_F2SDRAM0ACK 1U
#define RSTMGR_HDSKACK_F2SDRAM0ACK_DASRT 0U
#define RSTMGR_HDSKEN_F2S_FLUSH 1U
#define RSTMGR_HDSKEN_FPGAHSEN 1U
#define SYSMGR_SOC_BRIDGE_CTRL_EN 1U
#define SYSMGR_LWSOC_BRIDGE_CTRL_EN 1U
#define SYSMGR_F2S_BRIDGE_CTRL_EN 1U
#define FLAGOUTCLR0_F2SDRAM0_ENABLE 1U
#define VERBOSE(...) ((void)0)
#define ERROR(...) ((void)0)

static unsigned fail_mask, poll_count;
#ifdef FES_TEST_ENABLE
static unsigned read_count;
static uint32_t mmio_read_32(uint32_t a)
{
    (void)a;
    unsigned n = read_count++;
    return n == 0 || n == 2 ? 15U : 0U;
}
#define BRIDGE_FUNCTION socfpga_bridges_enable
#define EXPECTED_POLLS 8U
#define RESET_READS() (read_count = 0)
#else
#define BRIDGE_FUNCTION socfpga_bridges_disable
#define EXPECTED_POLLS 6U
#define RESET_READS() ((void)0)
#endif
static void socfpga_s2f_bridge_mask(uint32_t mask, uint32_t *bridge, uint32_t *noc)
{ (void)mask; *bridge = 3; *noc = 0; }
static void socfpga_f2s_bridge_mask(uint32_t mask, uint32_t *bridge, uint32_t *idle,
                                    uint32_t *drain, uint32_t *enable, uint32_t *ack,
                                    uint32_t *empty, uint32_t *cmd)
{
    (void)mask; *bridge = 12; *idle = *drain = *enable = *ack = *empty = *cmd = 0;
}
static void mmio_setbits_32(uint32_t a, uint32_t b) { (void)a; (void)b; }
static void mmio_clrbits_32(uint32_t a, uint32_t b) { (void)a; (void)b; }
static void udelay(unsigned usec) { (void)usec; }
static int poll_idle_status(uint32_t a, uint32_t b, uint32_t c, unsigned d)
{
    (void)a; (void)b; (void)c; (void)d;
    unsigned n = poll_count++;
    return fail_mask & (1U << n) ? -(int)(11U + n) : 0;
}
int BRIDGE_FUNCTION(uint32_t mask);

static int exercise(unsigned failures)
{
    fail_mask = failures; poll_count = 0; RESET_READS();
    return BRIDGE_FUNCTION(~0U);
}

int main(void)
{
#ifdef FES_TEST_VENDOR_ORIGINAL
    /* Demonstrate the vendor bug: the first timeout is masked by later success. */
    return exercise(1) == 0 && poll_count == EXPECTED_POLLS ? 0 : 1;
#else
    if (exercise(0) != 0 || poll_count != EXPECTED_POLLS) return 1;
    for (unsigned i = 0; i < EXPECTED_POLLS; i++) {
        if (exercise(1U << i) != -(int)(11U + i) || poll_count != EXPECTED_POLLS) return 2;
    }
    return exercise((1U << 1) | (1U << 4)) == -12 && poll_count == EXPECTED_POLLS ? 0 : 3;
#endif
}
