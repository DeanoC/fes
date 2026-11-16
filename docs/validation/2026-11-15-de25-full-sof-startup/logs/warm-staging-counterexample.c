/* Actual controller, injected clock reproducing the retained prewrite stop. */
#define main old_snapshot_main
#include "/home/deano/fes/out/dev/de25-setup-release/platform/embedded/firmware/tests/sdm_retire/command_snapshot_test.c"
#undef main
static int clock_recorded(void *context,uint64_t *out)
{
 struct model *m=context;
 if(m->queue.stage==CMDQ_INITIAL_SECOND)m->now=m->queue.start+101122;
 return clock_us(context,out);
}
int main(void)
{
 struct model m={.owned=1,.producer=6,.consumer=6};
 struct nano_cmdq_binding b={17,7,10,0x94008000,6};
 b.generation=4;
 struct nano_cmdq_io io={&m,owns,clock_recorded,read32,write32,store,barrier,0,0,0};
 assert(!nano_cmdq_init(&m.queue,b,io));struct nano_cmdq_receipt r={0};
 assert(nano_cmdq_submit(&m.queue,&r,CMDQ_PREPARATION_FINAL_VALIDATION));
 assert(m.queue.failed && m.queue.first_error==-3 && m.queue.stage==CMDQ_INITIAL_SECOND);
 assert(m.queue.reads==8 && m.queue.observed-m.queue.start==101123 && !m.queue.slots_attempted && !m.queue.publish_attempted);
 assert(!m.stores && !m.writes && !r.commands_completed && !r.DMA_access_granted);
 unsigned reads=m.reads;assert(nano_cmdq_submit(&m.queue,&r,CMDQ_PREPARATION_FINAL_VALIDATION) && m.reads==reads);
 printf("{\"status\":\"reproduced-prepublication-command-clock-expiry\",\"queue_error\":%d,\"stage\":%u,\"observed_us\":%llu,\"reads\":%u,\"slots\":%u,\"published\":%u,\"hardware_actions\":false}\n",m.queue.first_error,m.queue.stage,(unsigned long long)(m.queue.observed-m.queue.start),m.queue.reads,m.queue.slots_attempted,m.queue.publish_attempted);
}
