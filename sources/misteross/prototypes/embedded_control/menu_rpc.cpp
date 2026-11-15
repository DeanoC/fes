// SPDX-License-Identifier: GPL-3.0-or-later
// Native Go drives actual CSR/video/AXI RTL. Memory is an independent CPU arena.
#include "menu_firmware.hpp"
#include <sstream>
#include <string>
static unsigned nibble(char c) {
    if(c>='0' && c<='9')return c-'0';
    if(c>='a' && c<='f')return c-'a'+10;
    throw std::runtime_error("bad CPU byte encoding");
}
int main(int argc,char** argv) {
    try {
        require(argc==3,"two actual Tenfoot frames required");
        std::array<std::vector<uint8_t>,2> frames{load(argv[1]),load(argv[2])};
        Bench b(frames,false,true);MenuFirmware firmware(b);
        std::string line;
        while(std::getline(std::cin,line)) {
            require(line.size()<65550,"menu RPC command bound");
            std::istringstream in(line);std::string op;uint32_t a=0,v=0,s=15;
            in>>op>>std::hex;bool ok=true;std::vector<uint64_t> values;
            if(op=="read" || op=="write") {
                require(bool(in>>a) && a<256,"CSR address bound");
                if(op=="write") {require(bool(in>>v),"write value missing");if(in>>s){}}
                b.d.csr_address=a;b.d.csr_wdata=v;b.d.csr_wstrb=s;
                b.d.csr_write=op=="write";b.d.csr_valid=1;
                if(op=="write" && a==0x80 && v==3)b.strict=false;
                b.until([&]{return bool(b.d.csr_ready);},8,"CSR blocked");b.tick();b.d.csr_valid=0;
                require(b.d.csr_response_valid,"CSR response missing");ok=!b.d.csr_error;
                if(op=="read")values.push_back(b.d.csr_rdata);
                b.tick();
            } else if(op=="memory") {
                std::string bytes;require(bool(in>>a>>bytes) && bytes.size()%2==0 && bytes.size()<=40960,"CPU write bound");
                unsigned slot=a/Bench::slot_bytes,offset=a%Bench::slot_bytes;
                require(slot<2 && offset+bytes.size()/2<=frames[0].size(),"CPU write outside visible slot");
                require(!b.d.running || !b.have_frame || slot!=b.fetch_slot,"CPU overwrote the scanned slot");
                for(const auto& q:b.queue)require((q.address-Bench::base)/Bench::slot_bytes!=slot,"CPU overwrote in-flight memory");
                require(!b.d.m_arvalid || (b.d.m_araddr-Bench::base)/Bench::slot_bytes!=slot,"CPU overwrote held memory request");
                b.published_frame[slot]=-1;
                for(size_t i=0;i<bytes.size()/2;i++)b.memory[a+i]=(nibble(bytes[i*2])<<4)|nibble(bytes[i*2+1]);
                b.tick();
            } else if(op=="publish") {
                require(bool(in>>a) && a<2,"publication slot bound");
                bool matched=false;
                for(unsigned index=0;index<2;index++) {
                    if(std::equal(frames[index].begin(),frames[index].end(),b.memory.begin()+a*Bench::slot_bytes)) {
                        b.published_frame[a]=index;matched=true;break;
                    }
                }
                require(matched,"CPU copy/publication differs from actual Tenfoot frame");b.tick();
            } else if(op=="tick") {
                require(bool(in>>a) && a<=65536,"tick bound");for(uint32_t i=0;i<a;i++)b.tick();
            } else if(op=="mode") {
                require(bool(in>>a) && a<=3,"mode bound");
                b.allow_ar=a!=1;b.allow_r=a!=2;b.bad_response=a==3;
                b.strict=a==0;
            } else if(op=="reset-video") {
                b.d.video_reset_n=0;b.tick();b.d.video_reset_n=1;b.tick();
            } else if(op=="fw-stop") {
                int status=fes_nano_fabric_stop(&firmware.stop);ok=status==0;
                values={firmware.stop.done,firmware.stop.disable_issued,firmware.stop.probe.cancel_issued,firmware.reads,firmware.writes};
            } else if(op=="fw-stats") {
                values={firmware.stop.done,firmware.stop.failed,firmware.reads,firmware.writes};
            } else if(op=="fw-layout") {
                require(bool(in>>a),"firmware layout syntax");firmware.stop.bytes=a;
            } else if(op=="fw-owned") {
                require(bool(in>>a) && a<=1,"firmware owner syntax");firmware.owned=a;
            } else if(op=="stats") {
                values={b.addresses,b.beats,b.queue.size(),b.checked_pixels,b.acknowledgements,b.d.underflows,b.d.fault,b.d.stopped,b.d.m_arvalid,b.d.m_arready};
            } else throw std::runtime_error("unknown menu RPC command");
            std::cout<<"R "<<(ok?1:0);for(auto value:values)std::cout<<" "<<std::hex<<value;
            std::cout<<std::dec<<"\n"<<std::flush;
        }
        return 0;
    } catch(const std::exception& e){std::cerr<<"menu CSR model: "<<e.what()<<"\n";return 1;}
}
