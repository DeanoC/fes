#include "Vcontract_top.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
#include <map>
Vcontract_top d;
long halfedge=0,last_read=-1,last_write=-1,last_data_change=0,last_oe_change=0;
unsigned old_data=0,old_oe=0,reads=0,writes=0,retired=0;
std::map<unsigned,long> tags;
void require(bool ok,const char* msg){if(!ok){fprintf(stderr,"FAIL %s edge %ld\n",msg,halfedge);exit(1);}}
void step(){
 ++halfedge;
 bool rise=halfedge%2==0;
 unsigned state=d.state,wait=d.wait_count,staged=d.staged;
 if(!rise){unsigned tag=(halfedge/2)%60000+1;d.dq_sample=tag;tags[tag]=halfedge;}
 d.clk=rise;d.eval();
 if(d.dq_out!=old_data){last_data_change=halfedge;old_data=d.dq_out;}
 if(d.dq_oe!=old_oe){
  if(!d.dq_oe && last_write>=0)printf("{\"event\":\"OE_RELEASE\",\"edge\":%ld,\"after_write_halfcycles\":%ld}\n",halfedge,halfedge-last_write);
  if(d.dq_oe && last_read>=0)printf("{\"event\":\"OE_ASSERT\",\"edge\":%ld,\"after_read_halfcycles\":%ld}\n",halfedge,halfedge-last_read);
  last_oe_change=halfedge;old_oe=d.dq_oe;
 }
 if(!rise && d.sdram_cke && d.sdram_nras && !d.sdram_ncas){
  if(d.sdram_nwe){require(!d.dq_oe,"READ command overlaps FPGA output enable");last_read=halfedge;++reads;printf("{\"event\":\"READ\",\"edge\":%ld}\n",halfedge);}
  else{last_write=halfedge;++writes;require(d.dq_oe,"WRITE without OE");require(halfedge-last_data_change>=7,"DQ not held 3 cycles before WRITE");require(halfedge-last_oe_change>=7,"OE not held 3 cycles before WRITE");printf("{\"event\":\"WRITE\",\"edge\":%ld,\"data_stable_halfcycles\":%ld,\"oe_stable_halfcycles\":%ld}\n",halfedge,halfedge-last_data_change,halfedge-last_oe_change);}
 }
 if(rise && state==12 && d.state==13){
  ++retired;if(d.consumed!=staged)fprintf(stderr,"DEBUG before_staged=%u after_consumed=%u after_staged=%u captured=%u state=%u wait=%u\n",staged,d.consumed,d.staged,d.captured,d.state,d.wait_count);require(d.consumed==staged,"rdata did not select old controller input stage");require(tags.count(staged),"consumed sample has no pin tag");
  long selected=tags[staged];require(selected-last_read==6,"unexpected selected pin-capture edge");require(halfedge-last_read==9,"unexpected rdata consumption edge");
  printf("{\"event\":\"CONSUME\",\"edge\":%ld,\"read_edge\":%ld,\"selected_capture_edge\":%ld,\"sample_delay_halfcycles\":%ld,\"consume_delay_halfcycles\":%ld}\n",halfedge,last_read,selected,selected-last_read,halfedge-last_read);
 }
}
void cycle(){step();step();}
void await_ready(){for(int n=0;n<30000 && !d.memory_ready;++n)cycle();require(d.memory_ready,"ready timeout");}
void request(bool write,unsigned value){await_ready();d.memory_valid=1;d.write_cycle=write;d.wdata=value;d.address=0x100;bool done=false;for(int n=0;n<200;++n){cycle();if(d.memory_done){if(!write)require(d.memory_rdata==(d.consumed & 255),"frontend byte differs from selected pin tag");done=true;break;}}require(done,"request timeout");d.memory_valid=0;cycle();}
int main(int argc,char**argv){Verilated::commandArgs(argc,argv);d.clk=0;d.cold_reset=1;d.reset=0;d.memory_valid=0;d.eval();for(int i=0;i<4;++i)cycle();d.cold_reset=0;
 for(unsigned x=0;x<256;++x){request(true,x);request(false,0);}
 require(reads==256 && retired==256 && writes==257,"nonvacuity counts");printf("{\"status\":\"PASS\",\"writes\":%u,\"reads\":%u,\"read_consumptions\":%u}\n",writes,reads,retired);}
