/* SPDX-License-Identifier: BSD-3-Clause */
#include "source_retire.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>
struct model {uint32_t reg[48],writes[8][2];uint64_t now;unsigned owners,times,reads,written,stores,source;
 unsigned fail_owner,fail_time,fail_read,fail_write,fail_store,late_write,back_time;
 struct nano_retire *current;unsigned command_fault,injected,edge_reads,edge_stores,edge_writes;};
static unsigned index_of(uint32_t o){if(o==0x100a8)return 44;if(o==0x100ac)return 45;assert(o<=0xb4 && !(o&3));return o/4;}
static int owns(void *p,const struct nano_sdm_input_binding *b){struct model *m=p;return ++m->owners!=m->fail_owner && b->owner==17 && b->epoch==7 && b->generation==4;}
static int source(void *p,const struct nano_sdm_input_binding *b){(void)b;struct model *m=p;m->source++;return 1;}
static int time_us(void *p,uint64_t *v){struct model *m=p;m->times++;if(m->times==m->fail_time)return -5;*v=m->times==m->back_time ? 0:++m->now;
 if(m->command_fault && !m->injected && m->current->phase==RET_INVALIDATE &&
    m->current->commands.submitted && !m->current->commands.timed){
  struct nano_retire *s=m->current;m->injected=1;
  m->edge_reads=m->reads;m->edge_stores=m->stores;m->edge_writes=m->written;
  switch(m->command_fault){
  case 1:s->io.context=0;break;case 2:s->io.owns=0;break;
  case 3:s->io.source_valid=0;break;case 4:s->io.time_us=0;break;
  case 5:s->io.read32=0;break;case 6:s->io.write32=0;break;
  case 7:s->io.store_command=0;break;case 8:s->io.barrier=0;break;
  case 9:m->fail_owner=m->owners+1;break;
  case 10:s->phase=RET_DISABLE;break;case 11:s->binding.owner++;break;
  case 12:*v=s->start+NANO_RETIRE_US;break;case 13:*v=0;break;
  case 14:return -5;default:assert(0);
  }
 }
 return 0;}
static int read32(void *p,uint32_t o,uint32_t *v){struct model *m=p;if(++m->reads==m->fail_read)return -5;unsigned n=index_of(o);if(o==0x44)m->reg[n]&=~0x80000000U;if(o==0x9c && m->reg[0x98/4]==12)m->reg[n]=12;*v=m->reg[n];return 0;}
static int write32(void *p,uint32_t o,uint32_t v){struct model *m=p;unsigned n=++m->written;assert(n<=5);m->writes[n-1][0]=o;m->writes[n-1][1]=v;if(n==m->fail_write && !m->late_write)return -5;m->reg[index_of(o)]=v;if(o==0x20)m->reg[0x24/4]=v;return n==m->fail_write ? -5:0;}
static int store(void *p,unsigned slot,const uint8_t b[16]){struct model *m=p;unsigned n=m->stores++;uint8_t expected[16];assert(n<6 && slot==((6+n)&7) && !nano_cmdq_encode(n,expected) && !memcmp(b,expected,16));return m->stores==m->fail_store ? -5:0;}
static void barrier(void *p){(void)p;}
static void setup(struct model *m){memset(m,0,sizeof(*m));m->reg[0]=0x080f7e3f;m->reg[1]=0x0e739d18;m->reg[5]=0x400075;m->reg[8]=m->reg[9]=13;m->reg[10]=0x820;m->reg[11]=4;m->reg[0x80/4]=0x94000000;m->reg[0x88/4]=8;m->reg[0x90/4]=0x94008003;m->reg[0x98/4]=m->reg[0x9c/4]=6;m->reg[0xa0/4]=0x94009003;}
static struct nano_sdm_input_binding binding(void){struct nano_sdm_input_binding b={.owner=17,.epoch=7,.generation=4,.bytes=1708032,.chunk=1048512,.idr0=0x080f7e3f,.idr1=0x0e739d18,.idr5=0x400075};memset(b.digest384,1,48);return b;}
static unsigned cases;
static struct model run(unsigned kind,unsigned at){struct model m;setup(&m);struct nano_retire s={0};struct nano_retire_receipt r={0};
 if(kind==1)m.fail_read=at;if(kind==2)m.fail_write=at;if(kind==3){m.fail_write=at;m.late_write=1;}if(kind==4)m.fail_store=at;if(kind==5)m.fail_owner=at;if(kind==6)m.fail_time=at;if(kind==7)m.back_time=at;
 struct nano_retire_io io={&m,owns,source,time_us,read32,write32,store,barrier};int e=nano_source_retire(&s,binding(),io,&r);
 if(!kind || (kind==7 && at==1)){assert(!e && s.retired && r.translation_retired && r.memory_retained && !r.source_reuse_authorized && !r.interconnect_drained && !r.ordinary_activation);assert(m.written==5 && m.stores==6);
 static const uint32_t expected[5][2]={{0x44,0x80100000},{0x20,12},{0x20,8},{0x98,12},{0x20,0}};assert(!memcmp(m.writes,expected,sizeof(expected)));}
 else {if(!e || !s.failed)fprintf(stderr,"kind=%u at=%u e=%d failed=%u retired=%u\n",kind,at,e,s.failed,s.retired);assert(e && s.failed && !s.retired && !r.translation_retired && r.memory_retained && !r.source_reuse_authorized && !r.ordinary_activation);}
 unsigned before=m.written;assert(nano_source_retire(&s,binding(),io,&r));assert(m.written==before);cases++;return m;}
static void command_counter_edges(void){
 for(unsigned kind=1;kind<=14;kind++){
  struct model m;setup(&m);struct nano_retire s={0};struct nano_retire_receipt r={0};
  m.current=&s;m.command_fault=kind;
  struct nano_retire_io io={&m,owns,source,time_us,read32,write32,store,barrier};
  assert(nano_source_retire(&s,binding(),io,&r) && m.injected && s.failed && !s.retired);
  assert(m.edge_writes==3 && !m.edge_stores && m.reads==m.edge_reads &&
    m.stores==m.edge_stores && m.written==m.edge_writes && !s.commands.publish_attempted);
  assert(r.memory_retained && !r.translation_retired && !r.source_reuse_authorized && !r.ordinary_activation);
  assert(nano_source_retire(&s,binding(),io,&r) && m.written==m.edge_writes && !m.stores);
  cases++;
 }
}
int main(void){struct model measured=run(0,0);
 for(unsigned n=1;n<=measured.reads;n++)run(1,n);
 for(unsigned n=1;n<=measured.written;n++){run(2,n);run(3,n);}
 for(unsigned n=1;n<=6;n++)run(4,n);
 for(unsigned kind=5;kind<=7;kind++){unsigned total=kind==5 ? measured.owners:measured.times;for(unsigned n=1;n<=total;n++)run(kind,n);}
 command_counter_edges();
 printf("{\"status\":\"passed\",\"cases\":%u,\"command_counter_edge_cases\":14,\"reads_in_success\":%u,\"owners_in_success\":%u,\"times_in_success\":%u,\"hardware_actions\":false}\n",cases,measured.reads,measured.owners,measured.times);}
