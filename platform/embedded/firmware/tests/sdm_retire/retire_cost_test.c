/* SPDX-License-Identifier: GPL-3.0-or-later */
/* Inject supplier cost into the same joined vendor/platform fixture. Counts
 * are operation-cost observations; they are not physical latency evidence. */
static void retire_meter(unsigned);
#define FES_TEST_RETIRE_METER(kind) retire_meter(kind)
#define FES_TEST_RETIRE_COST_MODE 1
#define FES_TEST_JOINED_MAIN joined_fixture_main
#include "joined_test.c"
static uint64_t read_cost,counter_cost,counts[7][6];
static void retire_meter(unsigned kind)
{
 if(!source_retirement.attempted)return;
 assert(source_retirement.phase<7 && kind<6);
 if(!source_retirement.continuation_attempted)counts[source_retirement.phase][kind]++;
 if(kind==0)ticks+=read_cost;
 if(kind==5)ticks+=counter_cost;
}
int main(int argc,char **argv)
{
 assert(argc==3);
 read_cost=strtoul(argv[1],0,10);counter_cost=strtoul(argv[2],0,10);
 assert(read_cost<=1000 && counter_cost<=1000);
 trial_case=0;fixture_credits=4;joined_case();
 assert(!source_retirement.continued || (source_retirement.retired && !source_retirement.failed));
 if(source_retirement.failed && !source_retirement.retired){
  assert(source_retirement.first_error==-110 ||
   (source_retirement.phase==RET_INVALIDATE && source_retirement.first_error==-3));
  assert(!source_retirement.retired);
 }
 assert(!source_retirement.command_receipt.interconnect_drained &&
        !source_retirement.command_receipt.DMA_access_granted);
 printf("{\"status\":\"passed\",\"hardware_actions\":false,\"retired\":%u,\"phase\":%u,\"first_error\":%d,\"writes\":%u,\"publish_attempted\":%u,\"phase_counts\":[",
  source_retirement.retired,source_retirement.phase,source_retirement.first_error,
  source_retirement.writes,source_retirement.commands.publish_attempted);
 for(unsigned p=0;p<7;p++){
  printf("%s[",p?",":"");
  for(unsigned k=0;k<6;k++)printf("%s%llu",k?",":"",(unsigned long long)counts[p][k]);
  printf("]");
 }
 printf("]}\n");return 0;
}
