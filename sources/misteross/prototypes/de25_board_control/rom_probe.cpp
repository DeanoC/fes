// SPDX-License-Identifier: GPL-3.0-or-later
#include "Vfes_embedded_rom.h"
#include <verilated.h>
#include <cstdint>
#include <iostream>
#include <stdexcept>
#include <string>
#include <vector>

static void require(bool ok, const char* reason) {
    if (!ok) throw std::runtime_error(reason);
}

struct Test {
    Vfes_embedded_rom d;
    const unsigned size;
    const uint32_t crc;
    std::vector<uint8_t> image;
    explicit Test(unsigned n, uint32_t expected) : size(n), crc(expected), image(n) {
        for (unsigned i=0; i<n; ++i) image[i]=uint8_t(i*37+(i>>3)+0x5a);
        reset();
    }
    void tick() { d.clk=0;d.eval();d.clk=1;d.eval(); }
    void reset() {
        d.reset_n=0;d.cpu_held=1;d.start=0;d.abort_load=0;
        d.stream_valid=0;d.stream_last=0;d.read_address=0;tick();
        d.reset_n=1;tick();
        require(!d.loaded&&!d.failed&&d.received==0&&d.written==0,"reset leaked readiness");
    }
    void begin(uint32_t expected) { d.expected_crc32=expected;d.start=1;tick();d.start=0;d.eval(); }
    void offer(unsigned offset, bool last) {
        require(d.stream_ready,"beat offered outside ready");
        for (unsigned word=0; word<4; ++word) {
            uint32_t value=0;
            for (unsigned byte=0; byte<4; ++byte) value|=uint32_t(image[offset+4*word+byte])<<(8*byte);
            d.stream_data[word]=value;
        }
        d.stream_last=last;d.stream_valid=1;tick();d.stream_valid=0;
        // The producer is free to reuse its data bus after the handshake.
        for (unsigned i=0;i<4;++i) d.stream_data[i]=0xabcdef12;
    }
    void feed(bool good_crc=true) {
        begin(good_crc ? crc : crc^1);
        for (unsigned offset=0; offset<size; offset+=16) {
            for (unsigned pause=0; pause<(offset/16)%3; ++pause) tick();
            offer(offset,offset+16==size);
            require(d.received==offset+16&&!d.stream_ready,"accepted beat was not fenced");
            for (unsigned byte=0; byte<16; ++byte) {
                tick();
                require(d.written==offset+byte+1,"RAM byte count differs");
                if (!d.loaded) require(d.read_data==0,"partial ROM was exposed");
            }
        }
        require(d.received==size&&d.written==size&&d.observed_crc32==crc,"completed bytes/CRC differ");
        require(bool(d.loaded)==good_crc&&bool(d.failed)!=good_crc,"CRC did not govern readiness");
    }
    void check_image() {
        for (unsigned address=0;address<size;++address) {
            d.read_address=address;tick();
            require(d.read_data==image[address],"synchronous ROM byte differs");
        }
    }
    void check_failed() {
        require(d.failed&&!d.loaded&&!d.stream_ready,"uncertain load was not retired");
        const auto count=d.written;
        d.cpu_held=1;d.abort_load=0;d.start=1;tick();d.start=0;
        d.stream_valid=1;d.stream_last=1;
        for(unsigned i=0;i<32;++i)tick();
        require(d.failed&&!d.loaded&&d.written==count&&d.read_data==0,"failed image was replayed or exposed");
    }
    void run() {
        feed();d.cpu_held=0;tick();require(d.loaded&&!d.failed,"Run revoked a complete image");check_image();
        d.stream_valid=1;for(unsigned i=0;i<32;++i)tick();
        require(d.loaded&&d.written==size&&!d.stream_ready,"extra unaccepted data changed the ROM");
        d.stream_valid=0;check_image();
        d.cpu_held=1;d.start=1;tick();d.start=0;check_failed();
        reset();feed(false);check_failed();
        reset();begin(crc);offer(0,true);require(d.written==0&&d.received==0,"early LAST wrote bytes");check_failed();
        reset();begin(crc);
        for(unsigned offset=0;offset<size;offset+=16){
            offer(offset,false);
            if(offset+16<size)for(unsigned i=0;i<16;++i)tick();
        }
        require(d.written==size-16,"missing LAST wrote final beat");check_failed();
        for(bool abort : {false,true}) {
            reset();begin(crc);offer(0,false);tick();const auto count=d.written;
            if(abort)d.abort_load=1;else d.cpu_held=0;
            tick();require(d.written==count,"lost Hold/abort wrote another byte");check_failed();
        }
        reset();begin(crc);offer(0,false);for(unsigned i=0;i<16;++i)tick();
        for(unsigned i=0;i<100;++i)tick();
        require(!d.loaded&&!d.failed&&d.written==16,"underrun fabricated completion");
        d.abort_load=1;tick();check_failed();
        reset();begin(crc);offer(0,false);tick();
        d.reset_n=0;tick();require(d.written==0&&!d.loaded,"reset wrote or exposed a byte");
        reset();feed();check_image();
        std::cout<<"PASS "<<size<<"-byte ROM: every byte, CRC, gaps, bus reuse, reset, abort, Hold loss, LAST, replay and underrun\n";
    }
};

int main(int argc,char** argv) {
    try {
        require(argc==3,"size and independently computed CRC required");
        Test test(std::stoul(argv[1]),std::stoul(argv[2],nullptr,16));test.run();return 0;
    } catch(const std::exception& e) {std::cerr<<"FAIL "<<e.what()<<'\n';return 1;}
}
