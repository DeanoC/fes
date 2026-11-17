#include "Vpipeline_top.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
Vpipeline_top d;
long tick=0,last_read=-10000;
unsigned reads=0,writes=0,consumes=0,word=0;
void require(bool ok,const char* msg){if(!ok){fprintf(stderr,"FAIL %s tick=%ld read=%ld\n",msg,tick,last_read);exit(1);}}
// One tick = 125 ps. All clocks remain 100 MHz: core 0, forward +1.25 ns,
// capture +8.125 ns. SDRAM_CLK rises on the forwarded clock's falling edge.
void step(){
 ++tick;long age=tick-last_read;
 // The first CL2 word is guaranteed only from launch+6 ns until the next
 // launch+2.5 ns. Poison all other pin times, including later CAS words.
 d.dq_sample=(age>=208 && age<260)?word:((tick*173+0x6543)&65535);
 unsigned oldclk=d.clk,oldpin=d.sdram_clk,oldstate=d.state;
 d.clk=(tick%80)<40;
 d.clk_forward=((tick+80-10)%80)<40;
 d.clk_capture=((tick+80-65)%80)<40;
 d.eval();
 if(!oldpin && d.sdram_clk && d.sdram_cke && d.sdram_nras && !d.sdram_ncas){
  if(d.sdram_nwe){require(!d.dq_oe,"READ overlaps OE");last_read=tick;word=(((reads/2)&255)<<8)|(((reads/2)^0xa5)&255);++reads;}
  else{require(d.dq_oe,"WRITE without OE");++writes;}
 }
 if(!oldclk && d.clk && oldstate==12 && d.state==13){
  ++consumes;
  require(tick-last_read==350,"changed controller read deadline");
  require(d.consumed==word,"read deadline selected wrong pin window");
 }
}
void cycle(){for(int i=0;i<80;++i)step();}
void ready(){for(int n=0;n<30000 && !d.memory_ready;++n)cycle();require(d.memory_ready,"ready timeout");}
void request(unsigned odd){
 ready();d.memory_valid=1;d.write_cycle=0;d.address=0x100+odd;
 bool done=false;for(int n=0;n<200;++n){cycle();if(d.memory_done){require(d.memory_rdata==((word>>(odd?8:0))&255),"frontend lane mismatch");done=true;break;}}
 require(done,"request timeout");d.memory_valid=0;cycle();
}
int main(int argc,char**argv){Verilated::commandArgs(argc,argv);d.clk=1;d.clk_forward=0;d.clk_capture=0;d.cold_reset=1;d.reset=0;d.memory_valid=0;d.eval();for(int i=0;i<4;++i)cycle();d.cold_reset=0;
 for(unsigned i=0;i<512;++i)request(i&1);
 require(reads==512 && consumes==512 && writes==1,"nonvacuity counts");
 printf("{\"status\":\"PASS\",\"reads\":%u,\"consumptions\":%u,\"warm_flag_writes\":%u,\"controller_deadline_ps\":43750,\"pin_valid_start_ps\":26000,\"pin_valid_end_ps\":32500,\"selected_capture_ps\":31875,\"handoff_ps\":38750}\n",reads,consumes,writes);
}
