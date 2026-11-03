// SPDX-License-Identifier: GPL-3.0-or-later
// Real endpoint/window RTL with an independent external AXI memory model.
#include "Vembedded_window_top.h"
#include "fes_embedded_control.h"
#include <array>
#include <cstdint>
#include <deque>
#include <fstream>
#include <iomanip>
#include <iostream>
#include <iterator>
#include <stdexcept>
#include <sstream>
#include <string>
#include <vector>

static void require(bool value, const char *why) {
    if (!value) throw std::runtime_error(why);
}
static std::vector<uint8_t> load(const char *path) {
    std::ifstream file(path, std::ios::binary);
    require(bool(file), "frame file missing");
    return {std::istreambuf_iterator<char>(file), std::istreambuf_iterator<char>()};
}
struct Burst { uint32_t address, beats, index; };
static std::string expected_build;
static bool inject_data_error=false;
struct Bench {
    Vembedded_window_top d;
    std::deque<Burst> queue;
    std::vector<uint8_t> memory;
    const uint32_t base, stride;
    static constexpr uint32_t row_bytes=5120, rows=720, frame_bytes=row_bytes*rows;
    uint64_t cycles=0, accepted_addresses=0, accepted_beats=0, stream_bytes=0;
    unsigned max_outstanding=0, completions=0, error=0;
    uint32_t request_row=0, request_offset=0, requested_frame_bytes=0;
    bool addresses=true, responses=true, consumer=true, ar_held=false, r_held=false;
    bool stream_held=false, unsolicited=false;
    uint32_t held_address=0, held_length=0;
    std::array<uint32_t,4> held_data{};
    unsigned held_markers=0;

    Bench(const std::vector<uint8_t>& bytes,uint32_t address,uint32_t row_stride):
        memory(bytes),base(address),stride(row_stride) {
        require(memory.size()==size_t(stride)*rows,"frame geometry differs");
        d.reset_n=0;d.window_reset_n=1;d.control_reset_n=1;d.clk=0;d.csr_valid=0;d.csr_write=0;d.csr_address=0;
        d.csr_wdata=0;d.csr_wstrb=15;d.csr_response_ready=1;d.start=0;
        step();step();d.reset_n=1;step();
        require(!d.start_ready && !d.complete,"disabled endpoint granted a frame");
        require(d.invalid_fault==255 && d.invalid_ready==0 && d.invalid_request==0,
                "invalid geometry/window admitted a read");
        require(read(0)==FES_EMBEDDED_MAGIC && read(4)==FES_EMBEDDED_VERSION &&
                read(8)==0x10203042,"window endpoint signature/ABI differs");
        std::ostringstream observed;
        for(int i=3;i>=0;i--)observed<<std::hex<<std::setfill('0')<<std::setw(8)<<read(12+4*i);
        require(observed.str()==expected_build,"window live build identity differs");
    }
    uint32_t word(uint32_t address) const {
        require(address>=base,"read before window");
        uint64_t offset=uint64_t(address)-base;
        require(offset+4<=memory.size(),"read beyond window");
        return uint32_t(memory[offset])|uint32_t(memory[offset+1])<<8|
               uint32_t(memory[offset+2])<<16|uint32_t(memory[offset+3])<<24;
    }
    void step() {
        d.clk=0;
        d.m_arready=addresses && cycles%7!=2 && cycles%7!=3;
        d.stream_ready=consumer && cycles%11!=4 && cycles%11!=5;
        d.m_rvalid=unsolicited || (responses && !queue.empty() && (r_held || cycles%5!=1));
        d.m_rlast=0;d.m_rresp=0;d.m_rid=0;
        for (unsigned i=0;i<4;i++)d.m_rdata[i]=0;
        if (d.m_rvalid && !queue.empty()) {
            const auto& q=queue.front();d.m_rlast=q.index+1==q.beats;
            for (unsigned i=0;i<4;i++)d.m_rdata[i]=word(q.address+q.index*16+i*4);
            if (inject_data_error && accepted_beats==7)d.m_rdata[0]^=0x100;
            if (accepted_beats==7) {
                if (error==1)d.m_rresp=2;
                if (error==2)d.m_rid=1;
                if (error==3)d.m_rlast=!d.m_rlast;
            }
            if ((error==4 && d.m_rlast) || (error==5 && accepted_beats==frame_bytes/16-1))d.m_rlast=0;
        }
        d.eval();
        if (d.reset_n && d.control_reset_n && ar_held) require(d.m_arvalid && d.m_araddr==held_address &&
            d.m_arlen==held_length,"stalled AXI address changed or disappeared");
        if (d.reset_n && d.window_reset_n && stream_held && !d.reader_abort) {
            require(d.stream_valid,"stalled pixel beat disappeared");
            for(unsigned i=0;i<4;i++)require(d.stream_data[i]==held_data[i],"stalled pixels changed");
            require((unsigned(d.stream_first)|unsigned(d.stream_row_last)<<1|unsigned(d.stream_last)<<2)==held_markers,
                    "stalled pixel markers changed");
        }
        bool ar=d.m_arvalid && d.m_arready, r=d.m_rvalid && d.m_rready;
        Burst offered{d.m_araddr,uint32_t(d.m_arlen)+1,0};
        if (ar) {
            require(d.m_arsize==4 && d.m_arburst==1 && d.m_arid==0,"AXI read attributes differ");
            require(offered.address>=base && offered.address%16==0,"unaligned/outside read");
            uint64_t offset=uint64_t(offered.address)-base;
            require(offset/stride<rows && offset%stride<row_bytes &&
                    offset%stride+offered.beats*16<=row_bytes,"reader fetched row padding/outside frame");
            require((offered.address&4095)+offered.beats*16<=4096,"burst crossed a 4KiB boundary");
            uint32_t expected_address=base+request_row*stride+request_offset;
            uint32_t bytes=row_bytes-request_offset,page=4096-(expected_address&4095);
            if(bytes>page)bytes=page;
            require(requested_frame_bytes<frame_bytes && offered.address==expected_address &&
                    offered.beats*16==bytes,"AXI request sequence skipped/replayed frame bytes");
            requested_frame_bytes+=bytes;request_offset+=bytes;
            if(request_offset==row_bytes){request_offset=0;++request_row;}
        }
        if (d.stream_valid && d.stream_ready) {
            require(stream_bytes<frame_bytes,"excess frame pixels");
            uint32_t row=uint32_t(stream_bytes/row_bytes),within=uint32_t(stream_bytes%row_bytes);
            for (unsigned i=0;i<4;i++)require(d.stream_data[i]==word(base+row*stride+within+i*4),
                                            "pixel bytes differ from native XRGB framebuffer");
            require(bool(d.stream_first)==(stream_bytes==0),"first marker differs");
            require(bool(d.stream_row_last)==(within==row_bytes-16),"row-last marker differs");
            require(bool(d.stream_last)==(stream_bytes==frame_bytes-16),"frame-last marker differs");
            stream_bytes+=16;
        }
        ar_held=d.m_arvalid && !d.m_arready;held_address=d.m_araddr;held_length=d.m_arlen;
        r_held=d.m_rvalid && !d.m_rready;
        stream_held=d.stream_valid && !d.stream_ready;
        for(unsigned i=0;i<4;i++)held_data[i]=d.stream_data[i];
        held_markers=unsigned(d.stream_first)|unsigned(d.stream_row_last)<<1|unsigned(d.stream_last)<<2;
        d.clk=1;d.eval();
        if (r && !queue.empty()) {
            ++accepted_beats;if (++queue.front().index==queue.front().beats)queue.pop_front();
        }
        if (ar) {queue.push_back(offered);++accepted_addresses;}
        if (queue.size()>max_outstanding)max_outstanding=unsigned(queue.size());
        require(max_outstanding<=4,"more than four outstanding reads");
        d.clk=0;d.eval();++cycles;
        if (d.complete) {require(stream_bytes==frame_bytes,"partial frame completed");++completions;}
    }
    uint32_t access(bool write,uint32_t offset,uint32_t value=0,bool reject=false) {
        d.csr_valid=1;d.csr_write=write;d.csr_address=offset;d.csr_wdata=value;d.eval();
        require(d.csr_ready,"CSR busy");step();
        require(d.csr_response_valid && bool(d.csr_error)==reject,"CSR access rejection differs");
        value=d.csr_rdata;d.csr_valid=0;step();return value;
    }
    uint32_t read(uint32_t offset){return access(false,offset);}
    void write(uint32_t offset,uint32_t value){access(true,offset,value);}
    void enable(uint32_t generation) {
        write(FES_EMBEDDED_REG_REQUEST_GENERATION_LO,generation);
        write(FES_EMBEDDED_REG_REQUEST_GENERATION_HI,0);
        write(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_ENABLE);
        require(d.start_ready,"owned enable did not admit a frame");
    }
    void start() {
        require(d.start_ready,"frame start outside owned idle scope");
        stream_bytes=0;request_row=0;request_offset=0;requested_frame_bytes=0;
        d.start=1;step();d.start=0;
    }
    void disable(uint32_t cookie) {
        write(FES_EMBEDDED_REG_REQUEST_COOKIE_LO,cookie);
        write(FES_EMBEDDED_REG_REQUEST_COOKIE_HI,0);
        write(FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_DISABLE);
    }
    void drain(uint32_t generation,uint32_t cookie) {
        for (unsigned i=0;i<10000;i++) {
            if (read(FES_EMBEDDED_REG_STATUS)==FES_EMBEDDED_STATUS_DRAINED) {
                require(read(FES_EMBEDDED_REG_PENDING)==0 && queue.empty(),"drain with outstanding reads");
                require(read(FES_EMBEDDED_REG_ACK_GENERATION_LO)==generation &&
                    read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==cookie,"wrong generation/cookie drain");
                return;
            }
        }
        throw std::runtime_error("bounded drain did not finish");
    }
    void frame() {
        for (unsigned i=0;i<3000000 && !d.complete;i++)step();
        require(d.complete && !d.fault && !d.aborted,"full frame did not complete");
        require(d.delivered_bytes==frame_bytes && d.requested_bytes==frame_bytes,"full frame accounting differs");
        require(queue.empty(),"completed frame still has external reads");
    }
};

int main(int argc,char**argv) {
    try {
        require(argc==6 || argc==7,"usage: window_probe frame0 frame1 base stride expected-build-id [corrupt-data]");
        expected_build=argv[5];require(expected_build.size()==32,"build ID must have 32 hex digits");
        if(argc==7){require(std::string(argv[6])=="corrupt-data","unknown fault option");inject_data_error=true;}
        auto first=load(argv[1]),second=load(argv[2]);
        uint32_t base=uint32_t(std::stoull(argv[3],nullptr,0)),stride=uint32_t(std::stoul(argv[4]));
        unsigned cases=0;
        { // Repeated full frames within one owner, then an independently acknowledged retirement.
            Bench b(first,base,stride);b.enable(1);b.start();b.frame();b.step();
            b.memory=second;b.start();b.frame();b.disable(7);b.drain(1,7);
            require(b.completions==2,"frame reuse did not complete exactly twice");++cases;
        }
        { // Disable with a real stalled AR; consumer refusal cannot block endpoint drain.
            Bench b(first,base,stride);b.enable(1);b.addresses=false;b.consumer=false;b.start();
            for(unsigned i=0;i<30;i++)b.step();
            require(b.d.m_arvalid && b.accepted_addresses==0,"no real held address");
            b.disable(7);for(unsigned i=0;i<30;i++)b.step();
            require(!b.d.complete && !b.d.stream_valid && b.d.aborted &&
                b.read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==0,"held address falsely drained");
            b.addresses=true;b.drain(1,7);
            require(b.stream_bytes==0 && b.accepted_beats>0,"disable did not drain real responses");
            b.access(true,FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_ENABLE,true);
            require(!b.d.start_ready,"aborted frame accepted stale generation");
            b.consumer=true;b.memory=second;b.enable(2);b.start();b.frame();b.disable(8);b.drain(2,8);++cases;
        }
        { // A partial frame is retired, never published as complete or replayed.
            Bench b(first,base,stride);b.enable(1);b.start();
            while(b.stream_bytes<176)b.step();
            b.disable(7);auto delivered=b.stream_bytes;b.drain(1,7);
            require(delivered<Bench::frame_bytes && b.stream_bytes==delivered &&
                b.completions==0 && b.d.aborted,"partial frame survived retirement");++cases;
        }
        { // No response is not a timeout-based drain receipt.
            Bench b(first,base,stride);b.enable(1);b.responses=false;b.start();
            for(unsigned i=0;i<2000;i++)b.step();
            require(b.accepted_addresses==4 && b.max_outstanding==4 && b.stream_bytes==0,
                    "missing-response backpressure/accounting differs");
            b.disable(7);for(unsigned i=0;i<2000;i++)b.step();
            require(b.read(FES_EMBEDDED_REG_STATUS)==FES_EMBEDDED_STATUS_DRAINING &&
                b.read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==0 && b.completions==0,"missing responses falsely acknowledged");
            b.responses=true;b.drain(1,7);++cases;
        }
        { // Withhold the final beat: only complete consumer delivery publishes a frame.
            Bench b(first,base,stride);b.enable(1);b.start();
            while(b.stream_bytes<Bench::frame_bytes-16)b.step();
            b.consumer=false;for(unsigned i=0;i<80;i++)b.step();
            require(b.stream_bytes==Bench::frame_bytes-16 && b.completions==0 && b.d.busy,
                    "blocked final pixels published an incomplete frame");
            b.disable(7);b.drain(1,7);
            require(b.d.aborted && !b.d.start_ready && b.completions==0,
                    "late frame retirement granted same-generation reuse");++cases;
        }
        { // An invalid DISABLE can pulse abort without admitting a new generation.
            Bench b(first,base,stride);b.enable(1);b.addresses=false;b.responses=false;b.start();
            for(unsigned i=0;i<30;i++)b.step();
            b.access(true,FES_EMBEDDED_REG_COMMAND,FES_EMBEDDED_COMMAND_DISABLE,true);
            require(b.d.aborted && !b.d.start_ready && !b.d.busy &&
                b.read(FES_EMBEDDED_REG_STATUS)==FES_EMBEDDED_STATUS_ENABLED,
                "rejected stop allowed old-generation frame replay");
            b.disable(7);b.addresses=true;b.responses=true;b.drain(1,7);
            b.enable(2);b.start();b.frame();b.disable(8);b.drain(2,8);++cases;
        }
        for(unsigned error=1;error<=5;error++) {
            Bench b(first,base,stride);b.enable(1);b.error=error;b.start();
            for(unsigned i=0;i<3000000 && !(b.read(FES_EMBEDDED_REG_STATUS)&FES_EMBEDDED_STATUS_FAULT);i++)b.step();
            require(b.read(FES_EMBEDDED_REG_STATUS)&FES_EMBEDDED_STATUS_FAULT,"bad AXI response not sticky");
            for(unsigned i=0;i<30;i++)b.step();
            require(b.d.reader_abort && !b.d.start_ready && !b.d.stream_valid && !b.completions &&
                b.read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==0,"fault granted frame/reuse/drain");++cases;
        }
        { // An unsolicited response cannot manufacture an enabled window or drain.
            Bench b(first,base,stride);b.unsolicited=true;b.step();b.unsolicited=false;b.step();
            require(b.read(FES_EMBEDDED_REG_STATUS)&FES_EMBEDDED_STATUS_FAULT,"unsolicited response ignored");
            require(!b.d.start_ready && !b.d.complete && b.read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==0,
                    "unsolicited response granted authority");++cases;
        }
        { // Reset supplies no receipt; old external responses fault the reset endpoint.
            Bench b(first,base,stride);b.enable(1);b.responses=false;b.start();
            for(unsigned i=0;i<60;i++)b.step();require(!b.queue.empty(),"reset case has no real outstanding reads");
            b.d.reset_n=0;b.step();b.ar_held=false;b.d.reset_n=1;
            require(b.read(FES_EMBEDDED_REG_STATUS)==0 && b.read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==0,
                    "reset claimed drain");
            b.responses=true;for(unsigned i=0;i<10;i++)b.step();
            require(b.read(FES_EMBEDDED_REG_STATUS)&FES_EMBEDDED_STATUS_FAULT,"stale post-reset response accepted");++cases;
        }
        { // Resetting just the window source cannot adopt an old active read ledger.
            Bench b(first,base,stride);b.enable(1);b.responses=false;b.start();
            for(unsigned i=0;i<60;i++)b.step();
            require(b.accepted_addresses==4,"source-reset case lacks pending AXI reads");
            b.d.window_reset_n=0;b.step();b.d.window_reset_n=1;b.step();
            require(!b.d.start_ready && !b.d.busy && b.read(FES_EMBEDDED_REG_PENDING)!=0 &&
                b.read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==0,"source reset adopted stale ownership");
            b.disable(7);b.responses=true;b.drain(1,7);b.enable(2);b.start();b.frame();
            b.disable(8);b.drain(2,8);++cases;
        }
        { // Losing only the endpoint generation poisons the still-owned source.
            Bench b(first,base,stride);b.enable(1);b.responses=false;b.start();
            for(unsigned i=0;i<60;i++)b.step();
            require(b.accepted_addresses==4,"endpoint-reset case lacks outstanding reads");
            b.d.control_reset_n=0;b.step();b.ar_held=false;b.d.control_reset_n=1;b.step();
            require(b.d.fault && b.d.aborted && !b.d.start_ready && !b.d.complete &&
                b.read(FES_EMBEDDED_REG_ACK_COOKIE_LO)==0,"endpoint reset granted source reuse/drain");++cases;
        }
        std::cout<<"{\"status\":\"passed\",\"cases\":"<<cases<<",\"width\":1280,\"height\":720,"
                 <<"\"frame_bytes\":"<<Bench::frame_bytes<<",\"stride_bytes\":"<<stride
                 <<",\"window_base\":"<<base<<",\"physical_ddr\":false,\"runtime_admission\":false}\n";
        return 0;
    } catch(const std::exception& e) {std::cerr<<"FAIL "<<e.what()<<"\n";return 1;}
}
