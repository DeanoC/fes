/* SPDX-License-Identifier: BSD-3-Clause */
/* Real secure MENU, SMMU, channel/PHY and release suppliers. The physical
 * registers/clock, application frame and NS driver's ring writes are modeled. */
#include "nano_mac_joined_fixture.h"
#ifdef FES_NANO_MAC_C64_SCOPE_SUPPLIER
#include <assert.h>
#ifndef FES_NANO_C64_SCOPE_IDENTITY
/* Unit fixture identity. Private matched-component tests can supply the real fitted package. */
#define FES_NANO_C64_SCOPE_IDENTITY {2002944U,{1},{2},{3},{4},{{5}}}
#endif
#include "generated/fes_de25_computer.h"
static void test_c64_window(void);
static unsigned test_problem,fetch_reads,gp_error;
static uint32_t gp_read(uint64_t address)
{
 switch(address-0x20010300U){
 case FES_DE25_COMPUTER_ID_OFFSET:return FES_DE25_COMPUTER_IDENTITY;
 case FES_DE25_COMPUTER_VERSION_OFFSET:return FES_DE25_COMPUTER_VERSION;
 case FES_DE25_COMPUTER_FAULT_OFFSET:return gp_error;
 case FES_DE25_COMPUTER_STATUS_OFFSET:return 1;
 case FES_DE25_COMPUTER_FETCH_COUNT_OFFSET:fetch_reads++;return test_problem==6 ? 1:fetch_reads;
 case FES_DE25_COMPUTER_FETCH_ADDRESS_OFFSET:return 0xfce0;
 default:assert(0);return 0;
 }
}
#endif
#define FES_NANO_SPECTRUM48 1
#define FES_NANO_SPECTRUM48_SCANOUT_BASE 0x20010200U
static uint32_t scanout_status=8, scanout_serial;
static uint32_t scanout_read(unsigned);
struct fes_nano_bridges;
static int spectrum48_held_return(const struct fes_nano_bridges *,uint64_t *);
#define FES_TEST_MAC_COEXIST_MAIN coexist_model_main
#include "mac_coexist_test.c"
#include "spectrum48_return_test.inc"
static uint32_t scanout_read(unsigned offset)
{
 if(offset==0)return 0x44325343;
 if(offset==4)return 0x10003;
 if(offset==8 || offset==0x30)return scanout_status;
 if(offset==0x2c)return scanout_serial;
 if(offset>=0x64 && offset<=0x70)return fabric_stop.build[(offset-0x64)/4];
 return 0;
}

static uint64_t invoke(unsigned op,uint64_t generation)
{
 uint64_t r[4];
 fes_nano_mac_sip(&fes_nano_owned_mac,op,op==FES_EMBEDDED_MAC_QUERY ? 0:17,
                  generation,0,r);
 return r[0];
}
#ifdef FES_NANO_MAC_C64_SCOPE_SUPPLIER
#include "mac_c64_checks.inc"
#endif
static void reconfigure(unsigned role,uint64_t generation)
{
 terminal_log[0]=0;terminal_used=0;
 uint64_t r[4];
 fes_nano_configuration_release_source(&configuration,17,generation,0,r);
 assert(!r[0] && sdm_source_released && configuration.source_released);
 next_role=role;selected_bytes=role==FES_NANO_PROGRAM_ROLE_MENU ? 1708032:2101248;
 selected_chunk=configuration.chunk;selected_rom=0;
 for(unsigned i=0;i<48;i++) selected_digest[i]=(uint8_t)(generation+i);
#ifdef FES_NANO_MAC_C64_SCOPE_SUPPLIER
 if(role==FES_NANO_PROGRAM_ROLE_SPECTRUM48){selected_bytes=c64_mac_identity.bytes;memcpy(selected_digest,c64_mac_identity.sha384,48);}
#endif
 configuration.io.bind=selected_bind;
 if(!lifecycle_control(FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,17,generation,r))
  fes_nano_configuration_rearm(&configuration,17,generation,0,r);
 assert(!r[0] && configuration.generation==generation);
 configuration.io.complete=lifecycle_complete;
 expected.generation=generation;expected.bytes=selected_bytes;expected.chunk=selected_chunk;
 memcpy(expected.digest384,selected_digest,48);sdm_bytes=0;
 if(setjmp(terminal)){fprintf(stderr,"rearm terminal %s\n",terminal_log);abort();}
 assert(!transfer_cycle() && configuration.finished && identity_configured(&provider)==1);
}
static void window(unsigned problem)
{
#ifdef FES_NANO_MAC_C64_SCOPE_SUPPLIER
 test_problem=problem;
#endif
 fes_mac_fixture_boot();
 joined_setup();expected_sd_io=sd_platform.io;configuration.attempted=1;
 memcpy(configuration_program.header+FES_NANO_PROGRAM_BUILD,expected.digest384,16);
 fes_nano_owned_sd.ops.returned=window_sd_return;lifecycle_setup();
 configuration.io.complete=lifecycle_complete;
 assert(!transfer_cycle() && configuration.finished && identity_configured(&provider)==1);
 uint64_t r[4];assert(lifecycle_control(FES_EMBEDDED_BRIDGE_CONTROL,17,4,r) && !r[0]);
 assert(lifecycle_control(FES_EMBEDDED_BRIDGE_RELEASE | (8ULL<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT),17,r[3],r) && !r[0]);
 assert(!fes_nano_menu_mac_admit() && !fes_nano_owned_mac.attempted);
 displayed=3;
 for(unsigned i=0;i<2;i++){
  uint64_t generation=3*i;
  assert(fes_nano_menu_mac_admit()==1 && !invoke(FES_EMBEDDED_MAC_QUERY,0));
  if(problem==4 && i==1){
   /* A displayed counter remains latched after the presenter quits running. */
   fault=13;
   assert(invoke(FES_EMBEDDED_MAC_ACQUIRE,generation)==FES_EMBEDDED_MAC_ERROR);
   assert(menu_mac.generation==1 && fes_mac_fixture_restored() && !mac_window.active);
   return;
  }
  assert(!invoke(FES_EMBEDDED_MAC_ACQUIRE,generation));
  assert(menu_mac.generation==i+1 && menu_mac.vmid==3+i && !(channel_mmio(0x10d11024)&0x101));
  fes_mac_fixture_driver_start();
  assert(!invoke(FES_EMBEDDED_MAC_CONFIRM,generation+1) && menu_mac.phase==MENU_CONFIRMED);
  struct fes_nano_bridge_snapshot snapshot;
  assert(!fes_nano_usb_service_snapshot(&fes_nano_owned_usb,&snapshot) &&
        snapshot.reset==5 && !snapshot.masters_contained && snapshot.generation==6);
  assert(!fes_nano_menu_mac_source_safe());
  fes_mac_fixture_driver_stop();
  if(problem==1)fes_mac_fixture_fault(FES_FIXTURE_RX_PENDING);
  if(problem==2)fes_mac_fixture_fault(FES_FIXTURE_DMA_FAULT);
  if(problem==3)registers[44]=1;
  uint64_t status=invoke(FES_EMBEDDED_MAC_STOP,generation+2);
  if(problem && problem<=3){
   assert(status==FES_EMBEDDED_MAC_ERROR && fes_nano_owned_mac.failed && fes_nano_owned_mac.reservation &&
          !fes_nano_menu_mac_source_safe() && !fabric_stop.done);return;
  }
  assert(!status && menu_mac.phase==MENU_NO_MAC && fes_mac_fixture_restored());
  assert(mac_window.completed==16 && !mac_window.active && menu_mac.channel.phase==FES_CHANNEL_RETAINED);
  assert(fes_nano_menu_mac_source_safe() && registers[0x98/4]==12 && registers[0x9c/4]==12);
  assert(registers[0x80/4]==0x94000000 && registers[0x90/4]==0x94008003 && !registers[0x24/4]);
 }
 lifecycle_registers[FES_EMBEDDED_REG_GENERATION_LO/4]=1;
 lifecycle_registers[FES_EMBEDDED_REG_REQUEST_GENERATION_LO/4]=1;
 lifecycle_registers[FES_EMBEDDED_REG_REQUEST_COOKIE_LO/4]=7;
 lifecycle_registers[FES_EMBEDDED_REG_ACK_GENERATION_LO/4]=1;
 lifecycle_registers[FES_EMBEDDED_REG_ACK_COOKIE_LO/4]=7;
 lifecycle_registers[FES_EMBEDDED_REG_STATUS/4]=FES_EMBEDDED_STATUS_DRAINED;
 assert(lifecycle_control(FES_EMBEDDED_BRIDGE_CLOSE,17,6,r) && !r[0] && r[3]==7);
 assert(!fes_nano_menu_mac_admit() && fabric_stop.done);
 assert(!call(&model.base,FES_EMBEDDED_SD_RESUME));native_prepare(&model.base);
 assert(!call(&model.base,FES_EMBEDDED_SD_PREPARE));model.base.h.srs[0x234/4]=0;
 assert(!call(&model.base,FES_EMBEDDED_SD_STOP) && fes_nano_owned_bridges.state.generation==10);
 reconfigure(FES_NANO_PROGRAM_ROLE_SPECTRUM48,10);
 assert(!fes_nano_menu_mac_admit() && registers[0x98/4]==8 && fes_nano_menu_mac_source_safe());
 lifecycle_active_reader=1;lifecycle_return();assert(fes_nano_owned_bridges.state.generation==15 && !fes_nano_menu_mac_admit());
 reconfigure(FES_NANO_PROGRAM_ROLE_MENU,15);
 displayed=0;
 assert(lifecycle_control(FES_EMBEDDED_BRIDGE_CONTROL,17,15,r) && !r[0]);
 assert(lifecycle_control(FES_EMBEDDED_BRIDGE_RELEASE | (8ULL<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT),17,r[3],r) && !r[0]);
 assert(!fes_nano_menu_mac_admit() && mac_window.generation==15 && registers[0x98/4]==10);
 displayed=1;
 uint64_t returned=fes_nano_owned_mac.generation;
 assert(fes_nano_menu_mac_admit()==1 && !invoke(FES_EMBEDDED_MAC_ACQUIRE,returned));
 fes_mac_fixture_driver_start();
 assert(!invoke(FES_EMBEDDED_MAC_CONFIRM,returned+1) && menu_mac.vmid==3+returned/3 && menu_mac.menu_generation==15);
 fes_mac_fixture_driver_stop();
 assert(!invoke(FES_EMBEDDED_MAC_STOP,returned+2) && registers[0x98/4]==4 && registers[0x9c/4]==4);
 assert(fes_nano_menu_mac_source_safe() && fes_mac_fixture_restored());
}
int main(void)
{
#ifdef FES_NANO_MAC_C64_SCOPE_SUPPLIER
 const unsigned grants=4,faults=8;
#else
 const unsigned grants=3,faults=4;
#endif
 for(unsigned n=0;n<=faults;n++){
  pid_t pid=fork();assert(pid>=0);if(!pid){window(n);_exit(0);}
  int status;assert(waitpid(pid,&status,0)==pid);
  if(!WIFEXITED(status)||WEXITSTATUS(status)){fprintf(stderr,"MENU MAC case %u failed\n",n);return 1;}
 }
 printf("{\"status\":\"passed\",\"grants\":%u,\"faults\":%u,\"hardware_actions\":false}\n",grants,faults);
 return 0;
}
