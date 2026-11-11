#include "Vfes_de25_ram_transport_test.h"
#include "verilated.h"
#include "generated/fes_de25_ramtest.hpp"
#include <array>
#include <cstdint>
#include <deque>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

static void require(bool condition, const char *message) {
    if (!condition) throw std::runtime_error(message);
}
using Word = std::array<uint32_t, 4>;
using Wide = std::array<uint32_t, 8>;
using namespace mister::native::generated;
struct Command { uint32_t address; unsigned size, beats, id, position=0, delay=0; bool error=false; };
struct WriteData { Wide data; uint32_t enables; bool last; };
struct Completion { unsigned id, delay; bool error; };

struct Rig {
    Vfes_de25_ram_transport_test d;
    std::vector<uint8_t> memory = std::vector<uint8_t>(65536);
    std::deque<Command> writes, reads;
    std::deque<WriteData> write_data;
    std::deque<Completion> completions;
    unsigned cycles=0, aw_count=0, ar_count=0, w_count=0, b_count=0, r_count=0;
    unsigned avalon_reads=0, avalon_writes=0, partial_writes=0, upper_writes=0, lower_writes=0;
    bool hold_b=false, hold_r=false, error_b=false, error_r=false, stray_r=false, corrupt_r=false;
    bool held_aw=false, held_ar=false, held_w=false;
    std::array<uint32_t,8> prior_aw{}, prior_ar{};
    std::array<uint32_t,10> prior_w{};
    unsigned aw_stalls=0, ar_stalls=0, w_stalls=0;
    bool last_wait=false, last_rvalid=false, last_bvalid=false;
    unsigned last_response=0;
    Word last_data{};

    explicit Rig(bool raw=false) {
        for (unsigned i=0;i<memory.size();++i) memory[i]=uint8_t(i*17+53);
        d.raw_mode=raw; d.reset_n=0; d.calibrated=1; d.memory_ready=1;
        d.address=0; d.read=0; d.write=0; d.byteenable=15; d.writedata=0;
        d.raw_address=0; d.raw_burstcount=1; d.raw_read=0; d.raw_write=0; d.raw_byteenable=0xffff;
        for (int n=0;n<4;++n) cycle();
        d.reset_n=1;
        for (int n=0;n<12;++n) cycle();
    }
    static uint8_t byte(const Wide &data, unsigned lane) { return data[lane/4] >> ((lane%4)*8); }
    void command_check(uint32_t address, unsigned size, unsigned beats, unsigned burst) {
        require(size<=5 && size>=4, "unexpected AXI transfer width");
        require(burst==1 && beats>0 && beats<=256, "AXI command is not bounded INCR");
        require(address% (1u<<size)==0, "unaligned AXI command");
        require((address&4095)+beats*(1u<<size)<=4096, "AXI burst crosses a 4K boundary");
        require(address+beats*(1u<<size)<=memory.size(), "AXI address escaped modeled allocation");
    }
    void cycle() {
        d.clk=0;
        d.axi_awready=d.reset_n && cycles%7!=0 && cycles%7!=1;
        d.axi_wready=d.reset_n && cycles%5!=0;
        d.axi_arready=d.reset_n && cycles%6!=0;
        d.axi_bvalid=d.reset_n && !hold_b && !completions.empty() && completions.front().delay==0;
        d.axi_bid=completions.empty()?0:completions.front().id;
        d.axi_bresp=completions.empty()?0:(completions.front().error?2:0);
        bool ordinary=d.reset_n && !hold_r && !reads.empty() && reads.front().delay==0;
        d.axi_rvalid=ordinary || (d.reset_n && stray_r);
        d.axi_rid=ordinary?reads.front().id:0;
        d.axi_rresp=ordinary && reads.front().error && reads.front().position==0?2:0;
        d.axi_rlast=!ordinary || reads.front().position+1==reads.front().beats;
        for (int n=0;n<8;++n) d.axi_rdata[n]=0;
        if (ordinary) {
            const auto &c=reads.front();
            uint32_t aligned=(c.address+(c.position<<c.size))&~31u;
            for (unsigned i=0;i<32;++i) d.axi_rdata[i/4]|=uint32_t(memory[aligned+i])<<((i%4)*8);
            if (corrupt_r && c.position==1) d.axi_rdata[((c.address+(c.position<<c.size))&31)/4]^=1;
        }
        d.eval();
        last_wait=d.raw_waitrequest; last_rvalid=d.raw_readdatavalid; last_bvalid=d.raw_writeresponsevalid;
        last_response=d.raw_response;
        for (unsigned n=0;n<4;++n) last_data[n]=d.raw_readdata[n];
        if (d.reset_n) {
            std::array<uint32_t,8> aw{d.axi_awaddr,d.axi_awid,d.axi_awlen,d.axi_awsize,d.axi_awburst,d.axi_awlock,d.axi_awprot,d.axi_awuser};
            std::array<uint32_t,8> ar{d.axi_araddr,d.axi_arid,d.axi_arlen,d.axi_arsize,d.axi_arburst,d.axi_arlock,d.axi_arprot,d.axi_aruser};
            std::array<uint32_t,10> w{};
            for (unsigned i=0;i<8;++i) w[i]=d.axi_wdata[i];
            w[8]=d.axi_wstrb;w[9]=d.axi_wlast;
            if (held_aw) require(d.axi_awvalid && aw==prior_aw,"AXI AW changed under backpressure");
            if (held_ar) require(d.axi_arvalid && ar==prior_ar,"AXI AR changed under backpressure");
            if (held_w) require(d.axi_wvalid && w==prior_w,"AXI W changed under backpressure");
            held_aw=d.axi_awvalid&&!d.axi_awready;held_ar=d.axi_arvalid&&!d.axi_arready;held_w=d.axi_wvalid&&!d.axi_wready;
            prior_aw=aw;prior_ar=ar;prior_w=w;
            aw_stalls+=held_aw;ar_stalls+=held_ar;w_stalls+=held_w;
            if (last_rvalid) ++avalon_reads;
            if (last_bvalid) ++avalon_writes;
            if (d.axi_awvalid && d.axi_awready) {
                command_check(d.axi_awaddr,d.axi_awsize,d.axi_awlen+1,d.axi_awburst);
                writes.push_back({d.axi_awaddr,d.axi_awsize,unsigned(d.axi_awlen+1),d.axi_awid,0,0,error_b});
                error_b=false; ++aw_count;
            }
            if (d.axi_arvalid && d.axi_arready) {
                command_check(d.axi_araddr,d.axi_arsize,d.axi_arlen+1,d.axi_arburst);
                reads.push_back({d.axi_araddr,d.axi_arsize,unsigned(d.axi_arlen+1),d.axi_arid,0,5,error_r});
                error_r=false; ++ar_count;
            }
            if (d.axi_wvalid && d.axi_wready) {
                WriteData w{};
                for (unsigned n=0;n<8;++n) w.data[n]=d.axi_wdata[n];
                w.enables=d.axi_wstrb; w.last=d.axi_wlast;
                write_data.push_back(w); ++w_count;
                if (w.enables!=0xffffffff) ++partial_writes;
                if (w.enables&0xffff0000) ++upper_writes;
                if (w.enables&0x0000ffff) ++lower_writes;
            }
            if (d.axi_bvalid && d.axi_bready) { completions.pop_front(); ++b_count; }
            if (ordinary && d.axi_rready) {
                auto &c=reads.front();
                if (corrupt_r && c.position==1) corrupt_r=false;
                ++r_count;
                if (++c.position==c.beats) reads.pop_front(); else c.delay=2;
            }
            while (!writes.empty() && !write_data.empty()) {
                auto &c=writes.front(); const auto w=write_data.front(); write_data.pop_front();
                require(w.last==(c.position+1==c.beats), "incorrect AXI WLAST");
                uint32_t addr=c.address+(c.position<<c.size), aligned=addr&~31u;
                uint32_t permitted=c.size==5?0xffffffffu:(0xffffu<<(addr&31));
                require((w.enables&~permitted)==0, "AXI WSTRB escaped the addressed transfer");
                for (unsigned i=0;i<32;++i) if (w.enables&(1u<<i)) memory[aligned+i]=byte(w.data,i);
                if (++c.position==c.beats) { completions.push_back({c.id,5,c.error}); writes.pop_front(); }
            }
            for (auto &c:completions) if (c.delay) --c.delay;
            if (!reads.empty() && reads.front().delay) --reads.front().delay;
        } else held_aw=held_ar=held_w=false;
        d.clk=1;d.eval();++cycles;
        require(cycles<400000, "simulation cycle bound exceeded");
    }
    void idle(unsigned count=1) { while (count--) cycle(); }
    void wr(unsigned address,uint32_t value,bool error=false) {
        d.address=address;d.writedata=value;d.write=1;cycle();
        require(d.writeresponsevalid,"missing endpoint CSR write completion");
        require((d.response!=0)==error,"incorrect endpoint CSR write response");
        d.write=0;cycle();
    }
    uint32_t rd(unsigned address) {
        d.address=address;d.read=1;cycle();
        require(d.readdatavalid && d.response==0,"missing endpoint CSR read completion");
        uint32_t result=d.readdata;d.read=0;cycle();return result;
    }
    void start() { wr(0x30,0xfe0);wr(0x34,512);wr(12,1); }
    void finish() {
        for (unsigned n=0;n<200000;++n) { if (!(rd(8)&1)) return; }
        throw std::runtime_error("endpoint did not reach idle");
    }
    void await_read() {
        for (unsigned n=0;n<5000 && reads.empty();++n) cycle();
        require(!reads.empty(),"no AXI read reached the generated transport");
    }
    void await_write_response() {
        for (unsigned n=0;n<5000 && completions.empty();++n) cycle();
        require(!completions.empty(),"no complete AXI write reached the transport");
    }
    void raw_write(uint32_t address,const std::vector<Word> &data,const std::vector<uint16_t> &enable) {
        auto before=avalon_writes;
        d.raw_address=address;d.raw_burstcount=data.size();d.raw_write=1;
        unsigned pos=0;
        for (unsigned n=0;n<10000 && pos<data.size();++n) {
            for (unsigned i=0;i<4;++i) d.raw_writedata[i]=data[pos][i];
            d.raw_byteenable=enable[pos];cycle();if (!last_wait) ++pos;
        }
        require(pos==data.size(),"raw Avalon write stalled indefinitely");
        d.raw_write=0;
        for (unsigned n=0;n<10000 && avalon_writes==before;++n) cycle();
        require(avalon_writes==before+1 && last_response==0,"raw Avalon write lost its completion");
    }
    std::vector<Word> raw_read(uint32_t address,unsigned count) {
        d.raw_address=address;d.raw_burstcount=count;d.raw_byteenable=0xffff;d.raw_read=1;
        std::vector<Word> data;
        for (unsigned n=0;n<10000;++n) {
            cycle();if (last_rvalid) { require(last_response==0,"raw read response error");data.push_back(last_data); }
            if (!last_wait) { d.raw_read=0;break; }
        }
        require(!d.raw_read,"raw Avalon read stalled indefinitely");
        for (unsigned n=0;n<10000 && data.size()<count;++n) {
            cycle();if (last_rvalid) { require(last_response==0,"raw read response error");data.push_back(last_data); }
        }
        require(data.size()==count,"raw Avalon read returned wrong beat count");return data;
    }
    void result(const char *name) {
        std::cout<<"{\"case\":\""<<name<<"\",\"status\":\"passed\",\"axi_aw\":"<<aw_count
                 <<",\"axi_ar\":"<<ar_count<<",\"axi_w_beats\":"<<w_count
                 <<",\"axi_r_beats\":"<<r_count<<",\"avalon_write_responses\":"<<avalon_writes
                 <<",\"avalon_read_beats\":"<<avalon_reads<<",\"aw_stalls\":"<<aw_stalls
                 <<",\"ar_stalls\":"<<ar_stalls<<",\"w_stalls\":"<<w_stalls<<"}\n";
    }
};

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv);
    try {
        if (argc==2 && std::string(argv[1])=="--unsolicited") {
            Rig r;r.stray_r=true;r.idle(40);
            throw std::runtime_error("unsolicited response did not trigger generated assertion");
        }
        {
            Rig r(true);
            for (auto address:{0x10u,0x20u,0xe80u}) {
                unsigned count=address==0xe80?16:2;
                std::vector<Word> data(count); std::vector<uint16_t> masks(count);
                auto expected=r.memory;
                for (unsigned n=0;n<count;++n) {
                    masks[n]=n%2?0x8421:0xffff;
                    for (unsigned i=0;i<16;++i) {
                        uint8_t value=uint8_t(address+n*37+i*11);
                        data[n][i/4]|=uint32_t(value)<<((i%4)*8);
                        if (masks[n]&(1u<<i)) expected[address+n*16+i]=value;
                    }
                }
                r.raw_write(address,data,masks);
                auto returned=r.raw_read(address,count);
                require(r.memory==expected,"transport wrote outside Avalon byte enables");
                for (unsigned n=0;n<count;++n) for (unsigned i=0;i<16;++i)
                    require(uint8_t(returned[n][i/4]>>((i%4)*8))==expected[address+n*16+i],"Avalon/AXI byte lane mismatch");
            }
            require(r.partial_writes && r.upper_writes && r.lower_writes,"did not exercise both halves and partial lanes");
            require(r.aw_stalls && r.ar_stalls && r.w_stalls,"did not exercise each AXI command/data stall");
            r.result("raw_width_lanes_and_stalls");
        }
        {
            Rig r;
            require(r.rd(FesDe25RamtestIdOffset)==FesDe25RamtestIdentity && r.rd(FesDe25RamtestChannelOffset)==FesDe25RamtestFpgaChannel,"wrong real endpoint identity");
            std::array<uint32_t,4> build_id{BUILD_ID_0,BUILD_ID_1,BUILD_ID_2,BUILD_ID_3};
            for (unsigned i=0;i<4;++i) require(r.rd(FesDe25RamtestBuildIdOffset+i*4)==build_id[i],"endpoint build identity differs from source receipt");
            require(!r.aw_count && !r.ar_count,"endpoint accessed memory before START");
            r.start();r.finish();require((r.rd(8)&15)==2,"real endpoint scan failed");
            r.wr(12,8);require(r.rd(0x64)==0,"scan reported data errors");
            require(r.rd(0x6c)==7*512 && r.rd(0x74)==7*512+1024*32,"scan accounting differs");
            require(r.rd(0xbc)==1024,"latency sampling was incomplete");
            require(r.partial_writes && r.upper_writes && r.lower_writes,"real endpoint omitted byte-enable pattern");
            r.result("endpoint_complete_scan");
        }
        {
            Rig r;r.corrupt_r=true;r.start();r.finish();r.wr(12,8);
            require(r.rd(FesDe25RamtestErrorsOffset)==1,"read corruption was not detected exactly once");
            require(r.rd(FesDe25RamtestFirstAddressOffset)==0xff0,"corruption mapped to the wrong Avalon address");
            require(r.rd(FesDe25RamtestExpectedOffset)==0 && r.rd(FesDe25RamtestActualOffset)==1,"first error data did not survive width adaptation");
            require(r.rd(FesDe25RamtestBadBitsOffset)==1,"bad bit mask lost through generated path");
            r.result("read_data_corruption");
        }
        {
            Rig r;r.error_r=true;r.start();r.finish();
            require((r.rd(0x10)&2) && !(r.rd(8)&2),"read SLVERR lost through generated response path");
            r.result("read_response_error");
        }
        {
            Rig r;r.error_b=true;r.start();r.finish();
            require((r.rd(0x10)&2) && !(r.rd(8)&2),"write SLVERR lost through generated response path");
            r.result("write_response_error");
        }
        {
            Rig r;r.hold_b=true;r.start();r.await_write_response();r.wr(12,2);r.idle(40);
            require(r.rd(8)&1,"STOP invented write drain without B response");
            auto count=r.aw_count;r.hold_b=false;r.finish();
            require((r.rd(8)&6)==4 && r.aw_count==count,"write STOP issued new work or false DONE");
            r.result("stop_waits_for_write_response");
        }
        {
            Rig r;r.hold_r=true;r.start();r.await_read();r.wr(12,2);r.idle(40);
            require(r.rd(8)&1,"STOP invented read drain without R responses");
            auto count=r.ar_count;r.hold_r=false;r.finish();
            require((r.rd(8)&6)==4 && r.ar_count==count,"read STOP issued new work or false DONE");
            r.result("stop_waits_for_read_response");
        }
        {
            Rig r;r.hold_r=true;r.start();r.await_read();r.d.reset_n=0;r.idle(4);r.d.reset_n=1;r.idle(12);
            require((r.rd(8)&40)==40 && !(r.rd(8)&2),"reset invented DONE or healthy epoch");
            r.wr(12,4,true);require(r.rd(8)&32,"CLEAR healed a reset with outstanding traffic");
            require(!r.reads.empty(),"reset model silently cancelled controller ownership");
            r.result("reset_does_not_invent_completion");
        }
        return 0;
    } catch (const std::exception &error) {
        std::cerr<<"FAIL: "<<error.what()<<"\n";return 1;
    }
}
