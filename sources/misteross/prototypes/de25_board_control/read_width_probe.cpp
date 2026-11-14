// SPDX-License-Identifier: GPL-3.0-or-later
// Exercise the actual full-width boundary under independent response stalls.
#include "Vfes_de25_read_width.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>
static void require(bool value,const char *message){if(!value)throw std::runtime_error(message);}
struct Bench {
    Vfes_de25_read_width d;
    Bench(){
        d.clk=0;d.reset_n=0;d.reader_arvalid=0;d.reader_araddr=0x95000000;
        d.reader_arlen=1;d.reader_arsize=4;d.reader_arburst=1;d.reader_arid=0;
        d.reader_rready=0;d.ddr_arready=0;d.ddr_rvalid=0;d.ddr_rlast=0;d.ddr_rresp=0;d.ddr_rid=0;
        for(unsigned i=0;i<8;i++)d.ddr_rdata[i]=0;
        tick();tick();d.reset_n=1;d.eval();
    }
    void tick(){d.clk=0;d.eval();d.clk=1;d.eval();d.clk=0;d.eval();}
    void data(uint32_t start,bool last,unsigned response=0,unsigned id=0){
        for(unsigned i=0;i<8;i++)d.ddr_rdata[i]=start+i;
        d.ddr_rvalid=1;d.ddr_rlast=last;d.ddr_rresp=response;d.ddr_rid=id;d.eval();
    }
    void expect(uint32_t start,bool last,unsigned response,unsigned id){
        require(d.reader_rvalid && d.reader_rlast==last && d.reader_rresp==response && d.reader_rid==id,
            "response metadata changed or disappeared");
        for(unsigned i=0;i<4;i++)require(d.reader_rdata[i]==start+i,"response words lost, repeated or reordered");
    }
};
int main(){try{
    for(unsigned length:{1U,3U,255U}){
        Bench b;b.d.reader_arvalid=1;b.d.reader_arlen=length;b.d.eval();
        for(unsigned i=0;i<8;i++){
            require(b.d.ddr_arvalid && b.d.ddr_araddr==0x95000000 && b.d.ddr_arlen==(length>>1) && !b.d.reader_arready,
                "held full-width address changed");b.tick();
        }
        b.d.ddr_arready=1;b.d.eval();require(b.d.reader_arready,"address acceptance not propagated");b.tick();
    }
    for(unsigned bad=0;bad<6;bad++){
        Bench b;b.d.reader_arvalid=1;b.d.ddr_arready=1;
        if(bad==0)b.d.reader_araddr+=16;
        if(bad==1)b.d.reader_arlen=0;
        if(bad==2)b.d.reader_arlen=254;
        if(bad==3)b.d.reader_arsize=3;
        if(bad==4)b.d.reader_arburst=0;
        if(bad==5)b.d.reader_arid=1;
        b.d.eval();require(!b.d.ddr_arvalid && !b.d.reader_arready,"invalid descriptor reached HPS");
    }
    {
        Bench b;b.data(0x1000,true,2,1);
        for(unsigned i=0;i<8;i++){b.expect(0x1000,false,2,1);require(!b.d.ddr_rready,"stalled lower half accepted");b.tick();}
        b.d.reader_rready=1;b.d.eval();require(b.d.ddr_rready,"lower half never accepted");b.tick();
        b.d.ddr_rvalid=0;b.d.reader_rready=0;b.d.eval();
        for(unsigned i=0;i<8;i++){b.expect(0x1004,true,2,1);b.tick();}
        b.data(0x2000,true);
        for(unsigned i=0;i<8;i++){b.expect(0x1004,true,2,1);require(!b.d.ddr_rready,"new response overwrote held upper half");b.tick();}
        b.d.reader_rready=1;b.d.eval();b.tick();
        b.expect(0x2000,false,0,0);require(b.d.ddr_rready,"next raw response hidden while empty");b.tick();
        b.d.ddr_rvalid=0;b.d.eval();b.expect(0x2004,true,0,0);b.tick();
        require(!b.d.reader_rvalid,"completed upper half replayed");
    }
    {
        Bench b;unsigned wide=0,narrow=0,lasts=0;
        for(unsigned cycle=0;cycle<2048 && narrow<256;cycle++){
            if(wide<128)b.data(wide*8,wide==127);else b.d.ddr_rvalid=0;
            b.d.reader_rready=cycle%3!=1;b.d.eval();
            bool accepted=b.d.ddr_rvalid && b.d.ddr_rready;
            if(b.d.reader_rvalid && b.d.reader_rready){
                b.expect(narrow*4,narrow==255,0,0);lasts+=b.d.reader_rlast;narrow++;
            }
            b.tick();if(accepted)wide++;
        }
        require(wide==128 && narrow==256 && lasts==1,"full-page response accounting differs");
        b.d.ddr_rvalid=0;b.d.eval();require(!b.d.reader_rvalid,"full page left a buffered half");
    }
    {
        Bench b;b.data(0x3000,true,3,1);
        require(b.d.reader_rvalid && !b.d.reader_rready && !b.d.ddr_rready,
            "unsolicited raw RVALID hidden from endpoint fault/drain checks");
        b.d.reader_rready=1;b.d.eval();b.expect(0x3000,false,3,1);b.tick();
        b.d.ddr_rvalid=0;b.d.reader_rready=0;b.d.eval();b.expect(0x3004,true,3,1);
        for(unsigned i=0;i<8;i++){b.tick();b.expect(0x3004,true,3,1);}
        b.d.reader_rready=1;b.tick();require(!b.d.reader_rvalid,"unsolicited response half replayed");
    }
    std::cout<<"{\"status\":\"passed\",\"groups\":4,\"invalid_descriptors\":6,\"page_bytes\":4096,\"hps_data_bits\":256,\"reader_data_bits\":128}\n";
    return 0;
}catch(const std::exception &e){std::cerr<<"read width: "<<e.what()<<"\n";return 1;}}
