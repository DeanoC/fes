// SPDX-License-Identifier: MIT
#include "Vpcxt_hid_keyboard.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <vector>

static void require(bool ok, const char *message) {
    if (!ok) { std::cerr << "FAIL XT HID: " << message << '\n'; std::exit(1); }
}
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vpcxt_hid_keyboard d;
    std::vector<uint8_t> codes;
    auto tick = [&] {
        d.clk=0; d.eval();
        if (d.code_valid && d.code_ready) codes.push_back(d.code);
        d.clk=1; d.eval();
    };
    auto run = [&](int count) { while (count--) tick(); };
    auto key = [&](int bit, bool pressed) {
        const uint32_t mask = uint32_t(1) << (bit & 31);
        if (pressed) d.rows[bit/32] |= mask; else d.rows[bit/32] &= ~mask;
    };
    auto expect = [&](std::initializer_list<uint8_t> expected, const char *message) {
        if (codes != std::vector<uint8_t>(expected)) {
            std::cerr << message << " got";
            for (auto c:codes) std::cerr << ' ' << std::hex << int(c);
            std::cerr << '\n'; std::exit(1);
        }
        codes.clear();
    };
    d.clk=0; d.reset=1; d.code_ready=1; for(int i=0;i<5;++i) d.rows[i]=0;
    run(2); d.reset=0;
    key(129,true); key(4,true); run(1000); expect({0x2a,0x1e}, "shift+A make");
    key(129,false); key(4,false); run(1000); expect({0x9e,0xaa}, "shift+A break");
    key(128,true); run(1000); expect({0x1d}, "left ctrl");
    key(132,true); run(1000); expect({}, "both ctrl");
    key(128,false); run(1000); expect({}, "one ctrl remains");
    key(132,false); run(1000); expect({0x9d}, "last ctrl released");
    key(0x52,true); run(1000); expect({0x48}, "up arrow");
    key(0x60,true); key(0x52,false); run(1000); expect({}, "keypad alias");
    key(0x60,false); run(1000); expect({0xc8}, "keypad alias release");
    key(5,true); run(10); key(5,false); run(1000); expect({}, "unstable snapshot");
    d.code_ready=0; key(7,true); run(1000);
    require(d.code_valid && d.code==0x20, "pending D make");
    run(100); require(d.code_valid && d.code==0x20, "pending byte held");
    d.code_ready=1; run(1000); expect({0x20}, "backpressure release");
    key(7,false); run(1000); expect({0xa0}, "D break");
    // Preserve a queued make even if a later snapshot arrives while blocked;
    // after it drains, reconcile to the latest snapshot in release/make order.
    d.code_ready=0; key(5,true); run(1000); key(6,true); key(5,false); run(1000);
    require(d.code_valid && d.code==0x30, "queued B make");
    d.code_ready=1; run(2000); expect({0x30,0xb0,0x2e}, "latest snapshot after backpressure");
    key(6,false); run(1000); expect({0xae}, "C break");
    // A complete settled pulse survives while another byte is blocked.
    d.code_ready=0; key(4,true); run(1000);
    key(6,true); run(1000); key(6,false); run(1000);
    d.code_ready=1; run(3000); expect({0x1e,0x2e,0xae}, "queued intermediate pulse");
    key(4,false); run(1000); expect({0x9e}, "A break after pulse");
    // Overflow coalesces intermediate snapshots but preserves the final state.
    d.code_ready=0; key(4,true); run(1000);
    for(int i=0;i<11;++i) { key(5,(i & 1)==0); run(1000); }
    d.code_ready=1; run(4000);
    expect({0x1e,0x30,0xb0,0x30,0xb0,0x30,0xb0,0x30,0xb0,0x30}, "overflow reconciliation");
    key(4,false); key(5,false); run(1500); expect({0x9e,0xb0}, "overflow final releases");
    key(0x1b,true); run(6500);
    require(codes.size()>=2 && codes[0]==0x2d && codes[1]==0x2d, "typematic");
    codes.clear(); key(0x1b,false); run(1000); expect({0xad}, "repeat stop");
    run(5000); expect({}, "repeat stopped");
    key(8,true); run(1000); expect({0x12}, "E make");
    d.reset=1; run(2); d.reset=0; run(1000); expect({0x12}, "held key remade after reset");
    key(8,false); run(1000); expect({0x92}, "E break after reset");
    key(0x44,true); run(1000); key(0x44,false); run(1000); expect({}, "unsupported F11");
    // ISO usage 0x32 shares the XT backslash byte with usage 0x31. Treat
    // them as one logical key so either usage can remain held independently.
    key(0x31,true); run(1000); expect({0x2b}, "US backslash make");
    key(0x32,true); run(1000); expect({}, "ISO #~ aliases backslash");
    key(0x31,false); run(1000); expect({}, "ISO #~ keeps aliased backslash held");
    key(0x32,false); run(1000); expect({0xab}, "ISO #~ alias release");
    key(0x32,true); run(1000); expect({0x2b}, "ISO #~ make");
    key(0x32,false); run(1000); expect({0xab}, "ISO #~ break");
    // Independent set-1 reference for ordinary DOS text and keyboard controls.
    const uint8_t letters[][2] = {{0x04,0x1e},{0x05,0x30},{0x06,0x2e},{0x07,0x20},
        {0x08,0x12},{0x09,0x21},{0x0a,0x22},{0x0b,0x23},{0x0c,0x17},{0x0d,0x24},
        {0x0e,0x25},{0x0f,0x26},{0x10,0x32},{0x11,0x31},{0x12,0x18},{0x13,0x19},
        {0x14,0x10},{0x15,0x13},{0x16,0x1f},{0x17,0x14},{0x18,0x16},{0x19,0x2f},
        {0x1a,0x11},{0x1b,0x2d},{0x1c,0x15},{0x1d,0x2c},{0x28,0x1c},{0x29,0x01},
        {0x2a,0x0e},{0x2b,0x0f},{0x2c,0x39},{0x39,0x3a},{0x3a,0x3b},{0x43,0x44}};
    for (const auto &entry: letters) {
        key(entry[0],true); run(1000); expect({entry[1]}, "reference make");
        key(entry[0],false); run(1000); expect({uint8_t(entry[1] | 0x80)}, "reference break");
    }
    std::cout << "PASS XT HID: snapshot ordering, aliases, debounce, backpressure, repeat and reset\n";
}
