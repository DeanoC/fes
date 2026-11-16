/* Host fixture; the helpers, service and case are replaced with actual source. */
#include <assert.h>
#include <stdint.h>
#define FES_NANO_TFA_DIAG
#include "nano_bridge_diag.h"
#define SOCFPGA_RSTMGR(x) REG_##x
#define REG_HDSKACK 1U
#define REG_HDSKREQ 2U
#define REG_BRGMODRST 3U
#define SOCFPGA_BRIDGE_ENABLE 1U
#define SOCFPGA_BRIDGE_HAS_MASK 2U
#define INTEL_SIP_SMC_STATUS_ERROR 4U
#define INTEL_SIP_SMC_STATUS_OK 0U
#define INTEL_SIP_SMC_HPS_SET_BRIDGES 0xc2000032U
static int injected_error;
static uint32_t injected_stage;
static unsigned calls;
static uint32_t mmio_read_32(uint32_t reg) { return 0xaabbcc00U | reg; }
/* INSERT_HELPERS */
static int socfpga_bridges_disable(uint32_t mask)
{
    assert(mask == 15U); calls++;
    if (injected_error) fes_nano_bridge_diag_record(injected_stage, injected_error);
    return injected_error;
}
static int socfpga_bridges_enable(uint32_t mask) { assert(mask == 15U); calls++; return 0; }
/* INSERT_SERVICE */
typedef struct { uint64_t r[4]; } Reply;
#define SMC_RET1(h,a) do { (h)->r[0]=(a); return; } while (0)
#define SMC_RET4(h,a,b,c,d) do { (h)->r[0]=(a); (h)->r[1]=(b); (h)->r[2]=(c); (h)->r[3]=(d); return; } while (0)
static void dispatch(uint32_t id, uint64_t x1, uint64_t x2, uint64_t x3, Reply *handle)
{
    uint32_t status;
    switch (id) {
/* INSERT_CASE */
    default: assert(0);
    }
}
int main(void)
{
    Reply reply;
    for (unsigned cookie = 0; cookie < 3; cookie++) {
        reply = (Reply){{0, 0x1111, 0x2222, 0x3333}};
        injected_error = -110; injected_stage = 5; calls = 0;
        dispatch(INTEL_SIP_SMC_HPS_SET_BRIDGES, 2, 15, cookie ? cookie : 0, &reply);
        assert(calls == 1 && reply.r[0] == 4 && reply.r[1] == 0x1111 && reply.r[2] == 0x2222 && reply.r[3] == 0x3333);
    }
    for (unsigned stage = 1; stage <= 6; stage++) {
        injected_error = -110; injected_stage = stage; calls = 0;
        dispatch(INTEL_SIP_SMC_HPS_SET_BRIDGES, 2, 15, FES_NANO_BRIDGE_DIAG_REQUEST, &reply);
        assert(calls == 1 && reply.r[0] == 4 && reply.r[1] == ((uint64_t)FES_NANO_BRIDGE_DIAG_TAG << 32 | stage));
        assert(reply.r[2] == ((uint64_t)(uint32_t)-110 << 32 | 0xaabbcc01U));
        assert(reply.r[3] == ((uint64_t)0xaabbcc02U << 32 | 0xaabbcc03U));
        injected_error = 0; calls = 0;
        dispatch(INTEL_SIP_SMC_HPS_SET_BRIDGES, 2, 15, FES_NANO_BRIDGE_DIAG_REQUEST, &reply);
        assert(calls == 1 && reply.r[0] == 0 && reply.r[1] == (uint64_t)FES_NANO_BRIDGE_DIAG_TAG << 32 && !reply.r[2] && !reply.r[3]);
        injected_error = -110; calls = 0;
        dispatch(INTEL_SIP_SMC_HPS_SET_BRIDGES, 3, 15, FES_NANO_BRIDGE_DIAG_REQUEST, &reply);
        assert(calls == 1 && reply.r[0] == 0 && reply.r[1] == (uint64_t)FES_NANO_BRIDGE_DIAG_TAG << 32 && !reply.r[2] && !reply.r[3]);
    }
    return 0;
}
