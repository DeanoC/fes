"""Host extraction of the exact controlled SiP entry and its configuration engine."""
import hashlib

import prepare_nano_owned
from verify import function

HERE = prepare_nano_owned.HERE


def build(reference, subset, output, run):
    owned = prepare_nano_owned.service_source(reference.read_bytes()).decode()
    source = subset.read_text() + '''
#define fes_nano_owned_bridges fes_nano_fixture_service
#define INTEL_SIP_SMC_FPGA_CONFIG_START 0xc2000001U
#define INTEL_SIP_SMC_FPGA_CONFIG_WRITE 0x42000002U
#define INTEL_SIP_SMC_FPGA_CONFIG_COMPLETED_WRITE 0xc2000003U
#define INTEL_SIP_SMC_FPGA_CONFIG_ISDONE 0xc2000004U
#include "nano_sd.h"
struct fes_nano_sd fes_nano_owned_sd;
#include "nano_owned_sip.inc"
typedef uint64_t u_register_t;
typedef struct { uint64_t r[4]; } Reply;
#define SIP_SVC_V3 1
#define INTEL_SIP_SMC_CMD_MASK 0xffffU
#define INTEL_SIP_SMC_CMD_V2_RANGE_BEGIN 0x400U
#define INTEL_SIP_SMC_CMD_V2_RANGE_END 0x4ffU
#define INTEL_SIP_SMC_CMD_V3_RANGE_BEGIN 0xc8U
#define INTEL_SIP_SMC_CMD_V3_RANGE_END 0x1f4U
#define SMC_RET1(h,a) do { Reply *r=(h); r->r[0]=(a); return 0; } while (0)
#define SMC_RET4(h,a,b,c,d) do { Reply *r=(h); r->r[0]=(a); r->r[1]=(b); r->r[2]=(c); r->r[3]=(d); return 0; } while (0)
static unsigned legacy_routes;
#define MOCK(name) static uintptr_t name(uint32_t f,u_register_t a,u_register_t b,u_register_t c,u_register_t d,void *cookie,void *handle,u_register_t flags) { (void)f;(void)a;(void)b;(void)c;(void)d;(void)cookie;(void)flags;legacy_routes++;((Reply*)handle)->r[0]=2;return 0; }
MOCK(sip_smc_handler_v1)
MOCK(sip_smc_handler_v2)
MOCK(sip_smc_handler_v3)
'''
    source += function(owned, "sip_smc_handler")
    source += '''
void fes_tfa_owned_sip(uint32_t fid,uint64_t x1,uint64_t x2,uint64_t x3,uint64_t x4,uint64_t r[4]) {
 Reply reply={{0}};sip_smc_handler(fid,x1,x2,x3,x4,0,&reply,0);
 for(unsigned i=0;i<4;i++)r[i]=reply.r[i];
}
unsigned fes_tfa_legacy_routes(void) { return legacy_routes; }
'''
    path = output / "tfa_owned_subset.c"
    path.write_text(source)
    flags = ["-std=c11", "-Wall", "-Wextra", "-Werror", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I" + str(HERE / "tests"), "-I" + str(HERE)]
    engine = output / "tfa_owned_subset.o"
    codec = output / "nano_bridge_sip.o"
    # Match the existing verifier's flags for the pinned vendor engine, whose
    # host buffer fixture mixes signed sizes and unsigned block lengths.
    run("owned-sip-compile", ["cc", *(f for f in flags if f != "-Wextra"), "-c", path, "-o", engine])
    run("owned-codec-compile", ["cc", *flags, "-c", HERE / "nano_bridge_sip.c", "-o", codec])
    sd = output / "nano_sd.o"
    run("owned-sd-compile", ["cc", *flags, "-c", HERE / "nano_sd.c", "-o", sd])
    return [engine, codec, sd], {"controlled_service_sha256": hashlib.sha256(owned.encode()).hexdigest(), "host_subset_sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
