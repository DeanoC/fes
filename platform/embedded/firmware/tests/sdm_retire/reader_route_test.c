/* SPDX-License-Identifier: GPL-3.0-or-later */
#include "nano_smmu.h"
#include "nano_ddr_firewall.h"
struct fes_nano_cold;
static int smmu_report(struct fes_nano_cold *,void *);
static int firewall_report(struct fes_nano_cold *,void *);
static void reader_meter(unsigned);
#define FES_TEST_RETIRE_METER(kind) reader_meter(kind)
#define FES_TEST_CONTROL_REPORTS 1
#define FES_TEST_CONTROL_MAIN control_fixture_main
#include "control_return_test.c"
static unsigned firewall_covered=1,inject_reader,meter_loads,meter_fault;
static void reader_meter(unsigned kind)
{
 if(!inject_reader || kind!=2)return;
 if(++meter_loads==6000){
  if(meter_fault==1)registers[52]=FES_NANO_SMMU_ABORT;
  if(meter_fault==2)provider.record.boot.owner++;
 }
}
static int smmu_owns(void *context)
{return context==&provider && fes_nano_cold_owns(&provider,17,provider.record.boot.epoch)==1;}
static int observer_read(void *context,uint32_t offset,uint32_t *value)
{assert(context==&provider && fes_nano_smmu_offset(offset));*value=probe_read(FES_NANO_SMMU_BASE+offset);return 0;}
static int smmu_report(struct fes_nano_cold *s,void *out)
{
 struct fes_nano_smmu_probe p={.io={s,smmu_owns,observer_read}};
 struct fes_nano_smmu_observation v={0};smmu_queries++;
 int status=fes_nano_smmu_observe(&p,&v);if(!status && out)*(struct fes_nano_smmu_observation *)out=v;return status;
}
static int firewall_report(struct fes_nano_cold *s,void *out)
{
 assert(s==&provider);firewall_queries++;
 if(out)*(struct fes_nano_ddr_firewall_observation *)out=(struct fes_nano_ddr_firewall_observation){.layout_known=1,.window_covered=firewall_covered};
 return 0;
}
static uint32_t active_reader_bytes(void){return 8U<<20;}
#include "window_gate_test.inc"
static int reader_enable(void *p,uint32_t mask,unsigned fabric)
{
 (void)fabric;assert(p==&model.base.master_boot && mask==FES_NANO_BRIDGES_CONTROL);
 int status=fes_nano_cold_control_target(&provider,17,provider.record.boot.epoch,1);
 if(!status)model.base.h.bridge&=~mask;return status;
}
static void setup_reader(void)
{
 setup_completed();assert(identity_configured(&provider)==1);
 fes_nano_owned_bridges.ops.enable=reader_enable;
 struct fes_nano_bridge_snapshot opened;
 assert(!fes_nano_bridges_open(&fes_nano_owned_bridges,17,4,FES_NANO_BRIDGES_CONTROL,&opened));
 assert(opened.generation==5 && opened.reset==13 && !opened.never_enabled && opened.masters_contained);
 fabric_generation=5;
 identity_probe=(struct fes_nano_identity_probe){.bridges=&fes_nano_owned_bridges,.io={.context=&provider},
  .initialized=1,.attempted=1,.active=1,.stage=FES_ID_DDR_PREPARE,.reader=FES_NANO_READER_WINDOW,.abi=0x10203043};
 assert(fes_nano_cold_owns(&provider,17,provider.record.boot.epoch)==1);
}
static void reader_case(unsigned fault)
{
 setup_reader();struct nano_retire before=source_retirement;
 struct nano_sdm_transfer_result old_transfer,new_transfer;fes_nano_sdm_transfer_result(&old_transfer);
 unsigned writes=smmu_writes,stores=cb_stores,posts=data_posts;
 switch(fault){
 case 0:break;
 case 1:registers[0x44/4]=0;break;
 case 2:registers[49]=1;break;
 case 3:registers[50]=1;break;
 case 4:registers[51]=1;break;
 case 5:registers[52]=FES_NANO_SMMU_ABORT;break;
 case 6:registers[53]=1;break;
 case 7:registers[48]^=0x80000000;break;
 case 8:registers[7]=0;break;
 case 9:configuration.finished=0;break;
 case 10:configuration.rearmed=1;break;
 case 11:configuration.generation++;break;
 case 12:source_retirement.continued=0;break;
 case 13:source_retirement.retired=0;break;
 case 14:control_pending=1;break;
 case 15:control_header_seen=0;break;
 case 16:provider.record.boot.owner++;break;
 case 17:provider.control_expected=0;break;
 case 18:provider.ddr_expected=1;break;
 case 19:identity_probe.active=0;break;
 case 20:identity_probe.stage=FES_ID_DDR_READ;break;
 case 21:identity_probe.bridges=0;break;
 case 22:identity_probe.abi=0x10203042;break;
 case 23:identity_probe.io.context=&model;break;
 case 24:window_prepared=1;break;
 case 25:window_issued=1;break;
 case 26:fabric_generation=4;break;
 case 27:registers[44]=1;break;
 case 28:arena[0]^=1;break;
 case 29:configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;break;
 case 30:fes_nano_owned_bridges.state.generation=6;break;
 case 31:fes_nano_owned_bridges.state.never_enabled=1;break;
 case 32:firewall_covered=0;break;
 case 33:inject_reader=1;meter_fault=1;break;
 case 34:inject_reader=1;meter_fault=2;break;
 case 35:service_error=-13;break;
 default:assert(0);
 }
 if(!fault){
  assert(!window_gate());identity_probe.stage=FES_ID_DDR_RELEASE;window_issued=1;
  assert(!window_gate());
  assert(!memcmp(&before,&source_retirement,sizeof(before)));
  fes_nano_sdm_transfer_result(&new_transfer);assert(!memcmp(&old_transfer,&new_transfer,sizeof(old_transfer)));
  assert(registers[0x44/4]==FES_NANO_SMMU_ABORT && !registers[49] && !registers[52]);
 }else assert(window_gate()<0);
 assert(smmu_writes==writes && cb_stores==stores && data_posts==posts);
}
int main(void)
{
 for(unsigned n=0;n<36;n++){
  pid_t p=fork();assert(p>=0);if(!p){reader_case(n);_exit(0);}int status;
  assert(waitpid(p,&status,0)==p);
  if(!WIFEXITED(status) || WEXITSTATUS(status)){fprintf(stderr,"reader fault=%u\n",n);return 1;}
 }
 puts("{\"status\":\"passed\",\"cases\":36,\"actual_window_gate\":true,\"nonsecure_abort_retained\":true,\"hardware_actions\":false}");return 0;
}
