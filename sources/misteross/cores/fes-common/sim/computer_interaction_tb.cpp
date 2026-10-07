// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vcomputer_interaction_sim_top.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>
static void require(bool ok,const char* message){if(!ok){std::cerr<<message<<"\n";std::exit(1);}}
struct Bench {
 Vcomputer_interaction_sim_top d;
 bool toggle=false, read_seen=false; unsigned read_delay=0, reads=0;
 std::vector<uint8_t> memory=std::vector<uint8_t>(737280);
 Bench(){d.clk=0;d.gpo=0;d.allow_mouse=1;d.media_write_busy=0;d.media_changed=0;d.media_read_ready=0;d.media_read_data=0;d.ac_req=0;d.ac_reg=0;d.ac_write=0;d.ac_wdata=0;d.eval();for(int i=0;i<5;++i)tick();}
 void tick(){
  d.clk=0;d.eval();
  if(!d.media_read_req){read_seen=false;read_delay=0;d.media_read_ready=0;}
  else if(!read_seen){if(++read_delay==3){read_seen=true;d.media_read_ready=1;d.media_read_data=memory.at(d.media_read_addr);++reads;}}
  if(d.media_write_enable&1)memory.at(d.media_write_addr)=uint8_t(d.media_write_data);
  if(d.media_write_enable&2)memory.at(d.media_write_addr+1)=uint8_t(d.media_write_data>>8);
  d.eval();d.clk=1;d.eval();d.clk=0;d.eval();
 }
 void begin(unsigned op,unsigned index=0,unsigned arg=0){toggle=!toggle;d.gpo=(toggle?0x80000000u:0)|(op<<24)|(index<<16)|arg;}
 bool ack()const{return bool(d.gpi&0x00800000)==toggle;}
 unsigned finish(bool error=false){for(int i=0;!ack();++i){require(i<10000,"GP ACK timeout");tick();}if(bool(d.gpi&0x00400000)!=error){std::cerr<<"GPO "<<std::hex<<d.gpo<<" GPI "<<d.gpi<<" expected error "<<error<<"\n";}require(bool(d.gpi&0x00400000)==error,"GP error mismatch");return d.gpi&65535;}
 unsigned gp(unsigned op,unsigned index=0,unsigned arg=0,bool error=false){begin(op,index,arg);return finish(error);}
 unsigned ac(bool reg,bool write=false,unsigned data=0){d.ac_req=1;d.ac_reg=reg;d.ac_write=write;d.ac_wdata=data;for(int i=0;!d.ac_ack;++i){require(i<10,"ACIA ACK timeout");tick();}unsigned r=d.ac_rdata;d.ac_req=0;tick();return r;}
 unsigned byte(){for(int i=0;!(ac(false)&1);++i)require(i<10000,"ACIA receive timeout");return ac(true);}
};
int main(int argc,char**argv){Verilated::commandArgs(argc,argv);Bench b;
 require(b.gp(11,1,0x807f,true)==4,"held mouse accepted");b.gp(2,0,1);b.ac(false,true,0x96);
 b.d.allow_mouse=0;b.begin(11,1,0xfb05);for(int i=0;i<20;++i)b.tick();require(!b.ack()&&b.d.mouse_accepts==0,"backpressure ACK/motion");b.d.allow_mouse=1;b.finish();for(int i=0;i<20;++i)b.tick();require(b.d.mouse_accepts==1,"held GPO duplicated mouse");
 require(b.byte()==0xfa&&b.byte()==5&&b.byte()==251,"GP to IKBD to ACIA mouse vector");b.gp(11,0,0);require(b.byte()==0xf8&&b.byte()==0&&b.byte()==0,"button release");require(b.gp(11,4,1,true)==2,"bad buttons");
 // Four-byte image CRC for 01 02 03 04 is b63cfbcd.
 b.gp(6,0,4);b.gp(6,1,0);b.gp(6,2,0xfbcd);b.gp(6,3,0xb63c);b.gp(7,0,0);b.gp(7,1,0);b.gp(7,2,4);b.gp(8,0,0x0201);b.gp(8,1,0x0403);b.gp(9);
 require(b.d.unit0_state==3,"image not ready");b.d.media_changed=1;b.tick();b.d.media_changed=0;
 b.begin(13);for(int i=0;!b.d.media_frozen;++i){require(i<20,"freeze did not latch");b.tick();}require(!b.ack(),"Freeze ACK raced first-edge writer");b.d.media_write_busy=1;for(int i=0;i<20;++i)b.tick();require(b.d.media_frozen&&!b.ack()&&!b.d.exec_reset,"freeze did not block new writer and drain");b.d.media_write_busy=0;b.finish();require(b.gp(12)==7,"snapshot flags");require(b.gp(12,7)==1,"snapshot epoch");
 require(b.gp(10,0,0,true)==4,"unsaved frozen eject accepted");require(b.gp(6,0,4,true)==4,"unsaved frozen replacement accepted");
 require(b.gp(14,1,0,true)==2,"out of order header");b.gp(14,0,0);b.gp(14,1,0);require(b.gp(14,2,3,true)==3,"odd length accepted");b.gp(14,2,4);
 require(b.gp(15,1,0,true)==2&&b.reads==0,"bad ordinal caused RAM side effect");require(b.gp(15,0,1,true)==3&&b.reads==0,"bad argument caused RAM side effect");
 require(b.gp(15)==0x0201,"snapshot low byte order");for(int i=0;i<15;++i)b.tick();require(b.reads==2,"held snapshot request duplicated read");require(b.gp(15,1)==0x0403&&b.reads==4,"second snapshot word");
 b.gp(13,0,2);require(b.d.media_frozen&&b.gp(12)==5,"Saved released freeze or kept dirty");b.gp(10);require(!b.d.media_frozen&&b.d.unit0_state==1,"saved destruction not atomic");b.gp(13,0,1);b.gp(13,0,1);require(!b.d.media_frozen,"Resume not idempotent");
 std::cout<<"computer interaction: mouse GP/IKBD/ACIA and snapshot freeze/read/save PASS\n";
}
