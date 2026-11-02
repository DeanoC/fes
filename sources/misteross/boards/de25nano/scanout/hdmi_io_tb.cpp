#include "Vfes_de25_hdmi_io_tb.h"
#include "verilated.h"
#include "expected_lut.h"
#include <array>
#include <cstdio>
#include <cstdlib>
#include <vector>

static void check(bool value, const char* message) {
    if (!value) { std::fprintf(stderr, "FAIL: %s\n", message); std::exit(1); }
}

struct Slave {
    Vfes_de25_hdmi_io_tb& t;
    bool scl = true, sda = true, active = false;
    unsigned bits = 0, byte = 0, nack_byte;
    unsigned ack_delay = 0;
    unsigned long long cycles = 0, last_rise = 0;
    unsigned long long observations = 0, last_fall_ns = 0, minimum_master_hold_ns = ~0ULL;
    unsigned master_hold_samples = 0;
    std::array<unsigned,3> frame{};
    std::vector<std::array<unsigned,3>> frames;
    explicit Slave(Vfes_de25_hdmi_io_tb& top, unsigned nack) : t(top), nack_byte(nack) {}
    void observe() {
        // Each observation is one half of the 50 MHz reference period.
        const unsigned long long now_ns = 10 * ++observations;
        if (ack_delay && !--ack_delay) {
            check(!t.scl, "delayed ACK missed the low clock phase");
            t.slave_low = 1; t.eval();
        }
        bool now_scl = t.scl, now_sda = t.sda;
        if (active && scl && !now_scl) last_fall_ns = now_ns;
        // Ordinary master data bits only; the explicit slave model's ACK
        // assertion/release and START/STOP transitions are separate boundaries.
        if (active && !now_scl && sda != now_sda && bits < 27 &&
            bits % 9 >= 1 && bits % 9 <= 7 && !t.slave_low) {
            const auto hold = now_ns - last_fall_ns;
            if (hold < minimum_master_hold_ns) minimum_master_hold_ns = hold;
            ++master_hold_samples;
        }
        if (scl && now_scl && sda && !now_sda) {
            if (active) std::fprintf(stderr,"repeated START after bits=%u writes=%zu\n",bits,frames.size());
            check(!active, "unexpected repeated start");
            active = true; bits = byte = 0; last_rise = 0; frame = {};
        }
        if (scl && now_scl && !sda && now_sda && active) {
            if (!(bits >= 27 && bits <= 28))
                std::fprintf(stderr,"STOP after bits=%u writes=%zu frame=%02x %02x %02x\n",bits,frames.size(),frame[0],frame[1],frame[2]);
            check(bits >= 27 && bits <= 28, "incomplete write before stop");
            frames.push_back(frame); active = false;
        }
        if (active && !scl && now_scl) {
            if (bits && bits < 27) check(cycles - last_rise == 5002, "vendor bus period changed");
            last_rise = cycles;
            if (bits < 27) {
                if (bits % 9 < 8) {
                    byte = (byte << 1) | unsigned(now_sda);
                    if (bits % 9 == 7) frame[bits / 9] = byte;
                } else {
                    const bool nack = frames.empty() && bits / 9 == nack_byte;
                    check(now_sda == nack, "slave ACK/NACK bus level");
                    byte = 0;
                }
            }
            ++bits;
        }
        if (active && scl && !now_scl) {
            if (bits < 27 && bits % 9 == 8) {
                // Assert ACK after 3.51 us in the low phase, rather than
                // providing a zero-latency slave response at the clock edge.
                ack_delay = (frames.empty() && bits / 9 == nack_byte) ? 0 : 351;
                t.slave_low = 0;
            } else if (bits % 9 == 0) { ack_delay = 0; t.slave_low = 0; }
            t.eval();
        }
        scl = t.scl; sda = t.sda;
    }
    void tick() {
        t.clk = 0; t.eval(); observe();
        t.clk = 1; t.eval(); observe();
        ++cycles;
    }
    void until_ready(unsigned count) {
        for (unsigned cycle = 0; cycle < 8000000 && !t.ready; ++cycle) tick();
        check(t.ready, "configuration did not complete within fixed bound");
        check(frames.size() == count, "configuration transaction count");
    }
};

static void scenario(unsigned nack) {
    Vfes_de25_hdmi_io_tb t;
    t.clk = 0; t.reset = 1; t.irq_n = 1; t.slave_low = 0; t.eval();
    // Give the two-state simulator a real asynchronous assertion edge;
    // power-up X-to-0 reset behavior is not modeled by Verilator.
    auto reset_tick = [&]() { t.clk = 0; t.eval(); t.clk = 1; t.eval(); };
    t.reset = 0;
    for (unsigned i = 0; i < 4; ++i) reset_tick();
    t.reset = 1;
    for (unsigned i = 0; i < 8; ++i) reset_tick();
    if (!(t.scl && t.sda && !t.ready && t.irq_filtered))
        std::fprintf(stderr,"reset SCL=%u SDA=%u ready=%u irq=%u\n",t.scl,t.sda,t.ready,t.irq_filtered);
    check(t.scl && t.sda && !t.ready && t.irq_filtered, "safe reset boundary");
    Slave bus(t, nack);
    t.reset = 0;
    const unsigned retried = nack < 3;
    bus.until_ready(33 + retried);
    for (unsigned i = 0; i < bus.frames.size(); ++i) {
        const unsigned index = i >= retried ? i - retried : 0;
        const auto& frame = bus.frames[i];
        if (!(frame[0] == 0x72 && frame[1] == (expected_lut[index] >> 8) &&
              frame[2] == (expected_lut[index] & 0xff))) {
            std::fprintf(stderr,"frame %u: %02x %02x %02x, expected LUT[%u]=%04x\n",i,
                frame[0],frame[1],frame[2],index,expected_lut[index]);
            check(false, "register sequence changed");
        }
    }
    if (!retried) {
        t.irq_n = 0; t.eval();
        check(t.irq_filtered, "raw interrupt bypassed synchronizer");
        bus.tick(); check(t.irq_filtered, "first IRQ stage reached configuration input");
        bus.tick(); check(!t.irq_filtered, "second IRQ stage did not propagate");
        for (unsigned i = 0; i < 15000 && bus.frames.size() == 33; ++i) bus.tick();
        t.irq_n = 1;
        // Wait for a complete reconfiguration, including READY dropping.
        for (unsigned i = 0; i < 15000 && t.ready; ++i) bus.tick();
        check(!t.ready, "interrupt did not invalidate configuration");
        bus.until_ready(66);
        for (unsigned i = 33; i < 66; ++i)
            check(bus.frames[i][0] == 0x72 && bus.frames[i][1] == (expected_lut[i-33] >> 8) &&
                  bus.frames[i][2] == (expected_lut[i-33] & 0xff), "interrupt reconfiguration register sequence");
    }
    check(bus.master_hold_samples != 0, "master SDA timing monitor was not exercised");
    if (bus.minimum_master_hold_ns < 100)
        std::fprintf(stderr, "master SDA minimum hold=%llu ns, observed transitions=%u\n",
                     bus.minimum_master_hold_ns, bus.master_hold_samples);
    check(bus.minimum_master_hold_ns >= 100, "ADV7513 master SDA data hold below 100 ns");
    std::printf("{\"case\":\"hdmi-inputs-nack-%u\",\"status\":\"pass\",\"writes\":%zu,\"clock_cycles\":%llu,\"minimum_master_data_hold_ns\":%llu,\"data_hold_samples\":%u}\n",nack,bus.frames.size(),bus.cycles, bus.minimum_master_hold_ns, bus.master_hold_samples);
}

int main(int argc,char** argv) {
    Verilated::commandArgs(argc,argv);
    for (unsigned nack : {3u,0u,1u,2u}) scenario(nack);
    return 0;
}
