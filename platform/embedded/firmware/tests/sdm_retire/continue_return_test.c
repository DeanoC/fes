/* SPDX-License-Identifier: GPL-3.0-or-later */
#define FES_TEST_JOINED_MAIN joined_fixture_main
#include "joined_test.c"
static unsigned return_fault;
static int checked_completion(void *c,uint64_t o,uint64_t g,unsigned rearmed)
{
 int status=complete(c,o,g,rearmed);
 assert(source_retirement.continued && source_retirement.retired);
 if(return_fault==1)return -5;
 if(return_fault==2)configuration.bridges->state.owner++;
 if(return_fault==3)configuration.generation++;
 return status;
}
static int checked_source(void *c,uint64_t a,uint32_t n,unsigned whole)
{
 int status=configuration_check(c,a,n,whole);
 if(return_fault==4 && source_retirement.continued)return -5;
 return status;
}
static void one_return(void)
{
 joined_setup();configuration.io.complete=checked_completion;configuration.io.check=checked_source;
 uint64_t r[4];assert(!dispatch(FES_NANO_CONFIG_START,0,0,r));
 uint32_t offset=0;
 while(configuration.completed<configuration.bytes){
  if(offset<configuration.bytes && configuration.pending<configuration.chunk*4){
   uint32_t amount=configuration.bytes-offset;if(amount>configuration.chunk)amount=configuration.chunk;
   int e=dispatch(FES_NANO_CONFIG_WRITE,configuration.base+offset,amount,r);assert(e==0 || e==1);offset+=amount;
  }
  flush_pending();int e=dispatch(FES_NANO_CONFIG_COMPLETED,0,0,r);assert(e==0 || e==1);
 }
 assert(!setjmp(terminal));
 int status=dispatch(FES_NANO_CONFIG_ISDONE,0,0,r);
 assert(!parks && source_retirement.continued && source_retirement.retired && smmu_writes==17);
 if(!return_fault)assert(!status && configuration.finished && !configuration.failed && complete_calls==1);
 else assert(status==4 && configuration.failed && !configuration.finished);
 assert(!source_retirement.command_receipt.interconnect_drained && !source_retirement.command_receipt.DMA_access_granted);
}
int main(void)
{
 for(return_fault=0;return_fault<=4;return_fault++){
  pid_t p=fork();assert(p>=0);if(!p){one_return();_exit(0);}
  int status;assert(waitpid(p,&status,0)==p && WIFEXITED(status) && !WEXITSTATUS(status));
 }
 printf("{\"status\":\"passed\",\"cases\":5,\"hardware_actions\":false}\n");return 0;
}
