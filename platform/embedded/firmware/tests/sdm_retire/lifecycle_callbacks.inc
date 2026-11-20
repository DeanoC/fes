/* SPDX-License-Identifier: BSD-3-Clause */
/* Real activation/dispatch/master-stop/bridge/SD-return callbacks. Only MMIO,
 * ROM device work and translation/firewall observations are injected. */
#include "generated/fes_de25_ddr_probe.h"
#define FES_NANO_COLD_CONFIG 1
#define FES_NANO_COLD_COMPUTER 1
#define FES_NANO_COLD_ROM 1
#define FES_NANO_WINDOW_READ_DIAGNOSTIC 1
#define FES_NANO_MENU_MEMORY 1
#define FES_NANO_COMPUTER_STORAGE_RETURN 1
#define FES_NANO_WINDOW_PRESENT_DIAGNOSTIC 1
#define FES_NANO_CONTROL_BASE UINT64_C(0x20010000)
#define FES_NANO_PROBE_CPU_BASE UINT64_C(0x20010100)
#define FES_NANO_CONTROL_ABI FES_NANO_PROGRAM_MENU_ABI
#define FES_NANO_CONTROL_BUILD {1,2,3,4}
#define FES_NANO_READER_BUS_BASE UINT64_C(0x95000000)
#define FES_NANO_READER_BYTES 8192U
static uint64_t fabric_reported_generation;
static uint32_t lifecycle_registers[64];
static unsigned lifecycle_endpoint_reads,lifecycle_endpoint_writes,lifecycle_rom_calls;
static unsigned lifecycle_active_reader,lifecycle_drained_reader;
static int cold_master_cpu_scope(void){return release_cpu && release_frequency==400000000U;}
static int smmu_owns(void *p){return p==&provider && provider.control_expected &&
 fes_nano_cold_owns(&provider,17,provider.record.boot.epoch)==1;}
static int window_gate(void){return smmu_owns(&provider) && !provider.ddr_expected &&
 fes_nano_sdm_source_reader_ready()==1 ? 0:-1;}
static uint32_t lifecycle_read(uint64_t address)
{
 lifecycle_endpoint_reads++;
 if(address>=FES_NANO_CONTROL_BASE && address<FES_NANO_PROBE_CPU_BASE)
  return lifecycle_registers[(address-FES_NANO_CONTROL_BASE)/4];
 if(!(address>=FES_NANO_PROBE_CPU_BASE && address<FES_NANO_PROBE_CPU_BASE+0x100))
  fprintf(stderr,"unexpected lifecycle MMIO %llx\n",(unsigned long long)address);
 assert(address>=FES_NANO_PROBE_CPU_BASE && address<FES_NANO_PROBE_CPU_BASE+0x100);
 switch(address-FES_NANO_PROBE_CPU_BASE){
 case FES_DE25_DDR_PROBE_ID_OFFSET:return FES_DE25_DDR_PROBE_IDENTITY;
 case FES_DE25_DDR_PROBE_VERSION_OFFSET:return FES_DE25_DDR_PROBE_VERSION;
 case FES_DE25_DDR_PROBE_STATUS_OFFSET:return FES_DE25_DDR_PROBE_STATUS_IDLE;
 default:return 0; /* The inherited diagnostic probe was never started. */
 }
}
static void lifecycle_write(uint64_t address,uint32_t value)
{
 assert(address>=FES_NANO_CONTROL_BASE && address<FES_NANO_PROBE_CPU_BASE);
 lifecycle_endpoint_writes++;
 unsigned offset=address-FES_NANO_CONTROL_BASE;
 if(offset==FES_EMBEDDED_REG_COMMAND){
  assert(value==FES_EMBEDDED_COMMAND_DISABLE);
  lifecycle_registers[FES_EMBEDDED_REG_STATUS/4]=FES_EMBEDDED_STATUS_DRAINED;
  lifecycle_registers[FES_EMBEDDED_REG_ACK_GENERATION_LO/4]=lifecycle_registers[FES_EMBEDDED_REG_GENERATION_LO/4];
  lifecycle_registers[FES_EMBEDDED_REG_ACK_GENERATION_HI/4]=lifecycle_registers[FES_EMBEDDED_REG_GENERATION_HI/4];
  lifecycle_registers[FES_EMBEDDED_REG_ACK_COOKIE_LO/4]=lifecycle_registers[FES_EMBEDDED_REG_REQUEST_COOKIE_LO/4];
  lifecycle_registers[FES_EMBEDDED_REG_ACK_COOKIE_HI/4]=lifecycle_registers[FES_EMBEDDED_REG_REQUEST_COOKIE_HI/4];
 }else lifecycle_registers[offset/4]=value;
}
static int computer_rom_prepare(uint64_t owner,uint64_t generation)
{assert(owner==17 && generation==configuration.generation && configuration_program.rom_bytes);return 0;}
static int computer_rom_retire(uint64_t owner,uint64_t generation)
{assert(owner==17 && generation==configuration.generation && !configuration_program.rom_bytes);return 0;}
int fes_nano_platform_computer_rom(struct fes_nano_configuration *c,uint64_t owner,uint64_t generation)
{assert(c==&configuration && owner==17 && generation==c->generation+1 && !identity_probe.reader);lifecycle_rom_calls++;return 0;}
void fes_nano_platform_computer_rom_report(void){}
void fes_nano_platform_window_report(unsigned phase){assert(phase>=1 && phase<=4);}
struct fes_nano_identity_probe *fes_nano_platform_identity_probe(void){return &identity_probe;}
#include "nano_computer_control.inc"
#include "nano_window_control.inc"
#include "nano_program_control.inc"
/* Redirect only the platform's physical I/O/CPU primitives, not its decisions. */
#define mmio_read_32 lifecycle_read
#define mmio_write_32 lifecycle_write
#define dsbsy() ((void)0)
#define udelay(n) ((void)(n))
#define FES_COLD_CPU_MODE() cold_master_cpu_scope()
#define probe_read lifecycle_probe_read
#define probe_write lifecycle_probe_write
#include "lifecycle_platform_test.inc"
#undef probe_read
#undef probe_write
#undef mmio_read_32
#undef mmio_write_32
#undef udelay

static int lifecycle_enable(void *p,uint32_t mask,unsigned fabric)
{
 assert(p==&model.base && fabric==FES_NANO_FABRIC_ABSENT);
 if(mask==2 && fes_nano_platform_control_target(1))return -1;
 if(mask==8 && fes_nano_platform_ddr_target(1))return -1;
 assert(mask==2 || mask==8);model.base.h.bridge&=~mask;return 0;
}
static int lifecycle_disable(void *p,uint32_t mask,unsigned fabric)
{
 assert(p==&model.base && fabric==FES_NANO_FABRIC_ABSENT);
 if(mask&8){if(fes_nano_platform_ddr_target(0))return -1;}
 else if((mask&2) && fes_nano_platform_control_target(0))return -1;
 model.base.h.bridge|=mask;return 0;
}
static int lifecycle_contain(void *p){assert(p==&provider);model.base.h.bridge|=2;return 0;}
static int lifecycle_identity_read(void *p,uint32_t offset,uint32_t *value)
{assert(p==&provider);*value=lifecycle_read(FES_NANO_CONTROL_BASE+offset);return 0;}
static void lifecycle_identity_setup(void)
{
 const uint32_t build[4]={1,2,3,4};
 struct fes_nano_identity_ops io={.context=&provider,.configured=identity_configured,
  .read32=lifecycle_identity_read,.contain=lifecycle_contain};
 assert(!fes_nano_identity_init(&identity_probe,&fes_nano_owned_bridges,io,FES_NANO_CONTROL_ABI,build,1));
}
static void lifecycle_setup(void)
{
 fabric_stop=new_fabric_stop(1);
 model.base.master_boot.raw.enable=lifecycle_enable;
 model.base.master_boot.raw.disable=lifecycle_disable;
 lifecycle_callbacks_active=1;
}
static int lifecycle_complete(void *context,uint64_t owner,uint64_t generation,unsigned rearmed)
{
 int result=production_configuration_complete(context,owner,generation,rearmed);
 if(result)return result;
 memset(lifecycle_registers,0,sizeof(lifecycle_registers));
 lifecycle_registers[0]=FES_EMBEDDED_MAGIC;lifecycle_registers[1]=FES_EMBEDDED_VERSION;
 lifecycle_registers[2]=fabric_stop.abi;
 memcpy(lifecycle_registers+3,fabric_stop.build,sizeof(fabric_stop.build));
 lifecycle_registers[0xb0/4]=fabric_stop.bus_base;lifecycle_registers[0xb4/4]=fabric_stop.bytes;
 lifecycle_registers[(fabric_stop.abi==FES_NANO_PROGRAM_MENU_ABI ? 0xcc:0xb8)/4]=1;
 complete_calls++;return 0;
}
static int lifecycle_control(uint64_t operation,uint64_t owner,uint64_t generation,uint64_t r[4])
{
 memset(r,0,4*sizeof(*r));r[0]=2;
 int result=fes_nano_program_control(FES_EMBEDDED_BRIDGE_CALL,
 operation,owner,FES_EMBEDDED_BRIDGE_COOKIE,generation,&configuration,0,r);
 if(result && r[0])fprintf(stderr,"control op=%llx result=%d status=%llu phase=%u idstage=%u first=%d stop=%d scope=%u prepared=%u issued=%u reset=%x gen=%llu fabricgen=%llu\n",
  (unsigned long long)operation,result,(unsigned long long)r[0],fes_nano_window_phase,
  identity_probe.stage,identity_probe.first_error,fabric_stop.first_error,sdm_probe_scope_reason,
  window_prepared,window_issued,fes_nano_owned_bridges.state.reset,
  (unsigned long long)fes_nano_owned_bridges.state.generation,(unsigned long long)fabric_generation);
 return result;
}
static void lifecycle_return(void)
{
 uint64_t r[4];unsigned menu=configuration_program.role==FES_NANO_PROGRAM_ROLE_MENU;
 assert(lifecycle_control(FES_EMBEDDED_BRIDGE_CONTROL,17,configuration.generation,r)==1 && !r[0]);
 if(menu){
  assert(lifecycle_control(FES_EMBEDDED_BRIDGE_RELEASE | (8ULL<<FES_EMBEDDED_BRIDGE_RELEASE_SHIFT),17,r[3],r)==1 && !r[0]);
 }
 if(menu || lifecycle_active_reader || lifecycle_drained_reader){
  assert(menu || !fabric_stop.attempted);
  /* Model the endpoint's completed menu frame, never a secure receipt. */
  lifecycle_registers[FES_EMBEDDED_REG_GENERATION_LO/4]=1;
  lifecycle_registers[FES_EMBEDDED_REG_REQUEST_GENERATION_LO/4]=1;
  lifecycle_registers[FES_EMBEDDED_REG_REQUEST_COOKIE_LO/4]=7;
  unsigned drained=menu || lifecycle_drained_reader;
  lifecycle_registers[FES_EMBEDDED_REG_STATUS/4]=drained ? FES_EMBEDDED_STATUS_DRAINED:FES_EMBEDDED_STATUS_ENABLED;
  lifecycle_registers[FES_EMBEDDED_REG_ACK_GENERATION_LO/4]=drained ? 1:0;
  lifecycle_registers[FES_EMBEDDED_REG_ACK_COOKIE_LO/4]=drained ? 7:0;
 }
 uint64_t returned=configuration.generation+(menu ? 3:2);
 assert(lifecycle_control(FES_EMBEDDED_BRIDGE_CLOSE | (menu ? 0:FES_EMBEDDED_BRIDGE_REQUEST_HELD),17,r[3],r)==1 && !r[0]);
 assert(r[3]==returned && fabric_stop.done && !fabric_stop.failed);
 if(!menu)assert(fabric_stop.disable_issued==lifecycle_active_reader);
 assert(!call(&model.base,FES_EMBEDDED_SD_RESUME));native_prepare(&model.base);
 assert(!call(&model.base,FES_EMBEDDED_SD_PREPARE));model.base.h.srs[0x234/4]=0;
 assert(!call(&model.base,FES_EMBEDDED_SD_STOP));
 assert(fes_nano_owned_bridges.state.generation==returned+3);
}
