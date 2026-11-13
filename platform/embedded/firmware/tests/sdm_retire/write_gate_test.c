/* SPDX-License-Identifier: BSD-3-Clause */
#include "source_retire.h"
#include <assert.h>
#include <stdio.h>
#include <string.h>
struct model {uint32_t reg[48],writes[8][2];uint64_t now;unsigned owners,times,reads,written,stores,source;
 struct nano_retire *current;unsigned inject_next,inject_offset,injected,last_read;
 unsigned fail_owner,fail_time,fail_read,fail_write,fail_store,late_write,back_time;};
static unsigned index_of(uint32_t o){if(o==0x100a8)return 44;if(o==0x100ac)return 45;assert(o<=0xb4 && !(o&3));return o/4;}
static int owns(void *p,const struct nano_sdm_input_binding *b){struct model *m=p;
 static const unsigned phases[]={RET_ABORT,RET_NO_TRANSLATION,RET_COMMAND_ONLY,RET_INVALIDATE,RET_DISABLE};
 if(m->inject_next && !m->injected && m->current->phase==phases[m->inject_next-1] &&
    m->written==m->inject_next-1 && (m->current->phase==RET_ABORT || m->last_read==0x9c)){
  m->reg[index_of(m->inject_offset)]^=1U;m->injected=1;
 }
 return ++m->owners!=m->fail_owner && b->owner==17 && b->epoch==7 && b->generation==4;}
static int source(void *p,const struct nano_sdm_input_binding *b){(void)b;struct model *m=p;m->source++;return 1;}
static int time_us(void *p,uint64_t *v){struct model *m=p;m->times++;if(m->times==m->fail_time)return -5;*v=m->times==m->back_time ? 0:++m->now;return 0;}
static int read32(void *p,uint32_t o,uint32_t *v){struct model *m=p;m->last_read=o;if(++m->reads==m->fail_read)return -5;unsigned n=index_of(o);if(o==0x44)m->reg[n]&=~0x80000000U;if(o==0x9c && m->reg[0x98/4]==12 && !(m->injected && m->inject_offset==o))m->reg[n]=12;*v=m->reg[n];return 0;}
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

int main(void){
 static const uint32_t offsets[]={0,4,0x14,0x28,0x2c,0x48,0x50,0x54,0x60,0x64,0x80,0x84,0x88,0x90,0x94,0xa0,0xa4,0x100a8,0x100ac,0x44,0x20,0x24,0x98,0x9c};
 unsigned total=0;
 for(unsigned next=1;next<=5;next++)for(unsigned i=0;i<sizeof(offsets)/sizeof(offsets[0]);i++){
  struct model m;setup(&m);struct nano_retire s={0};struct nano_retire_receipt r={0};
  m.current=&s;m.inject_next=next;m.inject_offset=offsets[i];
  struct nano_retire_io io={&m,owns,source,time_us,read32,write32,store,barrier};
  int error=nano_source_retire(&s,binding(),io,&r);
  if(!error || !m.injected || m.written!=next-1)fprintf(stderr,"WRITE_GATE next=%u offset=%x injected=%u writes=%u error=%d phase=%u first=%d\n",next,offsets[i],m.injected,m.written,error,s.phase,s.first_error);
  assert(error && m.injected && m.written==next-1 && s.writes==next-1 && s.failed && !s.retired);
  assert(!r.translation_retired && r.memory_retained && !r.source_reuse_authorized && !r.ordinary_activation);
  unsigned before=m.written;assert(nano_source_retire(&s,binding(),io,&r));assert(m.written==before);total++;
 }
 printf("{\"status\":\"passed\",\"cases\":%u,\"control_write_boundaries\":5,\"registers_each\":24,\"hardware_actions\":false}\n",total);
}
