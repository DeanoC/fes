// SPDX-License-Identifier: GPL-3.0-or-later
// Independent register/protocol and published Yamaha frequency checks.
#include "Vst_input_audio_sim_top.h"
#include "verilated.h"
#include <array>
#include <cmath>
#include <cstdlib>
#include <iostream>
#include <string>
#include <utility>
#include <vector>

struct Test {
    Vst_input_audio_sim_top dut;
    unsigned transmitted = 0;
    std::vector<int> tx_bytes;
    void require(bool ok, const std::string &message) {
        if (!ok) { std::cerr << "ST input/audio: " << message << '\n'; std::exit(1); }
    }
    void tick() {
        dut.clk=0; dut.eval();
        if (dut.raw_tx_valid && dut.raw_tx_ready && dut.raw_mode) {
            ++transmitted; tx_bytes.push_back(dut.raw_tx_data);
        }
        dut.clk=1; dut.eval();
    }
    void run(int cycles) { while (cycles-- > 0) tick(); }
    void reset() {
        dut.ac_req=0; dut.raw_mode=1; dut.raw_rx_valid=0;
        dut.raw_rx_frame=0; dut.raw_rx_parity=0; dut.raw_tx_ready=1;
        dut.direct_ikbd=1; dut.ik_command_valid=0; dut.ik_response_ready=0;
        dut.controller_buttons=0; dut.mouse_valid=0; dut.mouse_dx=0; dut.mouse_dy=0;
        dut.mouse_buttons=0; dut.ym_req=0; dut.engine_ce=0;
        dut.engine_address_write=0; dut.engine_data_write=0; dut.engine_data=0;
        for (int i=0;i<5;++i) dut.keyboard[i]=0;
        dut.reset=1; tick(); dut.reset=0; tick();
        transmitted=0; tx_bytes.clear();
    }
    int ac_bus(bool writing, int reg, int value=0, int hold=1) {
        dut.ac_req=1; dut.ac_reg=reg; dut.ac_write=writing; dut.ac_wdata=value;
        tick(); require(dut.ac_ack,"ACIA acknowledge");
        const int result=dut.ac_rdata; run(hold-1);
        dut.ac_req=0; tick(); return result;
    }
    int status() { return ac_bus(false,0); }
    void receive(int value, bool framing=false, bool parity=false) {
        for (int n=0;!dut.raw_rx_ready;++n) { require(n<10000,"receive ready timeout"); tick(); }
        dut.raw_rx_data=value; dut.raw_rx_frame=framing; dut.raw_rx_parity=parity;
        dut.raw_rx_valid=1; tick(); dut.raw_rx_valid=0; run(3000);
    }
    void command(int value) {
        for (int n=0;!dut.ik_command_ready;++n) { require(n<1000,"IKBD command ready timeout"); tick(); }
        dut.ik_command_valid=1; dut.ik_command_data=value; tick();
        dut.ik_command_valid=0; tick();
    }
    void commands(std::initializer_list<int> values) { for (int value: values) command(value); }
    int response() {
        for (int n=0;!dut.ik_response_valid;++n) { require(n<1000,"IKBD response timeout"); tick(); }
        const int result=dut.ik_response_data;
        dut.ik_response_ready=1; tick(); dut.ik_response_ready=0; tick(); return result;
    }
    void expect(std::initializer_list<int> values, const std::string &message) {
        for (int value: values) {
            const int actual=response();
            require(actual==value,message+" expected="+std::to_string(value)+" actual="+std::to_string(actual));
        }
    }
    void key(int usage, bool down) {
        if (down) dut.keyboard[usage/32] |= 1U<<(usage%32);
        else dut.keyboard[usage/32] &= ~(1U<<(usage%32));
    }
    void mouse(int x, int y, int buttons) {
        dut.mouse_dx=x; dut.mouse_dy=y; dut.mouse_buttons=buttons;
        for (int n=0;!dut.mouse_ready;++n) { require(n<1000,"mouse ready timeout"); tick(); }
        dut.mouse_valid=1; tick(); dut.mouse_valid=0; tick();
    }
    int ym_bus(bool writing, int reg, int value=0, int hold=1) {
        dut.ym_req=1; dut.ym_reg=reg; dut.ym_write=writing; dut.ym_wdata=value;
        tick(); require(dut.ym_ack,"YM acknowledge"); const int result=dut.ym_rdata;
        run(hold-1); dut.ym_req=0; tick(); return result;
    }
    void ym_write(int reg,int value) { ym_bus(true,0,reg); ym_bus(true,1,value); }
    void engine_write(int reg,int value) {
        dut.engine_data=reg; dut.engine_address_write=1; tick();
        dut.engine_address_write=0; dut.engine_data=value; dut.engine_data_write=1;
        tick(); dut.engine_data_write=0; tick();
    }
    void engine_run(int cycles) { dut.engine_ce=1; run(cycles); dut.engine_ce=0; }
};

static void acia(Test &t) {
    t.reset(); t.require(t.status()==0,"master reset status");
    t.ac_bus(true,0,0x96); // /64, 8N1, receive interrupt.
    t.require(t.status()==2,"initial transmit ready");
    t.ac_bus(true,1,0x80,30); // A held request must send exactly once.
    t.run(2800); t.require(t.transmitted==1 && t.tx_bytes[0]==0x80,"held TX transaction");
    t.receive(0xf1);
    t.require((t.status()&0x83)==0x83 && t.dut.ac_irq,"receive data IRQ/status");
    t.require(t.ac_bus(false,1)==0xf1,"receive byte");
    t.require(t.status()==2 && !t.dut.ac_irq,"data read clears receive interrupt");
    t.receive(0x51,true,true); // 8N1 inhibits parity checker.
    t.require((t.status()&0x50)==0x10,"framing error and disabled parity");
    t.ac_bus(false,1); t.require((t.status()&0x50)==0,"receive error clear");
    t.ac_bus(true,0,0x9a); // /64, 8E1.
    t.receive(0x52,false,true); t.require((t.status()&0x40)!=0,"enabled parity error");
    t.ac_bus(false,1);
    t.receive(0x11); t.receive(0x22);
    t.require((t.status()&0x20)==0,"overrun waits until preserved byte read");
    t.require(t.ac_bus(false,1)==0x11,"overrun preserves first byte");
    t.require((t.status()&0x21)==0x21,"overrun visible after first byte");
    t.ac_bus(false,1); t.require((t.status()&0xa1)==0,"status then data clears overrun");
    t.ac_bus(true,0,0x35); // /16, 8N1, TX IRQ, MIDI cadence.
    t.require(t.dut.ac_irq,"transmit empty IRQ");
    const auto start=t.transmitted; t.ac_bus(true,1,0x7f); t.run(500);
    t.require(t.transmitted==start,"MIDI character not prematurely delivered");
    t.run(200); t.require(t.transmitted==start+1 && t.dut.ac_irq,"MIDI transmit and empty IRQ");
    t.ac_bus(true,0,0x03); t.require(!t.dut.ac_irq && t.status()==0,"master reset clears IRQ");

    // Real paired byte transport: firmware reset command and reply, then a
    // complete key make/break through the receiver's interrupt/status path.
    t.reset(); t.dut.raw_mode=0; t.dut.direct_ikbd=0;
    t.ac_bus(true,0,0x96); t.ac_bus(true,1,0x80);
    for (int n=0;!(t.status()&2);++n) { t.require(n<2000,"reset TX ready"); t.tick(); }
    t.ac_bus(true,1,0x01);
    for (int n=0;!(t.status()&1);++n) { t.require(n<8000,"reset reply over serial"); t.tick(); }
    t.require(t.ac_bus(false,1)==0xf1,"reset reply over ACIA");
    t.key(4,true);
    for (int n=0;!(t.status()&1);++n) { t.require(n<4000,"key ACIA interrupt"); t.tick(); }
    t.require(t.dut.ac_irq && t.ac_bus(false,1)==0x1e,"ACIA key make");
    t.key(4,false);
    for (int n=0;!(t.status()&1);++n) { t.require(n<4000,"key release ACIA interrupt"); t.tick(); }
    t.require(t.ac_bus(false,1)==0x9e,"ACIA key break");
}

static void ikbd(Test &t) {
    t.reset(); t.commands({0x80,0x01}); t.expect({0xf1},"reset reply");
    t.commands({0x80,0x02}); t.run(10); t.require(!t.dut.ik_response_valid,"invalid reset is NOP");
    t.key(4,true); t.key(129,true); t.run(4); t.expect({0x2a,0x1e},"modifier ordering");
    t.key(4,false); t.key(129,false); t.run(4); t.expect({0xaa,0x9e},"modifier/key breaks");
    t.key(128,true); t.key(132,true); t.run(3); t.expect({0x1d},"merged control make");
    t.key(128,false); t.run(3); t.require(!t.dut.ik_response_valid,"one control still held");
    t.key(132,false); t.run(3); t.expect({0x9d},"merged control break");
    t.mouse(300,-260,1);
    t.expect({0xfa,127,128,0xfa,127,128,0xfa,46,252},"relative mouse splitting and left button");
    t.mouse(0,0,0); t.expect({0xf8,0,0},"mouse button release");
    t.commands({0x0b,4,3}); t.mouse(2,1,0); t.run(10);
    t.require(!t.dut.ik_response_valid,"threshold accumulates small movement");
    t.mouse(2,2,0); t.expect({0xf8,4,3},"threshold retains exact counts");
    t.commands({0x0f}); t.mouse(0,3,0); t.expect({0xf8,0,253},"Y bottom origin");
    t.commands({0x10,0x09,0,100,0,80,0x0c,2,3}); t.mouse(60,30,0);
    t.commands({0x0d}); t.expect({0xf7,0,0,100,0,80},"absolute bounds and scale");
    t.commands({0x0e,0,0,25,0,30,0x0d}); t.expect({0xf7,0,0,25,0,30},"load absolute position");
    t.commands({0x89}); t.expect({0xf6,0x09,0,100,0,80,0,0},"mouse mode inquiry");
    t.commands({0x12}); t.mouse(10,10,0); t.run(10);
    t.require(!t.dut.ik_response_valid,"disabled mouse quiet");
    t.commands({0x1b,0x26,0x10,0x03,0x12,0x34,0x56,0x1c});
    t.expect({0xfc,0x26,0x10,0x03,0x12,0x34,0x56},"clock set/query");
    t.commands({0x80,1,0x1c}); t.expect({0xf1,0xfc,0x26,0x10,0x03,0x12,0x34,0x56},"reset retains clock");
    t.dut.controller_buttons=(1<<0)|(1<<3)|(1<<4); t.run(3);
    t.expect({0xff,0x89},"joystick event");
    t.commands({0x15}); t.dut.controller_buttons=0; t.run(4);
    t.require(!t.dut.ik_response_valid,"joystick interrogation mode quiet");
    t.commands({0x16}); t.expect({0xfd,0,0},"joystick interrogation");
    // Parameter bytes matching commands cannot escape their command parser.
    t.commands({0x17,0x80,0x19,0x80,0x01,0x1c,0x0d,0x16,0x13,0x1a,0x1c});
    t.expect({0xfc,0x26,0x10,0x03,0x12,0x34,0x56},"parameter consumption");
    t.commands({0x13}); t.key(5,true); t.run(4);
    t.require(!t.dut.ik_response_valid,"pause output");
    t.commands({0x11}); t.expect({0x30},"resume queued key");
    // Backpressure does not overwrite the bounded queue or lose final states.
    for (int i=0;i<64;++i) { t.key(6,(i&1)==0); t.run(2); }
    t.dut.ik_command_data=0x1c; t.dut.eval();
    t.require(!t.dut.ik_command_ready,"bounded FIFO applies inquiry backpressure");
    int drained=0, last=0;
    while (t.dut.ik_response_valid) {
        last=t.response(); t.require(last==0x2e||last==0xae,"FIFO key byte integrity");
        t.require(++drained<=64,"bounded keyboard queue");
    }
    t.require(drained>=56 && last==0xae,"backpressure preserves final released key state");
    t.run(3); while (t.dut.ik_response_valid) t.response();
    t.require(t.dut.ik_command_ready,"draining restores command readiness");
}

static void keyboard_priority(Test &t) {
    t.reset();
    // These HID/ST pairs cover the standard keyboard, extended/keypad
    // scancodes, and all shared HID aliases. One snapshot changes more
    // keys than the FIFO holds, so selection must resume in order as it drains.
    const std::vector<std::pair<int,int>> keys={
        {4,0x1e},{5,0x30},{6,0x2e},{7,0x20},{8,0x12},{9,0x21},
        {10,0x22},{11,0x23},{12,0x17},{13,0x24},{14,0x25},{15,0x26},
        {16,0x32},{17,0x31},{18,0x18},{19,0x19},{20,0x10},{21,0x13},
        {22,0x1f},{23,0x14},{24,0x16},{25,0x2f},{26,0x11},{27,0x2d},
        {28,0x15},{29,0x2c},{30,0x02},{31,0x03},{32,0x04},{33,0x05},
        {34,0x06},{35,0x07},{36,0x08},{37,0x09},{38,0x0a},{39,0x0b},
        {40,0x1c},{41,0x01},{42,0x0e},{43,0x0f},{44,0x39},{45,0x0c},
        {46,0x0d},{47,0x1a},{48,0x1b},{49,0x2b},{50,0x60},{51,0x27},
        {52,0x28},{53,0x29},{54,0x33},{55,0x34},{56,0x35},{57,0x3a},
        {58,0x3b},{59,0x3c},{60,0x3d},{61,0x3e},{62,0x3f},{63,0x40},
        {64,0x41},{65,0x42},{66,0x43},{67,0x44},{68,0x61},{69,0x62},
        {73,0x52},{74,0x47},{76,0x53},{79,0x4d},{80,0x4b},{81,0x50},
        {82,0x48},{83,0x63},{84,0x65},{85,0x66},{86,0x4a},{87,0x4e},
        {88,0x72},{89,0x6d},{90,0x6e},{91,0x6f},{92,0x6a},{93,0x6b},
        {94,0x6c},{95,0x67},{96,0x68},{97,0x69},{98,0x70},{99,0x71},
        {100,0x60},{117,0x62},{128,0x1d},{129,0x2a},{130,0x38},
        {132,0x1d},{133,0x36},{134,0x38}
    };
    for (int usage: {0,3,70,127,131,135,143}) t.key(usage,true);
    t.run(6); t.require(!t.dut.ik_response_valid,"unmapped HID usages never produce scancode zero");
    std::array<bool,128> present={};
    for (const auto &[usage,scan]: keys) { t.key(usage,true); present[scan]=true; }
    std::vector<int> order={0x1d,0x2a,0x36,0x38};
    for (int scan=1;scan<128;++scan)
        if (present[scan] && scan!=0x1d && scan!=0x2a && scan!=0x36 && scan!=0x38)
            order.push_back(scan);
    t.require(order.size()>64,"large simultaneous keyboard fixture crosses FIFO capacity");
    t.run(100); t.dut.ik_command_data=0x1c; t.dut.eval();
    t.require(!t.dut.ik_command_ready,"large key snapshot reaches FIFO backpressure");
    auto expect_batch=[&](bool pressed) {
        for (int scan: order) {
            const int expected=scan | (pressed ? 0 : 0x80);
            const int actual=t.response();
            t.require(actual==expected,"simultaneous key priority/state expected="+
                std::to_string(expected)+" actual="+std::to_string(actual));
        }
        t.run(6); t.require(!t.dut.ik_response_valid,"one event per changed ST key");
    };
    expect_batch(true);
    // Removing one HID alias, then swapping the surviving alias in the same
    // snapshot, must leave its ST key pressed without spurious breaks/makes.
    for (int usage: {50,69,128,130}) t.key(usage,false);
    t.run(6); t.require(!t.dut.ik_response_valid,"held HID aliases suppress duplicate breaks");
    for (int usage: {50,69,128,130}) t.key(usage,true);
    for (int usage: {100,117,132,134}) t.key(usage,false);
    t.run(6); t.require(!t.dut.ik_response_valid,"simultaneous HID alias replacement stays pressed");
    for (const auto &[usage,scan]: keys) { (void)scan; t.key(usage,false); }
    t.run(100); expect_batch(false);

    // Makes and breaks in one update use the state of the selected leaf,
    // including changes on opposite sides of the priority tree.
    for (int usage: {41,4,68,88}) t.key(usage,true);
    t.run(8); t.expect({0x01,0x1e,0x61,0x72},"separated tree branches make in scancode order");
    t.key(41,false); t.key(68,false); t.key(30,true); t.key(5,true);
    t.run(8); t.expect({0x81,0x02,0x30,0xe1},"mixed simultaneous makes and breaks retain pressed state");
    for (int usage: {4,88,30,5}) t.key(usage,false);
    t.run(8); t.expect({0x82,0x9e,0xb0,0xf2},"mixed update final releases");
    t.run(6); t.require(!t.dut.ik_response_valid,"priority test leaves no extra events");
}

static void two_joysticks(Test &t) {
    t.reset();
    // Port 0 belongs to the mouse after reset, while FES controller 0
    // retains its original ST joystick 1 mapping.
    t.dut.controller_buttons=0x2400; t.run(6);
    t.require(!t.dut.ik_response_valid,"default mouse port hides ST joystick 0");
    t.dut.controller_buttons|=0x19; t.run(6);
    t.expect({0xff,0x89},"default FES controller 0 remains ST joystick 1");
    t.commands({0x14}); t.expect({0xfe,0x84},"event mode enables FES controller 1 as ST joystick 0");
    t.dut.controller_buttons=0; t.run(6);
    t.expect({0xfe,0,0xff,0},"both joysticks release independently");
    for (int bit=0;bit<6;++bit) {
        const int expected=bit<4 ? 1<<bit : 0x80;
        t.dut.controller_buttons=1<<(bit+8); t.run(3);
        t.expect({0xfe,expected},"joystick 0 direction/fire mapping");
        t.dut.controller_buttons=0; t.run(3); t.expect({0xfe,0},"joystick 0 release");
        t.dut.controller_buttons=1<<bit; t.run(3);
        t.expect({0xff,expected},"joystick 1 direction/fire mapping");
        t.dut.controller_buttons=0; t.run(3); t.expect({0xff,0},"joystick 1 release");
    }
    t.dut.controller_buttons=0xc0c0; t.run(6);
    t.require(!t.dut.ik_response_valid,"unassigned FES controller bits stay quiet");
    t.commands({0x15}); t.dut.controller_buttons=0x1288; t.run(6);
    t.require(!t.dut.ik_response_valid,"interrogation mode suppresses both event streams");
    t.commands({0x16}); t.expect({0xfd,0x82,0x08},"FD returns joystick 0 before joystick 1");
    t.dut.controller_buttons=0x0025; t.run(6);
    t.commands({0x16}); t.expect({0xfd,0,0x85},"FD reports current states, including independent release");
    t.commands({0x14}); t.expect({0xff,0x85},"event reporting resumes current joystick state");
    t.mouse(7,-3,1); t.run(6);
    t.require(!t.dut.ik_response_valid,"joystick 0 owns the shared mouse port");
    t.commands({0x08});
    t.dut.controller_buttons|=0x1100; t.run(6);
    t.require(!t.dut.ik_response_valid,"relative mouse command reclaims port 0");
    t.mouse(2,3,1); t.expect({0xfa,2,3},"mouse reports resume without stale joystick-mode motion");
    t.commands({0x12}); t.dut.controller_buttons=0x2200; t.run(6);
    t.expect({0xff,0},"mouse disable preserves joystick 1 events");
    t.require(!t.dut.ik_response_valid,"mouse disable does not select joystick 0");
    t.commands({0x15,0x16}); t.expect({0xfd,0x82,0},"interrogation selects both joystick ports");
    t.commands({0x1a}); t.dut.controller_buttons=0x1111; t.run(6);
    t.require(!t.dut.ik_response_valid,"disable joysticks suppresses both ports");
    t.commands({0x80,1}); t.expect({0xf1,0xff,0x81},"soft reset restores default joystick 1 mode");
    t.require(!t.dut.ik_response_valid,"soft reset restores mouse ownership of port 0");
    // Both final states survive simultaneous changes while output is paused.
    t.dut.controller_buttons=0; t.run(6); t.expect({0xff,0},"reset joystick release");
    t.commands({0x14,0x13}); t.dut.controller_buttons=0x2821; t.run(6);
    t.require(!t.dut.ik_response_valid,"PAUSE gates both joystick streams");
    t.commands({0x11}); t.expect({0xfe,0x88,0xff,0x81},"PAUSE retains both joystick states");
    t.dut.controller_buttons=0; t.run(6); t.expect({0xfe,0,0xff,0},"two-controller release after PAUSE");
    t.commands({0x13});
    for (int event=0;event<32;++event) {
        t.dut.controller_buttons=(event&1) ? 0 : 0x1111; t.run(4);
    }
    t.commands({0x11});
    std::array<int,2> final={-1,-1};
    int packets=0;
    while (t.dut.ik_response_valid) {
        const int marker=t.response(), state=t.response();
        t.require((marker==0xfe || marker==0xff) && (state==0 || state==0x81),
            "full joystick FIFO preserves complete independent packets");
        final[marker-0xfe]=state;
        t.require(++packets<=32,"bounded two-controller queue");
    }
    t.run(6);
    t.require(!t.dut.ik_response_valid && final[0]==0 && final[1]==0,
        "full FIFO eventually publishes both final released states");
}

static int ym_level(int level) {
    return level==0 ? 0 : std::lround(85*std::pow(10,(level-31)*1.5/20.0));
}
static void calendar(Test &t) {
    struct Date { int year,month,day,next_year,next_month,next_day; };
    for (const Date date: {Date{0x26,0x02,0x28,0x26,0x03,0x01},
                          Date{0x24,0x02,0x28,0x24,0x02,0x29},
                          Date{0x24,0x02,0x29,0x24,0x03,0x01},
                          Date{0x26,0x04,0x30,0x26,0x05,0x01},
                          Date{0x99,0x12,0x31,0x00,0x01,0x01}}) {
        t.reset(); t.commands({0x1b,date.year,date.month,date.day,0x23,0x59,0x59});
        t.run(2'000'000); t.commands({0x1c});
        t.expect({0xfc,date.next_year,date.next_month,date.next_day,0,0,0},"BCD calendar rollover");
        t.commands({0x1b,0xff,0xff,0xff,0xff,0xff,0xff,0x1c});
        t.expect({0xfc,date.next_year,date.next_month,date.next_day,0,0,0},"invalid BCD fields are unchanged");
    }
}
static int envelope(int shape,int step) {
    if (shape<4) return step<32 ? 31-step : 0;
    if (shape<8) return step<32 ? step : 0;
    switch(shape) {
        case 8:return 31-step%32; case 9:return step<32 ? 31-step : 0;
        case 10:return (step/32)%2 ? step%32 : 31-step%32;
        case 11:return step<32 ? 31-step : 31; case 12:return step%32;
        case 13:return step<32 ? step : 31;
        case 14:return (step/32)%2 ? 31-step%32 : step%32;
        default:return step<32 ? step : 0;
    }
}
static void ym(Test &t) {
    t.reset(); t.require(t.dut.port_a==255,"reset input port pulled high");
    t.ym_write(14,0xf9); t.require(t.dut.port_a==255,"input mode inhibits floppy pin drive");
    t.ym_write(7,0x7f); t.require(t.dut.port_a==0xf9,"floppy side/select output latch");
    t.ym_bus(true,0,14); t.require(t.ym_bus(false,0)==0xf9,"port A read");
    t.ym_write(15,0xa5); t.ym_bus(true,0,15);
    t.require(t.ym_bus(false,0)==255,"input port B pullups");
    t.ym_write(7,0xff); t.ym_bus(true,0,15);
    t.require(t.ym_bus(false,0)==0xa5,"second YM I/O latch");
    t.ym_write(8,15); t.ym_write(9,15); t.ym_write(10,15); t.run(50);
    t.require(t.dut.pcm_signed==32640,"48k PCM three-channel headroom");
    t.ym_write(8,0); t.ym_write(9,0); t.ym_write(10,0); t.run(50);
    t.require(t.dut.pcm_signed==0,"PCM silence");
    int samples=0; for (int i=0;i<2000;++i) { t.tick(); samples+=t.dut.sample_valid; }
    t.require(samples==48,"exact 48k sample cadence");
    const std::array<int,16> masks={255,15,255,15,255,15,31,255,31,31,31,255,255,15,255,255};
    for(int reg=0;reg<16;++reg) {
        t.engine_write(reg,255); t.require(t.dut.engine_read==masks[reg],"YM register mask");
    }
    for (int volume=0;volume<16;++volume) {
        t.reset(); t.engine_write(7,63); t.engine_write(8,volume);
        t.require(t.dut.engine_pcm==ym_level(volume==0 ? 0 : volume*2+1),"Yamaha fixed volume scale");
    }
    for (int period: {0,1,2,17,257,4095}) {
        t.reset(); t.engine_write(0,period&255); t.engine_write(1,period>>8);
        t.engine_write(7,62); t.engine_write(8,15);
        const int half=8*(period==0 ? 1 : period);
        for(int n=0;n<4;++n) {
            const int before=t.dut.engine_pcm;
            t.engine_run(half-1); t.require(t.dut.engine_pcm==before,"tone equation before edge");
            t.engine_run(1); t.require(t.dut.engine_pcm!=before,"tone equation f=clock/(16*period)");
        }
    }
    for (int shape=0;shape<16;++shape) {
        t.reset(); t.engine_write(7,63); t.engine_write(8,16);
        t.engine_write(11,3); t.engine_write(12,0); t.engine_write(13,shape);
        for (int step=0;step<100;++step) {
            t.require(t.dut.engine_pcm==ym_level(envelope(shape,step)),"32-step Yamaha envelope shape="+std::to_string(shape)+" step="+std::to_string(step));
            t.engine_run(24); // /8 envelope versus AY /16.
        }
    }
}

int main(int argc,char **argv) {
    Verilated::commandArgs(argc,argv); Test test;
    acia(test); ikbd(test); keyboard_priority(test); two_joysticks(test); calendar(test); ym(test);
    std::cout << "ST input/audio: ACIA timing/errors/IRQ, IKBD key priority/aliases/packets/backpressure and shared YM2149 sound passed\n";
}
