/* SPDX-License-Identifier: BSD-3-Clause */
#include <stdlib.h>
#define main retire_fixture_main
#define time_us fixture_time_us
#include "retire_test.c"
#undef time_us
#undef main
static unsigned last_guard,final_clocks,injected;
static uint64_t elapsed;
static int time_us(void *p,uint64_t *out)
{
 struct model *m=p;struct nano_retire *s=m->current;
 unsigned is_command_clock=s->guard_calls==last_guard;
 last_guard=s->guard_calls;
 if(is_command_clock && s->commands.stage==CMDQ_FINAL_FIRST && ++final_clocks==2 && elapsed){
  m->now=s->commands.start+elapsed-1;injected=1;
 }
 return fixture_time_us(p,out);
}
int main(int argc,char **argv)
{
 assert(argc==2);elapsed=strtoull(argv[1],0,10);assert(elapsed<=1000001);
 struct model m;setup(&m);struct nano_retire s={0};m.current=&s;
 struct nano_retire_receipt r={0};struct nano_retire_io io={&m,owns,source,time_us,read32,write32,store,barrier,prepared_queue()};
 int error=nano_source_retire(&s,binding(),io,&r);
 assert(s.retired!=s.failed && r.memory_retained && !r.source_reuse_authorized && !r.ordinary_activation && !r.interconnect_drained);
 if(elapsed)assert(injected);
 if(!error)assert(r.translation_retired && s.commands.stage==CMDQ_COMPLETE && m.written==5);
 else assert(!r.translation_retired && s.commands.stage==CMDQ_FINAL_FIRST && m.written==4);
 unsigned reads=m.reads,writes=m.written,times=m.times;
 assert(nano_source_retire(&s,binding(),io,&r) && m.reads==reads && m.written==writes && m.times==times);
 printf("{\"status\":\"passed\",\"retired\":%u,\"first_error\":%d,\"phase\":%u,\"stage\":%u,\"writes\":%u,\"reads\":%u,\"times\":%u,\"source_calls\":%u,\"final_clocks\":%u,\"hardware_actions\":false}\n",
  s.retired,s.first_error,s.phase,s.commands.stage,m.written,m.reads,m.times,m.source,final_clocks);
}
