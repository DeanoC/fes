// SPDX-License-Identifier: GPL-3.0-or-later
#include "sim.hpp"
#include "../../zx81-hps/cosim/basic_observer.hpp"
#include "verilated.h"
#include <cstring>
#include <sys/socket.h>
#include <sys/un.h>
int main(int argc,char**argv){
    try{
        wire::need(argc>=2,"expected socket path");Verilated::commandArgs(argc,argv);
        std::setvbuf(stdout,nullptr,_IOLBF,0);
        Computer b;auto& t=b.t;
        sockaddr_un address{};address.sun_family=AF_UNIX;
        wire::need(std::strlen(argv[1])<sizeof(address.sun_path),"socket path too long");std::strcpy(address.sun_path,argv[1]);
        int listener=::socket(AF_UNIX,SOCK_STREAM,0);wire::need(listener>=0,"socket");
        wire::need(::bind(listener,reinterpret_cast<sockaddr*>(&address),sizeof(address))==0&&::listen(listener,1)==0,"bind/listen");
        int client=::accept(listener,nullptr,nullptr);wire::need(client>=0,"accept");
        std::array<unsigned char,16> request{};
        while(wire::transfer(client,request.data(),request.size(),false)){
            unsigned op=wire::decode(request.data()),arg=wire::decode(request.data()+4),value=wire::decode(request.data()+8);
            unsigned result=0,status=0;
            if(op==wire::Read||op==wire::Write||op==combined::PrRead||op==combined::PrWrite){
                bool pr=op==combined::PrRead||op==combined::PrWrite;
                wire::need(arg<(pr?64u:24u)&&arg%4==0,"invalid register offset");
                if(op==wire::Read||op==combined::PrRead)result=b.read(arg,pr);
                else status=b.write(arg,value,pr);
                if(pr&&t.drop_response)b.cycles(1024);
            }else if(op==combined::Select){
                wire::need(t.frozen&&t.slot_reset&&!t.running,"selection before actual freeze acknowledgement");
                wire::need(arg==0xc001a001u||arg==0xc002b002u,"unknown persona");
                b.configuration_window=true;t.persona_b=arg==0xc002b002u;++b.selections;b.ramtop=0;b.ramtop_writes=0;
                b.expect_load=false;b.tape_tag[0]=b.tape_tag[1]=false;b.cycles(8);
            }else if(op==combined::DropResponse){t.drop_response=arg!=0;b.cycles(1);
            }else if(op>=basic::HasLine&&op<=basic::WaitNewline){result=basic::observe(b,op,arg,value,b.captured);
            }else if(op==wire::Advance){wire::need(arg<=800000000,"advance budget exceeded");b.cycles(arg);
            }else if(op==wire::PixelClock){b.pixel.running=arg!=0;
            }else if(op==wire::Reset){t.reset_n=0;b.cycles(4);t.reset_n=1;b.cycles(4);
            }else if(op==wire::State||op==combined::Counts){
                switch(arg){
                case wire::MachineReset:result=C(machine_reset);break;
                case wire::KeyboardLow:result=C(keyboard);break;
                case wire::KeyboardHigh:result=C(keyboard)>>32;break;
                case wire::TapeBusy:result=C(tape_busy);break;
                case wire::TapeReady:result=C(tape_ready);break;
                case wire::LoadedBytes:result=b.loaded_bytes;break;
                case wire::Frames:result=b.complete_frames;break;
                case wire::PictureFrames:result=b.picture_frames;break;
                case wire::ELine:result=b.word(0x4014);break;
                case wire::BridgeFault:result=C(faults);break;
                case combined::Ramtop:result=b.ramtop;break;
                case combined::RamtopWrites:result=b.ramtop_writes;break;
                case combined::Frozen:result=t.frozen;break;
                case combined::Running:result=t.running;break;
                case combined::MemoryWrites:result=b.memory_writes;break;
                case combined::PrWrites:result=b.pr_writes;break;
                case combined::Selections:result=b.selections;break;
                default:throw std::runtime_error("unknown field");
                }
            }else if(op==wire::PeekRam){wire::need(arg>=0x4000&&arg<0x8000,"RAM observation range");result=b.peek(arg);
            }else if(op==wire::BeginLoad){wire::need(!b.captured.empty(),"no ROM SAVE image");b.loaded_bytes=0;b.expect_load=true;
            }else if(op==combined::WaitRamtop){
                b.wait_editor(770000000,arg);wire::need(b.word(0x4004)==arg,"actual RAMTOP differs from CPU write observation");result=b.ramtop;
                std::printf("PASS real BASIC RAMTOP=%04x CPUwrite-observed cycles=%llu\n",result,(unsigned long long)b.system_edges);
            }else if(op==wire::WaitEditor){
                b.wait_editor(arg,b.ramtop);
            }else if(op==wire::WaitToken){
                auto end=b.system_edges+25000000;while(b.token_count(arg)<value&&b.system_edges<end)b.cycles(4096);
                wire::need(b.token_count(arg)>=value,"real ROM missed HID token");result=b.token_count(arg);
            }else if(op==wire::WaitLoaded||op==combined::WaitSomeLoad){
                auto wanted=op==wire::WaitLoaded?b.captured.size():arg;
                auto end=b.system_edges+500000000;
                while((b.loaded_bytes<wanted||(op==wire::WaitLoaded&&C(tape_busy)))&&b.system_edges<end)b.cycles(16);
                wire::need(b.loaded_bytes>=wanted,"external LOAD did not commit expected bytes");result=b.loaded_bytes;
                if(op==wire::WaitLoaded){wire::need(result==b.captured.size(),"external LOAD byte count differs");b.expect_load=false;}
            }else if(op==wire::WaitPicture){
                b.inspect_picture=true;auto end=b.system_edges+20000000,target=b.picture_frames+arg;
                while(b.picture_frames<target&&b.system_edges<end)b.step();
                wire::need(b.picture_frames>=target&&b.source_ink,"no complete real machine picture");result=b.picture_frames;
            }else if(op!=wire::Done)throw std::runtime_error("unknown operation");
            std::array<unsigned char,32> reply{};
            unsigned words[]={status,result,unsigned(b.system_edges),unsigned(b.system_edges>>32),unsigned(b.retired),unsigned(b.retired>>32),b.mmio_writes+b.pr_writes,b.mmio_reads};
            for(unsigned i=0;i<8;++i)wire::encode(reply.data()+i*4,words[i]);
            wire::need(wire::transfer(client,reply.data(),reply.size(),true),"send reply");if(op==wire::Done)break;
        }
        ::close(client);::close(listener);::unlink(argv[1]);
        std::printf("PASS combined RTL: selections=%u memory-writes=%llu loader-bytes=%u frames=%llu\n",b.selections,(unsigned long long)b.memory_writes,b.loaded_bytes,(unsigned long long)b.complete_frames);
    }catch(const std::exception&e){std::fprintf(stderr,"FAIL combined RTL: %s\n",e.what());return 1;}
}
