/* SPDX-License-Identifier: BSD-3-Clause */
#define main retire_fixture_main
#define FES_TEST_RETIRE_READ fixture_read32
#include "retire_test.c"
#undef FES_TEST_RETIRE_READ
#undef main
static unsigned progress_mode,progress_at,progress_reads,injected;
static int read32(void *p,uint32_t o,uint32_t *value)
{
 struct model *m=p;int e=fixture_read32(p,o,value);struct nano_retire *s=m->current;
 if(s->phase==RET_INVALIDATE && s->commands.polls==1 && m->written==4){
  progress_reads++;
  if(progress_mode && progress_reads==progress_at){
   injected=1;
   switch(progress_mode){
   case 1:s->io.read32=0;break;
   case 2:s->io.time_us=0;break;
   case 3:s->io.context=0;break;
   case 4:s->io.barrier=0;break;
   case 5:s->failed=1;break;
   case 6:s->phase=RET_DISABLE;break;
   case 7:s->binding.owner++;break;
   case 8:m->now+=NANO_RETIRE_US;break;
   case 9:m->fail_owner=m->owners+1;break;
   case 10:s->commands.io.progress=0;break;
   default:assert(0);
   }
  }
 }
 return e;
}
static unsigned progress_case(unsigned mode,unsigned at)
{
 struct model m;setup(&m);struct nano_retire s={0};struct nano_retire_receipt r={0};m.current=&s;
 progress_mode=mode;progress_at=at;progress_reads=injected=0;
 struct nano_retire_io io={&m,owns,source,time_us,read32,write32,store,barrier,prepared_queue()};
 int e=nano_source_retire(&s,binding(),io,&r);
 if(!mode){assert(!e && s.retired && r.translation_retired && m.written==5);}
 else{
  if(!(injected && e && s.failed && !s.retired && m.written==4))fprintf(stderr,"PROGRESS mode=%u at=%u seen=%u injected=%u e=%d failed=%u retired=%u writes=%u phase=%u error=%d\n",mode,at,progress_reads,injected,e,s.failed,s.retired,m.written,s.phase,s.first_error);
  assert(injected && e && s.failed && !s.retired && m.written==4);
  assert(!r.translation_retired && r.memory_retained && !r.source_reuse_authorized && !r.ordinary_activation);
  assert(nano_source_retire(&s,binding(),io,&r) && m.written==4);
 }
 return progress_reads;
}
int main(void)
{
 unsigned reads=progress_case(0,0),total=1;
 for(unsigned mode=1;mode<=10;mode++)for(unsigned at=1;at<=reads;at++){progress_case(mode,at);total++;}
 printf("{\"status\":\"passed\",\"cases\":%u,\"poll_phase_read_positions\":%u,\"mutation_modes\":10,\"hardware_actions\":false}\n",total,reads);
}
