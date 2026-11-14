/* SPDX-License-Identifier: GPL-3.0-or-later */
#define FES_TEST_RETIRE_COST_MAIN previous_cost_main
#include "retire_cost_test.c"
#undef FES_TEST_RETIRE_COST_MAIN
int main(int argc,char **argv)
{
 assert(argc==3);read_cost=strtoul(argv[1],0,10);counter_cost=strtoul(argv[2],0,10);
 assert(read_cost<=1000 && counter_cost<=100000);
 trial_case=0;fixture_credits=4;joined_case();
 struct nano_cmdq *q=&source_retirement.commands;
 assert(!source_retirement.command_receipt.interconnect_drained &&
        !source_retirement.command_receipt.DMA_access_granted);
 if(source_retirement.continued){
  assert(source_retirement.retired && !source_retirement.failed && !source_retirement.first_error);
  assert(configuration.finished && complete_calls==1 && !parks);
  assert(q->publication_seen && q->completion_seen && q->stage==CMDQ_COMPLETE);
  assert(q->publication_us-q->start<=NANO_CMDQ_RETIRE_STAGE_US);
  assert(q->completion_us-q->publication_us<=NANO_CMDQ_RETIRE_COMPLETION_US);
  assert(source_retirement.continuation_last-source_retirement.start<NANO_RETIRE_US);
 }else assert(source_retirement.failed && parks==1 && !complete_calls);
 printf("{\"status\":\"passed\",\"hardware_actions\":false,\"retired\":%u,\"continued\":%u,\"failed\":%u,\"first_error\":%d,\"phase\":%u,\"queue_stage\":%u,\"writes\":%u,\"publication_seen\":%u,\"completion_seen\":%u,\"staging_us\":%llu,\"completion_us\":%llu,\"retirement_us\":%llu,\"continuation_us\":%llu}\n",
  source_retirement.retired,source_retirement.continued,source_retirement.failed,source_retirement.first_error,
  source_retirement.phase,q->stage,source_retirement.writes,q->publication_seen,q->completion_seen,
  (unsigned long long)(q->publication_seen ? q->publication_us-q->start:0),
  (unsigned long long)(q->completion_seen ? q->completion_us-q->publication_us:0),
  (unsigned long long)(source_retirement.last-source_retirement.start),
  (unsigned long long)(source_retirement.continuation_last ? source_retirement.continuation_last-source_retirement.start:0));
}
