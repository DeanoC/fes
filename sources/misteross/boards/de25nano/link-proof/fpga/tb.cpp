#include "Vfes_link_sim.h"
#include "verilated.h"
#include <cstdint>
#include <deque>
#include <iostream>
#include <stdexcept>

static void need(bool ok, const char *why) { if (!ok) throw std::runtime_error(why); }
struct Reply { uint64_t cycle; uint32_t data; };
class Bench {
public:
    Vfes_link_sim d;
    uint64_t cycles = 0;
    uint32_t count = 0, checked = 0;
    bool monitor = true;
    std::deque<Reply> replies;
    explicit Bench(bool reset_already_released = false) {
        d.clk = 0; d.reset_n = reset_already_released; d.persona_b = 0; d.address = 0;
        d.read = 0; d.write = 0; d.byteenable = 15; d.writedata = 0;
        d.drop_response = 0; d.inject_response = reset_already_released;
        for (int n=0;n<4;n++) tick();
        d.inject_response = 0;
        d.reset_n = 1;
        for (int n=0;n<10;n++) tick();
        need(d.frozen && d.slot_reset, "cold start must be frozen and held reset");
    }
    void tick() {
        d.clk=0;d.eval();
        const uint32_t old_retired=d.retired;
        if (monitor) {
            if (d.slot_reset) {
                need(replies.empty(), "slot reset asserted before accepted replies drained");
                count=0;
            }
            if (d.req) {
                count += d.persona_b ? 3 : 1;
                uint32_t word=d.persona_b ? 0x5a000000u ^ (uint32_t(d.slot_address)*0x00110011u)
                                         : 0xa5000000u ^ (uint32_t(d.slot_address)*0x01010101u);
                uint32_t id=d.persona_b ? 0xc002b002u : 0xc001a001u;
                replies.push_back({cycles,d.slot_address ? word ^ count ^ id : id});
            }
            if (d.frozen) {
                need(d.slot_reset && !d.req, "frozen outputs are not held");
                need(d.isolated_data==0, "frozen input is not isolated");
            }
        }
        d.clk=1;d.eval();
        if (monitor && d.retired!=old_retired) {
            need(!replies.empty(), "unsolicited reply retired");
            need(cycles-replies.front().cycle==4, "registered slot latency changed");
            need(d.last_data==replies.front().data, "ROM/counter/ID response differs");
            replies.pop_front();checked++;
        }
        cycles++;
    }
    uint32_t read32(unsigned addr) {
        d.address=addr;d.read=1;tick();
        need(d.readdatavalid,"read completion missing");
        uint32_t value=d.readdata;d.read=0;tick();return value;
    }
    void write32(unsigned addr,uint32_t value,unsigned enables=15,bool ok=true) {
        d.address=addr;d.writedata=value;d.byteenable=enables;d.write=1;tick();
        need(d.writeresponsevalid,"write completion missing");
        need((d.response==0)==ok,"write error response differs");
        d.write=0;d.byteenable=15;tick();
    }
    void command(unsigned opcode,unsigned seq,uint32_t argument) {
        write32(12,argument);write32(16,(opcode<<24)|seq);
    }
    void completed(unsigned seq) {
        for (int n=0;n<20;n++) {
            need(read32(20)==0,"unexpected command fault");
            if (read32(24)==seq) return;
        }
        throw std::runtime_error("command completion missing");
    }
    void fault(uint32_t wanted) {
        need((read32(20)&wanted)!=0,"expected fault absent");
        need((read32(8)&16)!=0 && !d.running && !d.frozen,"fault invented safe/running state");
    }
};
int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    uint32_t checked=0;
    {Bench b(true);need(b.read32(20)==0,"cold unknown PR state leaked before reset settling");}
    {
        Bench b;
        need(b.read32(0)==0x44325052 && b.read32(4)==0x00010000,"identity differs");
        const uint32_t build_id[]={0x11223344,0x55667788,0x99aabbcc,0xddeeff00};
        for(unsigned i=0;i<4;i++) need(b.read32(44+4*i)==build_id[i],"build ID order");
        b.command(1,1,0);b.completed(1);
        for(unsigned i=0;i<4;i++) {
            b.d.persona_b=i&1;
            b.command(2,2+2*i,(i&1)?0xc002b002:0xc001a001);b.completed(2+2*i);
            need(b.d.running && b.read32(40)==((i&1)?0xc002b002u:0xc001a001u),"persona probe differs");
            auto before=b.d.static_count;
            for(int n=0;n<83;n++) b.tick();
            b.command(1,3+2*i,0);b.completed(3+2*i);
            need(b.d.frozen && b.replies.empty(),"freeze acknowledged before drain");
            need(b.d.static_count>before+83,"static state stopped or reset");
        }
        checked=b.checked;
    }
    {Bench b;b.monitor=false;b.write32(12,0,7,false);b.fault(2);}
    {Bench b;b.monitor=false;b.write32(16,0x01000001,15,false);b.fault(8);}
    {Bench b;b.command(1,1,0);b.completed(1);b.monitor=false;b.write32(12,0);b.write32(16,0x01000001,15,false);b.fault(8);}
    {Bench b;b.monitor=false;b.command(2,1,0xc001a001);b.write32(12,0,15,false);b.fault(4);}
    {Bench b;b.monitor=false;b.d.persona_b=1;b.command(2,1,0xc001a001);for(int n=0;n<20;n++)b.tick();b.fault(64);}
    {Bench b;b.monitor=false;b.d.drop_response=1;b.command(2,1,0xc001a001);for(int n=0;n<70;n++)b.tick();b.fault(32);need(b.read32(24)==0,"timeout acknowledged command");}
    {Bench b;b.monitor=false;b.command(2,1,0xc001a001);b.completed(1);b.d.inject_response=1;for(int n=0;n<3;n++)b.tick();b.fault(128);}
    {Bench b;b.d.inject_response=1;for(int n=0;n<20;n++)b.tick();need(b.d.frozen && b.read32(20)==0,"frozen PR output noise was consumed");b.d.inject_response=0;b.command(2,1,0xc001a001);b.completed(1);}
    {Bench b;b.monitor=false;b.d.reset_n=0;b.d.eval();need(!b.d.frozen && !b.d.running,"stopped-clock reset did not revoke state");b.d.reset_n=1;for(int n=0;n<4;n++)b.tick();b.fault(1);b.write32(12,0,15,false);}
    {Bench b;b.monitor=false;need(b.read32(3)==0,"invalid address data");b.fault(16);}
    std::cout<<"PASS: HPS CSR A/B/A/B, "<<checked<<" four-cycle ROM/ID replies; drain/reset, sequence, busy, partial, mismatch, timeout, reset and unsolicited faults\n";
}
