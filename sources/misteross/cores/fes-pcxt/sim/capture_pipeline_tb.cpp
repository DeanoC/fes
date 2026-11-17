#include "Vpipeline_top.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
Vpipeline_top d;
long tick=0,last_read=-10000;
unsigned reads=0,writes=0,consumes=0,latches=0,landings=0,word=0;
void require(bool ok,const char* msg){if(!ok){fprintf(stderr,"FAIL %s tick=%ld read=%ld\n",msg,tick,last_read);exit(1);}}
// One tick = 125 ps. All clocks remain 100 MHz: core 0, forward +1.250 ns,
// capture +9.375 ns, handoff +8.750 ns. SDRAM_CLK uses DDR forwarding.
void step(){
 ++tick;long age=tick-last_read;
 // Logical input contract: only the selected capture edge carries this
 // transaction's first word. Physical tAC/tOH and routed clock insertion
 // are separate all-corner timing obligations, not a zero-delay chip model.
 d.dq_sample=(age>=264 && age<266)?word:((tick*173+0x6543)&65535);
 unsigned oldclk=d.clk,oldpin=d.sdram_clk,oldstate=d.state,oldhand=d.clk_handoff,enable=d.sample_enable;
 d.clk=(tick%80)<40;
 d.clk_forward=((tick+80-10)%80)<40;
 d.clk_capture=((tick+80-75)%80)<40;
 d.clk_handoff=((tick+80-70)%80)<40;
 d.eval();
 if(!oldhand && d.clk_handoff && enable){++latches;require(tick-last_read==340,"unexpected read-word handoff edge");require(d.staged==word,"handoff selected wrong pin word");}
 if(oldclk && !d.clk && tick-last_read==390){++landings;require(d.consumed==word,"landing selected wrong word");}
 if(!oldpin && d.sdram_clk && d.sdram_cke && d.sdram_nras && !d.sdram_ncas){
  if(d.sdram_nwe){require(!d.dq_oe,"READ overlaps OE");last_read=tick;word=(((reads/2)&255)<<8)|(((reads/2)^0xa5)&255);++reads;}
  else{require(d.dq_oe,"WRITE without OE");++writes;}
 }
 if(!oldclk && d.clk && oldstate==12 && d.state==13){
  ++consumes;
  require(tick-last_read==350,"changed controller read deadline");
  require(d.staged==word,"read deadline selected wrong pin window");
 }
}
void cycle(){for(int i=0;i<80;++i)step();}
void ready(){for(int n=0;n<30000 && !d.memory_ready;++n)cycle();require(d.memory_ready,"ready timeout");}
void request(unsigned odd){
 ready();d.memory_valid=1;d.write_cycle=0;d.address=0x100+odd;
 bool done=false;for(int n=0;n<200;++n){cycle();if(d.memory_done){require(tick-last_read==590,"changed frontend read deadline");require(d.memory_rdata==((word>>(odd?8:0))&255),"frontend lane mismatch");done=true;break;}}
 require(done,"request timeout");d.memory_valid=0;cycle();
}
int main(int argc,char**argv){Verilated::commandArgs(argc,argv);d.clk=1;d.clk_forward=0;d.clk_capture=0;d.clk_handoff=0;d.cold_reset=1;d.reset=0;d.memory_valid=0;d.eval();for(int i=0;i<4;++i)cycle();d.cold_reset=0;
 for(unsigned i=0;i<512;++i)request(i&1);
 require(reads==512 && consumes==512 && latches==512 && landings==512 && writes==1,"nonvacuity counts");
 printf("{\"status\":\"PASS\",\"reads\":%u,\"consumptions\":%u,\"warm_flag_writes\":%u,\"controller_state_deadline_ps\":43750,\"landing_ps\":48750,\"frontend_deadline_ps\":73750,\"pin_valid_start_ps\":33000,\"pin_valid_end_ps\":33250,\"selected_capture_ps\":33125,\"handoff_ps\":42500}\n",reads,consumes,writes);
}
