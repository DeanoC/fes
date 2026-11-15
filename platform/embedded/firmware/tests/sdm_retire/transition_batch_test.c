/* SPDX-License-Identifier: BSD-3-Clause */
/* Execute the real controller with faults at every read in each transition
 * poll. All MMIO and ownership are injected; this grants no hardware proof. */
#define main retire_fixture_main
#define FES_TEST_RETIRE_READ fixture_read32
#include "retire_test.c"
#undef FES_TEST_RETIRE_READ
#undef main

static unsigned target_phase,target_writes,target_poll,mode,at,seen,injected;
static const uint32_t fixed_offsets[]={
 0,4,0x14,0x28,0x2c,0x48,0x50,0x54,0x60,0x64,
 0x80,0x84,0x88,0x90,0x94,0xa0,0xa4,0x100a8,0x100ac};

static int transition_read(void *p,uint32_t offset,uint32_t *value)
{
 struct model *m=p;struct nano_retire *s=m->current;
 int e=fixture_read32(p,offset,value);
 if(s->phase!=target_phase || m->written!=target_writes ||
    s->polls!=target_poll || seen==22)return e;
 unsigned n=seen++;
 const uint32_t abort_tail[]={0x20,0x24,0x44},control_tail[]={0x44,0x20,0x24};
 assert(offset==(n<19 ? fixed_offsets[n]:(target_phase==RET_ABORT ? abort_tail:control_tail)[n-19]));
 if(!mode || seen!=at)return e;
 injected=1;
 switch(mode){
 case 1:*value^=0x20000000U;break;
 case 2:return -5;
 case 3:m->fail_owner=m->owners+1;break;
 case 4:m->now+=NANO_RETIRE_US;break;
 case 5:s->io.read32=0;break;
 case 6:s->io.barrier=0;break;
 case 7:s->io.context=0;break;
 case 8:s->binding.owner++;break;
 case 9:s->phase=RET_ENTRY;break;
 case 10:s->io.prepared=0;break;
 default:assert(0);
 }
 return e;
}

static void transition_case(unsigned phase,unsigned fault,unsigned position)
{
 struct model m;setup(&m);struct nano_retire s={0};struct nano_retire_receipt r={0};m.current=&s;
 target_phase=phase;mode=fault;at=position;seen=injected=0;
 target_writes=phase==RET_DISABLE ? 5:phase;
 target_poll=phase==RET_DISABLE ? 4:phase;
 struct nano_retire_io io={&m,owns,source,time_us,transition_read,write32,store,barrier,prepared_queue(),0};
 int e=nano_source_retire(&s,binding(),io,&r);
 if(!fault){assert(!e && s.retired && seen==22 && m.written==5);return;}
 assert(injected && e && s.failed && !s.retired && m.written==target_writes);
 assert(!r.translation_retired && r.memory_retained && !r.source_reuse_authorized &&
        !r.interconnect_drained && !r.ordinary_activation);
 unsigned reads=m.reads,stores=m.stores,writes=m.written;
 assert(nano_source_retire(&s,binding(),io,&r));
 assert(m.reads==reads && m.stores==stores && m.written==writes);
}

int main(void)
{
 const unsigned phases[]={RET_ABORT,RET_NO_TRANSLATION,RET_COMMAND_ONLY,RET_DISABLE};
 unsigned cases=0;
 for(unsigned n=0;n<4;n++){
  transition_case(phases[n],0,0);cases++;
  for(unsigned fault=1;fault<=10;fault++)for(unsigned position=1;position<=22;position++){
   transition_case(phases[n],fault,position);cases++;
  }
 }
 printf("{\"status\":\"passed\",\"hardware_actions\":false,\"cases\":%u,\"phases\":4,\"read_positions\":22,\"fault_modes\":10}\n",cases);
}
