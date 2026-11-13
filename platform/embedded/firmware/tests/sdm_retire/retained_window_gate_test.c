/* SPDX-License-Identifier: BSD-3-Clause */
#include "nano_smmu.h"
#include "nano_cold.h"
#include "nano_bridges.h"
#include "nano_ddr_firewall.h"
#include <assert.h>
#include <stdio.h>
static struct fes_nano_cold provider;
static struct fes_nano_bridges fes_nano_owned_bridges;
static uint32_t values[13];static unsigned reads,covered=1;
static const uint32_t offsets[]={0,4,0x1c,0x8004,0x20,0x24,0x44,0x48,0x8020,0x8024,0x803c,0x8044,0x8048};
static int smmu_owns(void *p){return p==&provider && provider.control_expected==1 && !provider.ddr_expected;}
static int smmu_read(void *p,uint32_t off,uint32_t *out)
{assert(p==&provider);unsigned i=reads++%13;assert(off==offsets[i]);*out=values[i];return 0;}
static int smmu_report(struct fes_nano_cold *s,struct fes_nano_smmu_observation *out)
{
 struct fes_nano_smmu_probe p={.io={s,smmu_owns,smmu_read}};return fes_nano_smmu_observe(&p,out);
}
static int firewall_report(struct fes_nano_cold *s,struct fes_nano_ddr_firewall_observation *out)
{assert(s==&provider);*out=(struct fes_nano_ddr_firewall_observation){.layout_known=1,.window_covered=covered};return 0;}
static uint32_t active_reader_bytes(void){return 8U<<20;}
#define NOTICE(...) printf(__VA_ARGS__)
#include "window_gate_test.inc"
int main(void)
{
 provider.control_expected=1;fes_nano_owned_bridges.state.owner=17;
 fes_nano_owned_bridges.state.generation=5;fes_nano_owned_bridges.state.reset=13;
 values[0]=0x080f7e3f;values[1]=0x0e739d18;values[2]=1;values[3]=0x80000018;
 assert(!window_gate() && reads==26);
 /* The format13 control handoff does not make an aborted source route a
  * qualified reader route. The unchanged production gate must still deny it. */
 values[6]=FES_NANO_SMMU_ABORT;assert(window_gate()==-1 && reads==52);
 values[6]=0;covered=0;assert(window_gate()==-1 && reads==78);
 covered=1;values[0]^=1;assert(window_gate()==-1 && reads==104);
 values[0]^=1;values[8]=values[9]=1;assert(window_gate()==-1 && reads==130);
 values[8]=values[9]=0;provider.control_expected=0;assert(window_gate()==-1 && reads==130);
 puts("{\"status\":\"passed\",\"cases\":6,\"retained_abort_reader_gate\":\"denied\",\"hardware_actions\":false}");return 0;
}
