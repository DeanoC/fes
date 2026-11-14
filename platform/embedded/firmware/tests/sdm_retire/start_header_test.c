/* SPDX-License-Identifier: BSD-3-Clause */
/* Real service, source setup and vendor parser; peripheral I/O is injected. */
#define FES_TEST_JOINED_MAIN start_fixture_main
#include "joined_test.c"
static void run_header(uint32_t header,unsigned accepted)
{
 joined_setup();
 assert(!fes_nano_configuration_before(&configuration,FES_NANO_CONFIG_START,0,0,0));
 assert(!fes_nano_sdm_transfer_prepare() && prepared && !transfer.initialized);
 unsigned reads_before=smmu_reads,writes_before=smmu_writes,posts_before=data_posts;
 int status;
 if(accepted)status=fes_nano_sdm_transfer_peek_header(header);
 else{
  unsigned consumed_before=consumed,payload_before=payload_reads;
  push(header,1);
  status=mailbox_send_cmd(MBOX_JOB_ID,MBOX_CMD_FPGA_CONFIG_COMP,0,0,CMD_CASUAL,0,0);
  if(!status || consumed!=consumed_before || payload_reads!=payload_before)
   fprintf(stderr,"START parser header=%08x status=%d consumed=%u prior=%u payload=%u prior_payload=%u\n",
    header,status,consumed,consumed_before,payload_reads,payload_before);
  assert(status && consumed==consumed_before && payload_reads==payload_before);
 }
 assert((status==0)==accepted);
 assert(smmu_reads==reads_before && smmu_writes==writes_before && data_posts==posts_before);
 if(!accepted){
  assert(service_error && fes_nano_sdm_transfer_peek_header(0x1f000000U)==service_error);
  assert(fes_nano_sdm_transfer_prepare()==service_error && smmu_reads==reads_before && smmu_writes==writes_before);
 }
 assert(!mbox_db_lock && !mbox_read_lock && !mbox_write_lock);
}
int main(void)
{
 const uint32_t headers[]={0x1f000000U,0x1f002000U,0x1f003000U,
  0x10000000U,0x11000000U,0x1d000000U,0x1e000000U,0x0f000000U,0x2f000000U};
 for(unsigned n=0;n<sizeof(headers)/sizeof(headers[0]);n++){
  pid_t child=fork();assert(child>=0);if(!child){run_header(headers[n],n<3);_exit(0);}
  int status;assert(waitpid(child,&status,0)==child);
  if(!WIFEXITED(status) || WEXITSTATUS(status)){fprintf(stderr,"START header case=%u header=%08x\n",n,headers[n]);return 1;}
 }
 puts("{\"status\":\"passed\",\"cases\":9,\"hardware_actions\":false}");return 0;
}
