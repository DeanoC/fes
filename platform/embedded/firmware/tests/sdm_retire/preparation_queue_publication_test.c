/* SPDX-License-Identifier: BSD-3-Clause */
#define FES_TEST_PUBLICATION_MAIN retirement_publication_fixture_main
#include "command_publication_test.c"
#undef FES_TEST_PUBLICATION_MAIN
int main(void)
{
 for(fallback=0;fallback<=1;fallback++){
  run_case(4,101123,0,80000,0,0,1);
  run_case(4,350000,0,80000,2000000,0,1);
  for(uint64_t elapsed=99999;elapsed<=100001;elapsed++)
   run_case(4,350000,0,elapsed,0,0,elapsed<=100000);
  run_case(4,3950000,0,100000,0,0,0); /* original final bound wins */
  run_case(4,NANO_CMDQ_FINAL_BOUND_US+1,0,0,0,0,0);
  run_case(4,0,NANO_CMDQ_FINAL_BOUND_US+1,0,0,0,0);
  run_case(4,0,0,0,NANO_CMDQ_FINAL_BOUND_US+1,0,0);
  for(unsigned broken=1;broken<=8;broken++)run_case(4,350000,0,80000,0,broken,broken==8);
  struct model m={.owned=1,.producer=6,.consumer=6};
  struct nano_cmdq_binding b={17,7,4,0x94008000,6};
  struct nano_cmdq_io io={&m,owns,clock_us,read32,write32,store,barrier,snapshot,0,0};
  assert(!nano_cmdq_init(&m.queue,b,io));struct nano_cmdq_receipt r={0};
  assert(nano_cmdq_submit(&m.queue,&r,CMDQ_PREPARATION_PUBLICATION_BOUND));
  assert(!m.now && !m.stores && !m.writes && !m.reads && !r.commands_completed);cases++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"hardware_actions\":false}\n",cases);
}
