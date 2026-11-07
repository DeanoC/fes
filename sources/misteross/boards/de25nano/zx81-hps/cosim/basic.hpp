// SPDX-License-Identifier: GPL-3.0-or-later
#pragma once
#include "wire.hpp"
#include "native/de25_computer.hpp"
#include <functional>
#include <fstream>
#include <sstream>
#include <vector>
namespace basic {
struct Result {
    std::vector<unsigned char> image;
    unsigned loads=0,runs=0,editor_returns=0,saved_cdflag=0,printed_value=0;
    bool reload_removed_edit=false;
    std::string json()const{
        std::ostringstream s;s<<"{\"saved_bytes\":"<<image.size()<<",\"loads\":"<<loads<<",\"runs\":"<<runs
            <<",\"printed_value\":"<<printed_value<<",\"editor_returns\":"<<editor_returns<<",\"saved_cdflag\":"<<saved_cdflag
            <<",\"reload_removed_edit\":"<<(reload_removed_edit?"true":"false")<<"}";return s.str();
    }
    void save(const char*path)const{
        std::ofstream f(path,std::ios::binary);f.write(reinterpret_cast<const char*>(image.data()),image.size());
        wire::need(bool(f),"could not retain ROM SAVE fixture");
    }
};
class Session {
    mister::native::De25Computer& computer;
    std::function<wire::Reply(unsigned,unsigned,unsigned)> call;
    std::function<uint64_t()> deadline;
    Result evidence;
    wire::Reply command(unsigned op,unsigned arg=0,unsigned value=0){return call(op,arg,value);}
    void okay(const mister::Error&e){wire::need(e.ok(),"BASIC client operation failed");}
public:
    Session(mister::native::De25Computer&c,std::function<wire::Reply(unsigned,unsigned,unsigned)>io,std::function<uint64_t()>clock):computer(c),call(io),deadline(clock){}
    void keys(mister::KeyboardHidRows rows){okay(computer.SetKeyboardHid(rows,deadline()));command(wire::Advance,100100);}
    void neutral(){keys({});}
    void editor(){command(wire::WaitEditor,160000000);++evidence.editor_returns;}
    void token(unsigned usage,unsigned expected,unsigned count=1,bool shift=false){
        mister::KeyboardHidRows rows{};rows[usage/16]=1u<<(usage%16);if(shift)rows[8]=2;
        keys(rows);command(wire::WaitToken,expected,count);neutral();editor();
    }
    void enter(){mister::KeyboardHidRows rows{};rows[2]=1u<<8;okay(computer.SetKeyboardHid(rows,deadline()));}
    void submit(){enter();command(WaitNewline);neutral();}
    void load_command(bool complete=true){
        token(0x0d,0xef);token(0x13,0x0b,1,true);token(0x13,0x0b,2,true);
        command(wire::WaitPicture,2);if(complete)submit();else enter();
    }
    void load(){
        command(wire::BeginLoad);load_command();
        wire::need(command(wire::WaitLoaded).value==evidence.image.size(),"BASIC LOAD byte count differs from ROM SAVE");
        editor();++evidence.loads;
        wire::need(command(HasLine,1,0xf5).value,"LOAD lost the PRINT program");
    }
    void run(){
        token(0x15,0xf7);submit();evidence.printed_value=command(WaitPrint,7).value;editor();++evidence.runs;
        command(wire::WaitPicture,2);
    }
    Result roundtrip(){
        // Type 1 PRINT 7 through the actual HID client; the ROM tokenizes it.
        // A listing starts with 1, so it cannot satisfy the RUN output check.
        token(0x1e,0x1d);token(0x13,0xf5);token(0x24,0x23);
        submit();editor();wire::need(command(HasLine,1,0xf5).value,"ROM did not store the PRINT line");
        // Only the real ROM SAVE emitter supplies a loadable .p image.
        token(0x16,0xf8);token(0x13,0x0b,1,true);token(0x04,0x26);token(0x13,0x0b,2,true);
        submit();command(WaitSaved);unsigned bytes=command(CaptureSaved).value;
        for(unsigned n=0;n<bytes;++n)evidence.image.push_back(command(PeekSaved,n).value);
        evidence.saved_cdflag=evidence.image.at(0x403b-0x4009);
        wire::need(!(evidence.saved_cdflag&0x80),"ROM SAVE retained the live SLOW display flag");
        editor();
        // Pixel-clock loss cannot block the real CRC/chunked upload or CPU.
        command(wire::PixelClock,0);auto before=command(wire::State,wire::MachineReset).retired;
        okay(computer.LoadTape(evidence.image,deadline(),127));command(wire::Advance,500000);
        wire::need(command(wire::State,wire::MachineReset).retired>before,"pixel stop blocked CPU");command(wire::PixelClock,1);
        load();run();
        // Prove post-RUN keyboard input by inserting a second line, then prove
        // a real reload removes it without resetting/reconfiguring the machine.
        token(0x1f,0x1e);token(0x08,0xea);
        submit();editor();wire::need(command(HasLine,2,0xea).value,"ROM did not store the post-RUN edit");
        okay(computer.LoadTape(evidence.image,deadline(),127));load();
        evidence.reload_removed_edit=!command(HasLine,2,0xea).value;
        wire::need(evidence.reload_removed_edit,"reload retained the post-RUN edit");run();
        return evidence;
    }
};
} // namespace basic
