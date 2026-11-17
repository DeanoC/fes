/* SPDX-License-Identifier: BSD-3-Clause */
/* Joined off-board boundary. Real transfer/parser, selected source supplier,
 * retirement, release and SD-return engines; injected MMIO, source hashing,
 * ROM device work and sealed metadata. Activation, control/held close and
 * master-stop callbacks are production code. Never a boot or DMA proof. */
#define FES_TEST_WARM_SERVICE_MAIN unused_warm_service_main
#define FES_TEST_LIFECYCLE_CALLBACKS 1
static void lifecycle_identity_setup(void);
#include "warm_service_test.c"
#include <inttypes.h>
#include "lifecycle_callbacks.inc"

static uint32_t selected_bytes,selected_chunk,selected_rom;
static uint8_t selected_digest[48];
static unsigned configurations,releases;

static int selected_bind(void *context,uint64_t owner,uint64_t generation,uint32_t *bytes)
{
 assert(context==&provider && owner==configuration.owner && generation==configuration.generation);
 configuration_program.generation=generation;
 configuration_program.bytes=selected_bytes;configuration_program.chunk=selected_chunk;
 configuration_program.role=next_role;configuration_program.rom_bytes=selected_rom;
 configuration_program.control_abi=next_role==FES_NANO_PROGRAM_ROLE_MENU ?
  FES_NANO_PROGRAM_MENU_ABI:FES_NANO_PROGRAM_COMPUTER_ABI;
 memcpy(configuration_program.header+FES_NANO_PROGRAM_SHA384,selected_digest,48);
 memcpy(configuration_program.header+FES_NANO_PROGRAM_BUILD,selected_digest,16);
 *bytes=selected_bytes;return 0;
}
int main(void)
{
 char operation[16],digest[97];uint64_t owner,generation;unsigned fault;
 setvbuf(stdout,0,_IOLBF,0);
 while(scanf("%15s",operation)==1){
  terminal_used=0;terminal_log[0]=0;
  if(!strcmp(operation,"configure")){
   assert(scanf("%" SCNx64 " %" SCNu64 " %u %u %u %u %96s",&owner,&generation,
    &next_role,&selected_bytes,&selected_chunk,&selected_rom,digest)==7 && strlen(digest)==96);
   for(unsigned n=0;n<48;n++){unsigned value;assert(sscanf(digest+2*n,"%2x",&value)==1);selected_digest[n]=(uint8_t)value;}
   if(!configurations){
    joined_setup();expected_sd_io=sd_platform.io;configuration.attempted=1;
    fes_nano_owned_sd.ops.returned=window_sd_return;
    configuration.bytes=selected_bytes;configuration.chunk=selected_chunk;
   assert(!selected_bind(&provider,owner,generation,&configuration.bytes));
   lifecycle_setup();
   }else{
    uint64_t r[4];assert(sdm_source_released && configuration.source_released);
    configuration.io.bind=selected_bind;
    if(!lifecycle_control(FES_EMBEDDED_BRIDGE_REARM_CONFIGURATION,owner,generation,r))
     fes_nano_configuration_rearm(&configuration,owner,generation,0,r);
    assert(!r[0]);
   }
   assert(owner==configuration.owner && generation==configuration.generation);
   configuration.io.complete=lifecycle_complete;
   expected.generation=generation;expected.bytes=selected_bytes;expected.chunk=selected_chunk;
   memcpy(expected.digest384,selected_digest,48);sdm_bytes=0;
   if(setjmp(terminal)){fprintf(stderr,"terminal transfer: %s\n",terminal_log);return 2;}
   assert(!transfer_cycle() && configuration.finished && !service_error && !sdm_source_failed);
   assert(identity_configured(&provider)==1);
   configurations++;
   printf("R 0 %" PRIu64 " %u\n",configuration.generation,configurations);
  }else if(!strcmp(operation,"release")){
   assert(scanf("%" SCNx64 " %" SCNu64 " %u",&owner,&generation,&fault)==3);
   assert(configurations && owner==configuration.owner);
   lifecycle_return();
   assert(generation==fes_nano_owned_bridges.state.generation);
   /* Failures enter through real predicates; never manufacture an SMC result. */
   switch(fault){
   case 0:break;
   case 1:identity_probe.abi^=1;break;
   case 2:provider.record.boot.epoch++;break;
   case 3:arena[0xb000/8]^=1;break;
   case 4:packet(1,0);break;
   case 5:ticks+=UINT64_C(90000000000);break; /* A long session is legal. */
   case 6:transfer.completed_buffer_bytes-=4;break;
   case 7:case 8:
    release_fault=fault==7 ? 56:57;inject=1;mutation_seen=0;break;
   default:assert(0);
   }
   uint64_t r[4];unsigned writes=smmu_writes,stores=cb_stores,posts=data_posts;
   fes_nano_configuration_release_source(&configuration,owner,generation,0,r);
   if(r[0] && terminal_used)fputs(terminal_log,stdout);
   assert(writes==smmu_writes && stores==cb_stores && posts==data_posts);
   releases++;
   printf("R %" PRIu64 " %" PRIx64 " %" PRIx64 " %" PRIu64 " %u %u\n",
    r[0],r[1],r[2],r[3],sdm_probe_scope_reason,releases);
  }else assert(0);
 }
 assert(!mbox_db_lock && !mbox_read_lock && !mbox_write_lock);
 return 0;
}
