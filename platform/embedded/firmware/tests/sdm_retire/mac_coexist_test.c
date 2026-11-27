/* SPDX-License-Identifier: BSD-3-Clause */
#include <stdint.h>
#include <assert.h>
#define FES_NANO_MAC_HANDOFF 1
#define FES_NANO_MAC_TABLE_TRIAL 1
#define FES_NANO_MAC_COEXISTENCE_TRIAL 1
#define FES_TEST_MAC_TABLE_IO 1
#define FES_TEST_MAC_COEXIST 1
#define FES_NANO_CONTROL_BASE UINT64_C(0x20010000)
static uint32_t mac_reset=0xff7fb7b7,mac_route=3,mac_sids=0x10001,mac_table_start=0x94010000;
static uint64_t table[0x7000/8],commands[32],command_writes;
static uint32_t test_mac_frame(uint64_t),fault,displayed;
static uint64_t test_mac_cmd_load(uint64_t a){return commands[(a-0x94018000)/8];}
static void test_mac_cmd_store(uint64_t a,uint64_t v){commands[(a-0x94018000)/8]=v;command_writes++;}
static uint64_t test_mac_load(uint64_t a){return table[(a-0x94010000)/8];}
static void test_mac_store(uint64_t a,uint64_t v){table[(a-0x94010000)/8]=v;}
static void test_mac_base(uint32_t);
#define FES_TEST_LIFECYCLE_RPC_MAIN unused_lifecycle_rpc
#include "lifecycle_rpc.c"

static void test_mac_base(uint32_t value)
{
 assert(!registers[0x20/4] && !registers[0x24/4] && (mac_reset&0x101)==0x101);
 assert(value==0x94000000 || value==0x94010000);
}
static uint32_t test_mac_frame(uint64_t a)
{
 unsigned off=a-FES_NANO_CONTROL_BASE;
#ifdef FES_NANO_MAC_C64_SCOPE_SUPPLIER
 if(off<=FES_EMBEDDED_REG_BUILD3)return lifecycle_registers[off/4];
#endif
 switch(off){
 case 0xc4:return 0x31534d46;
 case 0xc8:return 1;
 case 0xb0:return 0x95000000;
 case 0xb4:return 8U<<20;
 case 0x94:return displayed && fault!=13 ? 6:0;
 case 0x98:return !!displayed;
 case 0x9c:return 0;
 case 0xa8:return fault==11;
 case 0xac:return fault==12;
 case 0xa0:return displayed;
 case 0xa4:return fault!=1 ? displayed:0;
 default:assert(0);return 0;
 }
}
static void run(unsigned problem)
{
 joined_setup();expected_sd_io=sd_platform.io;configuration.attempted=1;
 memcpy(configuration_program.header+FES_NANO_PROGRAM_BUILD,expected.digest384,16);
 fes_nano_owned_sd.ops.returned=window_sd_return;lifecycle_setup();
 configuration.io.complete=lifecycle_complete;
 assert(!transfer_cycle() && configuration.finished && identity_configured(&provider)==1);
 uint64_t r[4];assert(lifecycle_control(FES_EMBEDDED_BRIDGE_CONTROL,17,4,r) && !r[0]);
 uint64_t original_cmd[16];memcpy(original_cmd,&arena[0x8000/8],sizeof(original_cmd));
 assert(lifecycle_control(FES_EMBEDDED_BRIDGE_RELEASE | (8ULL<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT),17,r[3],r));
 assert(!r[0]);
 assert(mac_window.active && mac_window.completed==6 && registers[0x80/4]==0x94010000 && registers[0x24/4]==13);
 assert(registers[0x98/4]==2 && registers[0x9c/4]==2 && command_writes==12);
 displayed=3;
 assert(identity_probe.stage==FES_ID_DDR_READ && fes_nano_mac_window_progress()==1 && mac_window.displayed==3);
 /* The same local renderer quiescence receipt used by the real dispatcher. */
 lifecycle_registers[FES_EMBEDDED_REG_GENERATION_LO/4]=1;
 lifecycle_registers[FES_EMBEDDED_REG_REQUEST_GENERATION_LO/4]=1;
 lifecycle_registers[FES_EMBEDDED_REG_REQUEST_COOKIE_LO/4]=7;
 lifecycle_registers[FES_EMBEDDED_REG_ACK_GENERATION_LO/4]=1;
 lifecycle_registers[FES_EMBEDDED_REG_ACK_COOKIE_LO/4]=7;
 lifecycle_registers[FES_EMBEDDED_REG_STATUS/4]=FES_EMBEDDED_STATUS_DRAINED;
 fault=problem;
 switch(fault){
 case 2:registers[49]=1;break;
 case 3:registers[0x98/4]=0;break;
 case 4:table[80]^=4;break;
 case 5:arena[0x6000/8]^=1;break;
 case 6:mac_reset&=~1;break;
 case 7:mac_sids^=1;break;
 case 8:registers[44]=1;break;
 case 9:registers[0x24/4]=12;break;
 case 10:configuration_program.header[FES_NANO_PROGRAM_SHA384]^=1;break;
 }
 assert(lifecycle_control(FES_EMBEDDED_BRIDGE_CLOSE,17,r[3],r));
 if(!fault){
  assert(!r[0] && !mac_window.active && mac_window.completed==16);
  assert(command_writes==32 && !mac_handoff.failed && !registers[0x24/4]);
  assert(registers[0x80/4]==0x94000000 && registers[0x90/4]==0x94008003 && registers[0xa0/4]==0x94009003);
  assert(registers[0x98/4]==12 && registers[0x9c/4]==12 && fabric_stop.done);
 }else{
  assert(r[0] && mac_window.active && mac_handoff.failed);
  assert(command_writes==12 && !fabric_stop.done);
 }
 assert(!memcmp(original_cmd,&arena[0x8000/8],sizeof(original_cmd)));
 assert(!fes_nano_owned_mac.attempted && !fes_nano_owned_mac.initialized);
}
#ifndef FES_TEST_MAC_COEXIST_MAIN
#define FES_TEST_MAC_COEXIST_MAIN main
#endif
int FES_TEST_MAC_COEXIST_MAIN(void)
{
 for(unsigned i=0;i<=12;i++){
  pid_t pid=fork();assert(pid>=0);
  if(!pid){run(i);_exit(0);}
  int status;assert(waitpid(pid,&status,0)==pid);
  if(!WIFEXITED(status)||WEXITSTATUS(status)){fprintf(stderr,"coexist case %u failed\n",i);return 1;}
 }
 puts("{\"status\":\"passed\",\"commands\":16,\"faults\":12,\"hardware_actions\":false}");return 0;
}
